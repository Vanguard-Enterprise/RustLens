//! Track-group analytics route
//! (`/guilds/{guildID}/track-groups/{groupID}/analytics`). Feeds the shared
//! weekday/hour compute engine in `track_analytics.rs` (which also powers
//! player offline-analytics) with per-`(player, server)` cached BattleMetrics
//! sessions for every member of the group. Mirrors the Go
//! `GetTrackGroupAnalytics` handler (`track_analytics_handler.go`).

use std::collections::HashMap;
use std::sync::Arc;

use axum::Json;
use axum::extract::{Path, State};
use axum::http::{HeaderMap, StatusCode};
use axum::response::{IntoResponse, Response};
use chrono::{Duration, Utc};
use futures_util::stream::{self, StreamExt};

use rustlens_db::players_steam as store;
use rustlens_db::track_analytics as cache;
use rustlens_db::track_analytics::TrackAnalyticsSession as Session;
use rustlens_db::track_groups::{self, TrackAnalyticsMember, TrackGroupError};

use crate::auth::{err_json, is_snowflake, require_bot_jwt};
use crate::state::AppState;
use crate::track_analytics::{compute_days, convert_sessions, AnalyticsResponse};

type Shared = State<Arc<AppState>>;

const CACHE_TTL_HOURS: i64 = 24;
const PAGE_SIZE: i32 = 100;
const MAX_PAGES: i32 = 2;

fn parse_group_id(raw: &str) -> Option<i64> {
    raw.trim().parse::<i64>().ok().filter(|&n| n > 0)
}

/// Load cached/fresh sessions for every member, keyed by `player_bm_id`
/// (deduped, like Go's `results` map). Cache misses are fetched concurrently
/// with concurrency equal to the outbound provider's active proxy count (or 1
/// when there are none), mirroring `fetchTrackAnalyticsSessions`.
async fn fetch_group_sessions(
    st: &AppState,
    members: &[TrackAnalyticsMember],
    server_bm_id: &str,
) -> HashMap<String, Vec<Session>> {
    let mut results: HashMap<String, Vec<Session>> = HashMap::new();
    let mut to_fetch: Vec<String> = Vec::new();

    for m in members {
        let entry = match cache::get_track_analytics_cache(&st.pool, &m.bm_id, server_bm_id).await {
            Ok(v) => v,
            Err(e) => {
                tracing::warn!("players/track-group-analytics cache lookup {}: {e}", m.bm_id);
                None
            }
        };
        match entry {
            Some(e) if Utc::now() - e.fetched_at < Duration::hours(CACHE_TTL_HOURS) => {
                results.insert(m.bm_id.clone(), e.sessions);
            }
            _ => to_fetch.push(m.bm_id.clone()),
        }
    }

    if to_fetch.is_empty() {
        return results;
    }

    let concurrency = st.outbound.proxy_count().max(1).min(to_fetch.len());
    let fetched: Vec<(String, Vec<Session>)> = stream::iter(to_fetch)
        .map(|bm_id| async move {
            match st.bm.fetch_player_sessions(&bm_id, server_bm_id, PAGE_SIZE, MAX_PAGES).await {
                Ok(raw) => {
                    let converted = convert_sessions(&raw);
                    if let Err(e) =
                        cache::upsert_track_analytics_cache(&st.pool, &bm_id, server_bm_id, &converted).await
                    {
                        tracing::warn!("players/track-group-analytics cache write {bm_id}: {e}");
                    }
                    (bm_id, converted)
                }
                Err(e) => {
                    tracing::warn!("players/track-group-analytics fetch {bm_id}: {e}");
                    (bm_id, Vec::new())
                }
            }
        })
        .buffer_unordered(concurrency)
        .collect()
        .await;

    for (bm_id, sessions) in fetched {
        results.insert(bm_id, sessions);
    }
    results
}

/// `GET /guilds/{guildID}/track-groups/{groupID}/analytics`. Mirrors Go's
/// `GetTrackGroupAnalytics`.
pub async fn get_track_group_analytics(
    State(st): Shared,
    headers: HeaderMap,
    Path((guild_id, group_id)): Path<(String, String)>,
) -> Response {
    if let Some(resp) = require_bot_jwt(&headers, &st.config) {
        return resp;
    }
    if !is_snowflake(&guild_id) {
        return err_json(StatusCode::BAD_REQUEST, "guildID must be a valid Discord snowflake");
    }
    let Some(group_id) = parse_group_id(&group_id) else {
        return err_json(StatusCode::BAD_REQUEST, "groupID must be a positive integer");
    };

    let group = match track_groups::get(&st.pool, &guild_id, group_id).await {
        Ok(g) => g,
        Err(TrackGroupError::NotFound) => {
            return err_json(StatusCode::NOT_FOUND, "track group not found");
        }
        Err(e) => {
            tracing::error!("players/track-group-analytics load group {guild_id}/{group_id}: {e}");
            return err_json(StatusCode::INTERNAL_SERVER_ERROR, "failed to load track group");
        }
    };

    let members = match track_groups::list_track_analytics_members(&st.pool, group_id).await {
        Ok(m) => m,
        Err(e) => {
            tracing::error!("players/track-group-analytics load members {group_id}: {e}");
            return err_json(StatusCode::INTERNAL_SERVER_ERROR, "failed to load track group members");
        }
    };

    // Best-effort server display name; falls back to the raw BM ID on error,
    // matching Go (which ignores a `ListTrackedServers` failure here).
    let server_name = match store::list_tracked_servers(&st.pool).await {
        Ok(servers) => servers
            .into_iter()
            .find(|s| s.bm_id == group.server_bm_id)
            .map(|s| s.name)
            .unwrap_or_else(|| group.server_bm_id.clone()),
        Err(_) => group.server_bm_id.clone(),
    };

    let sessions = fetch_group_sessions(&st, &members, &group.server_bm_id).await;
    let analyzed = sessions.len() as i32;
    let players: Vec<Vec<Session>> = sessions.into_values().collect();
    let days = compute_days(&players);

    Json(AnalyticsResponse {
        group_name: group.name,
        server_name,
        time_zone: "Europe/London".to_string(),
        members: members.len() as i32,
        analyzed,
        days,
    })
    .into_response()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn group_id_must_be_a_positive_integer() {
        assert_eq!(parse_group_id("42"), Some(42));
        assert_eq!(parse_group_id(" 7 "), Some(7));
        assert_eq!(parse_group_id("0"), None);
        assert_eq!(parse_group_id("-1"), None);
        assert_eq!(parse_group_id("abc"), None);
        assert_eq!(parse_group_id(""), None);
    }
}
