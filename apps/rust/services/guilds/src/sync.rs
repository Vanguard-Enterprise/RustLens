//! Guild/user/install sync (bot-JWT): user-guilds create/leave/list, bot-guilds
//! upsert/leave/reconcile, bot-user-installs seen. Mirrors the Go handlers.

use std::sync::Arc;

use axum::Json;
use axum::body::Bytes;
use axum::extract::{Path, State};
use axum::http::{HeaderMap, StatusCode};
use axum::response::{IntoResponse, Response};
use chrono::SecondsFormat;
use serde::Deserialize;
use serde_json::json;

use rustlens_db::bot_guilds::{self, BotGuildMetadata};
use rustlens_db::bot_user_installs::{self, SeenInput};
use rustlens_db::user_guilds;

use crate::auth::{
    err_json, is_snowflake_value, parse_json, require_bot_jwt, validate_snowflake_path,
};
use crate::state::AppState;

type Shared = State<Arc<AppState>>;

const MAX_MEMBERS_PER_UPSERT_BATCH: usize = 1000;

fn status_ok() -> Response {
    Json(json!({ "status": "ok" })).into_response()
}

// ---- user-guilds ----

#[derive(Deserialize)]
struct UserGuildReq {
    #[serde(default, rename = "discordId")]
    discord_id: String,
    #[serde(default, rename = "guildId")]
    guild_id: String,
}

fn validate_user_guild_req(req: &UserGuildReq) -> Option<Response> {
    if !is_snowflake_value(&req.discord_id) {
        return Some(err_json(StatusCode::BAD_REQUEST, "discordId must be a valid Discord snowflake"));
    }
    if !is_snowflake_value(&req.guild_id) {
        return Some(err_json(StatusCode::BAD_REQUEST, "guildId must be a valid Discord snowflake"));
    }
    None
}

pub async fn create_user_guild_link(State(st): Shared, headers: HeaderMap, body: Bytes) -> Response {
    if let Some(resp) = require_bot_jwt(&headers, &st.config) {
        return resp;
    }
    let req: UserGuildReq = match parse_json(&body) {
        Ok(r) => r,
        Err(resp) => return *resp,
    };
    if let Some(resp) = validate_user_guild_req(&req) {
        return resp;
    }
    match user_guilds::create(&st.pool, &req.discord_id, &req.guild_id).await {
        Ok(link) => (StatusCode::CREATED, Json(link)).into_response(),
        Err(e) => {
            tracing::error!("guilds/user-guilds create: {e}");
            err_json(StatusCode::INTERNAL_SERVER_ERROR, "failed to create user guild link")
        }
    }
}

pub async fn mark_user_guild_left(State(st): Shared, headers: HeaderMap, body: Bytes) -> Response {
    if let Some(resp) = require_bot_jwt(&headers, &st.config) {
        return resp;
    }
    let req: UserGuildReq = match parse_json(&body) {
        Ok(r) => r,
        Err(resp) => return *resp,
    };
    if let Some(resp) = validate_user_guild_req(&req) {
        return resp;
    }
    match user_guilds::mark_left(&st.pool, &req.discord_id, &req.guild_id).await {
        Ok(link) => Json(link).into_response(),
        Err(e) => {
            tracing::error!("guilds/user-guilds leave: {e}");
            err_json(StatusCode::INTERNAL_SERVER_ERROR, "failed to update user guild link")
        }
    }
}

pub async fn list_user_guild_links_by_discord(
    State(st): Shared,
    headers: HeaderMap,
    Path(discord_id): Path<String>,
) -> Response {
    if let Some(resp) = require_bot_jwt(&headers, &st.config) {
        return resp;
    }
    if let Some(resp) = validate_snowflake_path("discordID", &discord_id) {
        return resp;
    }
    match user_guilds::list_by_discord(&st.pool, &discord_id).await {
        Ok(items) => Json(json!({ "items": items })).into_response(),
        Err(e) => {
            tracing::error!("guilds/user-guilds list: {e}");
            err_json(StatusCode::INTERNAL_SERVER_ERROR, "failed to fetch user guild links")
        }
    }
}

// ---- bot-guilds ----

#[derive(Deserialize)]
struct UpsertBotGuildReq {
    #[serde(default, rename = "guildId")]
    guild_id: String,
    #[serde(default)]
    name: String,
    #[serde(default, rename = "iconHash")]
    icon_hash: Option<String>,
    #[serde(default, rename = "bannerHash")]
    banner_hash: Option<String>,
    #[serde(default, rename = "ownerDiscordId")]
    owner_discord_id: Option<String>,
    #[serde(default, rename = "ownerUsername")]
    owner_username: Option<String>,
    #[serde(default, rename = "ownerGlobalName")]
    owner_global_name: Option<String>,
    #[serde(default, rename = "ownerAvatarHash")]
    owner_avatar_hash: Option<String>,
    #[serde(default, rename = "approximateMemberCount")]
    approximate_member_count: Option<i32>,
    #[serde(default)]
    members: Vec<String>,
    #[serde(default, rename = "membersSyncComplete")]
    members_sync_complete: bool,
}

pub async fn upsert_bot_guild(State(st): Shared, headers: HeaderMap, body: Bytes) -> Response {
    if let Some(resp) = require_bot_jwt(&headers, &st.config) {
        return resp;
    }
    let req: UpsertBotGuildReq = match parse_json(&body) {
        Ok(r) => r,
        Err(resp) => return *resp,
    };
    if !is_snowflake_value(&req.guild_id) {
        return err_json(StatusCode::BAD_REQUEST, "guildId must be a valid Discord snowflake");
    }
    if req.members.len() > MAX_MEMBERS_PER_UPSERT_BATCH {
        return err_json(StatusCode::BAD_REQUEST, "members batch exceeds maximum size");
    }
    if req.members.iter().any(|m| !is_snowflake_value(m)) {
        return err_json(StatusCode::BAD_REQUEST, "members must contain valid Discord snowflakes");
    }

    let meta = BotGuildMetadata {
        guild_id: req.guild_id.clone(),
        name: req.name,
        icon_hash: req.icon_hash,
        banner_hash: req.banner_hash,
        owner_discord_id: req.owner_discord_id,
        owner_username: req.owner_username,
        owner_global_name: req.owner_global_name,
        owner_avatar_hash: req.owner_avatar_hash,
        approximate_member_count: req.approximate_member_count,
    };
    if let Err(e) = bot_guilds::upsert_metadata(&st.pool, &meta).await {
        tracing::error!("guilds/bot-guilds upsert metadata: {e}");
        return err_json(StatusCode::INTERNAL_SERVER_ERROR, "failed to upsert bot guild");
    }
    if !req.members.is_empty() {
        if let Err(e) = user_guilds::bulk_upsert(&st.pool, &req.guild_id, &req.members).await {
            tracing::error!("guilds/bot-guilds upsert members: {e}");
            return err_json(StatusCode::INTERNAL_SERVER_ERROR, "failed to upsert members");
        }
    }
    if req.members_sync_complete {
        if let Err(e) = bot_guilds::stamp_members_synced(&st.pool, &req.guild_id).await {
            tracing::error!("guilds/bot-guilds stamp members_synced: {e}");
            return err_json(StatusCode::INTERNAL_SERVER_ERROR, "failed to stamp members_synced_at");
        }
    }
    let synced_at = match bot_guilds::members_synced_at(&st.pool, &req.guild_id).await {
        Ok(v) => v,
        Err(e) => {
            tracing::error!("guilds/bot-guilds read members_synced: {e}");
            return err_json(StatusCode::INTERNAL_SERVER_ERROR, "failed to read members_synced_at");
        }
    };
    // Plain RFC3339 (seconds), not RFC3339Nano — matches Go's `.Format(time.RFC3339)`.
    let synced_json = match synced_at {
        Some(ts) => json!(ts.to_rfc3339_opts(SecondsFormat::Secs, true)),
        None => json!(null),
    };
    Json(json!({ "status": "ok", "membersSyncedAt": synced_json })).into_response()
}

#[derive(Deserialize)]
struct GuildIdReq {
    #[serde(default, rename = "guildId")]
    guild_id: String,
}

pub async fn mark_bot_guild_left(State(st): Shared, headers: HeaderMap, body: Bytes) -> Response {
    if let Some(resp) = require_bot_jwt(&headers, &st.config) {
        return resp;
    }
    let req: GuildIdReq = match parse_json(&body) {
        Ok(r) => r,
        Err(resp) => return *resp,
    };
    if !is_snowflake_value(&req.guild_id) {
        return err_json(StatusCode::BAD_REQUEST, "guildId must be a valid Discord snowflake");
    }
    match bot_guilds::mark_left(&st.pool, &req.guild_id).await {
        Ok(()) => status_ok(),
        Err(e) => {
            tracing::error!("guilds/bot-guilds leave: {e}");
            err_json(StatusCode::INTERNAL_SERVER_ERROR, "failed to mark guild left")
        }
    }
}

#[derive(Deserialize)]
struct ReconcileReq {
    #[serde(default, rename = "shardId")]
    shard_id: i64,
    #[serde(default, rename = "shardCount")]
    shard_count: i64,
    #[serde(default, rename = "guildIds")]
    guild_ids: Vec<String>,
}

pub async fn reconcile_bot_guilds(State(st): Shared, headers: HeaderMap, body: Bytes) -> Response {
    if let Some(resp) = require_bot_jwt(&headers, &st.config) {
        return resp;
    }
    let req: ReconcileReq = match parse_json(&body) {
        Ok(r) => r,
        Err(resp) => return *resp,
    };
    if req.shard_count <= 0 || req.shard_id < 0 || req.shard_id >= req.shard_count {
        return err_json(StatusCode::BAD_REQUEST, "invalid shardId/shardCount");
    }
    if req.guild_ids.iter().any(|g| !is_snowflake_value(g)) {
        return err_json(StatusCode::BAD_REQUEST, "guildIds must contain valid Discord snowflakes");
    }
    match bot_guilds::reconcile(&st.pool, req.shard_id, req.shard_count, &req.guild_ids).await {
        Ok(()) => status_ok(),
        Err(e) => {
            tracing::error!("guilds/bot-guilds reconcile: {e}");
            err_json(StatusCode::INTERNAL_SERVER_ERROR, "failed to reconcile bot guilds")
        }
    }
}

// ---- bot-user-installs ----

#[derive(Deserialize)]
struct SeenReq {
    #[serde(default, rename = "discordId")]
    discord_id: String,
    #[serde(default)]
    username: String,
    #[serde(default, rename = "globalName")]
    global_name: Option<String>,
    #[serde(default, rename = "avatarHash")]
    avatar_hash: Option<String>,
}

pub async fn touch_bot_user_install_seen(State(st): Shared, headers: HeaderMap, body: Bytes) -> Response {
    if let Some(resp) = require_bot_jwt(&headers, &st.config) {
        return resp;
    }
    let req: SeenReq = match parse_json(&body) {
        Ok(r) => r,
        Err(resp) => return *resp,
    };
    if !is_snowflake_value(&req.discord_id) {
        return err_json(StatusCode::BAD_REQUEST, "discordId must be a valid Discord snowflake");
    }
    let input = SeenInput {
        discord_id: req.discord_id,
        username: req.username,
        global_name: req.global_name,
        avatar_hash: req.avatar_hash,
    };
    match bot_user_installs::touch_seen(&st.pool, &input).await {
        Ok(()) => status_ok(),
        Err(e) => {
            tracing::error!("guilds/bot-user-installs seen: {e}");
            err_json(StatusCode::INTERNAL_SERVER_ERROR, "failed to touch user install")
        }
    }
}
