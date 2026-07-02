//! Guild game-ban scan (`/guilds/{guildID}/game-bans`). light = guild-linked
//! Steam IDs; deep = expand to visible global hard links for guild members
//! (refreshing stale platformsync identities first). 6h ban-cache TTL, VAC bans
//! ignored, timeframe-filtered. Mirrors the Go `CheckGuildGameBans`.

use std::collections::{HashMap, HashSet};
use std::sync::Arc;

use axum::Json;
use axum::extract::{Path, RawQuery, State};
use axum::http::{HeaderMap, StatusCode};
use axum::response::{IntoResponse, Response};
use chrono::{DateTime, Duration, Utc};
use serde::Serialize;

use rustlens_db::game_bans::{
    list_active_user_guild_discord_ids, list_steam_game_ban_cache,
    upsert_steam_account_game_ban_cache, SteamGameBanCache,
};
use rustlens_db::platform_sync::get_platform_sync_state;
use rustlens_db::players_links::{list_guild_links, list_visible_hard_links_by_discord_ids};

use crate::auth::{err_json, is_snowflake, require_bot_jwt};
use crate::platformsync::sync_platform_identity;
use crate::state::AppState;

type Shared = State<Arc<AppState>>;

const BAN_CACHE_TTL_HOURS: i64 = 6;
const PLATFORM_SYNC_TTL_DAYS: i64 = 14;
const DEEP_SYNC_LIMIT: usize = 100;
const BAN_BATCH: usize = 100;
const DIRECT: &str = "direct_guild_link";
const LINKED: &str = "linked_account";

struct Candidate {
    guild_id: String,
    discord_id: String,
    steam_id: String,
    source: &'static str,
}

#[derive(Serialize)]
struct GameBanItem {
    #[serde(rename = "guildId")]
    guild_id: String,
    #[serde(rename = "discordId")]
    discord_id: String,
    #[serde(rename = "steamId")]
    steam_id: String,
    #[serde(rename = "gameBannedAt")]
    game_banned_at: DateTime<Utc>,
    #[serde(rename = "gameBannedUnix")]
    game_banned_unix: i64,
    source: String,
}

/// `(value, window)` — `window = None` means "all" (no time filter).
fn parse_timeframe(tf: &str) -> Option<Option<Duration>> {
    match tf {
        "" | "90d" => Some(Some(Duration::days(90))),
        "30d" => Some(Some(Duration::days(30))),
        "180d" => Some(Some(Duration::days(180))),
        "all" => Some(None),
        _ => None,
    }
}

fn internal(context: &str, e: impl std::fmt::Display) -> Response {
    tracing::error!("players/gamebans {context}: {e}");
    err_json(StatusCode::INTERNAL_SERVER_ERROR, "internal error")
}

pub async fn check_guild_game_bans(
    State(st): Shared,
    headers: HeaderMap,
    Path(guild_id): Path<String>,
    RawQuery(raw): RawQuery,
) -> Response {
    if let Some(resp) = require_bot_jwt(&headers, &st.config) {
        return resp;
    }
    if !is_snowflake(&guild_id) {
        return err_json(StatusCode::BAD_REQUEST, "guildID must be a valid snowflake");
    }

    // Parse method / timeframe / repeated scopeDiscordId from the raw query.
    let mut method = String::new();
    let mut timeframe_raw = String::new();
    let mut scope: Vec<String> = Vec::new();
    for (k, v) in url::form_urlencoded::parse(raw.unwrap_or_default().as_bytes()) {
        match k.as_ref() {
            "method" => method = v.trim().to_ascii_lowercase(),
            "timeframe" => timeframe_raw = v.trim().to_string(),
            "scopeDiscordId" => scope.push(v.trim().to_string()),
            _ => {}
        }
    }
    if method.is_empty() {
        method = "light".to_string();
    }
    if method != "light" && method != "deep" {
        return err_json(StatusCode::BAD_REQUEST, "method must be light or deep");
    }
    let Some(window) = parse_timeframe(&timeframe_raw) else {
        return err_json(StatusCode::BAD_REQUEST, "timeframe must be 30d, 90d, 180d, or all");
    };
    if scope.iter().any(|s| !is_snowflake(s)) {
        return err_json(StatusCode::BAD_REQUEST, "scopeDiscordId must contain valid Discord snowflakes");
    }
    let scope_set: HashSet<&str> = scope.iter().map(String::as_str).collect();

    // Base candidates: guild-linked Steam IDs (optionally scoped).
    let links = match list_guild_links(&st.pool, &guild_id).await {
        Ok(l) => l,
        Err(e) => return internal("list_guild_links", e),
    };
    let filtered: Vec<_> = links
        .into_iter()
        .filter(|l| scope_set.is_empty() || scope_set.contains(l.discord_id.as_str()))
        .collect();
    let mut candidates: Vec<Candidate> = filtered
        .iter()
        .map(|l| Candidate {
            guild_id: guild_id.clone(),
            discord_id: l.discord_id.clone(),
            steam_id: l.steam_id.clone(),
            source: DIRECT,
        })
        .collect();

    if method == "deep" {
        let deep_ids = if !scope.is_empty() {
            let mut v: Vec<String> = scope.clone();
            v.sort();
            v.dedup();
            v
        } else {
            let active = match list_active_user_guild_discord_ids(&st.pool, &guild_id).await {
                Ok(v) => v,
                Err(e) => return internal("list_active_user_guild_discord_ids", e),
            };
            let mut set: HashSet<String> = active.into_iter().collect();
            for l in &filtered {
                set.insert(l.discord_id.clone());
            }
            let mut v: Vec<String> = set.into_iter().collect();
            v.sort();
            v
        };

        // Refresh stale (missing or ≥14d) platformsync identities, capped at 100.
        let mut stale: Vec<&String> = Vec::new();
        for id in &deep_ids {
            match get_platform_sync_state(&st.pool, "discord", id).await {
                Ok(None) => stale.push(id),
                Ok(Some(s)) if Utc::now() - s.last_platform_sync_at >= Duration::days(PLATFORM_SYNC_TTL_DAYS) => {
                    stale.push(id)
                }
                Ok(Some(_)) => {}
                Err(e) => return internal("platform_sync_state", e),
            }
        }
        if stale.len() > DEEP_SYNC_LIMIT {
            return err_json(
                StatusCode::UNPROCESSABLE_ENTITY,
                "More than 100 people in the current scope need a deep lookup. Try setting a smaller scope or use light mode.",
            );
        }
        for id in stale {
            if let Err(e) = sync_platform_identity(&st, "discord", id).await {
                tracing::warn!("players/gamebans deep sync {id}: {e}");
            }
        }

        let hard = match list_visible_hard_links_by_discord_ids(&st.pool, &deep_ids).await {
            Ok(h) => h,
            Err(e) => return internal("list_visible_hard_links", e),
        };
        for l in hard {
            candidates.push(Candidate {
                guild_id: guild_id.clone(),
                discord_id: l.discord_id,
                steam_id: l.steam_id,
                source: LINKED,
            });
        }
    }

    // Dedupe on guild:discord:steam (first-wins keeps direct over linked).
    let mut seen = HashSet::new();
    candidates.retain(|c| seen.insert(format!("{}:{}:{}", c.guild_id, c.discord_id, c.steam_id)));

    // Distinct Steam IDs for the ban scan.
    let mut steam_ids: Vec<String> = {
        let mut set: HashSet<String> = candidates.iter().map(|c| c.steam_id.clone()).collect();
        let mut v: Vec<String> = set.drain().collect();
        v.sort();
        v
    };
    if steam_ids.is_empty() {
        return Json(serde_json::json!({ "items": [] })).into_response();
    }

    // Load + refresh the 6h ban cache.
    let now = Utc::now();
    let mut cache: HashMap<String, SteamGameBanCache> =
        match list_steam_game_ban_cache(&st.pool, &steam_ids).await {
            Ok(rows) => rows.into_iter().map(|r| (r.steam_id.clone(), r)).collect(),
            Err(e) => return internal("list_steam_game_ban_cache", e),
        };
    let stale_ids: Vec<String> = steam_ids
        .drain(..)
        .filter(|id| {
            cache
                .get(id)
                .and_then(|c| c.game_ban_checked_at)
                .is_none_or(|checked| now - checked >= Duration::hours(BAN_CACHE_TTL_HOURS))
        })
        .collect();
    for chunk in stale_ids.chunks(BAN_BATCH) {
        let batch: Vec<&str> = chunk.iter().map(String::as_str).collect();
        let bans = match st.steam.get_player_bans(&batch).await {
            Ok(b) => b,
            Err(e) => return internal("get_player_bans", e),
        };
        for ban in bans {
            let game_banned = ban.number_of_game_bans > 0;
            let game_banned_at = if game_banned {
                Some(now - Duration::days(ban.days_since_last_ban as i64))
            } else {
                None
            };
            if let Err(e) = upsert_steam_account_game_ban_cache(
                &st.pool,
                &ban.steam_id,
                game_banned,
                game_banned_at,
                now,
            )
            .await
            {
                return internal("upsert_game_ban_cache", e);
            }
            cache.insert(
                ban.steam_id.clone(),
                SteamGameBanCache {
                    steam_id: ban.steam_id,
                    game_banned,
                    game_banned_at,
                    game_ban_checked_at: Some(now),
                },
            );
        }
    }

    // Build the recent-ban items (timeframe-filtered), sorted newest first.
    let mut items: Vec<GameBanItem> = candidates
        .iter()
        .filter_map(|c| {
            let cached = cache.get(&c.steam_id)?;
            if !cached.game_banned {
                return None;
            }
            let at = cached.game_banned_at?;
            if let Some(w) = window {
                if now - at > w {
                    return None;
                }
            }
            Some(GameBanItem {
                guild_id: c.guild_id.clone(),
                discord_id: c.discord_id.clone(),
                steam_id: c.steam_id.clone(),
                game_banned_at: at,
                game_banned_unix: at.timestamp(),
                source: c.source.to_string(),
            })
        })
        .collect();
    items.sort_by_key(|i| std::cmp::Reverse(i.game_banned_at));

    Json(serde_json::json!({ "items": items })).into_response()
}
