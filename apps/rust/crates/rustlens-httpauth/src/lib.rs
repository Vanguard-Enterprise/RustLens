//! Shared auth helpers for the `/api/app/*` surface, mirroring the Go
//! `internal/appauth` middleware chain. Factored out of the identity service so
//! every app-facing Rust service reuses the same gate.
//!
//! The Next.js server (never the browser) calls these routes with two headers:
//!   - `X-RustLens-App-Secret` — the shared internal secret gating the surface,
//!   - `X-RustLens-Session`    — the signed session token of the acting user.
//!
//! Admin routes additionally require a permission. Header lookups are
//! case-insensitive (axum normalizes), so lowercase constants are fine.
//!
//! These functions take the primitives they need (`&PgPool`, `&str` secrets)
//! rather than a service-specific state struct, so any service can call them
//! and thread its own state.

use axum::Json;
use axum::http::{HeaderMap, StatusCode, header};
use axum::response::{IntoResponse, Response};

use rustlens_db::PgPool;
use rustlens_db::identity::{self, Revocation};
use rustlens_session::{SessionError, SessionUser, parse_session};

pub mod api_key;
pub use api_key::ApiKeyActor;

/// The shared-secret header gating `/api/app/*` (Go `appSecretHeader`).
pub const APP_SECRET_HEADER: &str = "x-rustlens-app-secret";
/// The forwarded session-token header (Go `sessionHeader`).
pub const SESSION_HEADER: &str = "x-rustlens-session";
/// The signed session cookie name — a cross-language wire contract (Go
/// `SessionCookieName`, JS `SESSION_COOKIE`). Used by the browser-direct SSE
/// path, not the header-forwarded `/api/app/*` routes.
pub const SESSION_COOKIE: &str = "rustlens_session";

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
pub type AuthError = Box<Response>;

pub(crate) fn err_json(status: StatusCode, code: &str) -> AuthError {
    Box::new((status, Json(serde_json::json!({ "error": code }))).into_response())
}

/// Read a single cookie value from the request `Cookie` header. Self-contained
/// so the crate has no dependency on a service's cookie module.
fn read_cookie(headers: &HeaderMap, name: &str) -> Option<String> {
    let raw = headers.get(header::COOKIE)?.to_str().ok()?;
    for part in raw.split(';') {
        let part = part.trim();
        if let Some((k, v)) = part.split_once('=') {
            if k == name {
                return Some(v.to_string());
            }
        }
    }
    None
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
///   - missing header            → `session_required`
///   - revoked/suspended/deleted → `session_revoked`
///   - bad signature / expired   → `invalid_session`
///
/// Note: the header path collapses expired→`invalid_session` (matching Go); the
/// cookie path below distinguishes `session_expired`.
pub async fn require_session(
    headers: &HeaderMap,
    pool: &PgPool,
    session_secret: &str,
) -> Result<SessionUser, AuthError> {
    let Some(token) = headers
        .get(SESSION_HEADER)
        .and_then(|v| v.to_str().ok())
        .filter(|s| !s.is_empty())
    else {
        return Err(err_json(StatusCode::UNAUTHORIZED, "session_required"));
    };

    let session = match parse_session(token, session_secret) {
        Ok(s) => s,
        Err(_) => return Err(err_json(StatusCode::UNAUTHORIZED, "invalid_session")),
    };

    match identity::revocation_check(pool, &session.user_id, session.epoch).await {
        Ok(Revocation::Valid) => Ok(session),
        Ok(Revocation::Revoked) => Err(err_json(StatusCode::UNAUTHORIZED, "session_revoked")),
        Err(e) => {
            tracing::error!("httpauth: revocation check failed: {e:#}");
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
    pool: &PgPool,
    session_secret: &str,
) -> Result<SessionUser, (StatusCode, &'static str)> {
    let Some(token) = read_cookie(headers, SESSION_COOKIE) else {
        return Err((StatusCode::UNAUTHORIZED, "session_required"));
    };
    let session = match parse_session(&token, session_secret) {
        Ok(s) => s,
        Err(SessionError::Expired) => return Err((StatusCode::UNAUTHORIZED, "session_expired")),
        Err(_) => return Err((StatusCode::UNAUTHORIZED, "invalid_session")),
    };
    match identity::revocation_check(pool, &session.user_id, session.epoch).await {
        Ok(Revocation::Valid) => Ok(session),
        Ok(Revocation::Revoked) => Err((StatusCode::UNAUTHORIZED, "session_revoked")),
        Err(e) => {
            tracing::error!("httpauth: SSE revocation check failed: {e:#}");
            Err((StatusCode::INTERNAL_SERVER_ERROR, "invalid_session"))
        }
    }
}

/// Require that `user_id` holds `perm`. Mirrors `requireAdminPerm`:
/// permission check failure → 500 `permission_check_failed`; denied → 403
/// `forbidden`.
pub async fn require_permission(pool: &PgPool, user_id: &str, perm: &str) -> Result<(), AuthError> {
    match rustlens_db::admin::has_permission(pool, user_id, perm).await {
        Ok(true) => Ok(()),
        Ok(false) => Err(err_json(StatusCode::FORBIDDEN, "forbidden")),
        Err(e) => {
            tracing::error!("httpauth: permission check failed: {e:#}");
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
    pool: &PgPool,
    app_secret: &str,
    session_secret: &str,
    perm: &str,
) -> Result<SessionUser, AuthError> {
    require_app_secret(headers, app_secret)?;
    let session = require_session(headers, pool, session_secret).await?;
    require_permission(pool, &session.user_id, perm).await?;
    Ok(session)
}

/// The resolved actor on an admin route: either a session user (app-secret +
/// session header) or an admin API key (`Authorization: Bearer rl_admin_...`).
/// Mirrors the Go actor resolution where the global API-key middleware wins over
/// the session gate when a key header is present.
pub struct AdminActor {
    pub user_id: String,
    /// Role snapshot for the audit `actor_role_snapshot`. Empty for API-key
    /// actors, matching Go's `actorResolver` returning nil roles for keys.
    pub admin_roles: Vec<String>,
    /// Present when authenticated via an admin API key. The key's snapshot
    /// permissions are intersected with the creator's *current* role
    /// permissions on every permission check (Go `WithAPIKeyPermissions`).
    pub api_key: Option<ApiKeyActor>,
}

/// Authenticate an admin route as EITHER an API key OR app-secret + session.
///
/// - When an `Authorization: Bearer rl_admin_...` header is present, it is
///   validated as an API key and the app-secret / session gate is bypassed
///   (exactly like the Go global middleware short-circuit). An invalid key is a
///   hard 401 (it does not fall through to the session path).
/// - Otherwise the standard app-secret + session gate runs.
///
/// This does NOT check a permission; call [`require_admin_permission`] next.
pub async fn authenticate_admin(
    headers: &HeaderMap,
    pool: &PgPool,
    app_secret: &str,
    session_secret: &str,
) -> Result<AdminActor, AuthError> {
    if api_key::has_api_key_header(headers) {
        let key = api_key::authenticate_api_key(headers, pool).await?;
        return Ok(AdminActor {
            user_id: key.user_id.clone(),
            admin_roles: Vec::new(),
            api_key: Some(key),
        });
    }
    require_app_secret(headers, app_secret)?;
    let session = require_session(headers, pool, session_secret).await?;
    Ok(AdminActor {
        user_id: session.user_id,
        admin_roles: session.admin_roles,
        api_key: None,
    })
}

/// Require that `actor` holds `perm`, applying the API-key intersection: an
/// API-key actor must have `perm` in its snapshot AND the creator must still
/// hold it via roles. Mirrors the Go resolver's floor-of-the-two semantics, so
/// revoking a permission from the creator immediately constrains every key.
pub async fn require_admin_permission(
    pool: &PgPool,
    actor: &AdminActor,
    perm: &str,
) -> Result<(), AuthError> {
    if let Some(k) = &actor.api_key {
        if !k.permissions.iter().any(|p| p == perm) {
            return Err(err_json(StatusCode::FORBIDDEN, "forbidden"));
        }
    }
    require_permission(pool, &actor.user_id, perm).await
}

/// Full admin gate accepting an API key or a session: authenticate then check a
/// single permission. Returns the resolved actor. Used by identity's admin read
/// routes.
pub async fn require_admin_or_key(
    headers: &HeaderMap,
    pool: &PgPool,
    app_secret: &str,
    session_secret: &str,
    perm: &str,
) -> Result<AdminActor, AuthError> {
    let actor = authenticate_admin(headers, pool, app_secret, session_secret).await?;
    require_admin_permission(pool, &actor, perm).await?;
    Ok(actor)
}

#[cfg(test)]
mod tests {
    use super::*;
    use axum::http::HeaderValue;

    fn headers_with(name: &str, value: &str) -> HeaderMap {
        let mut h = HeaderMap::new();
        h.insert(
            axum::http::HeaderName::from_bytes(name.as_bytes()).unwrap(),
            HeaderValue::from_str(value).unwrap(),
        );
        h
    }

    #[test]
    fn app_secret_missing_is_rejected() {
        let h = HeaderMap::new();
        assert!(require_app_secret(&h, "sekret").is_err());
    }

    #[test]
    fn app_secret_mismatch_is_rejected() {
        let h = headers_with(APP_SECRET_HEADER, "wrong");
        assert!(require_app_secret(&h, "sekret").is_err());
    }

    #[test]
    fn app_secret_match_is_accepted() {
        let h = headers_with(APP_SECRET_HEADER, "sekret");
        assert!(require_app_secret(&h, "sekret").is_ok());
    }

    #[test]
    fn ct_eq_matches_go_semantics() {
        assert!(ct_eq(b"abc", b"abc"));
        assert!(!ct_eq(b"abc", b"abd"));
        assert!(!ct_eq(b"abc", b"ab"));
        assert!(!ct_eq(b"", b"x"));
        assert!(ct_eq(b"", b""));
    }

    #[test]
    fn read_cookie_parses_named_value() {
        let h = headers_with("cookie", "a=1; rustlens_session=tok.en; b=2");
        assert_eq!(read_cookie(&h, SESSION_COOKIE).as_deref(), Some("tok.en"));
        assert_eq!(read_cookie(&h, "a").as_deref(), Some("1"));
        assert_eq!(read_cookie(&h, "missing"), None);
    }
}
