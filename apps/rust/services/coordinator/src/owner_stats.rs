//! Owner stats (`GET /api/internal/bot/owner/stats`, bot-JWT). Live shard stats
//! (Redis) + 7 Postgres counts, aggregated. Mirrors Go's GetOwnerStats.

use std::sync::Arc;

use axum::Json;
use axum::extract::State;
use axum::http::{HeaderMap, StatusCode};
use axum::response::{IntoResponse, Response};
use chrono::Utc;
use serde::Serialize;

use rustlens_db::owner_stats::owner_stats;

use crate::auth::{err_json, require_bot_jwt};
use crate::leases::{self, ShardStatSnapshot, WorkerStatSnapshot};
use crate::state::AppState;

type Shared = State<Arc<AppState>>;

#[derive(Serialize)]
struct OwnerStatsWorker {
    #[serde(rename = "workerId")]
    worker_id: String,
    #[serde(rename = "uptimeSec")]
    uptime_sec: i64,
    #[serde(rename = "rssBytes")]
    rss_bytes: i64,
    /// `null` when the worker reports no shards (Go's nil-slice marshalling).
    shards: Option<Vec<ShardStatSnapshot>>,
}

#[derive(Serialize)]
struct OwnerStatsResponse {
    #[serde(rename = "guildCount")]
    guild_count: i64,
    #[serde(rename = "memberCount")]
    member_count: i64,
    #[serde(rename = "configuredGuilds")]
    configured_guilds: i64,
    #[serde(rename = "steamAccountCount")]
    steam_account_count: i64,
    #[serde(rename = "hardLinkCount")]
    hard_link_count: i64,
    #[serde(rename = "uniqueLinkedUsers")]
    unique_linked_users: i64,
    #[serde(rename = "trackedServerCount")]
    tracked_server_count: i64,
    #[serde(rename = "onlineTrackedPlayers")]
    online_tracked_players: i64,
    #[serde(rename = "activeWipeReacts")]
    active_wipe_reacts: i64,
    #[serde(rename = "targetShards")]
    target_shards: i64,
    workers: Vec<OwnerStatsWorker>,
}

/// Load the active workers' stats, or `Err` (→ 500) when Redis is unavailable.
async fn load_worker_stats(st: &AppState) -> Result<Vec<(String, WorkerStatSnapshot)>, ()> {
    let Some(mut conn) = st.redis.clone() else {
        return Err(());
    };
    leases::active_worker_stats(&mut conn).await.map_err(|_| ())
}

pub async fn get_owner_stats(State(st): Shared, headers: HeaderMap) -> Response {
    if let Err(r) = require_bot_jwt(&headers, &st.config) {
        return *r;
    }
    let worker_stats = match load_worker_stats(&st).await {
        Ok(v) => v,
        Err(()) => return err_json(StatusCode::INTERNAL_SERVER_ERROR, "failed to load worker stats"),
    };
    let counts = match owner_stats(&st.pool, Utc::now().timestamp()).await {
        Ok(c) => c,
        Err(e) => {
            tracing::error!("coordinator/owner-stats db counts: {e}");
            return err_json(StatusCode::INTERNAL_SERVER_ERROR, "failed to load db counts");
        }
    };

    // active_worker_stats is already sorted by worker id.
    let mut guild_count = 0i64;
    let mut member_count = 0i64;
    let mut workers = Vec::with_capacity(worker_stats.len());
    for (worker_id, snap) in worker_stats {
        let mut shards = snap.shards;
        shards.sort_by_key(|s| s.shard_id);
        for s in &shards {
            guild_count += s.guilds;
            member_count += s.members;
        }
        let shards_out = if shards.is_empty() { None } else { Some(shards) };
        workers.push(OwnerStatsWorker {
            worker_id,
            uptime_sec: snap.uptime_sec,
            rss_bytes: snap.rss_bytes,
            shards: shards_out,
        });
    }

    Json(OwnerStatsResponse {
        guild_count,
        member_count,
        configured_guilds: counts.configured_guilds,
        steam_account_count: counts.steam_account_count,
        hard_link_count: counts.hard_link_count,
        unique_linked_users: counts.unique_linked_users,
        tracked_server_count: counts.tracked_server_count,
        online_tracked_players: counts.online_tracked_players,
        active_wipe_reacts: counts.active_wipe_reacts,
        target_shards: st.config.bot_target_shards,
        workers,
    })
    .into_response()
}
