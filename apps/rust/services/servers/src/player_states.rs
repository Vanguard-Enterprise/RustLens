//! Bot-internal player-presence read routes (JWT-gated). Mirrors the Go handlers.

use std::sync::Arc;

use axum::Json;
use axum::extract::{Path, Query, State};
use axum::http::{HeaderMap, StatusCode};
use axum::response::{IntoResponse, Response};
use serde::Deserialize;
use serde_json::{json, Value};

use rustlens_db::player_states as ps;
use rustlens_db::players_steam;

use crate::auth::{err_json, is_snowflake, require_bot_jwt};
use crate::state::AppState;

type Shared = State<Arc<AppState>>;

/// Search default + hard cap (Go caps at 21 — one overflow row past the 20 display cap).
const SEARCH_DEFAULT_LIMIT: i64 = 20;
const SEARCH_MAX_LIMIT: i64 = 21;

macro_rules! jwt_guard {
    ($h:expr, $st:expr) => {
        if let Some(resp) = require_bot_jwt(&$h, &$st.config) {
            return resp;
        }
    };
}

fn db_error(context: &str, e: impl std::fmt::Display) -> Response {
    tracing::error!("servers/player-states {context}: {e}");
    err_json(StatusCode::INTERNAL_SERVER_ERROR, "internal error")
}

fn state_map(s: &players_steam::PlayerState) -> Value {
    json!({
        "serverBmId": s.server_bm_id,
        "serverName": s.server_name,
        "playerBmId": s.player_bm_id,
        "isOnline": s.is_online,
        "updatedAt": s.updated_at,
        "onlineSince": s.online_since,
        "offlineSince": s.offline_since,
    })
}

pub async fn list_online_by_server(
    State(st): Shared,
    headers: HeaderMap,
    Path(server_id): Path<String>,
) -> Response {
    jwt_guard!(headers, st);
    if server_id.trim().is_empty() {
        return err_json(StatusCode::BAD_REQUEST, "serverID is required");
    }
    match ps::list_online_by_server(&st.pool, &server_id).await {
        Ok(items) => {
            let maps: Vec<Value> = items.iter().map(state_map).collect();
            Json(json!({ "items": maps })).into_response()
        }
        Err(e) => db_error("list_online_by_server", e),
    }
}

pub async fn list_states_by_player(
    State(st): Shared,
    headers: HeaderMap,
    Path(player_id): Path<String>,
) -> Response {
    jwt_guard!(headers, st);
    if player_id.trim().is_empty() {
        return err_json(StatusCode::BAD_REQUEST, "playerID is required");
    }
    match players_steam::list_player_states_by_player(&st.pool, &player_id).await {
        Ok(items) => {
            let maps: Vec<Value> = items.iter().map(state_map).collect();
            Json(json!({ "items": maps })).into_response()
        }
        Err(e) => db_error("list_states_by_player", e),
    }
}

pub async fn get_state(
    State(st): Shared,
    headers: HeaderMap,
    Path((server_id, player_id)): Path<(String, String)>,
) -> Response {
    jwt_guard!(headers, st);
    if server_id.trim().is_empty() || player_id.trim().is_empty() {
        return err_json(StatusCode::BAD_REQUEST, "serverID and playerID are required");
    }
    match ps::get_state(&st.pool, &server_id, &player_id).await {
        Ok(Some(s)) => Json(state_map(&s)).into_response(),
        Ok(None) => err_json(StatusCode::NOT_FOUND, "player state not found"),
        Err(e) => db_error("get_state", e),
    }
}

#[derive(Deserialize)]
pub struct SearchParams {
    q: Option<String>,
    limit: Option<String>,
    #[serde(rename = "includeOffline")]
    include_offline: Option<String>,
}

pub async fn search_states_on_server(
    State(st): Shared,
    headers: HeaderMap,
    Path(server_id): Path<String>,
    Query(params): Query<SearchParams>,
) -> Response {
    jwt_guard!(headers, st);
    if server_id.trim().is_empty() {
        return err_json(StatusCode::BAD_REQUEST, "serverID is required");
    }
    let q = params.q.unwrap_or_default();
    let q = q.trim();
    if q.is_empty() {
        return err_json(StatusCode::BAD_REQUEST, "q is required");
    }
    let limit = match params.limit.as_deref().map(str::trim).filter(|s| !s.is_empty()) {
        None => SEARCH_DEFAULT_LIMIT,
        Some(v) => match v.parse::<i64>() {
            Ok(n) if n > 0 => n.min(SEARCH_MAX_LIMIT),
            _ => return err_json(StatusCode::BAD_REQUEST, "limit must be a positive integer"),
        },
    };
    let include_offline = match params.include_offline.as_deref() {
        None | Some("") => false,
        Some("true") => true,
        Some("false") => false,
        Some(_) => return err_json(StatusCode::BAD_REQUEST, "includeOffline must be a boolean"),
    };
    match ps::search_by_name(&st.pool, &server_id, q, limit, include_offline).await {
        Ok(items) => Json(json!({ "items": items })).into_response(),
        Err(e) => db_error("search", e),
    }
}

pub async fn list_online_summary(
    State(st): Shared,
    headers: HeaderMap,
    Path(guild_id): Path<String>,
) -> Response {
    jwt_guard!(headers, st);
    if !is_snowflake(&guild_id) {
        return err_json(StatusCode::BAD_REQUEST, "guildID must be a valid snowflake");
    }
    match ps::list_online_summary(&st.pool, &guild_id).await {
        Ok(items) => Json(json!({ "items": items })).into_response(),
        Err(e) => db_error("online_summary", e),
    }
}

pub async fn list_tracked_server_log_events(
    State(st): Shared,
    headers: HeaderMap,
    Path(guild_id): Path<String>,
) -> Response {
    jwt_guard!(headers, st);
    if !is_snowflake(&guild_id) {
        return err_json(StatusCode::BAD_REQUEST, "guildID must be a valid snowflake");
    }
    match ps::list_log_events(&st.pool, &guild_id).await {
        Ok(items) => Json(json!({ "items": items })).into_response(),
        Err(e) => db_error("log_events", e),
    }
}
