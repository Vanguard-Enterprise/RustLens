//! HTTP handlers: Discord OAuth initiate/callback and `/api/app/me`.

use std::sync::Arc;

use axum::Json;
use axum::body::Bytes;
use axum::extract::{Query, State};
use axum::http::{HeaderMap, StatusCode, header};
use axum::response::{IntoResponse, Response};
use chrono::Utc;
use serde::Deserialize;

use rustlens_db::identity::{self, CurrentUser};
use rustlens_session::{SessionUser, serialize_session};

use crate::cookies::{
    self, SESSION_COOKIE, SESSION_MAX_AGE, SHORT_MAX_AGE, STATE_COOKIE, VERIFIER_COOKIE,
};
use crate::discord;
use crate::state::AppState;

type Shared = State<Arc<AppState>>;

/// `GET /api/auth/discord` — start the OAuth flow.
pub async fn initiate(State(st): Shared) -> Response {
    let verifier = discord::random_token_32();
    let state = discord::random_token_32();
    let challenge = discord::pkce_challenge(&verifier);
    let redirect_uri = st.config.discord_redirect_uri();
    let authorize = discord::authorize_url(
        &st.config.discord_client_id,
        &redirect_uri,
        &state,
        &challenge,
    );

    let mut headers = HeaderMap::new();
    cookies::append_set_cookie(
        &mut headers,
        &cookies::set_cookie(STATE_COOKIE, &state, SHORT_MAX_AGE),
    );
    cookies::append_set_cookie(
        &mut headers,
        &cookies::set_cookie(VERIFIER_COOKIE, &verifier, SHORT_MAX_AGE),
    );
    redirect_with_headers(&authorize, headers)
}

#[derive(Debug, Deserialize)]
pub struct CallbackQuery {
    code: Option<String>,
    state: Option<String>,
}

/// `GET /api/auth/discord/callback` — finish the OAuth flow, mint the session.
pub async fn callback(
    State(st): Shared,
    headers: HeaderMap,
    Query(q): Query<CallbackQuery>,
) -> Response {
    // Always clear the per-flow cookies on the way out, regardless of outcome.
    let mut out = HeaderMap::new();
    cookies::append_set_cookie(&mut out, &cookies::clear_cookie(STATE_COOKIE));
    cookies::append_set_cookie(&mut out, &cookies::clear_cookie(VERIFIER_COOKIE));

    let state_cookie = cookies::read_cookie(&headers, STATE_COOKIE);
    let verifier_cookie = cookies::read_cookie(&headers, VERIFIER_COOKIE);

    let (Some(code), Some(qstate), Some(state_cookie), Some(verifier)) = (
        q.code.as_deref().filter(|s| !s.is_empty()),
        q.state.as_deref().filter(|s| !s.is_empty()),
        state_cookie.as_deref(),
        verifier_cookie.as_deref(),
    ) else {
        return err_redirect(&st, "discord-state", out);
    };
    if qstate != state_cookie {
        return err_redirect(&st, "discord-state", out);
    }

    let redirect_uri = st.config.discord_redirect_uri();
    let access = match discord::exchange_code(
        &st.http,
        &st.config.discord_client_id,
        &st.config.discord_client_secret,
        code,
        verifier,
        &redirect_uri,
    )
    .await
    {
        Ok(t) => t,
        Err(e) => {
            tracing::warn!("discord callback: token exchange failed: {e:#}");
            return err_redirect(&st, "discord-token", out);
        }
    };

    let profile = match discord::fetch_profile(&st.http, &access).await {
        Ok(p) => p,
        Err(e) => {
            tracing::warn!("discord callback: profile fetch failed: {e:#}");
            return err_redirect(&st, "discord-profile", out);
        }
    };

    let new_id = discord::new_user_id();
    let user_id = match identity::find_or_create_by_discord_id(&st.pool, &new_id, &profile).await {
        Ok(id) => id,
        Err(e) => {
            tracing::error!("discord callback: upsert user failed: {e:#}");
            return err_redirect(&st, "discord-auth", out);
        }
    };

    // Block suspended/deleted accounts (log-and-continue on lookup failure,
    // matching the Go behavior).
    match identity::blocked_notice(&st.pool, &user_id).await {
        Ok(Some(notice)) => return notice_redirect(&st, notice, out),
        Ok(None) => {}
        Err(e) => tracing::error!("discord callback: account status check failed: {e:#}"),
    }

    if let Err(e) = identity::touch_last_login(&st.pool, &user_id).await {
        // Log-and-continue: failing to touch last_login_at must not block sign-in.
        tracing::warn!("discord callback: touch last login failed: {e:#}");
    }

    // Sync the user's guild list into user_guilds. Log-and-continue on either
    // the fetch or the sync (matches Go): a stale guild list must not block
    // sign-in. Keyed on the Discord ID, like the Go backend.
    match discord::fetch_user_guilds(&st.http, &access).await {
        Ok(guilds) => {
            let inputs: Vec<identity::UserGuildInput> = guilds
                .into_iter()
                .map(|g| identity::UserGuildInput {
                    guild_id: g.id,
                    name: g.name,
                    icon_hash: g.icon_hash,
                    approximate_member_count: g.approximate_member_count,
                    is_owner: g.owner,
                })
                .collect();
            if let Err(e) = identity::sync_user_guilds(&st.pool, &profile.id, &inputs).await {
                tracing::warn!("discord callback: sync user guilds failed: {e:#}");
            }
        }
        Err(e) => tracing::warn!("discord callback: fetch user guilds failed: {e:#}"),
    }

    let cu = match identity::load_current_user(&st.pool, &user_id).await {
        Ok(Some(cu)) => cu,
        Ok(None) => return err_redirect(&st, "discord-auth", out),
        Err(e) => {
            tracing::error!("discord callback: load current user failed: {e:#}");
            return err_redirect(&st, "discord-auth", out);
        }
    };

    let session = build_session(&cu);
    let token = match serialize_session(&session, &st.config.app_session_secret) {
        Ok(t) => t,
        Err(e) => {
            tracing::error!("discord callback: serialize session failed: {e:#}");
            return err_redirect(&st, "discord-auth", out);
        }
    };
    cookies::append_set_cookie(
        &mut out,
        &cookies::set_cookie(SESSION_COOKIE, &token, SESSION_MAX_AGE),
    );
    redirect_with_headers(&st.config.default_post_login_url(), out)
}

/// `GET /api/app/me` — current user, authenticated via the `/api/app/*` gate
/// (`X-RustLens-App-Secret` + the forwarded `X-RustLens-Session` header), matching
/// the Go backend. The browser never calls this directly; the Next.js server
/// validates the cookie and forwards the session token in the header.
pub async fn me(State(st): Shared, headers: HeaderMap) -> Response {
    if let Err(resp) =
        crate::appauth::require_app_secret(&headers, &st.config.app_api_internal_secret)
    {
        return *resp;
    }
    let session = match crate::appauth::require_session(&headers, &st).await {
        Ok(s) => s,
        Err(resp) => return *resp,
    };

    match identity::load_current_user(&st.pool, &session.user_id).await {
        Ok(Some(mut cu)) => {
            // When linked-roles is configured, attach the dashboard summary
            // (present with `linked:false` when there is no connection row).
            if st.config.role_connection_enabled() {
                if let Ok(summary) =
                    rustlens_db::role_connections::get_role_connection_summary(
                        &st.pool,
                        &session.user_id,
                    )
                    .await
                {
                    cu.discord_role_connection = Some(summary);
                }
            }
            (StatusCode::OK, Json(cu)).into_response()
        }
        Ok(None) => error_json(StatusCode::NOT_FOUND, "app user not found"),
        Err(e) => {
            tracing::error!("/api/app/me: load current user failed: {e:#}");
            error_json(StatusCode::INTERNAL_SERVER_ERROR, "failed to load current user")
        }
    }
}

/// `GET /api/app/me/preferences` — the user's notification toggles, lazily
/// defaulted. Header-authed like the rest of `/api/app/*`.
pub async fn get_preferences(State(st): Shared, headers: HeaderMap) -> Response {
    if let Err(resp) =
        crate::appauth::require_app_secret(&headers, &st.config.app_api_internal_secret)
    {
        return *resp;
    }
    let session = match crate::appauth::require_session(&headers, &st).await {
        Ok(s) => s,
        Err(resp) => return *resp,
    };
    match rustlens_db::preferences::load_notification_preferences(&st.pool, &session.user_id).await {
        Ok(p) => (StatusCode::OK, Json(p)).into_response(),
        Err(e) => {
            tracing::error!("/api/app/me/preferences GET: {e:#}");
            error_json(StatusCode::INTERNAL_SERVER_ERROR, "internal_error")
        }
    }
}

/// Strict-decoded PATCH body: unknown fields or non-boolean values are rejected
/// (matches Go's `DisallowUnknownFields`); absent fields are left unchanged.
#[derive(Debug, Default, Deserialize)]
#[serde(deny_unknown_fields)]
struct PrefsPatch {
    #[serde(rename = "notifyInApp")]
    notify_in_app: Option<bool>,
    #[serde(rename = "notifyDiscordAnnouncements")]
    notify_discord_announcements: Option<bool>,
}

/// `PATCH /api/app/me/preferences` — partial update; empty body is a no-op that
/// returns current values.
pub async fn patch_preferences(State(st): Shared, headers: HeaderMap, raw: Bytes) -> Response {
    if let Err(resp) =
        crate::appauth::require_app_secret(&headers, &st.config.app_api_internal_secret)
    {
        return *resp;
    }
    let session = match crate::appauth::require_session(&headers, &st).await {
        Ok(s) => s,
        Err(resp) => return *resp,
    };
    let patch: PrefsPatch = if raw.is_empty() {
        PrefsPatch::default()
    } else {
        match serde_json::from_slice(&raw) {
            Ok(p) => p,
            Err(_) => return error_json(StatusCode::BAD_REQUEST, "bad_request"),
        }
    };
    match rustlens_db::preferences::upsert_notification_preferences(
        &st.pool,
        &session.user_id,
        patch.notify_in_app,
        patch.notify_discord_announcements,
    )
    .await
    {
        Ok(p) => (StatusCode::OK, Json(p)).into_response(),
        Err(e) => {
            tracing::error!("/api/app/me/preferences PATCH: {e:#}");
            error_json(StatusCode::INTERNAL_SERVER_ERROR, "internal_error")
        }
    }
}

/// `POST /api/app/auth/logout` — app-secret-gated no-op. Sessions live in the
/// signed cookie the frontend holds and clears itself; this named route exists
/// for the frontend to call. Mirrors the Go `Logout`.
pub async fn logout(State(st): Shared, headers: HeaderMap) -> Response {
    if let Err(resp) =
        crate::appauth::require_app_secret(&headers, &st.config.app_api_internal_secret)
    {
        return *resp;
    }
    (StatusCode::OK, Json(serde_json::json!({ "ok": true }))).into_response()
}

fn build_session(cu: &CurrentUser) -> SessionUser {
    SessionUser {
        user_id: cu.user.id.clone(),
        display_name: cu.user.display_name.clone(),
        avatar_url: cu.user.avatar_url.clone(),
        roles: cu.user.roles.clone(),
        admin_roles: cu.admin_roles.clone(),
        permissions: cu.permissions.clone(),
        entitlements: cu.entitlements.clone(),
        discord_id: cu.user.discord_id.clone(),
        epoch: cu.user.session_epoch,
        expires_at: Utc::now().timestamp_millis() + SESSION_MAX_AGE * 1000,
    }
}

fn err_redirect(st: &AppState, code: &str, headers: HeaderMap) -> Response {
    redirect_with_headers(&st.config.frontend_url(&format!("/login?error={code}")), headers)
}

fn notice_redirect(st: &AppState, notice: &str, headers: HeaderMap) -> Response {
    redirect_with_headers(
        &st.config.frontend_url(&format!("/login?notice={notice}")),
        headers,
    )
}

fn redirect_with_headers(location: &str, mut headers: HeaderMap) -> Response {
    match header::HeaderValue::from_str(location) {
        Ok(hv) => {
            headers.insert(header::LOCATION, hv);
        }
        Err(_) => return (StatusCode::INTERNAL_SERVER_ERROR, "bad redirect target").into_response(),
    }
    (StatusCode::FOUND, headers).into_response()
}

fn error_json(status: StatusCode, msg: &str) -> Response {
    (status, Json(serde_json::json!({ "error": msg }))).into_response()
}
