//! Wipe-react schedules + the schedule-post queue (bot-JWT). Mirrors the Go
//! handlers. The `{scheduleID}` path param on complete/release is the POST id.

use std::sync::Arc;

use axum::Json;
use axum::body::Bytes;
use axum::extract::{Path, State};
use axum::http::{HeaderMap, StatusCode};
use axum::response::{IntoResponse, Response};
use chrono::Utc;
use serde::Deserialize;
use serde_json::json;

use rustlens_db::wipe_react_schedule_posts::{self as posts, SchedulePostError};
use rustlens_db::wipe_react_schedules::{self as sched, ScheduleError, ScheduleInput};

use crate::auth::{err_json, is_snowflake_value, parse_json, require_bot_jwt, validate_guild_id};
use crate::state::AppState;

type Shared = State<Arc<AppState>>;

const VALID_DAYS: [&str; 7] = [
    "monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday",
];
const VALID_POST_BEFORE: [&str; 6] = ["1d", "2d", "3d", "4d", "1w", "2w"];
const VALID_REPEAT: [&str; 3] = ["weekly", "biweekly", "monthly"];

fn parse_schedule_id(raw: &str) -> Option<i64> {
    raw.trim().parse::<i64>().ok().filter(|&n| n > 0)
}

fn is_hhmm_format(s: &str) -> bool {
    let b = s.as_bytes();
    b.len() == 5
        && b[0].is_ascii_digit()
        && b[1].is_ascii_digit()
        && b[2] == b':'
        && b[3].is_ascii_digit()
        && b[4].is_ascii_digit()
}

fn map_sched_err(context: &str, e: ScheduleError, msg_500: &str) -> Response {
    match e {
        ScheduleError::NotFound => err_json(StatusCode::NOT_FOUND, "wipe react schedule not found"),
        ScheduleError::LimitReached => {
            err_json(StatusCode::CONFLICT, "wipe react schedule limit reached")
        }
        ScheduleError::Db(e) => {
            tracing::error!("guilds/schedules {context}: {e}");
            err_json(StatusCode::INTERNAL_SERVER_ERROR, msg_500)
        }
    }
}

#[derive(Deserialize)]
struct SchedulePayload {
    #[serde(default)]
    days: Vec<String>,
    #[serde(default, rename = "includesForceWipe")]
    includes_force_wipe: bool,
    #[serde(default)]
    time: String,
    #[serde(default, rename = "timeZone")]
    time_zone: String,
    #[serde(default)]
    repeat: String,
    #[serde(default, rename = "postBefore")]
    post_before: String,
    #[serde(default)]
    name: String,
    #[serde(default)]
    description: String,
    #[serde(default, rename = "askLateReason")]
    ask_late_reason: bool,
    #[serde(default, rename = "askDeclinedReason")]
    ask_declined_reason: bool,
    #[serde(default, rename = "channelId")]
    channel_id: String,
    #[serde(default, rename = "createdBy")]
    created_by: String,
}

/// `validateWipeReactSchedulePayload` — first failure wins (all 400).
fn validate_payload(p: &SchedulePayload) -> Option<Response> {
    let bad = |m: &str| Some(err_json(StatusCode::BAD_REQUEST, m));
    if p.days.iter().any(|d| !VALID_DAYS.contains(&d.as_str())) {
        return bad("each day must be one of monday, tuesday, wednesday, thursday, friday, saturday, sunday");
    }
    if p.time_zone.is_empty() {
        return bad("timeZone is required");
    }
    if !VALID_POST_BEFORE.contains(&p.post_before.as_str()) {
        return bad("postBefore must be one of 1d, 2d, 3d, 4d, 1w, 2w");
    }
    if !is_snowflake_value(&p.created_by) {
        return bad("createdBy must be a valid Discord snowflake");
    }
    if !p.includes_force_wipe && p.days.is_empty() {
        return bad("days must not be empty when includesForceWipe is false");
    }
    if !p.days.is_empty() {
        if !is_hhmm_format(&p.time) {
            return bad("time must match HH:MM format");
        }
        if !VALID_REPEAT.contains(&p.repeat.as_str()) {
            return bad("repeat must be one of weekly, biweekly, monthly");
        }
    }
    None
}

fn payload_to_input(p: SchedulePayload) -> ScheduleInput {
    ScheduleInput {
        days: p.days,
        includes_force_wipe: p.includes_force_wipe,
        time: p.time,
        time_zone: p.time_zone,
        repeat: p.repeat,
        post_before: p.post_before,
        name: p.name,
        description: p.description,
        ask_late_reason: p.ask_late_reason,
        ask_declined_reason: p.ask_declined_reason,
        channel_id: p.channel_id,
        created_by: p.created_by,
    }
}

pub async fn list_wipe_react_schedules(
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
    match sched::list(&st.pool, &guild_id).await {
        Ok(items) => Json(json!({ "items": items })).into_response(),
        Err(e) => {
            tracing::error!("guilds/schedules list: {e}");
            err_json(StatusCode::INTERNAL_SERVER_ERROR, "failed to list wipe react schedules")
        }
    }
}

pub async fn create_wipe_react_schedule(
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
    let payload: SchedulePayload = match parse_json(&body) {
        Ok(p) => p,
        Err(resp) => return *resp,
    };
    if let Some(r) = validate_payload(&payload) {
        return r;
    }
    match sched::create(&st.pool, &guild_id, &payload_to_input(payload)).await {
        Ok(record) => (StatusCode::CREATED, Json(record)).into_response(),
        Err(e) => map_sched_err("create", e, "failed to create wipe react schedule"),
    }
}

pub async fn get_wipe_react_schedule(
    State(st): Shared,
    headers: HeaderMap,
    Path((guild_id, schedule_id)): Path<(String, String)>,
) -> Response {
    if let Some(r) = require_bot_jwt(&headers, &st.config) {
        return r;
    }
    if let Some(r) = validate_guild_id(&guild_id) {
        return r;
    }
    let Some(id) = parse_schedule_id(&schedule_id) else {
        return err_json(StatusCode::BAD_REQUEST, "scheduleID must be a positive integer");
    };
    match sched::get(&st.pool, &guild_id, id).await {
        Ok(record) => Json(record).into_response(),
        Err(e) => map_sched_err("get", e, "failed to fetch wipe react schedule"),
    }
}

pub async fn update_wipe_react_schedule(
    State(st): Shared,
    headers: HeaderMap,
    Path((guild_id, schedule_id)): Path<(String, String)>,
    body: Bytes,
) -> Response {
    if let Some(r) = require_bot_jwt(&headers, &st.config) {
        return r;
    }
    if let Some(r) = validate_guild_id(&guild_id) {
        return r;
    }
    let Some(id) = parse_schedule_id(&schedule_id) else {
        return err_json(StatusCode::BAD_REQUEST, "scheduleID must be a positive integer");
    };
    let payload: SchedulePayload = match parse_json(&body) {
        Ok(p) => p,
        Err(resp) => return *resp,
    };
    if let Some(r) = validate_payload(&payload) {
        return r;
    }
    match sched::update(&st.pool, &guild_id, id, &payload_to_input(payload)).await {
        Ok(record) => Json(record).into_response(),
        Err(e) => map_sched_err("update", e, "failed to update wipe react schedule"),
    }
}

pub async fn delete_wipe_react_schedule(
    State(st): Shared,
    headers: HeaderMap,
    Path((guild_id, schedule_id)): Path<(String, String)>,
) -> Response {
    if let Some(r) = require_bot_jwt(&headers, &st.config) {
        return r;
    }
    if let Some(r) = validate_guild_id(&guild_id) {
        return r;
    }
    let Some(id) = parse_schedule_id(&schedule_id) else {
        return err_json(StatusCode::BAD_REQUEST, "scheduleID must be a positive integer");
    };
    match sched::delete(&st.pool, &guild_id, id).await {
        Ok(()) => Json(json!({ "status": "deleted" })).into_response(),
        Err(e) => map_sched_err("delete", e, "failed to delete wipe react schedule"),
    }
}

// ---- schedule-post queue ----

#[derive(Deserialize)]
struct WorkerReq {
    #[serde(default, rename = "workerId")]
    worker_id: String,
}

pub async fn claim_next_schedule_post(
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
    let req: WorkerReq = match parse_json(&body) {
        Ok(r) => r,
        Err(resp) => return *resp,
    };
    let worker = req.worker_id.trim();
    if worker.is_empty() {
        return err_json(StatusCode::BAD_REQUEST, "workerId is required");
    }
    let now = Utc::now();
    let stale_before = now - chrono::Duration::minutes(5);
    match posts::claim_next(&st.pool, &guild_id, worker, now.timestamp(), stale_before).await {
        Ok(post) => Json(json!({ "item": post })).into_response(),
        // No claimable row is a 200 with a null item, NOT a 404.
        Err(SchedulePostError::NotFound) => Json(json!({ "item": null })).into_response(),
        Err(SchedulePostError::Db(e)) => {
            tracing::error!("guilds/schedule-posts claim: {e}");
            err_json(StatusCode::INTERNAL_SERVER_ERROR, "failed to claim wipe react schedule post")
        }
    }
}

#[derive(Deserialize)]
struct CompleteReq {
    #[serde(default, rename = "workerId")]
    worker_id: String,
    #[serde(default, rename = "messageId")]
    message_id: String,
    #[serde(default, rename = "channelId")]
    channel_id: String,
}

fn map_post_err(context: &str, e: SchedulePostError, msg_500: &str) -> Response {
    match e {
        SchedulePostError::NotFound => {
            err_json(StatusCode::NOT_FOUND, "wipe react schedule post not found")
        }
        SchedulePostError::Db(e) => {
            tracing::error!("guilds/schedule-posts {context}: {e}");
            err_json(StatusCode::INTERNAL_SERVER_ERROR, msg_500)
        }
    }
}

pub async fn complete_schedule_post(
    State(st): Shared,
    headers: HeaderMap,
    Path((guild_id, post_id_raw)): Path<(String, String)>,
    body: Bytes,
) -> Response {
    if let Some(r) = require_bot_jwt(&headers, &st.config) {
        return r;
    }
    if let Some(r) = validate_guild_id(&guild_id) {
        return r;
    }
    let Some(post_id) = parse_schedule_id(&post_id_raw) else {
        return err_json(StatusCode::BAD_REQUEST, "scheduleID must be a positive integer");
    };
    let req: CompleteReq = match parse_json(&body) {
        Ok(r) => r,
        Err(resp) => return *resp,
    };
    if req.worker_id.trim().is_empty() {
        return err_json(StatusCode::BAD_REQUEST, "workerId is required");
    }
    if req.message_id.trim().is_empty() {
        return err_json(StatusCode::BAD_REQUEST, "messageId is required");
    }
    if req.channel_id.trim().is_empty() {
        return err_json(StatusCode::BAD_REQUEST, "channelId is required");
    }
    match posts::complete(
        &st.pool,
        &guild_id,
        post_id,
        req.worker_id.trim(),
        req.message_id.trim(),
        req.channel_id.trim(),
    )
    .await
    {
        Ok(()) => Json(json!({ "status": "ok" })).into_response(),
        Err(e) => map_post_err("complete", e, "failed to complete wipe react schedule post"),
    }
}

pub async fn release_schedule_post(
    State(st): Shared,
    headers: HeaderMap,
    Path((guild_id, post_id_raw)): Path<(String, String)>,
    body: Bytes,
) -> Response {
    if let Some(r) = require_bot_jwt(&headers, &st.config) {
        return r;
    }
    if let Some(r) = validate_guild_id(&guild_id) {
        return r;
    }
    let Some(post_id) = parse_schedule_id(&post_id_raw) else {
        return err_json(StatusCode::BAD_REQUEST, "scheduleID must be a positive integer");
    };
    let req: WorkerReq = match parse_json(&body) {
        Ok(r) => r,
        Err(resp) => return *resp,
    };
    if req.worker_id.trim().is_empty() {
        return err_json(StatusCode::BAD_REQUEST, "workerId is required");
    }
    match posts::release(&st.pool, &guild_id, post_id, req.worker_id.trim()).await {
        Ok(()) => Json(json!({ "status": "ok" })).into_response(),
        Err(e) => map_post_err("release", e, "failed to release wipe react schedule post"),
    }
}
