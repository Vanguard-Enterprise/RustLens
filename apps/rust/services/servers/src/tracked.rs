//! Bot-internal tracked-server + guild-tracked-server routes (JWT-gated).
//! Mirrors the Go handlers.

use std::collections::HashSet;
use std::sync::Arc;

use axum::Json;
use axum::body::Bytes;
use axum::extract::{Path, State};
use axum::http::{HeaderMap, StatusCode};
use axum::response::{IntoResponse, Response};
use serde::Deserialize;
use serde_json::json;

use rustlens_db::tracked_servers::{self as ts, ServerFlags, TrackedServerError};

use crate::auth::{collapse_ws, err_json, is_snowflake, parse_json, require_bot_jwt};
use crate::state::AppState;

type Shared = State<Arc<AppState>>;

macro_rules! jwt_guard {
    ($h:expr, $st:expr) => {
        if let Some(resp) = require_bot_jwt(&$h, &$st.config) {
            return resp;
        }
    };
}

fn db_error(context: &str, e: impl std::fmt::Display) -> Response {
    tracing::error!("servers/tracked {context}: {e}");
    err_json(StatusCode::INTERNAL_SERVER_ERROR, "internal error")
}

#[derive(Deserialize)]
struct ServerReq {
    #[serde(rename = "bmId", default)]
    bm_id: String,
    #[serde(default)]
    name: String,
    #[serde(rename = "isVitality", default)]
    is_vitality: bool,
    #[serde(rename = "isAtlas", default)]
    is_atlas: bool,
    #[serde(rename = "isVital", default)]
    is_vital: bool,
    #[serde(rename = "isModded", default)]
    is_modded: bool,
    #[serde(rename = "isOfficial", default)]
    is_official: bool,
    #[serde(rename = "isBuild", default)]
    is_build: bool,
    #[serde(rename = "highFrequency", default)]
    high_frequency: bool,
}

impl ServerReq {
    fn flags(&self) -> ServerFlags {
        ServerFlags {
            is_vitality: self.is_vitality,
            is_atlas: self.is_atlas,
            is_vital: self.is_vital,
            is_modded: self.is_modded,
            is_official: self.is_official,
            is_build: self.is_build,
            high_frequency: self.high_frequency,
        }
    }
}

pub async fn list_tracked_servers(State(st): Shared, headers: HeaderMap) -> Response {
    jwt_guard!(headers, st);
    match ts::list(&st.pool).await {
        Ok(items) => Json(json!({ "items": items })).into_response(),
        Err(e) => db_error("list", e),
    }
}

pub async fn create_tracked_server(State(st): Shared, headers: HeaderMap, body: Bytes) -> Response {
    jwt_guard!(headers, st);
    let req: ServerReq = match parse_json(&body) {
        Ok(r) => r,
        Err(resp) => return *resp,
    };
    let bm_id = req.bm_id.trim();
    let name = collapse_ws(&req.name);
    if bm_id.is_empty() {
        return err_json(StatusCode::BAD_REQUEST, "bmId is required");
    }
    if name.is_empty() {
        return err_json(StatusCode::BAD_REQUEST, "name is required");
    }
    match ts::create(&st.pool, bm_id, &name, req.flags()).await {
        Ok(server) => (StatusCode::CREATED, Json(server)).into_response(),
        Err(TrackedServerError::AlreadyExists) => {
            err_json(StatusCode::CONFLICT, "tracked server already exists")
        }
        Err(TrackedServerError::Db(e)) => db_error("create", e),
        Err(_) => err_json(StatusCode::INTERNAL_SERVER_ERROR, "internal error"),
    }
}

pub async fn update_tracked_server(
    State(st): Shared,
    headers: HeaderMap,
    Path(bm_id): Path<String>,
    body: Bytes,
) -> Response {
    jwt_guard!(headers, st);
    let req: ServerReq = match parse_json(&body) {
        Ok(r) => r,
        Err(resp) => return *resp,
    };
    let next_bm_id = req.bm_id.trim();
    let name = collapse_ws(&req.name);
    if next_bm_id.is_empty() {
        return err_json(StatusCode::BAD_REQUEST, "bmId is required");
    }
    if name.is_empty() {
        return err_json(StatusCode::BAD_REQUEST, "name is required");
    }
    match ts::update(&st.pool, &bm_id, next_bm_id, &name, req.flags()).await {
        Ok(server) => Json(server).into_response(),
        Err(TrackedServerError::NotFound) => {
            err_json(StatusCode::NOT_FOUND, "tracked server not found")
        }
        Err(TrackedServerError::AlreadyExists) => {
            err_json(StatusCode::CONFLICT, "tracked server already exists")
        }
        Err(TrackedServerError::Db(e)) => db_error("update", e),
        Err(_) => err_json(StatusCode::INTERNAL_SERVER_ERROR, "internal error"),
    }
}

pub async fn delete_tracked_server(
    State(st): Shared,
    headers: HeaderMap,
    Path(bm_id): Path<String>,
) -> Response {
    jwt_guard!(headers, st);
    match ts::delete(&st.pool, &bm_id).await {
        Ok(()) => Json(json!({ "status": "deleted" })).into_response(),
        Err(TrackedServerError::NotFound) => {
            err_json(StatusCode::NOT_FOUND, "tracked server not found")
        }
        Err(TrackedServerError::Db(e)) => db_error("delete", e),
        Err(_) => err_json(StatusCode::INTERNAL_SERVER_ERROR, "internal error"),
    }
}

// ---- guild-scoped ----

#[derive(Deserialize)]
struct ReplaceReq {
    #[serde(rename = "bmIds", default)]
    bm_ids: Vec<String>,
}

pub async fn list_guild_tracked_servers(
    State(st): Shared,
    headers: HeaderMap,
    Path(guild_id): Path<String>,
) -> Response {
    jwt_guard!(headers, st);
    if !is_snowflake(&guild_id) {
        return err_json(StatusCode::BAD_REQUEST, "guildID must be a valid snowflake");
    }
    match ts::list_guild(&st.pool, &guild_id).await {
        Ok(items) => Json(json!({ "items": items })).into_response(),
        Err(e) => db_error("list_guild", e),
    }
}

pub async fn replace_guild_tracked_servers(
    State(st): Shared,
    headers: HeaderMap,
    Path(guild_id): Path<String>,
    body: Bytes,
) -> Response {
    jwt_guard!(headers, st);
    if !is_snowflake(&guild_id) {
        return err_json(StatusCode::BAD_REQUEST, "guildID must be a valid snowflake");
    }
    let req: ReplaceReq = match parse_json(&body) {
        Ok(r) => r,
        Err(resp) => return *resp,
    };
    if req.bm_ids.len() > 100 {
        return err_json(StatusCode::BAD_REQUEST, "bmIds exceeds maximum allowed size");
    }
    let mut deduped: Vec<String> = Vec::new();
    let mut seen = HashSet::new();
    for raw in &req.bm_ids {
        let id = raw.trim();
        if id.is_empty() {
            return err_json(StatusCode::BAD_REQUEST, "bmIds must not contain empty values");
        }
        if seen.insert(id.to_string()) {
            deduped.push(id.to_string());
        }
    }
    if !deduped.is_empty() {
        // All ids must reference existing tracked servers.
        let existing: HashSet<String> = match ts::list(&st.pool).await {
            Ok(servers) => servers.into_iter().map(|s| s.bm_id).collect(),
            Err(e) => return db_error("replace_guild list", e),
        };
        if deduped.iter().any(|id| !existing.contains(id)) {
            return err_json(StatusCode::BAD_REQUEST, "bmIds must reference existing tracked servers");
        }
    }
    if let Err(e) = ts::replace_guild(&st.pool, &guild_id, &deduped).await {
        return db_error("replace_guild", e);
    }
    match ts::list_guild(&st.pool, &guild_id).await {
        Ok(items) => Json(json!({ "items": items })).into_response(),
        Err(e) => db_error("replace_guild reread", e),
    }
}

pub async fn add_guild_tracked_server(
    State(st): Shared,
    headers: HeaderMap,
    Path((guild_id, bm_id)): Path<(String, String)>,
) -> Response {
    jwt_guard!(headers, st);
    if !is_snowflake(&guild_id) {
        return err_json(StatusCode::BAD_REQUEST, "guildID must be a valid snowflake");
    }
    match ts::add_guild(&st.pool, &guild_id, &bm_id).await {
        Ok(()) => Json(json!({ "status": "enabled" })).into_response(),
        Err(TrackedServerError::NotFound) => {
            err_json(StatusCode::NOT_FOUND, "tracked server not found")
        }
        Err(TrackedServerError::GuildAlreadyEnabled) => {
            err_json(StatusCode::CONFLICT, "tracked server already enabled for guild")
        }
        Err(TrackedServerError::Db(e)) => db_error("add_guild", e),
        Err(_) => err_json(StatusCode::INTERNAL_SERVER_ERROR, "internal error"),
    }
}

pub async fn remove_guild_tracked_server(
    State(st): Shared,
    headers: HeaderMap,
    Path((guild_id, bm_id)): Path<(String, String)>,
) -> Response {
    jwt_guard!(headers, st);
    if !is_snowflake(&guild_id) {
        return err_json(StatusCode::BAD_REQUEST, "guildID must be a valid snowflake");
    }
    match ts::remove_guild(&st.pool, &guild_id, &bm_id).await {
        Ok(()) => Json(json!({ "status": "disabled" })).into_response(),
        Err(TrackedServerError::GuildNotEnabled) => {
            err_json(StatusCode::NOT_FOUND, "tracked server not enabled for guild")
        }
        Err(TrackedServerError::Db(e)) => db_error("remove_guild", e),
        Err(_) => err_json(StatusCode::INTERNAL_SERVER_ERROR, "internal error"),
    }
}
