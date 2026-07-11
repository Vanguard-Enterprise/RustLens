//! Wipe-reacts (bot-JWT): list/active/create/get/update/delete + reaction
//! set/remove. Mirrors the Go handlers. Reaction mutations are transactional
//! with `FOR UPDATE` in the store.

use std::sync::Arc;

use axum::Json;
use axum::body::Bytes;
use axum::extract::{Path, State};
use axum::http::{HeaderMap, StatusCode};
use axum::response::{IntoResponse, Response};
use serde::Deserialize;
use serde_json::json;

use rustlens_db::wipe_reacts::{self as wr, WipeReact, WipeReactError};

use crate::auth::{
    err_json, is_snowflake_value, parse_json, require_bot_jwt, validate_guild_id,
    validate_snowflake_path,
};
use crate::state::AppState;

type Shared = State<Arc<AppState>>;

/// Current Unix seconds. Isolated so the (rare) pre-1970 clock error is a 500,
/// matching Go's `time.Now().Unix()` always succeeding.
fn now_unix() -> i64 {
    use std::time::{SystemTime, UNIX_EPOCH};
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_secs() as i64)
        .unwrap_or(0)
}

fn map_wr_err(context: &str, e: WipeReactError, not_found_ok: bool, msg_500: &str) -> Response {
    match e {
        WipeReactError::NotFound if not_found_ok => {
            err_json(StatusCode::NOT_FOUND, "wipe react not found")
        }
        other => {
            tracing::error!("guilds/wipe-reacts {context}: {other}");
            err_json(StatusCode::INTERNAL_SERVER_ERROR, msg_500)
        }
    }
}

#[derive(Deserialize)]
struct WipeReactPayload {
    #[serde(default, rename = "channelId")]
    channel_id: String,
    #[serde(default, rename = "messageId")]
    message_id: String,
    #[serde(default, rename = "creatorId")]
    creator_id: String,
    #[serde(default)]
    name: String,
    #[serde(default)]
    description: String,
    #[serde(default, rename = "timeZone")]
    time_zone: String,
    #[serde(default, rename = "scheduledUnix")]
    scheduled_unix: i64,
    #[serde(default, rename = "askLateReason")]
    ask_late_reason: bool,
    #[serde(default, rename = "askDeclinedReason")]
    ask_declined_reason: bool,
    #[serde(default, rename = "acceptedUserIds")]
    accepted_user_ids: Option<Vec<String>>,
    #[serde(default, rename = "declinedUserIds")]
    declined_user_ids: Option<Vec<String>>,
    #[serde(default, rename = "lateUserIds")]
    late_user_ids: Option<Vec<String>>,
    #[serde(default, rename = "embedTimestamp")]
    embed_timestamp: String,
}

/// `validateWipeReactPayload` — first failure wins (all 400).
fn validate_payload(p: &WipeReactPayload) -> Option<Response> {
    let bad = |m: &str| Some(err_json(StatusCode::BAD_REQUEST, m));
    if !is_snowflake_value(&p.channel_id) {
        return bad("channelId must be a valid Discord snowflake");
    }
    if !is_snowflake_value(&p.message_id) {
        return bad("messageId must be a valid Discord snowflake");
    }
    if !is_snowflake_value(&p.creator_id) {
        return bad("creatorId must be a valid Discord snowflake");
    }
    if p.name.is_empty() {
        return bad("name is required");
    }
    if p.time_zone.is_empty() {
        return bad("timeZone is required");
    }
    if p.scheduled_unix <= 0 {
        return bad("scheduledUnix must be greater than zero");
    }
    if p.embed_timestamp.is_empty() {
        return bad("embedTimestamp is required");
    }
    let lists = [&p.accepted_user_ids, &p.declined_user_ids, &p.late_user_ids];
    let total: usize = lists.iter().map(|o| o.as_ref().map_or(0, Vec::len)).sum();
    if total > 10000 {
        return bad("reaction user id lists exceed maximum allowed size");
    }
    if lists
        .iter()
        .filter_map(|o| o.as_ref())
        .flatten()
        .any(|id| !is_snowflake_value(id))
    {
        return bad("reaction user ids must be valid Discord snowflakes");
    }
    None
}

fn payload_to_wipe_react(guild_id: String, p: WipeReactPayload) -> WipeReact {
    WipeReact {
        guild_id,
        channel_id: p.channel_id,
        message_id: p.message_id,
        creator_id: p.creator_id,
        name: p.name,
        description: p.description,
        time_zone: p.time_zone,
        scheduled_unix: p.scheduled_unix,
        ask_late_reason: p.ask_late_reason,
        ask_declined_reason: p.ask_declined_reason,
        accepted_user_ids: p.accepted_user_ids,
        declined_user_ids: p.declined_user_ids,
        late_user_ids: p.late_user_ids,
        embed_timestamp: p.embed_timestamp,
    }
}

pub async fn list_active_wipe_reacts(
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
    match wr::list_active(&st.pool, &guild_id, now_unix()).await {
        Ok(items) => Json(json!({ "items": items })).into_response(),
        Err(e) => {
            tracing::error!("guilds/wipe-reacts list_active: {e}");
            err_json(StatusCode::INTERNAL_SERVER_ERROR, "failed to list active wipe reacts")
        }
    }
}

pub async fn list_wipe_reacts(
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
    match wr::list_all(&st.pool, &guild_id).await {
        Ok(items) => Json(json!({ "items": items })).into_response(),
        Err(e) => {
            tracing::error!("guilds/wipe-reacts list: {e}");
            err_json(StatusCode::INTERNAL_SERVER_ERROR, "failed to list wipe reacts")
        }
    }
}

pub async fn create_wipe_react(
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
    let payload: WipeReactPayload = match parse_json(&body) {
        Ok(p) => p,
        Err(resp) => return *resp,
    };
    if let Some(r) = validate_payload(&payload) {
        return r;
    }
    let record = payload_to_wipe_react(guild_id, payload);
    match wr::create(&st.pool, &record).await {
        // Go returns 200 and echoes the input record (no DB re-read).
        Ok(()) => Json(record).into_response(),
        Err(e) => {
            tracing::error!("guilds/wipe-reacts create: {e}");
            err_json(StatusCode::INTERNAL_SERVER_ERROR, "failed to create wipe react")
        }
    }
}

pub async fn get_wipe_react(
    State(st): Shared,
    headers: HeaderMap,
    Path((guild_id, message_id)): Path<(String, String)>,
) -> Response {
    if let Some(r) = require_bot_jwt(&headers, &st.config) {
        return r;
    }
    if let Some(r) = validate_guild_id(&guild_id) {
        return r;
    }
    if let Some(r) = validate_snowflake_path("messageID", &message_id) {
        return r;
    }
    match wr::get(&st.pool, &guild_id, &message_id).await {
        Ok(record) => Json(record).into_response(),
        Err(e) => map_wr_err("get", e, true, "failed to fetch wipe react"),
    }
}

pub async fn update_wipe_react(
    State(st): Shared,
    headers: HeaderMap,
    Path((guild_id, message_id)): Path<(String, String)>,
    body: Bytes,
) -> Response {
    if let Some(r) = require_bot_jwt(&headers, &st.config) {
        return r;
    }
    if let Some(r) = validate_guild_id(&guild_id) {
        return r;
    }
    let mut payload: WipeReactPayload = match parse_json(&body) {
        Ok(p) => p,
        Err(resp) => return *resp,
    };
    // Path wins over body before validation (mirrors Go).
    payload.message_id = message_id;
    if let Some(r) = validate_payload(&payload) {
        return r;
    }
    let record = payload_to_wipe_react(guild_id, payload);
    match wr::update(&st.pool, &record).await {
        Ok(fresh) => Json(fresh).into_response(),
        Err(e) => map_wr_err("update", e, true, "failed to update wipe react"),
    }
}

pub async fn delete_wipe_react(
    State(st): Shared,
    headers: HeaderMap,
    Path((guild_id, message_id)): Path<(String, String)>,
) -> Response {
    if let Some(r) = require_bot_jwt(&headers, &st.config) {
        return r;
    }
    if let Some(r) = validate_guild_id(&guild_id) {
        return r;
    }
    if let Some(r) = validate_snowflake_path("messageID", &message_id) {
        return r;
    }
    match wr::delete(&st.pool, &guild_id, &message_id).await {
        Ok(()) => Json(json!({ "status": "deleted" })).into_response(),
        Err(e) => map_wr_err("delete", e, true, "failed to delete wipe react"),
    }
}

#[derive(Deserialize)]
struct SetReactionReq {
    #[serde(default, rename = "userId")]
    user_id: String,
    #[serde(default, rename = "type")]
    reaction_type: String,
}

pub async fn set_wipe_react_reaction(
    State(st): Shared,
    headers: HeaderMap,
    Path((guild_id, message_id)): Path<(String, String)>,
    body: Bytes,
) -> Response {
    if let Some(r) = require_bot_jwt(&headers, &st.config) {
        return r;
    }
    if let Some(r) = validate_guild_id(&guild_id) {
        return r;
    }
    if let Some(r) = validate_snowflake_path("messageID", &message_id) {
        return r;
    }
    let req: SetReactionReq = match parse_json(&body) {
        Ok(r) => r,
        Err(resp) => return *resp,
    };
    if !is_snowflake_value(&req.user_id) {
        return err_json(StatusCode::BAD_REQUEST, "userId must be a valid Discord snowflake");
    }
    if !matches!(req.reaction_type.as_str(), "accepted" | "declined" | "late") {
        return err_json(StatusCode::BAD_REQUEST, "type must be one of accepted, declined, or late");
    }
    match wr::set_reaction(&st.pool, &guild_id, &message_id, &req.user_id, &req.reaction_type).await {
        Ok(o) => Json(json!({
            "changed": o.changed,
            "wipeReact": o.wipe_react,
            "previousType": o.prior_type,
        }))
        .into_response(),
        Err(e) => map_wr_err("set_reaction", e, true, "failed to update wipe react reaction"),
    }
}

pub async fn remove_wipe_react_reaction(
    State(st): Shared,
    headers: HeaderMap,
    Path((guild_id, message_id, user_id)): Path<(String, String, String)>,
) -> Response {
    if let Some(r) = require_bot_jwt(&headers, &st.config) {
        return r;
    }
    if let Some(r) = validate_guild_id(&guild_id) {
        return r;
    }
    if let Some(r) = validate_snowflake_path("messageID", &message_id) {
        return r;
    }
    if let Some(r) = validate_snowflake_path("userID", &user_id) {
        return r;
    }
    match wr::remove_reaction(&st.pool, &guild_id, &message_id, &user_id).await {
        Ok(o) => Json(json!({
            "changed": o.changed,
            "wipeReact": o.wipe_react,
            "removedType": o.prior_type,
        }))
        .into_response(),
        Err(e) => map_wr_err("remove_reaction", e, true, "failed to remove wipe react reaction"),
    }
}
