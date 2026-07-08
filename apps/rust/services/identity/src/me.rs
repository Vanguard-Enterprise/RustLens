//! `GET /api/app/me/connections`, `/me/guilds`, `/me/guilds/installed`, and
//! `/me/guilds/{guildID}/links`, session-authed identity aggregate routes.
//! Mirrors the Go handlers in `apps/backend/internal/http/app_handlers.go`
//! (`GetCurrentAppUserConnections`, `GetCurrentAppUserGuilds`,
//! `GetCurrentAppUserInstalledGuilds`, `GetCurrentAppUserGuildLinks`).
//!
//! All four routes sit behind the same `/api/app/*` gate as `handlers::me`:
//! `X-RustLens-App-Secret` + the forwarded `X-RustLens-Session` header. After
//! that gate, each handler re-loads the acting `app_users` row (mirroring the
//! Go `requireAppActor` helper): a missing row is `401 invalid_session`, a
//! query error is `500 failed to load app user`; only then does the
//! route-specific store call run under its own error message, matching Go's
//! two-step structure exactly.

use std::sync::Arc;

use axum::Json;
use axum::extract::{Path, State};
use axum::http::{HeaderMap, StatusCode};
use axum::response::{IntoResponse, Response};
use serde::Serialize;
use serde_json::json;

use rustlens_db::identity::{self, AppUser, ConnectedAccount, LinkedSteamAccount, UserGuildMembership};

use crate::bot::is_snowflake;
use crate::state::AppState;

type Shared = State<Arc<AppState>>;

fn error_json(status: StatusCode, msg: &str) -> Response {
    (status, Json(json!({ "error": msg }))).into_response()
}

/// Shared prologue: app-secret gate, session gate, then re-load the acting
/// `app_users` row. Returns `Err(Response)` on any failure (already the right
/// shape to return from the caller).
async fn require_actor(headers: &HeaderMap, st: &AppState) -> Result<AppUser, Response> {
    if let Err(resp) = crate::appauth::require_app_secret(headers, &st.config.app_api_internal_secret) {
        return Err(*resp);
    }
    let session = match crate::appauth::require_session(headers, st).await {
        Ok(s) => s,
        Err(resp) => return Err(*resp),
    };
    match identity::load_app_user(&st.pool, &session.user_id).await {
        Ok(Some(user)) => Ok(user),
        Ok(None) => Err(error_json(StatusCode::UNAUTHORIZED, "invalid_session")),
        Err(e) => {
            tracing::error!("me: load app user failed: {e:#}");
            Err(error_json(StatusCode::INTERNAL_SERVER_ERROR, "failed to load app user"))
        }
    }
}

#[derive(Serialize)]
struct ConnectionsResponse {
    #[serde(rename = "steamAccounts")]
    steam_accounts: Vec<LinkedSteamAccount>,
    #[serde(rename = "discordAccount", skip_serializing_if = "Option::is_none")]
    discord_account: Option<ConnectedAccount>,
}

/// `GET /api/app/me/connections`.
pub async fn connections(State(st): Shared, headers: HeaderMap) -> Response {
    let actor = match require_actor(&headers, &st).await {
        Ok(a) => a,
        Err(resp) => return resp,
    };

    if actor.discord_id.is_empty() {
        return Json(ConnectionsResponse {
            steam_accounts: Vec::new(),
            discord_account: None,
        })
        .into_response();
    }

    let steam_accounts = match identity::load_linked_steam_accounts(&st.pool, &actor.discord_id).await {
        Ok(v) => v,
        Err(e) => {
            tracing::error!("/api/app/me/connections: load steam accounts failed: {e:#}");
            return error_json(StatusCode::INTERNAL_SERVER_ERROR, "failed to load connections");
        }
    };

    // Raw column, not `actor.avatar_url`: that field carries the Discord CDN
    // fallback synthesis `load_app_user` applies for `GET /api/app/me`, which
    // Go's `ConnectedAccount.AvatarURL` (built from the plain store row) does
    // not have.
    let avatar_url = match identity::get_raw_avatar_url(&st.pool, &actor.id).await {
        Ok(v) => v,
        Err(e) => {
            tracing::error!("/api/app/me/connections: load avatar url failed: {e:#}");
            return error_json(StatusCode::INTERNAL_SERVER_ERROR, "failed to load connections");
        }
    };

    let display_name = actor
        .discord_global_name
        .clone()
        .filter(|n| !n.is_empty())
        .unwrap_or_else(|| actor.display_name.clone());
    let discord_account = ConnectedAccount {
        provider: "discord".to_string(),
        provider_account_id: actor.discord_id.clone(),
        username: actor.discord_username.clone(),
        display_name,
        avatar_url,
        profile_email: String::new(),
        created_at: actor.created_at,
        updated_at: actor.updated_at,
    };

    Json(ConnectionsResponse {
        steam_accounts,
        discord_account: Some(discord_account),
    })
    .into_response()
}

#[derive(Serialize)]
struct ItemsResponse<T: Serialize> {
    items: Vec<T>,
}

/// `GET /api/app/me/guilds`.
pub async fn guilds(State(st): Shared, headers: HeaderMap) -> Response {
    let actor = match require_actor(&headers, &st).await {
        Ok(a) => a,
        Err(resp) => return resp,
    };

    let items: Vec<UserGuildMembership> = if actor.discord_id.is_empty() {
        Vec::new()
    } else {
        match identity::load_user_guilds(&st.pool, &actor.discord_id).await {
            Ok(v) => v,
            Err(e) => {
                tracing::error!("/api/app/me/guilds: load guilds failed: {e:#}");
                return error_json(StatusCode::INTERNAL_SERVER_ERROR, "failed to load guilds");
            }
        }
    };

    Json(ItemsResponse { items }).into_response()
}

#[derive(Serialize)]
struct InstalledGuildOwner {
    id: String,
    username: String,
    #[serde(rename = "globalName")]
    global_name: Option<String>,
    #[serde(rename = "avatarHash")]
    avatar_hash: Option<String>,
}

#[derive(Serialize)]
struct InstalledGuildItem {
    id: String,
    name: String,
    #[serde(rename = "iconHash")]
    icon_hash: Option<String>,
    #[serde(rename = "bannerHash")]
    banner_hash: Option<String>,
    #[serde(rename = "approximateMemberCount")]
    approximate_member_count: Option<i32>,
    #[serde(rename = "trackedServerCount")]
    tracked_server_count: i32,
    owner: Option<InstalledGuildOwner>,
}

/// `GET /api/app/me/guilds/installed`.
pub async fn guilds_installed(State(st): Shared, headers: HeaderMap) -> Response {
    let actor = match require_actor(&headers, &st).await {
        Ok(a) => a,
        Err(resp) => return resp,
    };

    if actor.discord_id.is_empty() {
        return Json(ItemsResponse::<InstalledGuildItem> { items: Vec::new() }).into_response();
    }

    let rows = match identity::list_installed_guilds_for_user(&st.pool, &actor.discord_id).await {
        Ok(v) => v,
        Err(e) => {
            tracing::error!("/api/app/me/guilds/installed: list installed guilds failed: {e:#}");
            return error_json(StatusCode::INTERNAL_SERVER_ERROR, "failed to load installed guilds");
        }
    };

    let items: Vec<InstalledGuildItem> = rows
        .into_iter()
        .map(|r| InstalledGuildItem {
            id: r.guild_id,
            name: r.name,
            icon_hash: r.icon_hash,
            banner_hash: r.banner_hash,
            approximate_member_count: r.approximate_member_count,
            tracked_server_count: r.tracked_server_count,
            owner: r.owner_discord_id.map(|id| InstalledGuildOwner {
                id,
                username: r.owner_username.unwrap_or_default(),
                global_name: r.owner_global_name,
                avatar_hash: r.owner_avatar_hash,
            }),
        })
        .collect();

    Json(ItemsResponse { items }).into_response()
}

#[derive(Serialize)]
struct GuildLinkItem {
    #[serde(rename = "steamId")]
    steam_id: String,
    #[serde(rename = "linkedAt")]
    linked_at: String,
}

/// Format like Go's `row.CreatedAt.UTC().Format(time.RFC3339)`: whole seconds
/// only, `Z` suffix, no fractional digits regardless of stored precision.
fn format_rfc3339_seconds(dt: &chrono::DateTime<chrono::Utc>) -> String {
    dt.format("%Y-%m-%dT%H:%M:%SZ").to_string()
}

/// `GET /api/app/me/guilds/{guildID}/links`, the caller's Discord<->Steam
/// links scoped to a single guild. Membership in the guild is required.
pub async fn guild_links(State(st): Shared, headers: HeaderMap, Path(guild_id): Path<String>) -> Response {
    let actor = match require_actor(&headers, &st).await {
        Ok(a) => a,
        Err(resp) => return resp,
    };

    if !is_snowflake(&guild_id) {
        return error_json(StatusCode::BAD_REQUEST, "guildID must be a valid Discord snowflake");
    }

    if actor.discord_id.is_empty() {
        return error_json(StatusCode::FORBIDDEN, "not a member of this guild");
    }

    match identity::is_guild_member(&st.pool, &guild_id, &actor.discord_id).await {
        Ok(true) => {}
        Ok(false) => return error_json(StatusCode::FORBIDDEN, "not a member of this guild"),
        Err(e) => {
            tracing::error!("/api/app/me/guilds/{{guildID}}/links: is_guild_member failed: {e:#}");
            return error_json(StatusCode::INTERNAL_SERVER_ERROR, "failed to load guild links");
        }
    }

    let rows = match rustlens_db::players_links::list_guild_links_by_discord(&st.pool, &guild_id, &actor.discord_id).await {
        Ok(v) => v,
        Err(e) => {
            tracing::error!("/api/app/me/guilds/{{guildID}}/links: list guild links failed: {e:#}");
            return error_json(StatusCode::INTERNAL_SERVER_ERROR, "failed to load guild links");
        }
    };

    let items: Vec<GuildLinkItem> = rows
        .into_iter()
        .map(|r| GuildLinkItem {
            steam_id: r.steam_id,
            linked_at: format_rfc3339_seconds(&r.created_at),
        })
        .collect();

    Json(ItemsResponse { items }).into_response()
}
