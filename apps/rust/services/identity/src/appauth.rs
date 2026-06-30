//! Auth helpers for the `/api/app/*` surface, mirroring the Go
//! `internal/appauth` middleware chain.
//!
//! The Next.js server (never the browser) calls these routes with two headers:
//!   - `X-RustLens-App-Secret` — the shared internal secret gating the surface,
//!   - `X-RustLens-Session`    — the signed session token of the acting user.
//!
//! Admin routes additionally require a permission. Header lookups are
//! case-insensitive (axum normalizes), so lowercase constants are fine.

use axum::Json;
use axum::http::{HeaderMap, StatusCode};
use axum::response::{IntoResponse, Response};

use rustlens_db::identity::{self, Revocation};
use rustlens_session::{SessionError, SessionUser, parse_session};

use crate::cookies::{self, SESSION_COOKIE};
use crate::state::AppState;

/// The shared-secret header gating `/api/app/*` (Go `appSecretHeader`).
pub const APP_SECRET_HEADER: &str = "x-rustlens-app-secret";
/// The forwarded session-token header (Go `sessionHeader`).
pub const SESSION_HEADER: &str = "x-rustlens-session";

/// Constant-time byte comparison (avoids leaking the secret via timing).
fn ct_eq(a: &[u8], b: &[u8]) -> bool {
    if a.len() != b.len() {
        return false;
    }
    let mut diff = 0u8;
    for (x, y) in a.iter().zip(b) {
        diff |= x ^ y;
    }
    diff == 0
}

/// Error responses are boxed: a `Response` is large, and these helpers return
/// `Ok` on the hot path, so an unboxed `Err(Response)` would bloat every
/// `Result` (clippy `result_large_err`).
type AuthError = Box<Response>;

fn err_json(status: StatusCode, code: &str) -> AuthError {
    Box::new((status, Json(serde_json::json!({ "error": code }))).into_response())
}

/// Require a valid `X-RustLens-App-Secret`. Mirrors `RequireAppSecret`:
/// missing or mismatched → 401 `{"error":"app_secret_required"}`.
pub fn require_app_secret(headers: &HeaderMap, expected: &str) -> Result<(), AuthError> {
    let got = headers
        .get(APP_SECRET_HEADER)
        .and_then(|v| v.to_str().ok())
        .unwrap_or("");
    if got.is_empty() || !ct_eq(got.as_bytes(), expected.as_bytes()) {
        return Err(err_json(StatusCode::UNAUTHORIZED, "app_secret_required"));
    }
    Ok(())
}

/// Validate the forwarded session header (signature + expiry + DB revocation).
/// Mirrors `RequireSession`'s error classification:
///   - missing header           → `session_required`
///   - revoked/suspended/deleted → `session_revoked`
///   - bad signature / expired   → `invalid_session`
pub async fn require_session(headers: &HeaderMap, st: &AppState) -> Result<SessionUser, AuthError> {
    let Some(token) = headers
        .get(SESSION_HEADER)
        .and_then(|v| v.to_str().ok())
        .filter(|s| !s.is_empty())
    else {
        return Err(err_json(StatusCode::UNAUTHORIZED, "session_required"));
    };

    let session = match parse_session(token, &st.config.app_session_secret) {
        Ok(s) => s,
        Err(_) => return Err(err_json(StatusCode::UNAUTHORIZED, "invalid_session")),
    };

    match identity::revocation_check(&st.pool, &session.user_id, session.epoch).await {
        Ok(Revocation::Valid) => Ok(session),
        Ok(Revocation::Revoked) => Err(err_json(StatusCode::UNAUTHORIZED, "session_revoked")),
        Err(e) => {
            tracing::error!("appauth: revocation check failed: {e:#}");
            Err(err_json(StatusCode::UNAUTHORIZED, "invalid_session"))
        }
    }
}

/// Validate the session from the `rustlens_session` **cookie** (used by the
/// browser-direct SSE endpoint, not the header-forwarded `/api/app/*` routes).
/// Mirrors Go `ValidateSessionFromCookie`'s error classification:
///   - missing cookie → `session_required`
///   - expired        → `session_expired`
///   - revoked        → `session_revoked`
///   - otherwise      → `invalid_session`
pub async fn validate_session_cookie(
    headers: &HeaderMap,
    st: &AppState,
) -> Result<SessionUser, (StatusCode, &'static str)> {
    let Some(token) = cookies::read_cookie(headers, SESSION_COOKIE) else {
        return Err((StatusCode::UNAUTHORIZED, "session_required"));
    };
    let session = match parse_session(&token, &st.config.app_session_secret) {
        Ok(s) => s,
        Err(SessionError::Expired) => return Err((StatusCode::UNAUTHORIZED, "session_expired")),
        Err(_) => return Err((StatusCode::UNAUTHORIZED, "invalid_session")),
    };
    match identity::revocation_check(&st.pool, &session.user_id, session.epoch).await {
        Ok(Revocation::Valid) => Ok(session),
        Ok(Revocation::Revoked) => Err((StatusCode::UNAUTHORIZED, "session_revoked")),
        Err(e) => {
            tracing::error!("appauth: SSE revocation check failed: {e:#}");
            Err((StatusCode::INTERNAL_SERVER_ERROR, "invalid_session"))
        }
    }
}

/// Require that `user_id` holds `perm`. Mirrors `requireAdminPerm`:
/// permission check failure → 500 `permission_check_failed`; denied → 403
/// `forbidden`.
pub async fn require_permission(st: &AppState, user_id: &str, perm: &str) -> Result<(), AuthError> {
    match rustlens_db::admin::has_permission(&st.pool, user_id, perm).await {
        Ok(true) => Ok(()),
        Ok(false) => Err(err_json(StatusCode::FORBIDDEN, "forbidden")),
        Err(e) => {
            tracing::error!("appauth: permission check failed: {e:#}");
            Err(err_json(
                StatusCode::INTERNAL_SERVER_ERROR,
                "permission_check_failed",
            ))
        }
    }
}

/// Full admin gate: app-secret → session → permission. Returns the acting
/// session on success, or the appropriate error response.
pub async fn require_admin(
    headers: &HeaderMap,
    st: &AppState,
    perm: &str,
) -> Result<SessionUser, AuthError> {
    require_app_secret(headers, &st.config.app_api_internal_secret)?;
    let session = require_session(headers, st).await?;
    require_permission(st, &session.user_id, perm).await?;
    Ok(session)
}
