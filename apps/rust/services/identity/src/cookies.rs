//! Cookie helpers. Attributes match the Go backend exactly:
//! `Path=/; HttpOnly; Secure; SameSite=Lax`. We write raw `Set-Cookie` headers
//! rather than pulling in the `cookie` crate so the wire format is identical and
//! we control every attribute.

use axum::http::{HeaderMap, header};

/// The shared signed-session cookie (minted by Go, JS, and now Rust).
pub const SESSION_COOKIE: &str = "rustlens_session";
/// Per-flow OAuth state cookie (matches Go's `CookieState`).
pub const STATE_COOKIE: &str = "rustlens_discord_oauth_state";
/// Per-flow PKCE verifier cookie (matches Go's `CookieVerifier`).
pub const VERIFIER_COOKIE: &str = "rustlens_discord_oauth_verifier";

/// 14 days, in seconds (matches Go's session `MaxAge`).
pub const SESSION_MAX_AGE: i64 = 60 * 60 * 24 * 14;
/// 10 minutes, in seconds (matches Go's `CookieMaxAgeSeconds`).
pub const SHORT_MAX_AGE: i64 = 60 * 10;

/// Read a single cookie value from the request `Cookie` header.
pub fn read_cookie(headers: &HeaderMap, name: &str) -> Option<String> {
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

/// Build a `Set-Cookie` value with the standard attributes.
pub fn set_cookie(name: &str, value: &str, max_age: i64) -> String {
    format!("{name}={value}; Path=/; Max-Age={max_age}; HttpOnly; Secure; SameSite=Lax")
}

/// Build a `Set-Cookie` value that deletes a cookie (matches Go's
/// `clearCookie`, which uses `MaxAge < 0` => `Max-Age=0`).
pub fn clear_cookie(name: &str) -> String {
    format!("{name}=; Path=/; Max-Age=0; HttpOnly; Secure; SameSite=Lax")
}

/// Append a `Set-Cookie` header (multiple are allowed per response).
pub fn append_set_cookie(headers: &mut HeaderMap, value: &str) {
    if let Ok(hv) = value.parse() {
        headers.append(header::SET_COOKIE, hv);
    }
}
