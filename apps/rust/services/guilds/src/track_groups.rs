//! Track groups (bot-JWT): CRUD, members add/remove, online-state, and the
//! Redis notification-claim dedup. Mirrors the Go handlers. The five
//! sub-resource routes load the group first as an existence guard.

use std::sync::Arc;

use axum::Json;
use axum::body::Bytes;
use axum::extract::{Path, State};
use axum::http::{HeaderMap, StatusCode};
use axum::response::{IntoResponse, Response};
use serde::Deserialize;
use serde_json::json;

use rustlens_db::track_groups::{self as tg, CreateInput, TrackGroupError, UpdatePatch};

use crate::auth::{err_json, parse_json, require_bot_jwt, validate_guild_id};
use crate::state::AppState;

type Shared = State<Arc<AppState>>;

fn parse_group_id(raw: &str) -> Option<i64> {
    raw.trim().parse::<i64>().ok().filter(|&n| n > 0)
}

fn is_steam_id(s: &str) -> bool {
    s.len() == 17 && s.bytes().all(|b| b.is_ascii_digit())
}

/// Guard: load the group (404 / 500 `failed to load track group`). Returns the
/// error response on failure.
async fn guard_group(st: &AppState, guild_id: &str, id: i64) -> Option<Response> {
    match tg::get(&st.pool, guild_id, id).await {
        Ok(_) => None,
        Err(TrackGroupError::NotFound) => Some(err_json(StatusCode::NOT_FOUND, "track group not found")),
        Err(e) => {
            tracing::error!("guilds/track-groups guard: {e}");
            Some(err_json(StatusCode::INTERNAL_SERVER_ERROR, "failed to load track group"))
        }
    }
}

fn map_tg_err(context: &str, e: TrackGroupError, msg_500: &str) -> Response {
    match e {
        TrackGroupError::NotFound => err_json(StatusCode::NOT_FOUND, "track group not found"),
        TrackGroupError::NameTaken => {
            err_json(StatusCode::CONFLICT, "track group with that name already exists")
        }
        TrackGroupError::Db(e) => {
            tracing::error!("guilds/track-groups {context}: {e}");
            err_json(StatusCode::INTERNAL_SERVER_ERROR, msg_500)
        }
    }
}

pub async fn list_track_groups(
    State(st): Shared,
    headers: HeaderMap,
    Path(guild_id): Path<String>,
) -> Response {
    if let Some(r) = require_bot_jwt(&headers, &st.config) {
        return r;
    }
    if let Some(r) = validate_guild_id(&guild_id) {
        return r;
    }
    match tg::list(&st.pool, &guild_id).await {
        Ok(items) => Json(json!({ "items": items })).into_response(),
        Err(e) => {
            tracing::error!("guilds/track-groups list: {e}");
            err_json(StatusCode::INTERNAL_SERVER_ERROR, "failed to list track groups")
        }
    }
}

#[derive(Deserialize)]
struct CreateReq {
    #[serde(default)]
    name: String,
    #[serde(default, rename = "serverBmId")]
    server_bm_id: String,
    #[serde(default, rename = "channelId")]
    channel_id: String,
    #[serde(default, rename = "messageId")]
    message_id: String,
    #[serde(default, rename = "sendUpdates")]
    send_updates: bool,
    #[serde(default, rename = "pingAlertRoleOnEmpty")]
    ping_alert_role_on_empty: bool,
    #[serde(default, rename = "onlineCount")]
    online_count: i32,
    #[serde(default, rename = "createdBy")]
    created_by: String,
}

pub async fn create_track_group(
    State(st): Shared,
    headers: HeaderMap,
    Path(guild_id): Path<String>,
    body: Bytes,
) -> Response {
    if let Some(r) = require_bot_jwt(&headers, &st.config) {
        return r;
    }
    if let Some(r) = validate_guild_id(&guild_id) {
        return r;
    }
    let req: CreateReq = match parse_json(&body) {
        Ok(r) => r,
        Err(resp) => return *resp,
    };
    if req.name.is_empty() {
        return err_json(StatusCode::BAD_REQUEST, "name is required");
    }
    if req.server_bm_id.is_empty() {
        return err_json(StatusCode::BAD_REQUEST, "serverBmId is required");
    }
    let input = CreateInput {
        name: req.name,
        server_bm_id: req.server_bm_id,
        channel_id: req.channel_id,
        message_id: req.message_id,
        send_updates: req.send_updates,
        ping_alert_role_on_empty: req.ping_alert_role_on_empty,
        online_count: req.online_count,
        created_by: req.created_by,
    };
    match tg::create(&st.pool, &guild_id, &input).await {
        Ok(group) => Json(group).into_response(),
        Err(e) => map_tg_err("create", e, "failed to create track group"),
    }
}

pub async fn get_track_group(
    State(st): Shared,
    headers: HeaderMap,
    Path((guild_id, group_id)): Path<(String, String)>,
) -> Response {
    if let Some(r) = require_bot_jwt(&headers, &st.config) {
        return r;
    }
    if let Some(r) = validate_guild_id(&guild_id) {
        return r;
    }
    let Some(id) = parse_group_id(&group_id) else {
        return err_json(StatusCode::BAD_REQUEST, "groupID must be a positive integer");
    };
    match tg::get(&st.pool, &guild_id, id).await {
        Ok(group) => Json(group).into_response(),
        Err(e) => map_tg_err("get", e, "failed to load track group"),
    }
}

#[derive(Deserialize)]
struct UpdateReq {
    #[serde(default, rename = "channelId")]
    channel_id: Option<String>,
    #[serde(default, rename = "messageId")]
    message_id: Option<String>,
    #[serde(default, rename = "sendUpdates")]
    send_updates: Option<bool>,
    #[serde(default, rename = "pingAlertRoleOnEmpty")]
    ping_alert_role_on_empty: Option<bool>,
    #[serde(default, rename = "onlineCount")]
    online_count: Option<i32>,
}

pub async fn update_track_group(
    State(st): Shared,
    headers: HeaderMap,
    Path((guild_id, group_id)): Path<(String, String)>,
    body: Bytes,
) -> Response {
    if let Some(r) = require_bot_jwt(&headers, &st.config) {
        return r;
    }
    if let Some(r) = validate_guild_id(&guild_id) {
        return r;
    }
    let Some(id) = parse_group_id(&group_id) else {
        return err_json(StatusCode::BAD_REQUEST, "groupID must be a positive integer");
    };
    let req: UpdateReq = match parse_json(&body) {
        Ok(r) => r,
        Err(resp) => return *resp,
    };
    let patch = UpdatePatch {
        channel_id: req.channel_id,
        message_id: req.message_id,
        send_updates: req.send_updates,
        ping_alert_role_on_empty: req.ping_alert_role_on_empty,
        online_count: req.online_count,
    };
    match tg::update(&st.pool, &guild_id, id, &patch).await {
        Ok(group) => Json(group).into_response(),
        Err(e) => map_tg_err("update", e, "failed to update track group"),
    }
}

pub async fn delete_track_group(
    State(st): Shared,
    headers: HeaderMap,
    Path((guild_id, group_id)): Path<(String, String)>,
) -> Response {
    if let Some(r) = require_bot_jwt(&headers, &st.config) {
        return r;
    }
    if let Some(r) = validate_guild_id(&guild_id) {
        return r;
    }
    let Some(id) = parse_group_id(&group_id) else {
        return err_json(StatusCode::BAD_REQUEST, "groupID must be a positive integer");
    };
    match tg::delete(&st.pool, &guild_id, id).await {
        Ok(()) => Json(json!({ "status": "deleted" })).into_response(),
        Err(e) => map_tg_err("delete", e, "failed to delete track group"),
    }
}

/// Common prelude for the sub-resource routes: jwt + guild + groupID + guard.
/// Returns `Ok(id)` or the error response.
async fn sub_prelude(
    st: &AppState,
    headers: &HeaderMap,
    guild_id: &str,
    group_id_raw: &str,
) -> Result<i64, Response> {
    if let Some(r) = require_bot_jwt(headers, &st.config) {
        return Err(r);
    }
    if let Some(r) = validate_guild_id(guild_id) {
        return Err(r);
    }
    let Some(id) = parse_group_id(group_id_raw) else {
        return Err(err_json(StatusCode::BAD_REQUEST, "groupID must be a positive integer"));
    };
    if let Some(r) = guard_group(st, guild_id, id).await {
        return Err(r);
    }
    Ok(id)
}

pub async fn list_track_group_members(
    State(st): Shared,
    headers: HeaderMap,
    Path((guild_id, group_id)): Path<(String, String)>,
) -> Response {
    let id = match sub_prelude(&st, &headers, &guild_id, &group_id).await {
        Ok(id) => id,
        Err(r) => return r,
    };
    match tg::list_members(&st.pool, id).await {
        Ok(items) => Json(json!({ "items": items })).into_response(),
        Err(e) => {
            tracing::error!("guilds/track-groups members: {e}");
            err_json(StatusCode::INTERNAL_SERVER_ERROR, "failed to list members")
        }
    }
}

#[derive(Deserialize)]
struct MemberPayload {
    #[serde(default, rename = "steamId")]
    steam_id: String,
    #[serde(default, rename = "bmId")]
    bm_id: String,
}

#[derive(Deserialize)]
struct AddMembersReq {
    #[serde(default)]
    members: Vec<MemberPayload>,
}

pub async fn add_track_group_members(
    State(st): Shared,
    headers: HeaderMap,
    Path((guild_id, group_id)): Path<(String, String)>,
    body: Bytes,
) -> Response {
    let id = match sub_prelude(&st, &headers, &guild_id, &group_id).await {
        Ok(id) => id,
        Err(r) => return r,
    };
    let req: AddMembersReq = match parse_json(&body) {
        Ok(r) => r,
        Err(resp) => return *resp,
    };
    let members: Vec<(String, String)> =
        req.members.into_iter().map(|m| (m.steam_id, m.bm_id)).collect();
    match tg::add_members(&st.pool, id, &members).await {
        Ok(added) => {
            let count = added.len();
            Json(json!({ "added": added, "count": count })).into_response()
        }
        Err(e) => {
            tracing::error!("guilds/track-groups add_members: {e}");
            err_json(StatusCode::INTERNAL_SERVER_ERROR, "failed to add members")
        }
    }
}

#[derive(Deserialize)]
struct RemoveMembersReq {
    #[serde(default)]
    identifiers: Vec<String>,
}

pub async fn remove_track_group_members(
    State(st): Shared,
    headers: HeaderMap,
    Path((guild_id, group_id)): Path<(String, String)>,
    body: Bytes,
) -> Response {
    let id = match sub_prelude(&st, &headers, &guild_id, &group_id).await {
        Ok(id) => id,
        Err(r) => return r,
    };
    let req: RemoveMembersReq = match parse_json(&body) {
        Ok(r) => r,
        Err(resp) => return *resp,
    };
    match tg::remove_members(&st.pool, id, &req.identifiers).await {
        Ok(count) => Json(json!({ "count": count })).into_response(),
        Err(e) => {
            tracing::error!("guilds/track-groups remove_members: {e}");
            err_json(StatusCode::INTERNAL_SERVER_ERROR, "failed to remove members")
        }
    }
}

pub async fn list_track_group_online_state(
    State(st): Shared,
    headers: HeaderMap,
    Path((guild_id, group_id)): Path<(String, String)>,
) -> Response {
    let id = match sub_prelude(&st, &headers, &guild_id, &group_id).await {
        Ok(id) => id,
        Err(r) => return r,
    };
    match tg::list_online_state(&st.pool, id).await {
        Ok(items) => Json(json!({ "items": items })).into_response(),
        Err(e) => {
            tracing::error!("guilds/track-groups online-state: {e}");
            err_json(StatusCode::INTERNAL_SERVER_ERROR, "failed to load online state")
        }
    }
}

#[derive(Deserialize)]
struct ClaimReq {
    #[serde(default, rename = "steamId")]
    steam_id: String,
    #[serde(default, rename = "isOnline")]
    is_online: bool,
    #[serde(default, rename = "onlineUnix")]
    online_unix: i64,
    #[serde(default, rename = "workerId")]
    worker_id: String,
}

/// SetNX the per-transition claim key (10-minute TTL). No Redis → single-pod, so
/// there is no cross-pod duplication to guard against: grant the claim.
async fn try_claim_notification(
    st: &AppState,
    group_id: i64,
    steam_id: &str,
    is_online: bool,
    online_unix: i64,
    worker_id: &str,
) -> Result<bool, String> {
    let Some(conn) = &st.redis else {
        return Ok(true);
    };
    let mut conn = conn.clone();
    let state = if is_online { "online" } else { "offline" };
    let key = format!("rustlens:track:notification:claim:{group_id}:{steam_id}:{state}:{online_unix}");
    let res: redis::RedisResult<Option<String>> = redis::cmd("SET")
        .arg(&key)
        .arg(worker_id)
        .arg("NX")
        .arg("PX")
        .arg(600_000u64)
        .query_async(&mut conn)
        .await;
    match res {
        Ok(Some(_)) => Ok(true),
        Ok(None) => Ok(false),
        Err(e) => Err(e.to_string()),
    }
}

pub async fn claim_track_group_notification(
    State(st): Shared,
    headers: HeaderMap,
    Path((guild_id, group_id)): Path<(String, String)>,
    body: Bytes,
) -> Response {
    let id = match sub_prelude(&st, &headers, &guild_id, &group_id).await {
        Ok(id) => id,
        Err(r) => return r,
    };
    let req: ClaimReq = match parse_json(&body) {
        Ok(r) => r,
        Err(resp) => return *resp,
    };
    let steam_id = req.steam_id.trim();
    let worker_id = req.worker_id.trim();
    if !is_steam_id(steam_id) {
        return err_json(StatusCode::BAD_REQUEST, "steamId must be a 17-digit Steam ID");
    }
    if req.online_unix < 0 {
        return err_json(StatusCode::BAD_REQUEST, "onlineUnix must be non-negative");
    }
    if worker_id.is_empty() {
        return err_json(StatusCode::BAD_REQUEST, "workerId is required");
    }
    match try_claim_notification(&st, id, steam_id, req.is_online, req.online_unix, worker_id).await {
        Ok(claimed) => Json(json!({ "claimed": claimed })).into_response(),
        Err(e) => {
            tracing::error!("guilds/track-groups claim: {e}");
            err_json(StatusCode::INTERNAL_SERVER_ERROR, "failed to claim track notification")
        }
    }
}
