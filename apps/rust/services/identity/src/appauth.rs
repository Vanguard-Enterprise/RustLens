//! Thin `AppState` adapters over the shared `rustlens-httpauth` crate.
//!
//! The auth logic (app-secret gate, session-header validation, cookie-session
//! extraction, permission checks) lives in `rustlens_httpauth`, decoupled from
//! any service state so future app-facing services reuse it. These wrappers just
//! pull the pool + secrets out of `AppState` so identity's existing call sites
//! (`appauth::require_admin(&headers, &st, perm)`, etc.) are unchanged.

use axum::http::{HeaderMap, StatusCode};

use rustlens_session::SessionUser;

use crate::state::AppState;

use rustlens_httpauth::{AdminActor, AuthError};

/// Require a valid `X-RustLens-App-Secret` against the given expected secret.
pub fn require_app_secret(headers: &HeaderMap, expected: &str) -> Result<(), AuthError> {
    rustlens_httpauth::require_app_secret(headers, expected)
}

/// Validate the forwarded `X-RustLens-Session` header (signature + expiry + DB
/// revocation) using the identity service's pool + session secret.
pub async fn require_session(headers: &HeaderMap, st: &AppState) -> Result<SessionUser, AuthError> {
    rustlens_httpauth::require_session(headers, &st.pool, &st.config.app_session_secret).await
}

/// Validate the session from the `rustlens_session` cookie (SSE path).
pub async fn validate_session_cookie(
    headers: &HeaderMap,
    st: &AppState,
) -> Result<SessionUser, (StatusCode, &'static str)> {
    rustlens_httpauth::validate_session_cookie(headers, &st.pool, &st.config.app_session_secret)
        .await
}

/// Full admin gate accepting EITHER an admin API key OR app-secret + session,
/// then checking `perm`. Returns the resolved [`AdminActor`]. The admin read
/// handlers discard the actor and only care about the error path.
pub async fn require_admin(
    headers: &HeaderMap,
    st: &AppState,
    perm: &str,
) -> Result<AdminActor, AuthError> {
    rustlens_httpauth::require_admin_or_key(
        headers,
        &st.pool,
        &st.config.app_api_internal_secret,
        &st.config.app_session_secret,
        perm,
    )
    .await
}

/// Authenticate an admin route (API key or session) without a permission check.
/// The dispatcher uses this so it can apply its own per-action permission gate.
pub async fn authenticate_admin(headers: &HeaderMap, st: &AppState) -> Result<AdminActor, AuthError> {
    rustlens_httpauth::authenticate_admin(
        headers,
        &st.pool,
        &st.config.app_api_internal_secret,
        &st.config.app_session_secret,
    )
    .await
}
