//! Steam↔BM resolve + persist, including the HYBRID inline-timeout + detached
//! singleflight backfill. Mirrors the Go handler-layer orchestration
//! (`resolveSteamAccountBySteamID` and friends). See the players spec §5.
//!
//! Budget model: the bmsearch client is built with `default_wait = 90s` (the
//! backfill budget), and the *inline* attempt bounds it tighter with an outer
//! `tokio::time::timeout(BM_SEARCH_INLINE_TIMEOUT_MS)`. When that outer timeout
//! (or a resolver `Pending`) fires, we spawn the 90s backfill and return the
//! degraded "not found" now — exactly like Go remapping a shared-budget deadline
//! to `ErrPending`.

use std::sync::Arc;
use std::time::Duration;

use rustlens_battlemetrics::{BmError, PlayerProfile};
use rustlens_db::players_steam::{
    self as store, BmProfileCache, NameHistory, SteamAccount, SteamAccountCache, TrackedServer,
};
use rustlens_steam::steammatch::MatchError;
use rustlens_steam::Resolver;
use std::collections::HashMap;

use crate::state::AppState;

/// The Rust app id for Rust (Steam `GetOwnedGames`).
const RUST_APP_ID: i32 = 252490;
/// Detached backfill budget (Go `90 * time.Second`).
const BACKFILL_BUDGET: Duration = Duration::from_secs(90);

/// Resolve outcome. `NotFound`/`Pending` map to Go's `ErrSteamAccountNotFound` /
/// `ErrPending`; `Other` is a hard error (→ 500).
pub enum ResolveError {
    NotFound,
    Pending,
    Other(String),
}

impl std::fmt::Display for ResolveError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            ResolveError::NotFound => write!(f, "not found"),
            ResolveError::Pending => write!(f, "pending"),
            ResolveError::Other(e) => write!(f, "{e}"),
        }
    }
}

fn to_whole_hours(sec: i64) -> i64 {
    if sec <= 0 {
        0
    } else {
        sec / 3600
    }
}

fn db_err(e: sqlx::Error) -> ResolveError {
    ResolveError::Other(format!("db: {e}"))
}

/// Inline resolve bounded by `BM_SEARCH_INLINE_TIMEOUT_MS`. On a resolver
/// `Pending` or the inline budget elapsing, spawn the detached backfill and
/// return `NotFound` (degraded response now; the bot re-polls later).
pub async fn resolve_by_steam_id(
    st: &Arc<AppState>,
    steam_id: &str,
) -> Result<SteamAccount, ResolveError> {
    let inline = Duration::from_millis(st.config.bm_search_inline_timeout_ms);
    match tokio::time::timeout(inline, resolve_within(st, steam_id)).await {
        Ok(Ok(acct)) => Ok(acct),
        Ok(Err(ResolveError::Pending)) => {
            spawn_backfill(st, steam_id);
            Err(ResolveError::NotFound)
        }
        Ok(Err(e)) => Err(e),
        Err(_elapsed) => {
            spawn_backfill(st, steam_id);
            Err(ResolveError::NotFound)
        }
    }
}

/// Resolve a BM ID to a Steam account (BM-profile-first, resolver fallback). No
/// `Pending`/backfill on this side (bmsearch `bm_to_steam` is cache-only).
pub async fn resolve_by_bm_id(st: &AppState, bm_id: &str) -> Result<SteamAccount, ResolveError> {
    match st.bm.fetch_player_profile(bm_id).await {
        Ok(mut profile) => {
            if profile.steam_id.is_empty() {
                match st.resolver.bm_to_steam(bm_id).await {
                    Ok(s) if !s.trim().is_empty() => profile.steam_id = s.trim().to_string(),
                    Ok(_) | Err(MatchError::NotFound) | Err(MatchError::NotConfigured) => {
                        return Err(ResolveError::NotFound)
                    }
                    Err(MatchError::Pending) => return Err(ResolveError::NotFound),
                    Err(MatchError::Other(e)) => return Err(ResolveError::Other(e)),
                }
            }
            let steam_id = profile.steam_id.clone();
            upsert_resolved(st, &steam_id, bm_id, profile).await
        }
        Err(BmError::PlayerNotFound) => {
            // Fall back to the resolver to record a bare linkage.
            match st.resolver.bm_to_steam(bm_id).await {
                Ok(steam_id) if !steam_id.trim().is_empty() => {
                    let steam_id = steam_id.trim().to_string();
                    store::upsert_steam_account(&st.pool, &steam_id, Some(bm_id))
                        .await
                        .map_err(db_err)?;
                    store::get_steam_account_by_steam_id(&st.pool, &steam_id)
                        .await
                        .map_err(db_err)?
                        .ok_or(ResolveError::NotFound)
                }
                _ => Err(ResolveError::NotFound),
            }
        }
        Err(e) => Err(ResolveError::Other(e.to_string())),
    }
}

/// The core resolve+persist, bounded only by the caller's timeout. Propagates
/// `Pending` so the inline caller can backfill.
async fn resolve_within(st: &AppState, steam_id: &str) -> Result<SteamAccount, ResolveError> {
    let bm_id = match st.resolver.steam_to_bm(steam_id).await {
        Ok(v) => {
            let v = v.trim().to_string();
            if v.is_empty() {
                return Err(ResolveError::NotFound);
            }
            v
        }
        Err(MatchError::Pending) => return Err(ResolveError::Pending),
        Err(MatchError::NotFound) | Err(MatchError::NotConfigured) => {
            return Err(ResolveError::NotFound)
        }
        Err(MatchError::Other(e)) => return Err(ResolveError::Other(e)),
    };

    match st.bm.fetch_player_profile(&bm_id).await {
        Ok(profile) => upsert_resolved(st, steam_id, &bm_id, profile).await,
        Err(BmError::PlayerNotFound) => {
            // The resolver linked steam↔bm but BM doesn't see the player yet.
            // Record the bare linkage + warm the Steam-only cache.
            let account = store::upsert_steam_account(&st.pool, steam_id, Some(&bm_id))
                .await
                .map_err(db_err)?;
            match refresh_steam_only_cache(st, &account).await {
                Ok(cache) => {
                    if let Err(e) = store::upsert_steam_account_cache(&st.pool, &cache).await {
                        tracing::warn!("players/resolve steam-only cache upsert {steam_id}: {e}");
                    }
                }
                Err(e) => tracing::warn!("players/resolve steam-only warm {steam_id}: {e}"),
            }
            store::get_steam_account_by_steam_id(&st.pool, steam_id)
                .await
                .map_err(db_err)?
                .ok_or(ResolveError::NotFound)
        }
        Err(e) => Err(ResolveError::Other(e.to_string())),
    }
}

/// Full persist: `steam_accounts` + both caches + name history, then re-read.
async fn upsert_resolved(
    st: &AppState,
    steam_id: &str,
    bm_id: &str,
    mut profile: PlayerProfile,
) -> Result<SteamAccount, ResolveError> {
    if profile.steam_id.is_empty() {
        profile.steam_id = steam_id.to_string();
    }
    if profile.bm_id.trim().is_empty() {
        profile.bm_id = bm_id.to_string();
    }
    let steam_id = profile.steam_id.trim().to_string();
    let bm_id = profile.bm_id.trim().to_string();
    if steam_id.is_empty() || bm_id.is_empty() {
        return Err(ResolveError::NotFound);
    }

    let account = store::upsert_steam_account(&st.pool, &steam_id, Some(&bm_id))
        .await
        .map_err(db_err)?;
    let (steam_cache, bm_cache) = build_cache_from_profile(st, &account, &bm_id, &profile).await?;
    store::upsert_steam_account_cache(&st.pool, &steam_cache)
        .await
        .map_err(db_err)?;
    store::upsert_bm_profile_cache(&st.pool, &bm_cache)
        .await
        .map_err(db_err)?;
    if let Err(e) = persist_name_history(st, &profile).await {
        tracing::warn!("players/resolve name history {bm_id}: {e}");
    }
    store::get_steam_account_by_steam_id(&st.pool, &steam_id)
        .await
        .map_err(db_err)?
        .ok_or(ResolveError::NotFound)
}

/// Build both caches from a fresh BM profile + Steam calls. Mirrors
/// `buildSteamCacheFromProfile`.
pub async fn build_cache_from_profile(
    st: &AppState,
    account: &SteamAccount,
    bm_id: &str,
    profile: &PlayerProfile,
) -> Result<(SteamAccountCache, BmProfileCache), ResolveError> {
    let mut avatar = account.avatar_image.clone();
    let mut persona = String::new();
    let mut private = false;
    match st.steam.get_player_summaries(&account.steam_id).await {
        Ok(Some(s)) => {
            private = s.community_visibility_state == 1;
            if !s.avatar_full.is_empty() {
                avatar = s.avatar_full;
            }
            persona = s.persona_name;
        }
        Ok(None) => {}
        Err(e) => tracing::warn!("players/resolve steam summary {}: {e}", account.steam_id),
    }

    let bm_name = if profile.bm_name.is_empty() {
        account.bm_name.clone()
    } else {
        profile.bm_name.clone()
    };

    let tracked = store::list_tracked_servers(&st.pool)
        .await
        .map_err(db_err)?;
    let tracked_by_id: HashMap<&str, &TrackedServer> =
        tracked.iter().map(|t| (t.bm_id.as_str(), t)).collect();

    let mut steam_cache = SteamAccountCache {
        steam_id: account.steam_id.clone(),
        persona_name: persona,
        avatar_image: avatar,
        ..Default::default()
    };
    let mut bm_cache = BmProfileCache {
        bm_id: bm_id.to_string(),
        bm_name,
        first_seen_at: profile.created_at,
        ..Default::default()
    };

    if !private {
        match st.steam.get_owned_games(&account.steam_id).await {
            Ok(mut games) => {
                games.sort_by_key(|g| std::cmp::Reverse(g.playtime_forever));
                steam_cache.total_games = Some(games.len() as i32);
                steam_cache.top_games = games
                    .iter()
                    .take(5)
                    .map(|g| store::AnalyticsGame {
                        app_id: g.app_id,
                        name: g.name.clone(),
                        hours: g.playtime_forever / 60,
                    })
                    .collect();
                if let Some(rust) = games.iter().find(|g| g.app_id == RUST_APP_ID) {
                    steam_cache.rust_hours = Some(rust.playtime_forever / 60);
                }
            }
            Err(e) => tracing::warn!("players/resolve owned games {}: {e}", account.steam_id),
        }
    }

    apply_bm_profile_to_cache(&mut bm_cache, profile, &tracked_by_id);
    Ok((steam_cache, bm_cache))
}

/// Fill the BM hour buckets, top servers, and counts from the profile's Rust
/// servers. Mirrors `applyBmProfileToCache`.
fn apply_bm_profile_to_cache(
    bm_cache: &mut BmProfileCache,
    profile: &PlayerProfile,
    tracked_by_id: &HashMap<&str, &TrackedServer>,
) {
    let (mut vitality, mut atlas, mut vital, mut modded, mut official, mut build, mut bm) =
        (0i64, 0i64, 0i64, 0i64, 0i64, 0i64, 0i64);
    let mut rust_servers: Vec<&rustlens_battlemetrics::PlayerServer> = Vec::new();

    for server in &profile.servers {
        if !server.game.is_empty() && server.game != "rust" {
            continue;
        }
        rust_servers.push(server);
        bm += server.time_played;
        match server.rust_type.as_str() {
            "modded" => modded += server.time_played,
            "official" => official += server.time_played,
            _ => {}
        }
        if let Some(t) = tracked_by_id.get(server.server_id.as_str()) {
            if t.is_vitality {
                vitality += server.time_played;
            }
            if t.is_atlas {
                atlas += server.time_played;
            }
            if t.is_vital {
                vital += server.time_played;
            }
            if t.is_build {
                build += server.time_played;
            }
        }
    }

    bm_cache.vitality_hours = to_whole_hours(vitality);
    bm_cache.atlas_hours = to_whole_hours(atlas);
    bm_cache.vital_hours = to_whole_hours(vital);
    bm_cache.modded_hours = to_whole_hours(modded);
    bm_cache.official_hours = to_whole_hours(official);
    bm_cache.build_hours = to_whole_hours(build);
    bm_cache.bm_hours = to_whole_hours(bm);
    bm_cache.bm_servers_played = rust_servers.len() as i32;
    rust_servers.sort_by_key(|s| std::cmp::Reverse(s.time_played));
    bm_cache.top_servers = rust_servers
        .iter()
        .take(5)
        .map(|s| store::AnalyticsServer {
            bm_id: s.server_id.clone(),
            name: s.server_name.clone(),
            hours: to_whole_hours(s.time_played),
        })
        .collect();
}

/// Steam-only cache warm (no BM). Mirrors `refreshSteamOnlyCache`.
pub async fn refresh_steam_only_cache(
    st: &AppState,
    account: &SteamAccount,
) -> Result<SteamAccountCache, ResolveError> {
    let mut cache = SteamAccountCache {
        steam_id: account.steam_id.clone(),
        avatar_image: account.avatar_image.clone(),
        ..Default::default()
    };
    let summary = match st.steam.get_player_summaries(&account.steam_id).await {
        Ok(Some(s)) => s,
        Ok(None) => return Ok(cache),
        Err(e) => return Err(ResolveError::Other(format!("steam summary: {e}"))),
    };
    cache.persona_name = summary.persona_name;
    if !summary.avatar_full.is_empty() {
        cache.avatar_image = summary.avatar_full;
    }
    if summary.community_visibility_state == 1 {
        return Ok(cache);
    }
    match st.steam.get_owned_games(&account.steam_id).await {
        Ok(mut games) => {
            games.sort_by_key(|g| std::cmp::Reverse(g.playtime_forever));
            cache.total_games = Some(games.len() as i32);
            cache.top_games = games
                .iter()
                .take(5)
                .map(|g| store::AnalyticsGame {
                    app_id: g.app_id,
                    name: g.name.clone(),
                    hours: g.playtime_forever / 60,
                })
                .collect();
            if let Some(rust) = games.iter().find(|g| g.app_id == RUST_APP_ID) {
                cache.rust_hours = Some(rust.playtime_forever / 60);
            }
        }
        Err(e) => tracing::warn!("players/resolve steam-only owned games {}: {e}", account.steam_id),
    }
    Ok(cache)
}

/// Merge the profile's `name` identifiers into `bm_name_history`
/// (`upsertBmNameHistoryFromProfile`): whitespace-collapse names, skip empty /
/// missing lastSeen.
pub async fn persist_name_history(st: &AppState, profile: &PlayerProfile) -> Result<(), sqlx::Error> {
    let bm_id = profile.bm_id.trim();
    if bm_id.is_empty() || profile.name_identifiers.is_empty() {
        return Ok(());
    }
    let names: Vec<NameHistory> = profile
        .name_identifiers
        .iter()
        .filter_map(|n| {
            let name = n.name.split_whitespace().collect::<Vec<_>>().join(" ");
            match n.last_seen {
                Some(last_seen) if !name.is_empty() => Some(NameHistory { name, last_seen_at: last_seen }),
                _ => None,
            }
        })
        .collect();
    store::upsert_bm_name_history(&st.pool, bm_id, &names).await
}

fn spawn_backfill(st: &Arc<AppState>, steam_id: &str) {
    if !st.backfill.try_begin(steam_id) {
        return; // a backfill for this Steam ID is already in flight
    }
    let st = st.clone();
    let sid = steam_id.to_string();
    tokio::spawn(async move {
        if let Ok(Err(ResolveError::Other(e))) =
            tokio::time::timeout(BACKFILL_BUDGET, resolve_within(&st, &sid)).await
        {
            tracing::warn!("players/resolve backfill error for {sid}: {e}");
        }
        st.backfill.end(&sid);
    });
}

#[cfg(test)]
mod tests {
    use super::*;
    use rustlens_battlemetrics::{PlayerProfile, PlayerServer};

    fn server(id: &str, secs: i64, game: &str, rust_type: &str) -> PlayerServer {
        PlayerServer {
            server_id: id.into(),
            server_name: format!("srv-{id}"),
            time_played: secs,
            first_seen_at: None,
            last_seen_at: None,
            game: game.into(),
            rust_type: rust_type.into(),
        }
    }

    #[test]
    fn whole_hours_floors_and_clamps() {
        assert_eq!(to_whole_hours(7200), 2);
        assert_eq!(to_whole_hours(7199), 1);
        assert_eq!(to_whole_hours(0), 0);
        assert_eq!(to_whole_hours(-5), 0);
    }

    #[test]
    fn bm_buckets_from_tracked_flags_and_rust_type() {
        let profile = PlayerProfile {
            bm_id: "b1".into(),
            steam_id: String::new(),
            bm_name: "n".into(),
            created_at: None,
            servers: vec![
                server("100", 7200, "rust", "modded"),   // 2h modded, tracked vitality
                server("200", 3600, "rust", "official"), // 1h official, untracked
                server("300", 1800, "cs2", ""),          // non-rust -> skipped
            ],
            name_identifiers: vec![],
        };
        let vitality = TrackedServer {
            bm_id: "100".into(), name: "V".into(),
            is_vitality: true, is_atlas: false, is_vital: false,
            is_modded: false, is_official: false, is_build: false, high_frequency: false,
        };
        let map: HashMap<&str, &TrackedServer> = std::iter::once(("100", &vitality)).collect();
        let mut cache = BmProfileCache::default();
        apply_bm_profile_to_cache(&mut cache, &profile, &map);

        assert_eq!(cache.bm_servers_played, 2, "cs2 excluded");
        assert_eq!(cache.bm_hours, 3, "7200+3600 = 3h");
        assert_eq!(cache.modded_hours, 2);
        assert_eq!(cache.official_hours, 1);
        assert_eq!(cache.vitality_hours, 2, "server 100 tracked vitality");
        assert_eq!(cache.atlas_hours, 0);
        assert_eq!(cache.top_servers.len(), 2);
        assert_eq!(cache.top_servers[0].bm_id, "100", "highest playtime first");
    }
}
