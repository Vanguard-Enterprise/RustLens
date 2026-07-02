//! App-facing player routes (`/api/app/players/*`), session-authed + rate-limited.
//! Mirrors the Go `SearchPlayers` / `GetPlayerProfileApp`.

use std::sync::Arc;

use axum::Json;
use axum::extract::{Path, RawQuery, State};
use axum::http::{HeaderMap, StatusCode};
use axum::response::{IntoResponse, Response};
use serde::Serialize;
use serde_json::json;

use rustlens_db::admin::has_any_permission;
use rustlens_db::players_search::{search_players_by_name, PlayerSearchHit};
use rustlens_db::players_steam as store;
use rustlens_steam::Resolver;

use crate::auth::err_json;
use crate::classifier::{self, QueryKind, SteamUrl};
use crate::state::AppState;
use crate::{ratelimit, steam};

type Shared = State<Arc<AppState>>;

const SEARCH_MAX: i64 = 30;
const PROFILE_MAX: i64 = 60;
const WINDOW_MS: i64 = 60_000;

#[derive(Serialize)]
struct ServerInfo {
    #[serde(rename = "bmId")]
    bm_id: String,
    name: String,
}

#[derive(Serialize)]
struct Hit {
    #[serde(rename = "steamId")]
    steam_id: String,
    #[serde(rename = "bmId")]
    bm_id: Option<String>,
    #[serde(rename = "bmName")]
    bm_name: Option<String>,
    #[serde(rename = "avatarUrl")]
    avatar_url: Option<String>,
    #[serde(rename = "currentServer")]
    current_server: Option<ServerInfo>,
    #[serde(rename = "isOnline")]
    is_online: bool,
    #[serde(rename = "lastSeenAt")]
    last_seen_at: Option<String>,
}

enum ResolveIdError {
    NotFound,
    Other(String),
}

/// app-secret + session-header gate; returns the acting user id.
async fn authed(headers: &HeaderMap, st: &AppState) -> Result<String, Response> {
    rustlens_httpauth::require_app_secret(headers, &st.config.app_api_internal_secret)
        .map_err(|b| *b)?;
    let session =
        rustlens_httpauth::require_session(headers, &st.pool, &st.config.app_session_secret)
            .await
            .map_err(|b| *b)?;
    Ok(session.user_id)
}

fn query_params(raw: &Option<String>) -> (String, bool) {
    let mut q = String::new();
    let mut include_offline = false;
    for (k, v) in url::form_urlencoded::parse(raw.as_deref().unwrap_or("").as_bytes()) {
        match k.as_ref() {
            "q" => q = v.to_string(),
            "includeOffline" => include_offline = v == "true",
            _ => {}
        }
    }
    (q, include_offline)
}

async fn resolve_vanity(st: &AppState, vanity: &str) -> Result<String, ResolveIdError> {
    match st.steam.resolve_vanity_url(vanity).await {
        Ok(Some(id)) => Ok(id),
        Ok(None) => Err(ResolveIdError::NotFound),
        Err(e) => Err(ResolveIdError::Other(e.to_string())),
    }
}

/// Resolve any identifier (Steam ID / URL / vanity / BM ID) to a Steam64.
async fn resolve_to_steam_id(st: &AppState, identifier: &str) -> Result<String, ResolveIdError> {
    let q = identifier.trim();
    match classifier::classify(q) {
        QueryKind::Empty | QueryKind::Name => Err(ResolveIdError::NotFound),
        QueryKind::SteamId => Ok(q.to_string()),
        QueryKind::SteamUrl => match classifier::parse_steam_url(q) {
            Some(SteamUrl::Profiles(id)) => Ok(id),
            Some(SteamUrl::Vanity(v)) => resolve_vanity(st, &v).await,
            None => Err(ResolveIdError::NotFound),
        },
        QueryKind::SteamVanity => resolve_vanity(st, q).await,
        QueryKind::BmId => {
            let Some(bm) = classifier::extract_bm_id(q) else {
                return Err(ResolveIdError::NotFound);
            };
            match store::get_steam_account_by_bm_id(&st.pool, &bm).await {
                Ok(Some(a)) => Ok(a.steam_id),
                Ok(None) => match st.resolver.bm_to_steam(&bm).await {
                    Ok(s) if !s.trim().is_empty() => Ok(s.trim().to_string()),
                    _ => Err(ResolveIdError::NotFound),
                },
                Err(e) => Err(ResolveIdError::Other(e.to_string())),
            }
        }
    }
}

async fn name_search_response(st: &AppState, q: &str, include_offline: bool) -> Response {
    match search_players_by_name(&st.pool, q.trim(), include_offline).await {
        Ok(rows) => Json(json!({ "items": to_hits(rows), "mode": "name" })).into_response(),
        Err(e) => {
            tracing::error!("players/app name search: {e}");
            err_json(StatusCode::INTERNAL_SERVER_ERROR, "lookup failed")
        }
    }
}

fn to_hits(rows: Vec<PlayerSearchHit>) -> Vec<Hit> {
    rows.into_iter()
        .map(|r| Hit {
            steam_id: r.steam_id,
            bm_id: r.bm_id,
            bm_name: r.bm_name,
            avatar_url: r.avatar_image,
            current_server: match (r.current_server_bm, r.current_server) {
                (Some(b), Some(n)) => Some(ServerInfo { bm_id: b, name: n }),
                _ => None,
            },
            is_online: r.is_online,
            last_seen_at: r.last_seen_at,
        })
        .collect()
}

/// Single-identifier hit (Go `lookupSingleHit`): `None` when the account isn't
/// stored. Does not set `lastSeenAt`.
async fn lookup_single_hit(st: &AppState, steam_id: &str) -> Option<Hit> {
    let account = store::get_steam_account_by_steam_id(&st.pool, steam_id)
        .await
        .ok()
        .flatten()?;
    let mut hit = Hit {
        steam_id: account.steam_id.clone(),
        bm_id: account.bm_id.clone(),
        bm_name: None,
        avatar_url: None,
        current_server: None,
        is_online: false,
        last_seen_at: None,
    };
    if let Ok(Some(cache)) = store::get_steam_account_cache(&st.pool, steam_id).await {
        if !cache.avatar_image.is_empty() {
            hit.avatar_url = Some(cache.avatar_image);
        }
    }
    if let Some(bm_id) = &account.bm_id {
        if let Ok(Some(bc)) = store::get_bm_profile_cache(&st.pool, bm_id).await {
            if !bc.bm_name.is_empty() {
                hit.bm_name = Some(bc.bm_name);
            }
        }
        if let Ok(states) = store::list_player_states_by_player(&st.pool, bm_id).await {
            if let Some(s) = states.into_iter().find(|s| s.is_online) {
                hit.current_server = Some(ServerInfo {
                    bm_id: s.server_bm_id,
                    name: s.server_name,
                });
                hit.is_online = true;
            }
        }
    }
    Some(hit)
}

pub async fn search_players(State(st): Shared, headers: HeaderMap, RawQuery(raw): RawQuery) -> Response {
    let user_id = match authed(&headers, &st).await {
        Ok(u) => u,
        Err(resp) => return resp,
    };
    let (q, include_offline) = query_params(&raw);
    if q.trim().len() < 2 {
        return err_json(StatusCode::BAD_REQUEST, "q must be at least 2 characters");
    }
    if !ratelimit::allow(&st.redis, &user_id, "search", SEARCH_MAX, WINDOW_MS).await {
        return err_json(StatusCode::TOO_MANY_REQUESTS, "rate limited");
    }

    let kind = classifier::classify(&q);
    let is_admin = match has_any_permission(&st.pool, &user_id).await {
        Ok(b) => b,
        Err(e) => {
            tracing::error!("players/app permission check: {e}");
            return err_json(StatusCode::INTERNAL_SERVER_ERROR, "permission_check_failed");
        }
    };

    if kind == QueryKind::Name {
        if !is_admin {
            return err_json(StatusCode::FORBIDDEN, "name search requires admin");
        }
        return name_search_response(&st, &q, include_offline).await;
    }

    match resolve_to_steam_id(&st, &q).await {
        Ok(steam_id) => {
            let items: Vec<Hit> = lookup_single_hit(&st, &steam_id).await.into_iter().collect();
            Json(json!({ "mode": "identifier", "items": items })).into_response()
        }
        Err(ResolveIdError::NotFound) => {
            if kind == QueryKind::SteamVanity && is_admin {
                name_search_response(&st, &q, include_offline).await
            } else {
                Json(json!({ "items": [], "mode": "identifier" })).into_response()
            }
        }
        Err(ResolveIdError::Other(e)) => {
            tracing::error!("players/app resolve: {e}");
            err_json(StatusCode::INTERNAL_SERVER_ERROR, "lookup failed")
        }
    }
}

pub async fn get_player_profile_app(
    State(st): Shared,
    headers: HeaderMap,
    Path(identifier): Path<String>,
) -> Response {
    let user_id = match authed(&headers, &st).await {
        Ok(u) => u,
        Err(resp) => return resp,
    };
    if !ratelimit::allow(&st.redis, &user_id, "profile", PROFILE_MAX, WINDOW_MS).await {
        return err_json(StatusCode::TOO_MANY_REQUESTS, "rate limited");
    }
    match resolve_to_steam_id(&st, &identifier).await {
        Ok(steam_id) => steam::profile_for(&st, &steam_id).await,
        Err(ResolveIdError::NotFound) => err_json(StatusCode::NOT_FOUND, "player not found"),
        Err(ResolveIdError::Other(e)) => {
            tracing::error!("players/app profile resolve: {e}");
            err_json(StatusCode::INTERNAL_SERVER_ERROR, "lookup failed")
        }
    }
}
