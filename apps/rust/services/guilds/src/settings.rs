//! Guild settings: GET/PUT `/api/internal/bot/setup/guilds/{guildID}/settings`
//! (bot-JWT). Mirrors `GetGuildSettings`/`UpdateGuildSettings`.

use std::sync::Arc;

use axum::Json;
use axum::body::Bytes;
use axum::extract::{Path, State};
use axum::http::{HeaderMap, StatusCode};
use axum::response::{IntoResponse, Response};
use serde::Deserialize;

use rustlens_db::guild_settings::{self as gs, GuildSettingsPatch};

use crate::auth::{err_json, is_snowflake, parse_json, require_bot_jwt, validate_guild_id};
use crate::state::AppState;

type Shared = State<Arc<AppState>>;

pub async fn get_guild_settings(
    State(st): Shared,
    headers: HeaderMap,
    Path(guild_id): Path<String>,
) -> Response {
    if let Some(resp) = require_bot_jwt(&headers, &st.config) {
        return resp;
    }
    if let Some(resp) = validate_guild_id(&guild_id) {
        return resp;
    }
    match gs::get(&st.pool, &guild_id).await {
        Ok(settings) => Json(settings).into_response(),
        Err(e) => {
            tracing::error!("guilds/settings get: {e}");
            err_json(StatusCode::INTERNAL_SERVER_ERROR, "failed to fetch guild settings")
        }
    }
}

#[derive(Deserialize)]
struct UpdateReq {
    #[serde(default, rename = "wipeReactLogChannelId")]
    wipe_react_log_channel_id: Option<String>,
    #[serde(default, rename = "linkingLogChannelId")]
    linking_log_channel_id: Option<String>,
    #[serde(default, rename = "trackedServerLogChannelId")]
    tracked_server_log_channel_id: Option<String>,
    #[serde(default, rename = "trackedUsersCategoryId")]
    tracked_users_category_id: Option<String>,
    #[serde(default, rename = "adminRoleId")]
    admin_role_id: Option<String>,
    #[serde(default, rename = "alertRoleId")]
    alert_role_id: Option<String>,
    #[serde(default)]
    limited: Option<bool>,
}

/// `nil` → OK (preserve); `Some("")` → OK (clear); `Some(v)` must be a snowflake.
fn validate_optional_snowflake(value: &Option<String>, field: &str) -> Option<Response> {
    match value {
        None => None,
        Some(s) if s.is_empty() => None,
        Some(s) if is_snowflake(s) => None,
        Some(_) => Some(err_json(
            StatusCode::BAD_REQUEST,
            &format!("{field} must be a valid Discord snowflake"),
        )),
    }
}

pub async fn update_guild_settings(
    State(st): Shared,
    headers: HeaderMap,
    Path(guild_id): Path<String>,
    body: Bytes,
) -> Response {
    if let Some(resp) = require_bot_jwt(&headers, &st.config) {
        return resp;
    }
    if let Some(resp) = validate_guild_id(&guild_id) {
        return resp;
    }
    let req: UpdateReq = match parse_json(&body) {
        Ok(r) => r,
        Err(resp) => return *resp,
    };
    for (value, field) in [
        (&req.wipe_react_log_channel_id, "wipeReactLogChannelId"),
        (&req.linking_log_channel_id, "linkingLogChannelId"),
        (&req.tracked_server_log_channel_id, "trackedServerLogChannelId"),
        (&req.tracked_users_category_id, "trackedUsersCategoryId"),
        (&req.admin_role_id, "adminRoleId"),
        (&req.alert_role_id, "alertRoleId"),
    ] {
        if let Some(resp) = validate_optional_snowflake(value, field) {
            return resp;
        }
    }
    let patch = GuildSettingsPatch {
        wipe_react_log_channel_id: req.wipe_react_log_channel_id,
        linking_log_channel_id: req.linking_log_channel_id,
        tracked_server_log_channel_id: req.tracked_server_log_channel_id,
        tracked_users_category_id: req.tracked_users_category_id,
        admin_role_id: req.admin_role_id,
        alert_role_id: req.alert_role_id,
        limited: req.limited,
    };
    match gs::update(&st.pool, &guild_id, &patch).await {
        Ok(settings) => Json(settings).into_response(),
        Err(e) => {
            tracing::error!("guilds/settings update: {e}");
            err_json(StatusCode::INTERNAL_SERVER_ERROR, "failed to save guild settings")
        }
    }
}
