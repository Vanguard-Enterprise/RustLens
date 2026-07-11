//! Bot-internal Steam routes: resolve/backfill (`/steam/{id}`, `/steam/by-bm/{id}`)
//! and the profile/analytics pipeline (`/steam/{id}/profile`, `/steam/{id}/analytics`).
//! Mirrors the Go handlers; JSON shapes match byte-for-byte (players spec §6.1-6.2).

use std::sync::Arc;

use axum::Json;
use axum::body::Bytes;
use axum::extract::{Path, State};
use axum::http::{HeaderMap, StatusCode};
use axum::response::{IntoResponse, Response};
use chrono::{DateTime, Duration, Utc};
use serde::{Deserialize, Serialize};
use serde_json::json;

use rustlens_battlemetrics::BmError;
use rustlens_db::players_steam::{
    self as store, AnalyticsGame, AnalyticsServer, BmProfileCache, NameHistory, PlayerState,
    SteamAccount, SteamAccountCache,
};
use rustlens_steam::steamapi::SteamError;

use crate::auth::{err_json, is_bm_id, is_steam64, require_bot_jwt};
use crate::resolve::{self, ResolveError};
use crate::state::AppState;

type Shared = State<Arc<AppState>>;

const CACHE_TTL_HOURS: i64 = 24;
const GAP_THRESHOLD_DAYS: i64 = 30;

macro_rules! jwt_guard {
    ($h:expr, $st:expr) => {
        if let Some(resp) = require_bot_jwt(&$h, &$st.config) {
            return resp;
        }
    };
}

fn internal(context: &str, e: impl std::fmt::Display, msg: &str) -> Response {
    tracing::error!("players/steam {context}: {e}");
    err_json(StatusCode::INTERNAL_SERVER_ERROR, msg)
}

fn fresh(updated: DateTime<Utc>) -> bool {
    Utc::now() - updated < Duration::hours(CACHE_TTL_HOURS)
}

// ---- resolve routes ----

pub async fn get_steam_account(
    State(st): Shared,
    headers: HeaderMap,
    Path(steam_id): Path<String>,
) -> Response {
    jwt_guard!(headers, st);
    if !is_steam64(&steam_id) {
        return err_json(StatusCode::BAD_REQUEST, "steamID must be a 17-digit number");
    }
    match store::get_steam_account_by_steam_id(&st.pool, &steam_id).await {
        Ok(Some(account)) => {
            // Second backfill trigger: row exists but no BM linkage yet.
            if account.bm_id.as_deref().map(str::trim).unwrap_or("").is_empty() {
                return resolve_response(resolve::resolve_by_steam_id(&st, &steam_id).await);
            }
            Json(account).into_response()
        }
        Ok(None) => resolve_response(resolve::resolve_by_steam_id(&st, &steam_id).await),
        Err(e) => internal("get_steam_account", e, "internal error"),
    }
}

pub async fn get_steam_account_by_bm(
    State(st): Shared,
    headers: HeaderMap,
    Path(bm_id): Path<String>,
) -> Response {
    jwt_guard!(headers, st);
    if bm_id.trim().is_empty() {
        return err_json(StatusCode::BAD_REQUEST, "bmID is required");
    }
    match store::get_steam_account_by_bm_id(&st.pool, &bm_id).await {
        Ok(Some(account)) => Json(account).into_response(),
        Ok(None) => resolve_response(resolve::resolve_by_bm_id(&st, &bm_id).await),
        Err(e) => internal("get_steam_account_by_bm", e, "internal error"),
    }
}

fn resolve_response(result: Result<SteamAccount, ResolveError>) -> Response {
    match result {
        Ok(account) => Json(account).into_response(),
        Err(ResolveError::NotFound) | Err(ResolveError::Pending) => {
            err_json(StatusCode::NOT_FOUND, "steam account not found")
        }
        Err(ResolveError::Other(e)) => {
            tracing::error!("players/steam resolve: {e}");
            err_json(StatusCode::INTERNAL_SERVER_ERROR, "internal error")
        }
    }
}

// ---- profile ----

#[derive(Serialize)]
struct ProfileHours {
    vitality: i64,
    atlas: i64,
    vital: i64,
    modded: i64,
    official: i64,
    build: i64,
    bm: i64,
    rust: Option<i64>,
}

#[derive(Serialize)]
struct ProfileStatus {
    #[serde(rename = "isOnline")]
    is_online: bool,
    #[serde(rename = "serverBmId")]
    server_bm_id: String,
    #[serde(rename = "serverName")]
    server_name: String,
    #[serde(rename = "onlineUnix")]
    online_unix: Option<i64>,
    #[serde(rename = "offlineUnix")]
    offline_unix: Option<i64>,
}

#[derive(Serialize)]
struct Gap {
    #[serde(rename = "fromUnix")]
    from_unix: i64,
    #[serde(rename = "toUnix")]
    to_unix: i64,
}

#[derive(Serialize)]
struct ProfileResponse {
    #[serde(rename = "steamId")]
    steam_id: String,
    #[serde(rename = "bmId")]
    bm_id: String,
    #[serde(rename = "bmName")]
    bm_name: String,
    #[serde(rename = "personaName")]
    persona_name: String,
    #[serde(rename = "avatarImage")]
    avatar_image: String,
    #[serde(rename = "firstSeenUnix")]
    first_seen_unix: Option<i64>,
    hours: ProfileHours,
    #[serde(rename = "serversPlayed")]
    servers_played: i32,
    #[serde(rename = "nameGaps")]
    name_gaps: Vec<Gap>,
    #[serde(skip_serializing_if = "Option::is_none")]
    status: Option<ProfileStatus>,
}

#[derive(Serialize)]
struct AnalyticsResponse {
    #[serde(rename = "steamId")]
    steam_id: String,
    #[serde(rename = "bmId")]
    bm_id: String,
    #[serde(rename = "bmName")]
    bm_name: String,
    #[serde(rename = "avatarImage")]
    avatar_image: String,
    #[serde(rename = "totalGames")]
    total_games: Option<i32>,
    #[serde(rename = "topGames")]
    top_games: Vec<AnalyticsGame>,
    #[serde(rename = "totalServers")]
    total_servers: i32,
    #[serde(rename = "topServers")]
    top_servers: Vec<AnalyticsServer>,
    gaps: Vec<Gap>,
}

fn is_later(a: Option<DateTime<Utc>>, b: Option<DateTime<Utc>>) -> bool {
    match (a, b) {
        (Some(a), Some(b)) => a > b,
        (Some(_), None) => true,
        _ => false,
    }
}

/// Pick the online state with the latest `online_since`, else the offline state
/// with the latest `offline_since`. Mirrors `steamProfileStatusFromStates`.
fn pick_status(states: &[PlayerState]) -> Option<ProfileStatus> {
    let mut best_online: Option<&PlayerState> = None;
    let mut best_offline: Option<&PlayerState> = None;
    for s in states {
        if s.is_online {
            if best_online.is_none_or(|b| is_later(s.online_since, b.online_since)) {
                best_online = Some(s);
            }
        } else if best_offline.is_none_or(|b| is_later(s.offline_since, b.offline_since)) {
            best_offline = Some(s);
        }
    }
    let chosen = best_online.or(best_offline)?;
    Some(ProfileStatus {
        is_online: chosen.is_online,
        server_bm_id: chosen.server_bm_id.clone(),
        server_name: chosen.server_name.clone(),
        online_unix: chosen.online_since.map(|t| t.timestamp()),
        offline_unix: chosen.offline_since.map(|t| t.timestamp()),
    })
}

/// Gaps between consecutive `last_seen_at` values ≥ 30 days apart. Mirrors
/// `computeNameGaps` (history arrives sorted `last_seen_at ASC`).
fn compute_gaps(history: &[NameHistory]) -> Vec<Gap> {
    let threshold = Duration::days(GAP_THRESHOLD_DAYS);
    history
        .windows(2)
        .filter(|w| w[1].last_seen_at - w[0].last_seen_at >= threshold)
        .map(|w| Gap {
            from_unix: w[0].last_seen_at.timestamp(),
            to_unix: w[1].last_seen_at.timestamp(),
        })
        .collect()
}

async fn name_gaps(st: &AppState, bm_id: &str) -> Vec<Gap> {
    match store::list_bm_name_history(&st.pool, bm_id).await {
        Ok(h) => compute_gaps(&h),
        Err(e) => {
            tracing::warn!("players/steam name gaps {bm_id}: {e}");
            Vec::new()
        }
    }
}

async fn profile_response(
    st: &AppState,
    account: &SteamAccount,
    sc: &SteamAccountCache,
    bc: &BmProfileCache,
) -> ProfileResponse {
    let bm_id = account.bm_id.clone().unwrap_or_default();
    let gaps = name_gaps(st, &bm_id).await;
    let status = match store::list_player_states_by_player(&st.pool, &bm_id).await {
        Ok(states) => pick_status(&states),
        Err(e) => {
            tracing::warn!("players/steam status {bm_id}: {e}");
            None
        }
    };
    let first_seen_unix = bc
        .first_seen_at
        .or(account.first_seen_at)
        .map(|t| t.timestamp());
    ProfileResponse {
        steam_id: account.steam_id.clone(),
        bm_id,
        bm_name: if bc.bm_name.is_empty() { account.bm_name.clone() } else { bc.bm_name.clone() },
        persona_name: sc.persona_name.clone(),
        avatar_image: sc.avatar_image.clone(),
        first_seen_unix,
        hours: ProfileHours {
            vitality: bc.vitality_hours,
            atlas: bc.atlas_hours,
            vital: bc.vital_hours,
            modded: bc.modded_hours,
            official: bc.official_hours,
            build: bc.build_hours,
            bm: bc.bm_hours,
            rust: sc.rust_hours,
        },
        servers_played: bc.bm_servers_played,
        name_gaps: gaps,
        status,
    }
}

async fn analytics_response(
    st: &AppState,
    account: &SteamAccount,
    sc: &SteamAccountCache,
    bc: &BmProfileCache,
) -> AnalyticsResponse {
    let bm_id = account.bm_id.clone().unwrap_or_default();
    let gaps = name_gaps(st, &bm_id).await;
    AnalyticsResponse {
        steam_id: account.steam_id.clone(),
        bm_id,
        bm_name: if bc.bm_name.is_empty() { account.bm_name.clone() } else { bc.bm_name.clone() },
        avatar_image: if sc.avatar_image.is_empty() { account.avatar_image.clone() } else { sc.avatar_image.clone() },
        total_games: sc.total_games,
        top_games: sc.top_games.clone(),
        total_servers: bc.bm_servers_played,
        top_servers: bc.top_servers.clone(),
        gaps,
    }
}

/// Load an existing account requiring a non-empty bm_id, or the appropriate 404.
async fn require_linked_account(st: &AppState, steam_id: &str) -> Result<(SteamAccount, String), Response> {
    match store::get_steam_account_by_steam_id(&st.pool, steam_id).await {
        Ok(Some(account)) => {
            let bm_id = account.bm_id.as_deref().map(str::trim).unwrap_or("").to_string();
            if bm_id.is_empty() {
                Err(err_json(StatusCode::NOT_FOUND, "steam account has no linked battlemetrics id"))
            } else {
                Ok((account, bm_id))
            }
        }
        Ok(None) => Err(err_json(StatusCode::NOT_FOUND, "steam account not found")),
        Err(e) => Err(internal("require_linked_account", e, "internal error")),
    }
}

pub async fn get_steam_profile(
    State(st): Shared,
    headers: HeaderMap,
    Path(steam_id): Path<String>,
) -> Response {
    jwt_guard!(headers, st);
    if !is_steam64(&steam_id) {
        return err_json(StatusCode::BAD_REQUEST, "steamID must be a 17-digit number");
    }
    profile_for(&st, &steam_id).await
}

/// The shared profile pipeline (24h gate → refresh → stale/404/500), reused by
/// the bot-internal route and the app `/api/app/players/{id}/profile` route.
pub async fn profile_for(st: &AppState, steam_id: &str) -> Response {
    let (account, bm_id) = match require_linked_account(st, steam_id).await {
        Ok(v) => v,
        Err(resp) => return resp,
    };
    let cached_steam = match store::get_steam_account_cache(&st.pool, steam_id).await {
        Ok(v) => v,
        Err(e) => return internal("profile steam cache", e, "internal error"),
    };
    let cached_bm = match store::get_bm_profile_cache(&st.pool, &bm_id).await {
        Ok(v) => v,
        Err(e) => return internal("profile bm cache", e, "internal error"),
    };
    let both_fresh = cached_steam.as_ref().is_some_and(|c| fresh(c.updated_at))
        && cached_bm.as_ref().is_some_and(|c| fresh(c.updated_at));
    if both_fresh {
        let resp = profile_response(st, &account, cached_steam.as_ref().unwrap(), cached_bm.as_ref().unwrap()).await;
        return Json(resp).into_response();
    }

    let can_use_cached = cached_steam.is_some() && cached_bm.is_some();
    let stale = || async {
        let resp = profile_response(st, &account, cached_steam.as_ref().unwrap(), cached_bm.as_ref().unwrap()).await;
        Json(resp).into_response()
    };

    let profile = match st.bm.fetch_player_profile(&bm_id).await {
        Ok(p) => p,
        Err(BmError::PlayerNotFound) => {
            return err_json(StatusCode::NOT_FOUND, "battlemetrics profile private");
        }
        Err(e) => {
            tracing::warn!("players/steam profile refresh {bm_id}: {e}");
            return if can_use_cached {
                stale().await
            } else {
                err_json(StatusCode::INTERNAL_SERVER_ERROR, "failed to refresh steam profile")
            };
        }
    };
    let (sc, bc) = match resolve::build_cache_from_profile(st, &account, &bm_id, &profile).await {
        Ok(v) => v,
        Err(_) => {
            return if can_use_cached {
                stale().await
            } else {
                err_json(StatusCode::INTERNAL_SERVER_ERROR, "failed to refresh steam profile")
            };
        }
    };
    if let Err(e) = resolve::persist_name_history(st, &profile).await {
        tracing::warn!("players/steam profile name history {bm_id}: {e}");
    }
    let updated_steam = store::upsert_steam_account_cache(&st.pool, &sc).await.unwrap_or(sc);
    let updated_bm = store::upsert_bm_profile_cache(&st.pool, &bc).await.unwrap_or(bc);
    let resp = profile_response(st, &account, &updated_steam, &updated_bm).await;
    Json(resp).into_response()
}

pub async fn get_steam_analytics(
    State(st): Shared,
    headers: HeaderMap,
    Path(steam_id): Path<String>,
) -> Response {
    jwt_guard!(headers, st);
    if !is_steam64(&steam_id) {
        return err_json(StatusCode::BAD_REQUEST, "steamID must be a 17-digit number");
    }
    let (account, bm_id) = match require_linked_account(&st, &steam_id).await {
        Ok(v) => v,
        Err(resp) => return resp,
    };
    let cached_steam = match store::get_steam_account_cache(&st.pool, &steam_id).await {
        Ok(v) => v,
        Err(e) => return internal("analytics steam cache", e, "internal error"),
    };
    let cached_bm = match store::get_bm_profile_cache(&st.pool, &bm_id).await {
        Ok(v) => v,
        Err(e) => return internal("analytics bm cache", e, "internal error"),
    };
    let can_use_cached = cached_steam.is_some() && cached_bm.is_some();
    let both_fresh = can_use_cached
        && fresh(cached_steam.as_ref().unwrap().updated_at)
        && fresh(cached_bm.as_ref().unwrap().updated_at);
    if both_fresh {
        let resp = analytics_response(&st, &account, cached_steam.as_ref().unwrap(), cached_bm.as_ref().unwrap()).await;
        return Json(resp).into_response();
    }

    let stale = || async {
        let resp = analytics_response(&st, &account, cached_steam.as_ref().unwrap(), cached_bm.as_ref().unwrap()).await;
        Json(resp).into_response()
    };

    let profile = match st.bm.fetch_player_profile(&bm_id).await {
        Ok(p) => p,
        Err(e) => {
            // Analytics does NOT special-case PlayerNotFound (→ 502 when no cache).
            tracing::warn!("players/steam analytics refresh {bm_id}: {e}");
            return if can_use_cached {
                stale().await
            } else {
                err_json(StatusCode::BAD_GATEWAY, "failed to fetch analytics")
            };
        }
    };
    let (sc, bc) = match resolve::build_cache_from_profile(&st, &account, &bm_id, &profile).await {
        Ok(v) => v,
        Err(_) => {
            return if can_use_cached {
                stale().await
            } else {
                err_json(StatusCode::BAD_GATEWAY, "failed to fetch analytics")
            };
        }
    };
    if let Err(e) = resolve::persist_name_history(&st, &profile).await {
        tracing::warn!("players/steam analytics name history {bm_id}: {e}");
    }
    let updated_steam = store::upsert_steam_account_cache(&st.pool, &sc).await.unwrap_or(sc);
    let updated_bm = store::upsert_bm_profile_cache(&st.pool, &bc).await.unwrap_or(bc);
    let resp = analytics_response(&st, &account, &updated_steam, &updated_bm).await;
    Json(resp).into_response()
}

// ---- BM-keyed profile / analytics (no required Steam linkage) ----

/// A placeholder account for a BM ID with no discovered Steam linkage.
/// `created_at`/`updated_at` are never read by `profile_response`/
/// `analytics_response` — only `steam_id`/`bm_id`/`bm_name`/`avatar_image`/
/// `first_seen_at` are.
fn unlinked_account() -> SteamAccount {
    SteamAccount {
        steam_id: String::new(),
        bm_id: None,
        bm_name: String::new(),
        avatar_image: String::new(),
        first_seen_at: None,
        created_at: Utc::now(),
        updated_at: Utc::now(),
    }
}

/// Shared lookup prologue for `GetBmPlayerProfile`/`GetBmPlayerAnalytics`: find
/// an existing Steam linkage for enrichment (ignoring a "not found" lookup,
/// logging any other DB error), then force `bm_id` to the input BM ID when
/// unlinked. Mirrors the Go handlers' shared preamble.
async fn account_for_bm_id(st: &AppState, bm_id: &str) -> (SteamAccount, SteamAccountCache) {
    let mut account = match store::get_steam_account_by_bm_id(&st.pool, bm_id).await {
        Ok(Some(a)) => a,
        Ok(None) => unlinked_account(),
        Err(e) => {
            tracing::error!("players/steam bm lookup {bm_id}: {e}");
            unlinked_account()
        }
    };
    let mut steam_cache = SteamAccountCache::default();
    if !account.steam_id.is_empty() {
        if let Ok(Some(c)) = store::get_steam_account_cache(&st.pool, &account.steam_id).await {
            steam_cache = c;
        }
    }
    if account.steam_id.is_empty() {
        account.bm_id = Some(bm_id.to_string());
    }
    (account, steam_cache)
}

/// `GET /players/{bmID}/bm-profile`: the BM-keyed twin of `get_steam_profile`,
/// gated on the `bm_profile_cache` TTL alone (no Steam-side refresh). Mirrors
/// Go's `GetBmPlayerProfile`.
pub async fn get_bm_player_profile(
    State(st): Shared,
    headers: HeaderMap,
    Path(bm_id): Path<String>,
) -> Response {
    jwt_guard!(headers, st);
    let bm_id = bm_id.trim().to_string();
    if !is_bm_id(&bm_id) {
        return err_json(StatusCode::BAD_REQUEST, "bmID must be a numeric BattleMetrics player ID");
    }

    let (mut account, mut steam_cache) = account_for_bm_id(&st, &bm_id).await;

    let cached_bm = match store::get_bm_profile_cache(&st.pool, &bm_id).await {
        Ok(v) => v,
        Err(e) => return internal("bm profile cache", e, "failed to fetch bm profile cache"),
    };
    if let Some(cached) = &cached_bm {
        if fresh(cached.updated_at) {
            let resp = profile_response(&st, &account, &steam_cache, cached).await;
            return Json(resp).into_response();
        }
    }

    let profile = match st.bm.fetch_player_profile(&bm_id).await {
        Ok(p) => p,
        Err(BmError::PlayerNotFound) => {
            // BM doesn't see this player. Prefer a stale cache if we have one;
            // otherwise try to recover a Steam linkage via the resolver chain so
            // we can still serve Steam-side enrichment instead of 404'ing.
            if let Some(cached) = &cached_bm {
                let resp = profile_response(&st, &account, &steam_cache, cached).await;
                return Json(resp).into_response();
            }
            if account.steam_id.is_empty() {
                match resolve::resolve_bare_linkage_from_resolver(&st, &bm_id).await {
                    Ok(linked) => {
                        account = linked;
                        if let Ok(Some(c)) =
                            store::get_steam_account_cache(&st.pool, &account.steam_id).await
                        {
                            steam_cache = c;
                        }
                    }
                    Err(ResolveError::NotFound) | Err(ResolveError::Pending) => {}
                    Err(ResolveError::Other(e)) => {
                        tracing::warn!("players/steam bm profile resolver lookup {bm_id}: {e}");
                    }
                }
            }
            if !account.steam_id.is_empty() {
                let empty_bm = BmProfileCache { bm_id: bm_id.clone(), ..Default::default() };
                let resp = profile_response(&st, &account, &steam_cache, &empty_bm).await;
                return Json(resp).into_response();
            }
            return err_json(StatusCode::NOT_FOUND, "battlemetrics player not found");
        }
        Err(e) => {
            tracing::warn!("players/steam bm profile fetch {bm_id}: {e}");
            if let Some(cached) = &cached_bm {
                let resp = profile_response(&st, &account, &steam_cache, cached).await;
                return Json(resp).into_response();
            }
            return err_json(StatusCode::BAD_GATEWAY, "failed to fetch battlemetrics profile");
        }
    };
    let mut profile = profile;
    if profile.bm_id.trim().is_empty() {
        profile.bm_id = bm_id.clone();
    }

    let fresh_bm = match resolve::build_bm_only_cache(&st, &bm_id, &profile).await {
        Ok(c) => c,
        Err(e) => return internal("bm profile tracked servers", e, "failed to load tracked servers"),
    };
    let updated_bm = match store::upsert_bm_profile_cache(&st.pool, &fresh_bm).await {
        Ok(c) => c,
        Err(e) => {
            tracing::error!("players/steam bm profile cache upsert {bm_id}: {e}");
            let resp = profile_response(&st, &account, &steam_cache, &fresh_bm).await;
            return Json(resp).into_response();
        }
    };
    if let Err(e) = resolve::persist_name_history(&st, &profile).await {
        tracing::warn!("players/steam bm profile name history {bm_id}: {e}");
    }
    let resp = profile_response(&st, &account, &steam_cache, &updated_bm).await;
    Json(resp).into_response()
}

/// `GET /players/{bmID}/bm-analytics`: the BM-keyed twin of `get_steam_analytics`.
/// Unlike the profile route, a BattleMetrics fetch failure (including "not
/// found") never tries the resolver fallback — it just falls back to a stale
/// cache or 502s. Mirrors Go's `GetBmPlayerAnalytics`.
pub async fn get_bm_player_analytics(
    State(st): Shared,
    headers: HeaderMap,
    Path(bm_id): Path<String>,
) -> Response {
    jwt_guard!(headers, st);
    let bm_id = bm_id.trim().to_string();
    if !is_bm_id(&bm_id) {
        return err_json(StatusCode::BAD_REQUEST, "bmID must be a numeric BattleMetrics player ID");
    }

    let (account, steam_cache) = account_for_bm_id(&st, &bm_id).await;

    let cached_bm = match store::get_bm_profile_cache(&st.pool, &bm_id).await {
        Ok(v) => v,
        Err(e) => return internal("bm analytics cache", e, "failed to fetch bm profile cache"),
    };
    if let Some(cached) = &cached_bm {
        if fresh(cached.updated_at) {
            let resp = analytics_response(&st, &account, &steam_cache, cached).await;
            return Json(resp).into_response();
        }
    }

    let profile = match st.bm.fetch_player_profile(&bm_id).await {
        Ok(p) => p,
        Err(e) => {
            tracing::warn!("players/steam bm analytics fetch {bm_id}: {e}");
            if let Some(cached) = &cached_bm {
                let resp = analytics_response(&st, &account, &steam_cache, cached).await;
                return Json(resp).into_response();
            }
            return err_json(StatusCode::BAD_GATEWAY, "failed to fetch analytics");
        }
    };
    let mut profile = profile;
    if profile.bm_id.trim().is_empty() {
        profile.bm_id = bm_id.clone();
    }

    let fresh_bm = match resolve::build_bm_only_cache(&st, &bm_id, &profile).await {
        Ok(c) => c,
        Err(e) => return internal("bm analytics tracked servers", e, "failed to load tracked servers"),
    };
    let updated_bm = match store::upsert_bm_profile_cache(&st.pool, &fresh_bm).await {
        Ok(c) => c,
        Err(e) => {
            tracing::error!("players/steam bm analytics cache upsert {bm_id}: {e}");
            let resp = analytics_response(&st, &account, &steam_cache, &fresh_bm).await;
            return Json(resp).into_response();
        }
    };
    if let Err(e) = resolve::persist_name_history(&st, &profile).await {
        tracing::warn!("players/steam bm analytics name history {bm_id}: {e}");
    }
    let resp = analytics_response(&st, &account, &steam_cache, &updated_bm).await;
    Json(resp).into_response()
}

// ---- friends ----

/// `GET /steam/{steamID}/friends`. Mirrors Go's `GetSteamFriends`.
pub async fn get_steam_friends(
    State(st): Shared,
    headers: HeaderMap,
    Path(steam_id): Path<String>,
) -> Response {
    jwt_guard!(headers, st);
    if !is_steam64(&steam_id) {
        return err_json(StatusCode::BAD_REQUEST, "steamID must be a 17-digit number");
    }
    match st.steam.get_friend_list(&steam_id).await {
        Ok(friends) => Json(json!({ "items": friends, "private": false })).into_response(),
        Err(SteamError::FriendListPrivate) => {
            Json(json!({ "items": Vec::<String>::new(), "private": true })).into_response()
        }
        Err(SteamError::KeyRequired) => {
            err_json(StatusCode::SERVICE_UNAVAILABLE, "steam api key is not configured")
        }
        Err(e) => {
            tracing::error!("players/steam friends {steam_id}: {e}");
            err_json(StatusCode::INTERNAL_SERVER_ERROR, "failed to fetch friend list")
        }
    }
}

// ---- resolve (raw input -> canonical Steam ID) ----

#[derive(Deserialize, Default)]
struct ResolveSteamIdRequest {
    #[serde(default)]
    input: String,
}

enum ResolveInputError {
    Unsupported,
    KeyRequired,
    VanityNotFound,
    Other(String),
}

const UNSUPPORTED_INPUT_MSG: &str =
    "input must be a 17-digit Steam ID or a supported steamcommunity.com profile URL";

/// Mirrors Go's `steamapi.Client.ResolveSteamID`: accepts a raw 17-digit Steam
/// ID, a `steamcommunity.com/profiles/{id}` URL, or a `steamcommunity.com/id/{vanity}`
/// URL (resolved live through the Steam Web API). Anything else is unsupported.
/// Deliberately narrower than `classifier::classify` (used by the app-facing
/// search route), which also accepts bare vanity names and BM identifiers —
/// this mirrors the Go handler's own narrower `ResolveSteamID`.
async fn resolve_steam_id_input(
    steam: &rustlens_steam::steamapi::Client,
    input: &str,
) -> Result<String, ResolveInputError> {
    let normalized = input.trim();
    if normalized.is_empty() {
        return Err(ResolveInputError::Unsupported);
    }
    if is_steam64(normalized) {
        return Ok(normalized.to_string());
    }
    let Ok(url) = url::Url::parse(normalized) else {
        return Err(ResolveInputError::Unsupported);
    };
    if !matches!(url.scheme(), "http" | "https") {
        return Err(ResolveInputError::Unsupported);
    }
    let host = url.host_str().unwrap_or("").to_ascii_lowercase();
    if host != "steamcommunity.com" && host != "www.steamcommunity.com" {
        return Err(ResolveInputError::Unsupported);
    }
    let path = url.path().trim_matches('/');
    let segments: Vec<&str> = if path.is_empty() { Vec::new() } else { path.split('/').collect() };
    if segments.len() < 2 {
        return Err(ResolveInputError::Unsupported);
    }
    match segments[0].to_ascii_lowercase().as_str() {
        "profiles" => {
            if is_steam64(segments[1]) {
                Ok(segments[1].to_string())
            } else {
                Err(ResolveInputError::Unsupported)
            }
        }
        "id" => {
            let vanity = segments[1].trim();
            if vanity.is_empty() {
                return Err(ResolveInputError::Unsupported);
            }
            if is_steam64(vanity) {
                return Ok(vanity.to_string());
            }
            match steam.resolve_vanity_url(vanity).await {
                Ok(Some(id)) => Ok(id),
                Ok(None) => Err(ResolveInputError::VanityNotFound),
                Err(SteamError::KeyRequired) => Err(ResolveInputError::KeyRequired),
                Err(e) => Err(ResolveInputError::Other(e.to_string())),
            }
        }
        _ => Err(ResolveInputError::Unsupported),
    }
}

/// `POST /steam/resolve`. Mirrors Go's `ResolveSteamID`. Uses the literal Go
/// decode-error message here rather than the shared `parse_json` helper
/// (whose text differs from Go's for this handler).
pub async fn resolve_steam_id(State(st): Shared, headers: HeaderMap, body: Bytes) -> Response {
    jwt_guard!(headers, st);
    let req: ResolveSteamIdRequest = match serde_json::from_slice(&body) {
        Ok(r) => r,
        Err(_) => return err_json(StatusCode::BAD_REQUEST, "invalid request body"),
    };
    match resolve_steam_id_input(&st.steam, &req.input).await {
        Ok(steam_id) => Json(json!({ "steamId": steam_id })).into_response(),
        Err(ResolveInputError::KeyRequired) => {
            err_json(StatusCode::BAD_REQUEST, "vanity URL resolution is not available")
        }
        Err(ResolveInputError::Unsupported) => err_json(StatusCode::BAD_REQUEST, UNSUPPORTED_INPUT_MSG),
        Err(ResolveInputError::VanityNotFound) => {
            err_json(StatusCode::BAD_REQUEST, "steam vanity URL could not be resolved")
        }
        Err(ResolveInputError::Other(e)) => {
            tracing::error!("players/steam resolve: {e}");
            err_json(StatusCode::INTERNAL_SERVER_ERROR, "failed to resolve steam id")
        }
    }
}

// ---- steam-accounts/count ----

/// `GET /steam-accounts/count`. Mirrors Go's `GetSteamAccountCount`.
pub async fn get_steam_account_count(State(st): Shared, headers: HeaderMap) -> Response {
    jwt_guard!(headers, st);
    match store::count_steam_accounts(&st.pool).await {
        Ok(count) => Json(json!({ "count": count })).into_response(),
        Err(e) => internal("account count", e, "failed to count steam accounts"),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn ts(s: &str) -> DateTime<Utc> {
        s.parse().unwrap()
    }
    fn nh(name: &str, at: &str) -> NameHistory {
        NameHistory { name: name.into(), last_seen_at: ts(at) }
    }
    fn state(online: bool, online_since: Option<&str>, offline_since: Option<&str>, srv: &str) -> PlayerState {
        PlayerState {
            server_bm_id: srv.into(),
            server_name: format!("srv-{srv}"),
            player_bm_id: "p".into(),
            is_online: online,
            online_since: online_since.map(ts),
            offline_since: offline_since.map(ts),
            updated_at: ts("2024-01-01T00:00:00Z"),
        }
    }

    #[test]
    fn gaps_only_over_30_days() {
        let hist = vec![
            nh("a", "2024-01-01T00:00:00Z"),
            nh("b", "2024-01-10T00:00:00Z"), // 9d gap -> skip
            nh("c", "2024-03-01T00:00:00Z"), // ~50d gap -> keep
        ];
        let gaps = compute_gaps(&hist);
        assert_eq!(gaps.len(), 1);
        assert_eq!(gaps[0].from_unix, ts("2024-01-10T00:00:00Z").timestamp());
        assert_eq!(gaps[0].to_unix, ts("2024-03-01T00:00:00Z").timestamp());
    }

    #[test]
    fn status_prefers_latest_online() {
        let states = vec![
            state(false, None, Some("2024-01-05T00:00:00Z"), "s0"),
            state(true, Some("2024-01-01T00:00:00Z"), None, "s1"),
            state(true, Some("2024-01-03T00:00:00Z"), None, "s2"), // latest online
        ];
        let st = pick_status(&states).unwrap();
        assert!(st.is_online);
        assert_eq!(st.server_bm_id, "s2");
    }

    #[test]
    fn status_falls_back_to_latest_offline() {
        let states = vec![
            state(false, None, Some("2024-01-01T00:00:00Z"), "s0"),
            state(false, None, Some("2024-01-09T00:00:00Z"), "s1"), // latest offline
        ];
        let st = pick_status(&states).unwrap();
        assert!(!st.is_online);
        assert_eq!(st.server_bm_id, "s1");
        assert_eq!(st.offline_unix, Some(ts("2024-01-09T00:00:00Z").timestamp()));
    }

    #[test]
    fn status_none_when_empty() {
        assert!(pick_status(&[]).is_none());
    }

    fn no_key_client() -> rustlens_steam::steamapi::Client {
        rustlens_steam::steamapi::Client::new("").unwrap()
    }

    #[tokio::test]
    async fn resolve_input_accepts_bare_steam_id() {
        let c = no_key_client();
        let id = resolve_steam_id_input(&c, "76561198000000000").await;
        assert!(matches!(id, Ok(s) if s == "76561198000000000"));
    }

    #[tokio::test]
    async fn resolve_input_accepts_profiles_url() {
        let c = no_key_client();
        let id = resolve_steam_id_input(&c, "https://steamcommunity.com/profiles/76561198000000000/")
            .await;
        assert!(matches!(id, Ok(s) if s == "76561198000000000"));
    }

    #[tokio::test]
    async fn resolve_input_accepts_numeric_id_segment_without_api_call() {
        // A digit "vanity" under /id/ is returned directly (matches Go), so this
        // must not require a Steam API key.
        let c = no_key_client();
        let id = resolve_steam_id_input(&c, "https://www.steamcommunity.com/id/76561198000000000").await;
        assert!(matches!(id, Ok(s) if s == "76561198000000000"));
    }

    #[tokio::test]
    async fn resolve_input_rejects_bare_vanity_without_url() {
        let c = no_key_client();
        assert!(matches!(
            resolve_steam_id_input(&c, "gaben").await,
            Err(ResolveInputError::Unsupported)
        ));
    }

    #[tokio::test]
    async fn resolve_input_rejects_other_hosts_and_schemes() {
        let c = no_key_client();
        assert!(matches!(
            resolve_steam_id_input(&c, "https://evil.com/profiles/76561198000000000").await,
            Err(ResolveInputError::Unsupported)
        ));
        assert!(matches!(
            resolve_steam_id_input(&c, "ftp://steamcommunity.com/profiles/76561198000000000").await,
            Err(ResolveInputError::Unsupported)
        ));
        assert!(matches!(
            resolve_steam_id_input(&c, "https://steamcommunity.com/profiles/").await,
            Err(ResolveInputError::Unsupported)
        ));
    }

    #[tokio::test]
    async fn resolve_input_vanity_requires_api_key() {
        let c = no_key_client();
        assert!(matches!(
            resolve_steam_id_input(&c, "https://steamcommunity.com/id/gaben").await,
            Err(ResolveInputError::KeyRequired)
        ));
    }

    #[test]
    fn unlinked_account_has_empty_steam_side() {
        let a = unlinked_account();
        assert!(a.steam_id.is_empty());
        assert!(a.bm_id.is_none());
    }
}
