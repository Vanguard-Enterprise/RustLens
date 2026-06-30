//! RustLens signed-session token.
//!
//! This crate mints and verifies the `rustlens_session` cookie. Its wire format
//! is a **cross-language contract**: the same token is produced and consumed by
//!
//!   - the Go backend  (`apps/backend/internal/appauth/session.go`), and
//!   - the Next.js frontend (`apps/frontend/lib/session.ts`).
//!
//! The format is:
//!
//! ```text
//! base64url(json_payload) "." base64url(HMAC_SHA256(base64url(json_payload)))
//! ```
//!
//! where:
//!   - base64url is the URL-safe alphabet with **no padding** (Go's
//!     `base64.RawURLEncoding`, Node's `"base64url"`),
//!   - the HMAC key is `APP_SESSION_SECRET`,
//!   - the HMAC is computed over the **encoded payload string**, not the raw
//!     JSON bytes,
//!   - `expiresAt` and the wall-clock are **milliseconds** since the Unix epoch
//!     (JS `Date.now()` / Go `time.Now().UnixMilli()`).
//!
//! This crate is intentionally DB-free. Signature + structural + expiry checks
//! live here; the `epoch`-vs-`app_users.session_epoch` revocation check is a DB
//! concern handled by the service layer.

use base64::Engine;
use base64::engine::general_purpose::URL_SAFE_NO_PAD;
use hmac::{Hmac, Mac};
use serde::{Deserialize, Serialize};
use sha2::Sha256;
use subtle::ConstantTimeEq;

type HmacSha256 = Hmac<Sha256>;

/// The session payload. Field order and names mirror the Go `SessionUser`
/// struct and the JS `SessionUser` type exactly so the serialized JSON is
/// identical across all three implementations.
///
/// `adminRoles`, `permissions`, `entitlements`, and `discordId` use
/// `skip_serializing_if` to match Go's `omitempty`.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct SessionUser {
    #[serde(rename = "userId")]
    pub user_id: String,
    #[serde(rename = "displayName")]
    pub display_name: String,
    #[serde(rename = "avatarUrl")]
    pub avatar_url: String,
    pub roles: Vec<String>,
    #[serde(
        rename = "adminRoles",
        default,
        skip_serializing_if = "Vec::is_empty"
    )]
    pub admin_roles: Vec<String>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub permissions: Vec<String>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub entitlements: Vec<String>,
    #[serde(
        rename = "discordId",
        default,
        skip_serializing_if = "String::is_empty"
    )]
    pub discord_id: String,
    pub epoch: i64,
    #[serde(rename = "expiresAt")]
    /// Milliseconds since the Unix epoch (JS `Date.now()`).
    pub expires_at: i64,
}

#[derive(Debug, thiserror::Error, PartialEq, Eq)]
pub enum SessionError {
    #[error("malformed token")]
    Malformed,
    #[error("bad signature")]
    BadSignature,
    #[error("token expired")]
    Expired,
}

/// HMAC-SHA256 the encoded payload string and base64url(no-pad) the digest.
/// Equivalent to Go's `sign()` and JS's `signPayload()`.
pub fn sign(encoded_payload: &str, secret: &str) -> String {
    let mut mac = HmacSha256::new_from_slice(secret.as_bytes())
        .expect("HMAC accepts keys of any length");
    mac.update(encoded_payload.as_bytes());
    URL_SAFE_NO_PAD.encode(mac.finalize().into_bytes())
}

/// Serialize and sign a session into a cookie token.
/// Equivalent to Go's `SerializeSession()` / JS's `serializeSession()`.
pub fn serialize_session(session: &SessionUser, secret: &str) -> Result<String, serde_json::Error> {
    let raw = serde_json::to_vec(session)?;
    let encoded = URL_SAFE_NO_PAD.encode(&raw);
    let signature = sign(&encoded, secret);
    Ok(format!("{encoded}.{signature}"))
}

/// Parse and verify a token, checking expiry against the current wall clock.
/// Mirrors Go's `ParseSession()`. Does **not** perform the DB epoch revocation
/// check — that belongs to the service layer.
pub fn parse_session(token: &str, secret: &str) -> Result<SessionUser, SessionError> {
    let now_ms = now_unix_millis();
    parse_session_at(token, secret, now_ms)
}

/// Like [`parse_session`] but with an explicit `now` (milliseconds since the
/// Unix epoch) so expiry behavior is testable.
pub fn parse_session_at(
    token: &str,
    secret: &str,
    now_unix_ms: i64,
) -> Result<SessionUser, SessionError> {
    // Split on the first '.' only (Go uses IndexByte). The encoded payload must
    // be non-empty and the signature must not be empty.
    let dot = token.find('.').ok_or(SessionError::Malformed)?;
    if dot == 0 || dot == token.len() - 1 {
        return Err(SessionError::Malformed);
    }
    let encoded = &token[..dot];
    let signature = &token[dot + 1..];

    let expected = sign(encoded, secret);
    // Constant-time comparison of the base64url signature strings, matching
    // Go's `hmac.Equal` on the encoded signatures. `ct_eq` returns false for
    // differing lengths.
    if signature.as_bytes().ct_eq(expected.as_bytes()).unwrap_u8() != 1 {
        return Err(SessionError::BadSignature);
    }

    let raw = URL_SAFE_NO_PAD
        .decode(encoded)
        .map_err(|_| SessionError::Malformed)?;
    let session: SessionUser =
        serde_json::from_slice(&raw).map_err(|_| SessionError::Malformed)?;

    if session.user_id.is_empty() {
        return Err(SessionError::Malformed);
    }
    if now_unix_ms > session.expires_at {
        return Err(SessionError::Expired);
    }
    Ok(session)
}

fn now_unix_millis() -> i64 {
    use std::time::{SystemTime, UNIX_EPOCH};
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_millis() as i64)
        .unwrap_or(0)
}

#[cfg(test)]
mod tests {
    use super::*;

    const SECRET: &str = "test-secret-key";

    // Reference token produced by the exact JS algorithm in
    // apps/frontend/lib/session.ts (Node 24, crypto + base64url), with the
    // payload below. This is the cross-language known-answer: if Rust mints the
    // identical string and verifies this token, the wire format matches Go/JS.
    const REF_TOKEN: &str = "eyJ1c2VySWQiOiJ1c2VyLTEyMyIsImRpc3BsYXlOYW1lIjoiRnJhbmsgw6kiLCJhdmF0YXJVcmwiOiJodHRwczovL2Nkbi5leGFtcGxlL2EucG5nIiwicm9sZXMiOlsidXNlciJdLCJhZG1pblJvbGVzIjpbInN1cGVyYWRtaW4iXSwicGVybWlzc2lvbnMiOlsiYWRtaW5zLmdyYW50IiwiYm1fc2VydmVycy5yZWFkIl0sImVudGl0bGVtZW50cyI6WyJwcm8iXSwiZGlzY29yZElkIjoiOTg3NjU0MzIxIiwiZXBvY2giOjMsImV4cGlyZXNBdCI6MTg5MzQ1NjAwMDAwMH0.K-NTgeSPLV6nuRSrpWKG8tE5EnlT5glRTj8E5Z6nAoc";

    fn reference_user() -> SessionUser {
        SessionUser {
            user_id: "user-123".into(),
            display_name: "Frank é".into(),
            avatar_url: "https://cdn.example/a.png".into(),
            roles: vec!["user".into()],
            admin_roles: vec!["superadmin".into()],
            permissions: vec!["admins.grant".into(), "bm_servers.read".into()],
            entitlements: vec!["pro".into()],
            discord_id: "987654321".into(),
            epoch: 3,
            expires_at: 1_893_456_000_000,
        }
    }

    #[test]
    fn mint_matches_js_reference_byte_for_byte() {
        let token = serialize_session(&reference_user(), SECRET).unwrap();
        assert_eq!(
            token, REF_TOKEN,
            "Rust-minted token must equal the JS/Go reference exactly"
        );
    }

    #[test]
    fn verifies_js_reference_token() {
        // now is well before expiresAt (1893456000000 = 2030-01-01).
        let parsed = parse_session_at(REF_TOKEN, SECRET, 1_700_000_000_000).unwrap();
        assert_eq!(parsed, reference_user());
    }

    #[test]
    fn round_trip() {
        let user = reference_user();
        let token = serialize_session(&user, SECRET).unwrap();
        let parsed = parse_session_at(&token, SECRET, 1_700_000_000_000).unwrap();
        assert_eq!(parsed, user);
    }

    #[test]
    fn rejects_tampered_payload() {
        let mut bad = REF_TOKEN.to_string();
        // Flip a character in the payload section.
        let first = bad.as_bytes()[0];
        let replacement = if first == b'e' { 'f' } else { 'e' };
        bad.replace_range(0..1, &replacement.to_string());
        assert_eq!(
            parse_session_at(&bad, SECRET, 1_700_000_000_000),
            Err(SessionError::BadSignature)
        );
    }

    #[test]
    fn rejects_wrong_secret() {
        assert_eq!(
            parse_session_at(REF_TOKEN, "other-secret", 1_700_000_000_000),
            Err(SessionError::BadSignature)
        );
    }

    #[test]
    fn rejects_expired() {
        // now is after expiresAt.
        assert_eq!(
            parse_session_at(REF_TOKEN, SECRET, 1_893_456_000_001),
            Err(SessionError::Expired)
        );
    }

    #[test]
    fn rejects_malformed() {
        assert_eq!(
            parse_session_at("not-a-token", SECRET, 0),
            Err(SessionError::Malformed)
        );
        assert_eq!(
            parse_session_at(".sig", SECRET, 0),
            Err(SessionError::Malformed)
        );
        assert_eq!(
            parse_session_at("payload.", SECRET, 0),
            Err(SessionError::Malformed)
        );
    }

    #[test]
    fn omits_empty_optional_fields_like_go_omitempty() {
        // A user with empty admin_roles/permissions/entitlements/discord_id
        // must serialize without those keys (matching Go's omitempty), so the
        // JSON stays compatible with both consumers.
        let user = SessionUser {
            user_id: "u".into(),
            display_name: "n".into(),
            avatar_url: "a".into(),
            roles: vec!["user".into()],
            admin_roles: vec![],
            permissions: vec![],
            entitlements: vec![],
            discord_id: String::new(),
            epoch: 0,
            expires_at: 1_893_456_000_000,
        };
        let token = serialize_session(&user, SECRET).unwrap();
        let encoded = token.split('.').next().unwrap();
        let json = String::from_utf8(URL_SAFE_NO_PAD.decode(encoded).unwrap()).unwrap();
        assert_eq!(
            json,
            r#"{"userId":"u","displayName":"n","avatarUrl":"a","roles":["user"],"epoch":0,"expiresAt":1893456000000}"#
        );
    }
}
