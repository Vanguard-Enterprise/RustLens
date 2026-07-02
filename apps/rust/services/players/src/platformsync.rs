//! platform-sync route + the reusable sync routine (also driven by deep
//! game-bans). Mirrors `internal/http/platform_sync_handler.go`.

use std::collections::HashSet;
use std::sync::Arc;

use axum::Json;
use axum::extract::{Path, State};
use axum::http::{HeaderMap, StatusCode};
use axum::response::{IntoResponse, Response};
use chrono::{DateTime, Duration, Utc};
use serde::Serialize;

use rustlens_db::platform_sync::{get_platform_sync_state, upsert_platform_sync_state};
use rustlens_db::players_links::{bulk_upsert_hard_links, BulkHardLink};
use rustlens_steam::platformsync::{Lookup, PlatformSyncError};

use crate::auth::{err_json, is_snowflake, is_steam64, require_bot_jwt};
use crate::state::AppState;

type Shared = State<Arc<AppState>>;

const TTL_DAYS: i64 = 14;
const SOURCE: &str = "platformsync";

/// The platform-sync response (`platformSyncResponse`). Optional fields use
/// `skip_serializing_if` to match Go's `omitempty`.
#[derive(Serialize)]
pub struct PlatformSyncResponse {
    pub synced: bool,
    #[serde(skip_serializing_if = "String::is_empty")]
    pub reason: String,
    #[serde(rename = "lastSyncedAt")]
    pub last_synced_at: DateTime<Utc>,
    pub platform: String,
    #[serde(rename = "platformId")]
    pub platform_id: String,
    pub linked: bool,
    #[serde(rename = "steamId", skip_serializing_if = "String::is_empty")]
    pub steam_id: String,
    #[serde(rename = "discordId", skip_serializing_if = "String::is_empty")]
    pub discord_id: String,
    #[serde(rename = "previousSteamIds", skip_serializing_if = "Vec::is_empty")]
    pub previous_steam_ids: Vec<String>,
    #[serde(rename = "previousDiscordIds", skip_serializing_if = "Vec::is_empty")]
    pub previous_discord_ids: Vec<String>,
    #[serde(rename = "hardLinkCount")]
    pub hard_link_count: usize,
}

/// Outcome of the sync routine. `NotConfigured` → 503; upstream HTTP errors are
/// folded into `NotConfigured` by the platformsync client (matching Go).
pub enum SyncError {
    NotConfigured,
    Db(String),
}

impl std::fmt::Display for SyncError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            SyncError::NotConfigured => write!(f, "not configured"),
            SyncError::Db(e) => write!(f, "{e}"),
        }
    }
}

fn base(platform: &str, id: &str, last: DateTime<Utc>) -> PlatformSyncResponse {
    PlatformSyncResponse {
        synced: false,
        reason: String::new(),
        last_synced_at: last,
        platform: platform.to_string(),
        platform_id: id.to_string(),
        linked: false,
        steam_id: String::new(),
        discord_id: String::new(),
        previous_steam_ids: Vec::new(),
        previous_discord_ids: Vec::new(),
        hard_link_count: 0,
    }
}

/// Look up + record a `(platform, id)` identity, upserting discovered hard links.
/// Fresh (`< 14d`) states short-circuit without an upstream call.
pub async fn sync_platform_identity(
    st: &AppState,
    platform: &str,
    id: &str,
) -> Result<PlatformSyncResponse, SyncError> {
    let state = get_platform_sync_state(&st.pool, platform, id)
        .await
        .map_err(|e| SyncError::Db(e.to_string()))?;
    if let Some(s) = &state {
        if Utc::now() - s.last_platform_sync_at < Duration::days(TTL_DAYS) {
            let mut resp = base(platform, id, s.last_platform_sync_at);
            resp.reason = "fresh".to_string();
            return Ok(resp);
        }
    }

    match st.platform_sync.lookup(id).await {
        Err(PlatformSyncError::NotConfigured) => Err(SyncError::NotConfigured),
        Err(PlatformSyncError::NotLinked) => {
            let recorded = upsert_platform_sync_state(&st.pool, platform, id)
                .await
                .map_err(|e| SyncError::Db(e.to_string()))?;
            let mut resp = base(platform, id, recorded.last_platform_sync_at);
            resp.synced = true;
            Ok(resp)
        }
        Ok(lookup) => {
            let links = build_hard_links(platform, &lookup);
            let hard_link_count = links.len();
            if !links.is_empty() {
                bulk_upsert_hard_links(&st.pool, &links)
                    .await
                    .map_err(|e| SyncError::Db(e.to_string()))?;
            }
            let recorded = upsert_platform_sync_state(&st.pool, platform, id)
                .await
                .map_err(|e| SyncError::Db(e.to_string()))?;
            Ok(PlatformSyncResponse {
                synced: true,
                reason: String::new(),
                last_synced_at: recorded.last_platform_sync_at,
                platform: platform.to_string(),
                platform_id: id.to_string(),
                linked: true,
                steam_id: lookup.steam_id,
                discord_id: lookup.discord_id,
                previous_steam_ids: lookup.previous_steam_ids,
                previous_discord_ids: lookup.previous_discord_ids,
                hard_link_count,
            })
        }
    }
}

/// Cartesian product of valid steam IDs × discord IDs (queried id + previous
/// ids), each a `platformsync`-sourced hard link. Mirrors
/// `buildPlatformSyncHardLinks`.
fn build_hard_links(platform: &str, lookup: &Lookup) -> Vec<BulkHardLink> {
    if !lookup.linked {
        return Vec::new();
    }
    let (steam_ids, discord_ids) = if platform == "steam" {
        let steam = if is_steam64(&lookup.steam_id) {
            vec![lookup.steam_id.clone()]
        } else {
            Vec::new()
        };
        let discord = collect_valid(&lookup.discord_id, &lookup.previous_discord_ids, is_snowflake);
        (steam, discord)
    } else {
        let discord = if is_snowflake(&lookup.discord_id) {
            vec![lookup.discord_id.clone()]
        } else {
            Vec::new()
        };
        let steam = collect_valid(&lookup.steam_id, &lookup.previous_steam_ids, is_steam64);
        (steam, discord)
    };
    if steam_ids.is_empty() || discord_ids.is_empty() {
        return Vec::new();
    }
    let mut out = Vec::with_capacity(steam_ids.len() * discord_ids.len());
    for s in &steam_ids {
        for d in &discord_ids {
            out.push(BulkHardLink {
                steam_id: s.clone(),
                bm_id: None,
                discord_id: d.clone(),
                source: SOURCE.to_string(),
            });
        }
    }
    out
}

fn collect_valid(primary: &str, extras: &[String], valid: fn(&str) -> bool) -> Vec<String> {
    let mut seen = HashSet::new();
    let mut out = Vec::new();
    for raw in std::iter::once(primary).chain(extras.iter().map(String::as_str)) {
        let id = raw.trim();
        if valid(id) && seen.insert(id.to_string()) {
            out.push(id.to_string());
        }
    }
    out
}

pub async fn sync_platform(
    State(st): Shared,
    headers: HeaderMap,
    Path((platform, id)): Path<(String, String)>,
) -> Response {
    if let Some(resp) = require_bot_jwt(&headers, &st.config) {
        return resp;
    }
    if platform != "steam" && platform != "discord" {
        return err_json(StatusCode::BAD_REQUEST, "platform must be 'steam' or 'discord'");
    }
    if !is_snowflake(&id) {
        return err_json(StatusCode::BAD_REQUEST, "id must be a 17-20 digit snowflake / steam id");
    }
    if platform == "steam" && !is_steam64(&id) {
        return err_json(StatusCode::BAD_REQUEST, "steam id must be a 17-digit number");
    }
    match sync_platform_identity(&st, &platform, &id).await {
        Ok(resp) => Json(resp).into_response(),
        Err(SyncError::NotConfigured) => {
            err_json(StatusCode::SERVICE_UNAVAILABLE, "platformsync not configured")
        }
        Err(SyncError::Db(e)) => {
            tracing::error!("players/platformsync: {e}");
            err_json(StatusCode::INTERNAL_SERVER_ERROR, "internal error")
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn lookup(steam: &str, discord: &str, prev_s: &[&str], prev_d: &[&str]) -> Lookup {
        Lookup {
            linked: true,
            steam_id: steam.into(),
            discord_id: discord.into(),
            previous_steam_ids: prev_s.iter().map(|s| s.to_string()).collect(),
            previous_discord_ids: prev_d.iter().map(|s| s.to_string()).collect(),
        }
    }

    #[test]
    fn discord_platform_cartesian_across_steam_ids() {
        // querying by discord: discord is the key, steam ids expand (current + prev).
        let l = lookup(
            "76561190000000001",
            "111111111111111111",
            &["76561190000000002", "bad", "76561190000000001"],
            &[],
        );
        let links = build_hard_links("discord", &l);
        // 2 distinct valid steam ids (dup + invalid dropped) × 1 discord = 2.
        assert_eq!(links.len(), 2);
        assert!(links.iter().all(|x| x.discord_id == "111111111111111111"));
        assert!(links.iter().all(|x| x.source == "platformsync"));
        let steams: Vec<&str> = links.iter().map(|x| x.steam_id.as_str()).collect();
        assert!(steams.contains(&"76561190000000001") && steams.contains(&"76561190000000002"));
    }

    #[test]
    fn steam_platform_cartesian_across_discord_ids() {
        let l = lookup(
            "76561190000000001",
            "111111111111111111",
            &[],
            &["222222222222222222", "NA-invalid"],
        );
        let links = build_hard_links("steam", &l);
        assert_eq!(links.len(), 2, "1 steam × 2 valid discord");
        assert!(links.iter().all(|x| x.steam_id == "76561190000000001"));
    }

    #[test]
    fn empty_side_yields_no_links() {
        // steam invalid -> no steam ids -> no links.
        let l = lookup("bad", "111111111111111111", &[], &[]);
        assert!(build_hard_links("steam", &l).is_empty());
    }
}
