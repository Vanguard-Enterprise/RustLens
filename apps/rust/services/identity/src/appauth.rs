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

use rustlens_httpauth::AuthError;

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

/// Full admin gate: app-secret → session → permission.
pub async fn require_admin(
    headers: &HeaderMap,
    st: &AppState,
    perm: &str,
) -> Result<SessionUser, AuthError> {
    rustlens_httpauth::require_admin(
        headers,
        &st.pool,
        &st.config.app_api_internal_secret,
        &st.config.app_session_secret,
        perm,
    )
    .await
}
