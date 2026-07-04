//! Shard coordination handlers (bot-JWT): register/heartbeat/release/assignments.
//! Mirrors the Go `shards.Service` + handlers. Shard state is entirely in Redis
//! (`leases` module); no Postgres, no background sweeper. Service errors (incl.
//! redis failures) map to 400 with the message, matching Go.

use std::sync::Arc;

use axum::Json;
use axum::body::Bytes;
use axum::extract::{Query, State};
use axum::http::{HeaderMap, StatusCode};
use axum::response::{IntoResponse, Response};
use redis::aio::MultiplexedConnection;
use rustlens_botjwt::BotClaims;
use serde::{Deserialize, Serialize};
use serde_json::{Value, json};

use crate::auth::{err_json, parse_json, require_bot_jwt};
use crate::leases;
use crate::state::AppState;

type Shared = State<Arc<AppState>>;

/// Effective worker id: request value if non-empty, else the JWT `worker_id`.
fn effective_worker(req_id: &str, claims: &BotClaims) -> String {
    if !req_id.is_empty() {
        req_id.to_string()
    } else {
        claims.worker_id.clone().unwrap_or_default()
    }
}

/// Clone the Redis connection or a boxed `400 shard store unavailable` response
/// (matches Go's Service-error → 400 mapping).
fn conn(st: &AppState) -> Result<MultiplexedConnection, Box<Response>> {
    st.redis
        .clone()
        .ok_or_else(|| Box::new(err_json(StatusCode::BAD_REQUEST, "shard store unavailable")))
}

/// Map a redis error to `400 <message>` (Go returns Service errors as 400).
fn svc_err(e: redis::RedisError) -> Response {
    err_json(StatusCode::BAD_REQUEST, &e.to_string())
}

#[derive(Serialize)]
struct RegisterResponse {
    #[serde(rename = "workerId")]
    worker_id: String,
    #[serde(rename = "targetShards")]
    target_shards: i64,
    #[serde(rename = "heartbeatTtlSeconds")]
    heartbeat_ttl_seconds: u64,
}

#[derive(Deserialize)]
struct WorkerIdReq {
    #[serde(default, rename = "workerId")]
    worker_id: String,
}

pub async fn register_shard_worker(State(st): Shared, headers: HeaderMap, body: Bytes) -> Response {
    let claims = match require_bot_jwt(&headers, &st.config) {
        Ok(c) => c,
        Err(r) => return *r,
    };
    let req: WorkerIdReq = match parse_json(&body) {
        Ok(r) => r,
        Err(r) => return *r,
    };
    let worker = effective_worker(&req.worker_id, &claims);
    if worker.is_empty() {
        return err_json(StatusCode::BAD_REQUEST, "worker id required");
    }
    let mut conn = match conn(&st) {
        Ok(c) => c,
        Err(r) => return *r,
    };
    let ttl = st.config.bot_heartbeat_ttl_seconds;
    if let Err(e) = leases::mark_worker(&mut conn, &worker, ttl).await {
        return svc_err(e);
    }
    Json(RegisterResponse {
        worker_id: worker,
        target_shards: st.config.bot_target_shards,
        heartbeat_ttl_seconds: ttl,
    })
    .into_response()
}

#[derive(Deserialize)]
struct HeartbeatReq {
    #[serde(default, rename = "workerId")]
    worker_id: String,
    /// Accepted but ignored (health-only), matching Go.
    #[serde(default, rename = "shardIds")]
    _shard_ids: Vec<i64>,
    #[serde(default)]
    stats: Option<Value>,
}

pub async fn heartbeat_shard_worker(State(st): Shared, headers: HeaderMap, body: Bytes) -> Response {
    let claims = match require_bot_jwt(&headers, &st.config) {
        Ok(c) => c,
        Err(r) => return *r,
    };
    let req: HeartbeatReq = match parse_json(&body) {
        Ok(r) => r,
        Err(r) => return *r,
    };
    let worker = effective_worker(&req.worker_id, &claims);
    if worker.is_empty() {
        return err_json(StatusCode::BAD_REQUEST, "worker id required");
    }
    let mut conn = match conn(&st) {
        Ok(c) => c,
        Err(r) => return *r,
    };
    let ttl = st.config.bot_heartbeat_ttl_seconds;
    if let Err(e) = leases::mark_worker(&mut conn, &worker, ttl).await {
        return svc_err(e);
    }
    if let Some(stats) = &req.stats {
        let payload = stats.to_string();
        if let Err(e) = leases::set_worker_stats(&mut conn, &worker, &payload, ttl).await {
            return svc_err(e);
        }
    }
    Json(json!({ "status": "ok" })).into_response()
}

pub async fn release_shard_worker(State(st): Shared, headers: HeaderMap, body: Bytes) -> Response {
    let claims = match require_bot_jwt(&headers, &st.config) {
        Ok(c) => c,
        Err(r) => return *r,
    };
    let req: WorkerIdReq = match parse_json(&body) {
        Ok(r) => r,
        Err(r) => return *r,
    };
    let worker = effective_worker(&req.worker_id, &claims);
    if worker.is_empty() {
        return err_json(StatusCode::BAD_REQUEST, "worker id required");
    }
    let mut conn = match conn(&st) {
        Ok(c) => c,
        Err(r) => return *r,
    };
    if let Err(e) = leases::remove_worker(&mut conn, &worker).await {
        return svc_err(e);
    }
    if let Err(e) = leases::set_leases(&mut conn, &worker, &[]).await {
        return svc_err(e);
    }
    Json(json!({ "status": "released" })).into_response()
}

#[derive(Deserialize)]
pub struct AssignmentsParams {
    #[serde(rename = "workerId")]
    worker_id: Option<String>,
}

pub async fn get_shard_assignments(
    State(st): Shared,
    headers: HeaderMap,
    Query(params): Query<AssignmentsParams>,
) -> Response {
    let claims = match require_bot_jwt(&headers, &st.config) {
        Ok(c) => c,
        Err(r) => return *r,
    };
    let worker = params
        .worker_id
        .filter(|s| !s.is_empty())
        .or(claims.worker_id)
        .unwrap_or_default();
    let mut conn = match conn(&st) {
        Ok(c) => c,
        Err(r) => return *r,
    };
    let ttl = st.config.bot_heartbeat_ttl_seconds;

    // Refresh the caller's own lease before reading the active set (protects its
    // shards during a transient heartbeat gap).
    if !worker.is_empty() {
        if let Err(e) = leases::mark_worker(&mut conn, &worker, ttl).await {
            return svc_err(e);
        }
    }
    let workers = match leases::active_workers(&mut conn).await {
        Ok(w) => w,
        Err(e) => return svc_err(e),
    };
    let assignments = leases::assign_shards(st.config.bot_target_shards, &workers);
    for (owner, shards) in &assignments {
        if let Err(e) = leases::set_leases(&mut conn, owner, shards).await {
            return svc_err(e);
        }
    }
    let current = match leases::current_assignments(&mut conn).await {
        Ok(c) => c,
        Err(e) => return svc_err(e),
    };
    let mut owned = current.get(&worker).cloned().unwrap_or_default();
    owned.sort_unstable();
    // Owns nothing → `null` (never `[]`), matching Go's nil-slice marshalling.
    let owned_opt: Option<Vec<i64>> = if owned.is_empty() { None } else { Some(owned) };
    Json(json!({
        "shardIds": owned_opt,
        "targetShards": st.config.bot_target_shards,
        "workerId": worker,
    }))
    .into_response()
}
