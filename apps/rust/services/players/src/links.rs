//! Steam↔Discord link CRUD (global + guild-scoped) + link-visibility, all
//! bot-JWT-gated under `/api/internal/bot/setup/*`. Mirrors the Go handlers.

use std::sync::Arc;

use axum::Json;
use axum::body::Bytes;
use axum::extract::{Path, State};
use axum::http::{HeaderMap, StatusCode};
use axum::response::{IntoResponse, Response};
use serde::Deserialize;
use serde_json::json;

use rustlens_db::players_links::{self as links, BulkHardLink, LinkError};

use crate::auth::{err_json, is_snowflake, is_steam64, parse_json, require_bot_jwt};
use crate::state::AppState;

type Shared = State<Arc<AppState>>;

macro_rules! jwt_guard {
    ($headers:expr, $st:expr) => {
        if let Some(resp) = require_bot_jwt(&$headers, &$st.config) {
            return resp;
        }
    };
}

fn db_error(context: &str, e: impl std::fmt::Display) -> Response {
    tracing::error!("players/links {context}: {e}");
    err_json(StatusCode::INTERNAL_SERVER_ERROR, "internal error")
}

// ---- global links ----

#[derive(Deserialize)]
struct CreateSoftLinkReq {
    #[serde(rename = "steamId", default)]
    steam_id: String,
    #[serde(rename = "discordId", default)]
    discord_id: String,
    #[serde(default)]
    source: String,
}

pub async fn create_soft_link(State(st): Shared, headers: HeaderMap, body: Bytes) -> Response {
    jwt_guard!(headers, st);
    let req: CreateSoftLinkReq = match parse_json(&body) {
        Ok(r) => r,
        Err(resp) => return *resp,
    };
    if !is_steam64(&req.steam_id) {
        return err_json(StatusCode::BAD_REQUEST, "steamId must be a 17-digit number");
    }
    if !is_snowflake(&req.discord_id) {
        return err_json(StatusCode::BAD_REQUEST, "discordId must be a valid snowflake");
    }
    let source = req.source.trim();
    if source.is_empty() {
        return err_json(StatusCode::BAD_REQUEST, "source is required");
    }
    match links::create_soft_link(&st.pool, &req.steam_id, &req.discord_id, source).await {
        Ok(link) => (StatusCode::CREATED, Json(link)).into_response(),
        Err(LinkError::AlreadyExists) => err_json(
            StatusCode::CONFLICT,
            "soft link already exists for this steam and discord pair",
        ),
        Err(LinkError::Db(e)) => db_error("create_soft_link", e),
        Err(_) => err_json(StatusCode::INTERNAL_SERVER_ERROR, "internal error"),
    }
}

#[derive(Deserialize)]
struct BulkHardLinksReq {
    #[serde(default)]
    links: Vec<BulkHardLinkReq>,
}

#[derive(Deserialize)]
struct BulkHardLinkReq {
    #[serde(rename = "steamId", default)]
    steam_id: String,
    #[serde(rename = "bmId", default)]
    bm_id: Option<String>,
    #[serde(rename = "discordId", default)]
    discord_id: String,
    #[serde(default)]
    source: String,
}

pub async fn bulk_upsert_hard_links(State(st): Shared, headers: HeaderMap, body: Bytes) -> Response {
    jwt_guard!(headers, st);
    let req: BulkHardLinksReq = match parse_json(&body) {
        Ok(r) => r,
        Err(resp) => return *resp,
    };
    if req.links.is_empty() {
        return err_json(StatusCode::BAD_REQUEST, "links must be non-empty");
    }
    if req.links.len() > 1000 {
        return err_json(StatusCode::BAD_REQUEST, "links must not exceed 1000 entries");
    }
    let mut out = Vec::with_capacity(req.links.len());
    for l in &req.links {
        if !is_steam64(&l.steam_id) {
            return err_json(StatusCode::BAD_REQUEST, "steamId must be a 17-digit number");
        }
        if !is_snowflake(&l.discord_id) {
            return err_json(StatusCode::BAD_REQUEST, "discordId must be a valid snowflake");
        }
        if l.source.trim().is_empty() {
            return err_json(StatusCode::BAD_REQUEST, "source is required");
        }
        out.push(BulkHardLink {
            steam_id: l.steam_id.clone(),
            bm_id: l.bm_id.as_ref().map(|s| s.trim().to_string()).filter(|s| !s.is_empty()),
            discord_id: l.discord_id.clone(),
            source: l.source.trim().to_string(),
        });
    }
    match links::bulk_upsert_hard_links(&st.pool, &out).await {
        Ok(count) => Json(json!({ "status": "ok", "count": count })).into_response(),
        Err(e) => db_error("bulk_upsert_hard_links", e),
    }
}

pub async fn list_links_by_steam(
    State(st): Shared,
    headers: HeaderMap,
    Path(steam_id): Path<String>,
) -> Response {
    jwt_guard!(headers, st);
    if !is_steam64(&steam_id) {
        return err_json(StatusCode::BAD_REQUEST, "steamID must be a 17-digit number");
    }
    match links::list_links_by_steam(&st.pool, &steam_id).await {
        Ok(items) => Json(json!({ "items": items })).into_response(),
        Err(e) => db_error("list_links_by_steam", e),
    }
}

pub async fn list_links_by_discord(
    State(st): Shared,
    headers: HeaderMap,
    Path(discord_id): Path<String>,
) -> Response {
    jwt_guard!(headers, st);
    if !is_snowflake(&discord_id) {
        return err_json(StatusCode::BAD_REQUEST, "discordID must be a valid snowflake");
    }
    match links::list_links_by_discord(&st.pool, &discord_id).await {
        Ok(items) => Json(json!({ "items": items })).into_response(),
        Err(e) => db_error("list_links_by_discord", e),
    }
}

pub async fn delete_soft_link(
    State(st): Shared,
    headers: HeaderMap,
    Path((steam_id, discord_id)): Path<(String, String)>,
) -> Response {
    jwt_guard!(headers, st);
    if !is_steam64(&steam_id) {
        return err_json(StatusCode::BAD_REQUEST, "steamID must be a 17-digit number");
    }
    if !is_snowflake(&discord_id) {
        return err_json(StatusCode::BAD_REQUEST, "discordID must be a valid snowflake");
    }
    match links::delete_soft_link(&st.pool, &steam_id, &discord_id).await {
        Ok(()) => Json(json!({ "status": "deleted" })).into_response(),
        Err(LinkError::NotFound) => err_json(StatusCode::NOT_FOUND, "soft link not found"),
        Err(LinkError::Db(e)) => db_error("delete_soft_link", e),
        Err(_) => err_json(StatusCode::INTERNAL_SERVER_ERROR, "internal error"),
    }
}

pub async fn list_setsteam_candidates(
    State(st): Shared,
    headers: HeaderMap,
    Path(discord_id): Path<String>,
) -> Response {
    jwt_guard!(headers, st);
    if !is_snowflake(&discord_id) {
        return err_json(StatusCode::BAD_REQUEST, "discordID must be a valid snowflake");
    }
    match links::list_setsteam_candidates(&st.pool, &discord_id).await {
        Ok(items) => Json(json!({ "items": items })).into_response(),
        Err(e) => db_error("list_setsteam_candidates", e),
    }
}

// ---- link visibility ----

#[derive(Deserialize, Default)]
struct HideReq {
    #[serde(default)]
    reason: String,
    #[serde(rename = "createdBy", default)]
    created_by: String,
}

pub async fn hide_discord_link_identity(
    State(st): Shared,
    headers: HeaderMap,
    Path(discord_id): Path<String>,
    body: Bytes,
) -> Response {
    jwt_guard!(headers, st);
    if !is_snowflake(&discord_id) {
        return err_json(StatusCode::BAD_REQUEST, "discordID must be a valid snowflake");
    }
    // Body is optional; treat an empty/missing body as defaults.
    let req: HideReq = if body.is_empty() {
        HideReq::default()
    } else {
        match parse_json(&body) {
            Ok(r) => r,
            Err(resp) => return *resp,
        }
    };
    match links::hide_discord_link_identity(
        &st.pool,
        &discord_id,
        req.reason.trim(),
        req.created_by.trim(),
    )
    .await
    {
        Ok(row) => Json(row).into_response(),
        Err(e) => db_error("hide_discord_link_identity", e),
    }
}

pub async fn unhide_discord_link_identity(
    State(st): Shared,
    headers: HeaderMap,
    Path(discord_id): Path<String>,
) -> Response {
    jwt_guard!(headers, st);
    if !is_snowflake(&discord_id) {
        return err_json(StatusCode::BAD_REQUEST, "discordID must be a valid snowflake");
    }
    match links::unhide_discord_link_identity(&st.pool, &discord_id).await {
        Ok(()) => Json(json!({ "status": "deleted" })).into_response(),
        Err(e) => db_error("unhide_discord_link_identity", e),
    }
}

// ---- guild-scoped links ----

#[derive(Deserialize)]
struct CreateGuildLinkReq {
    #[serde(rename = "discordId", default)]
    discord_id: String,
    #[serde(rename = "steamId", default)]
    steam_id: String,
}

pub async fn create_guild_link(
    State(st): Shared,
    headers: HeaderMap,
    Path(guild_id): Path<String>,
    body: Bytes,
) -> Response {
    jwt_guard!(headers, st);
    if !is_snowflake(&guild_id) {
        return err_json(StatusCode::BAD_REQUEST, "guildID must be a valid snowflake");
    }
    let req: CreateGuildLinkReq = match parse_json(&body) {
        Ok(r) => r,
        Err(resp) => return *resp,
    };
    if !is_snowflake(&req.discord_id) {
        return err_json(StatusCode::BAD_REQUEST, "discordId must be a valid snowflake");
    }
    if !is_steam64(&req.steam_id) {
        return err_json(StatusCode::BAD_REQUEST, "steamId must be a 17-digit number");
    }
    match links::create_guild_link(&st.pool, &guild_id, &req.discord_id, &req.steam_id).await {
        Ok(link) => (StatusCode::CREATED, Json(link)).into_response(),
        Err(LinkError::AlreadyExists) => {
            err_json(StatusCode::CONFLICT, "guild link already exists")
        }
        Err(LinkError::Db(e)) => db_error("create_guild_link", e),
        Err(_) => err_json(StatusCode::INTERNAL_SERVER_ERROR, "internal error"),
    }
}

pub async fn list_guild_links(
    State(st): Shared,
    headers: HeaderMap,
    Path(guild_id): Path<String>,
) -> Response {
    jwt_guard!(headers, st);
    if !is_snowflake(&guild_id) {
        return err_json(StatusCode::BAD_REQUEST, "guildID must be a valid snowflake");
    }
    match links::list_guild_links(&st.pool, &guild_id).await {
        Ok(items) => Json(json!({ "items": items })).into_response(),
        Err(e) => db_error("list_guild_links", e),
    }
}

pub async fn list_guild_links_by_discord(
    State(st): Shared,
    headers: HeaderMap,
    Path((guild_id, discord_id)): Path<(String, String)>,
) -> Response {
    jwt_guard!(headers, st);
    if !is_snowflake(&guild_id) || !is_snowflake(&discord_id) {
        return err_json(StatusCode::BAD_REQUEST, "guildID and discordID must be valid snowflakes");
    }
    match links::list_guild_links_by_discord(&st.pool, &guild_id, &discord_id).await {
        Ok(items) => Json(json!({ "items": items })).into_response(),
        Err(e) => db_error("list_guild_links_by_discord", e),
    }
}

pub async fn list_guild_links_by_steam(
    State(st): Shared,
    headers: HeaderMap,
    Path((guild_id, steam_id)): Path<(String, String)>,
) -> Response {
    jwt_guard!(headers, st);
    if !is_snowflake(&guild_id) {
        return err_json(StatusCode::BAD_REQUEST, "guildID must be a valid snowflake");
    }
    if !is_steam64(&steam_id) {
        return err_json(StatusCode::BAD_REQUEST, "steamID must be a 17-digit number");
    }
    match links::list_guild_links_by_steam(&st.pool, &guild_id, &steam_id).await {
        Ok(items) => Json(json!({ "items": items })).into_response(),
        Err(e) => db_error("list_guild_links_by_steam", e),
    }
}

pub async fn delete_guild_link(
    State(st): Shared,
    headers: HeaderMap,
    Path((guild_id, discord_id, steam_id)): Path<(String, String, String)>,
) -> Response {
    jwt_guard!(headers, st);
    if !is_snowflake(&guild_id) || !is_snowflake(&discord_id) {
        return err_json(StatusCode::BAD_REQUEST, "guildID and discordID must be valid snowflakes");
    }
    if !is_steam64(&steam_id) {
        return err_json(StatusCode::BAD_REQUEST, "steamID must be a 17-digit number");
    }
    match links::delete_guild_link(&st.pool, &guild_id, &discord_id, &steam_id).await {
        Ok(()) => Json(json!({ "status": "deleted" })).into_response(),
        Err(LinkError::GuildNotFound) => err_json(StatusCode::NOT_FOUND, "guild link not found"),
        Err(LinkError::Db(e)) => db_error("delete_guild_link", e),
        Err(_) => err_json(StatusCode::INTERNAL_SERVER_ERROR, "internal error"),
    }
}

pub async fn list_guild_nickname_targets(
    State(st): Shared,
    headers: HeaderMap,
    Path(guild_id): Path<String>,
) -> Response {
    jwt_guard!(headers, st);
    if !is_snowflake(&guild_id) {
        return err_json(StatusCode::BAD_REQUEST, "guildID must be a valid snowflake");
    }
    match links::list_guild_nickname_targets(&st.pool, &guild_id).await {
        Ok(items) => Json(json!({ "items": items })).into_response(),
        Err(e) => db_error("list_guild_nickname_targets", e),
    }
}
