//! Bot-internal identity routes, authenticated with the bot service JWT and
//! dual-registered (D1 domain-prefix reorg) under both the legacy flat
//! `/api/internal/bot/setup/*` path and the new `/api/internal/bot/identity/*`
//! domain prefix (see `main::build_router`). Mirrors the Go bot-facing
//! handlers.

use std::sync::Arc;

use axum::Json;
use axum::body::Bytes;
use axum::extract::{Path, State};
use axum::http::{HeaderMap, StatusCode, header};
use axum::response::{IntoResponse, Response};
use serde::Deserialize;
use serde_json::json;

use rustlens_config::IdentityConfig;
use rustlens_db::bot::{self, CommandUsageInsert, CommandUsageTargetInsert};
use rustlens_db::identity;

use crate::state::AppState;

type Shared = State<Arc<AppState>>;

const MAX_TARGETS: usize = 500;
const MAX_ERROR_CODE_LEN: usize = 256;
const MAX_OPTIONS_BYTES: usize = 16 * 1024;
const MAX_DISPLAY_NAME_LEN: usize = 256;
const MAX_CHANNEL_NAME_LEN: usize = 100;
const MAX_CORRELATION_LEN: usize = 64;
const MAX_COMMAND_LEN: usize = 128;

const INTERACTION_TYPES: &[&str] = &["command", "button", "modal", "string_select"];
const OUTCOMES: &[&str] = &["success", "error", "banned", "denied"];
const CHANNEL_KINDS: &[&str] = &["guild", "dm", "group_dm", "unknown"];
const TARGET_KINDS: &[&str] = &["user", "role", "channel", "guild", "steam"];
const TARGET_RELATIONS: &[&str] = &["recipient", "scope", "subject"];
const BAN_CATEGORIES: &[&str] = &["setup", "linking", "lookup", "activity", "coordination", "other"];

fn err_json(status: StatusCode, msg: &str) -> Response {
    (status, Json(json!({ "error": msg }))).into_response()
}

/// Validate the bot JWT. Returns `Some(response)` on failure. Mirrors the Go
/// `botJWTMiddleware` (401 with the error string).
fn require_bot_jwt(headers: &HeaderMap, config: &IdentityConfig) -> Option<Response> {
    let auth = headers
        .get(header::AUTHORIZATION)
        .and_then(|v| v.to_str().ok())
        .unwrap_or("");
    let token = match rustlens_botjwt::extract_bearer(auth) {
        Ok(t) => t,
        Err(e) => return Some(err_json(StatusCode::UNAUTHORIZED, &e.to_string())),
    };
    match rustlens_botjwt::validate(
        token,
        &config.bot_service_jwt_secret,
        &config.bot_jwt_issuer,
        &config.bot_jwt_audience,
    ) {
        Ok(_) => None,
        Err(e) => Some(err_json(StatusCode::UNAUTHORIZED, &e.to_string())),
    }
}

pub(crate) fn is_snowflake(s: &str) -> bool {
    (17..=20).contains(&s.len()) && s.bytes().all(|b| b.is_ascii_digit())
}

fn is_steam64(s: &str) -> bool {
    s.len() == 17 && s.bytes().all(|b| b.is_ascii_digit())
}

fn valid_correlation(s: &Option<String>) -> bool {
    match s.as_deref() {
        None | Some("") => true,
        Some(v) => {
            v.len() <= MAX_CORRELATION_LEN
                && v.bytes()
                    .all(|b| b.is_ascii_alphanumeric() || b == b'-' || b == b'_')
        }
    }
}

/// Truncate to at most `max` chars (by char count, like Go's rune cap).
fn cap_chars(s: String, max: usize) -> String {
    if s.chars().count() > max {
        s.chars().take(max).collect()
    } else {
        s
    }
}

// ---------------------------------------------------------------------------
// POST /users/ensure
// ---------------------------------------------------------------------------

#[derive(Debug, Deserialize)]
struct EnsureAppUserRequest {
    #[serde(rename = "discordId", default)]
    discord_id: String,
    #[serde(default)]
    username: String,
    #[serde(rename = "globalName", default)]
    global_name: String,
    #[serde(rename = "avatarHash", default)]
    avatar_hash: String,
}

/// `POST /users/ensure`, idempotent. Inserts an
/// `app_users` row keyed on Discord ID if one does not exist, otherwise
/// refreshes cached profile fields and returns the existing row. Called by the
/// bot at the top of every interaction. Mirrors Go's `EnsureAppUser`.
pub async fn ensure_app_user(State(st): Shared, headers: HeaderMap, raw: Bytes) -> Response {
    if let Some(resp) = require_bot_jwt(&headers, &st.config) {
        return resp;
    }
    let req: EnsureAppUserRequest = match serde_json::from_slice(&raw) {
        Ok(r) => r,
        Err(_) => return err_json(StatusCode::BAD_REQUEST, "invalid request body"),
    };
    if !is_snowflake(&req.discord_id) {
        return err_json(StatusCode::BAD_REQUEST, "discordId must be a valid Discord snowflake");
    }

    let new_id = crate::discord::new_user_id();
    let profile = identity::DiscordProfile {
        id: req.discord_id.clone(),
        username: req.username,
        global_name: req.global_name,
        avatar_hash: req.avatar_hash,
    };
    match identity::find_or_create_by_discord_id_ensure(&st.pool, &new_id, &profile).await {
        Ok((user_id, created)) => {
            Json(json!({ "userId": user_id, "created": created })).into_response()
        }
        Err(e) => {
            tracing::error!("ensure_app_user: FindOrCreateByDiscordID error for {}: {e:#}", req.discord_id);
            err_json(StatusCode::INTERNAL_SERVER_ERROR, "failed to ensure user")
        }
    }
}

// ---------------------------------------------------------------------------
// POST /command-usage
// ---------------------------------------------------------------------------

#[derive(Debug, Deserialize)]
struct RecordTarget {
    #[serde(default)]
    kind: String,
    #[serde(default)]
    snowflake: String,
    #[serde(rename = "displayName", default)]
    display_name: Option<String>,
    #[serde(rename = "avatarHash", default)]
    avatar_hash: Option<String>,
    #[serde(default)]
    relation: String,
}

#[derive(Debug, Deserialize)]
struct RecordRequest {
    #[serde(rename = "discordId", default)]
    discord_id: String,
    #[serde(rename = "guildId", default)]
    guild_id: Option<String>,
    #[serde(rename = "channelId", default)]
    channel_id: Option<String>,
    #[serde(rename = "interactionType", default)]
    interaction_type: String,
    #[serde(default)]
    command: String,
    #[serde(default)]
    subcommand: Option<String>,
    #[serde(default)]
    target: Option<String>,
    #[serde(default)]
    options: serde_json::Value,
    #[serde(default)]
    outcome: String,
    #[serde(rename = "errorCode", default)]
    error_code: Option<String>,
    #[serde(rename = "latencyMs", default)]
    latency_ms: i32,
    #[serde(rename = "correlationId", default)]
    correlation_id: Option<String>,
    #[serde(rename = "parentCorrelationId", default)]
    parent_correlation_id: Option<String>,
    #[serde(rename = "channelKind", default)]
    channel_kind: Option<String>,
    #[serde(rename = "channelName", default)]
    channel_name: Option<String>,
    #[serde(default)]
    targets: Vec<RecordTarget>,
}

/// `POST /command-usage` — fire-and-forget interaction
/// logging. Returns 204 on success and on a missing app-user (dropped silently).
pub async fn record_command_usage(State(st): Shared, headers: HeaderMap, raw: Bytes) -> Response {
    if let Some(resp) = require_bot_jwt(&headers, &st.config) {
        return resp;
    }
    let mut req: RecordRequest = match serde_json::from_slice(&raw) {
        Ok(r) => r,
        Err(_) => return err_json(StatusCode::BAD_REQUEST, "invalid request body"),
    };

    if !is_snowflake(&req.discord_id) {
        return err_json(StatusCode::BAD_REQUEST, "discordId must be a valid Discord snowflake");
    }
    if req.guild_id.as_deref().is_some_and(|g| !g.is_empty() && !is_snowflake(g)) {
        return err_json(StatusCode::BAD_REQUEST, "guildId must be a valid Discord snowflake");
    }
    if req.channel_id.as_deref().is_some_and(|c| !c.is_empty() && !is_snowflake(c)) {
        return err_json(StatusCode::BAD_REQUEST, "channelId must be a valid Discord snowflake");
    }
    if !INTERACTION_TYPES.contains(&req.interaction_type.as_str()) {
        return err_json(StatusCode::BAD_REQUEST, "interactionType invalid");
    }
    if !OUTCOMES.contains(&req.outcome.as_str()) {
        return err_json(StatusCode::BAD_REQUEST, "outcome invalid");
    }
    if req.command.is_empty() || req.command.len() > MAX_COMMAND_LEN {
        return err_json(StatusCode::BAD_REQUEST, "command must be 1..128 chars");
    }
    if req.latency_ms < 0 {
        req.latency_ms = 0;
    }
    if serde_json::to_vec(&req.options).map(|v| v.len()).unwrap_or(0) > MAX_OPTIONS_BYTES {
        return err_json(StatusCode::BAD_REQUEST, "options payload too large");
    }
    req.error_code = req.error_code.map(|e| cap_chars(e, MAX_ERROR_CODE_LEN));
    if !valid_correlation(&req.correlation_id) {
        return err_json(StatusCode::BAD_REQUEST, "correlationId invalid");
    }
    if !valid_correlation(&req.parent_correlation_id) {
        return err_json(StatusCode::BAD_REQUEST, "parentCorrelationId invalid");
    }
    if let Some(k) = req.channel_kind.as_deref().filter(|s| !s.is_empty()) {
        if !CHANNEL_KINDS.contains(&k) {
            return err_json(StatusCode::BAD_REQUEST, "channelKind invalid");
        }
    }
    req.channel_name = req.channel_name.map(|n| cap_chars(n, MAX_CHANNEL_NAME_LEN));

    let app_user_id = match bot::find_by_discord_id(&st.pool, &req.discord_id).await {
        Ok(Some(id)) => id,
        Ok(None) => return StatusCode::NO_CONTENT.into_response(),
        Err(e) => {
            tracing::error!("record_command_usage: resolve user: {e:#}");
            return err_json(StatusCode::INTERNAL_SERVER_ERROR, "failed to resolve user");
        }
    };

    // Validate + convert targets; drop invalid entries silently, cap at 500.
    let mut targets = Vec::new();
    for t in req.targets.into_iter().take(MAX_TARGETS) {
        if !TARGET_KINDS.contains(&t.kind.as_str()) || !TARGET_RELATIONS.contains(&t.relation.as_str()) {
            continue;
        }
        let ok = if t.kind == "steam" {
            is_steam64(&t.snowflake)
        } else {
            is_snowflake(&t.snowflake)
        };
        if !ok {
            continue;
        }
        targets.push(CommandUsageTargetInsert {
            kind: t.kind,
            snowflake: t.snowflake,
            display_name: t.display_name.map(|d| cap_chars(d, MAX_DISPLAY_NAME_LEN)),
            avatar_hash: t.avatar_hash,
            relation: t.relation,
        });
    }

    let insert = CommandUsageInsert {
        app_user_id,
        discord_id: req.discord_id,
        guild_id: req.guild_id,
        channel_id: req.channel_id,
        interaction_type: req.interaction_type,
        command: req.command,
        subcommand: req.subcommand,
        target: req.target,
        options: req.options,
        outcome: req.outcome,
        error_code: req.error_code,
        latency_ms: req.latency_ms,
        correlation_id: req.correlation_id,
        parent_correlation_id: req.parent_correlation_id,
        channel_kind: req.channel_kind,
        channel_name: req.channel_name,
        targets,
    };
    match bot::insert_command_usage(&st.pool, &insert).await {
        Ok(()) => StatusCode::NO_CONTENT.into_response(),
        Err(e) => {
            tracing::error!("record_command_usage: insert: {e:#}");
            err_json(StatusCode::INTERNAL_SERVER_ERROR, "failed to record usage")
        }
    }
}

// ---------------------------------------------------------------------------
// bans / is-website-admin / has-pro-role
// ---------------------------------------------------------------------------

/// `GET /bans/{discordID}`.
pub async fn get_user_ban(State(st): Shared, headers: HeaderMap, Path(discord_id): Path<String>) -> Response {
    if let Some(resp) = require_bot_jwt(&headers, &st.config) {
        return resp;
    }
    if !is_snowflake(&discord_id) {
        return err_json(StatusCode::BAD_REQUEST, "discordID must be a valid Discord snowflake");
    }
    match bot::get_user_ban(&st.pool, &discord_id).await {
        Ok((categories, suspended)) => Json(json!({
            "discordId": discord_id, "categories": categories, "suspended": suspended
        }))
        .into_response(),
        Err(e) => {
            tracing::error!("get_user_ban: {e:#}");
            err_json(StatusCode::INTERNAL_SERVER_ERROR, "failed to fetch user ban")
        }
    }
}

#[derive(Debug, Deserialize)]
struct UpdateBanRequest {
    #[serde(default)]
    categories: Vec<String>,
    #[serde(rename = "updatedBy", default)]
    updated_by: String,
}

/// `PUT /bans/{discordID}`.
pub async fn update_user_ban(
    State(st): Shared,
    headers: HeaderMap,
    Path(discord_id): Path<String>,
    raw: Bytes,
) -> Response {
    if let Some(resp) = require_bot_jwt(&headers, &st.config) {
        return resp;
    }
    if !is_snowflake(&discord_id) {
        return err_json(StatusCode::BAD_REQUEST, "discordID must be a valid Discord snowflake");
    }
    let req: UpdateBanRequest = match serde_json::from_slice(&raw) {
        Ok(r) => r,
        Err(_) => return err_json(StatusCode::BAD_REQUEST, "invalid request body"),
    };
    let mut categories = Vec::new();
    for raw_cat in &req.categories {
        let cat = raw_cat.trim().to_lowercase();
        if cat.is_empty() {
            continue;
        }
        if !BAN_CATEGORIES.contains(&cat.as_str()) {
            return err_json(StatusCode::BAD_REQUEST, &format!("invalid ban category: {cat}"));
        }
        if !categories.contains(&cat) {
            categories.push(cat);
        }
    }
    match bot::upsert_user_ban(&st.pool, &discord_id, &categories, &req.updated_by).await {
        Ok(cats) => Json(json!({
            "discordId": discord_id, "categories": cats, "suspended": false
        }))
        .into_response(),
        Err(e) => {
            tracing::error!("update_user_ban: {e:#}");
            err_json(StatusCode::INTERNAL_SERVER_ERROR, "failed to update user ban")
        }
    }
}

/// `GET /discord/{discordID}/is-website-admin`.
pub async fn is_website_admin(State(st): Shared, headers: HeaderMap, Path(discord_id): Path<String>) -> Response {
    if let Some(resp) = require_bot_jwt(&headers, &st.config) {
        return resp;
    }
    if !is_snowflake(&discord_id) {
        return err_json(StatusCode::BAD_REQUEST, "discordID must be a valid Discord snowflake");
    }
    match bot::is_discord_website_admin(&st.pool, &discord_id).await {
        Ok(is_admin) => Json(json!({ "isAdmin": is_admin })).into_response(),
        Err(e) => {
            tracing::error!("is_website_admin: {e:#}");
            err_json(StatusCode::INTERNAL_SERVER_ERROR, "failed to check website admin status")
        }
    }
}

/// `GET /discord/{discordID}/has-pro-role`.
pub async fn has_pro_role(State(st): Shared, headers: HeaderMap, Path(discord_id): Path<String>) -> Response {
    if let Some(resp) = require_bot_jwt(&headers, &st.config) {
        return resp;
    }
    if !is_snowflake(&discord_id) {
        return err_json(StatusCode::BAD_REQUEST, "discordID must be a valid Discord snowflake");
    }
    match bot::discord_user_has_entitlement(&st.pool, &discord_id, "pro").await {
        Ok(has_role) => Json(json!({ "hasRole": has_role })).into_response(),
        Err(e) => {
            tracing::error!("has_pro_role: {e:#}");
            err_json(StatusCode::INTERNAL_SERVER_ERROR, "failed to check PRO role")
        }
    }
}
