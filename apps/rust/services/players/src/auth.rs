//! Shared HTTP helpers: bot-JWT gate, JSON error responses, request parsing, and
//! path-parameter validators. Mirrors the Go bot-facing middleware + validators.

use axum::Json;
use axum::body::Bytes;
use axum::http::{HeaderMap, StatusCode, header};
use axum::response::{IntoResponse, Response};
use serde::de::DeserializeOwned;
use serde_json::json;

use rustlens_config::PlayersConfig;

/// A `{"error": msg}` JSON body with the given status.
pub fn err_json(status: StatusCode, msg: &str) -> Response {
    (status, Json(json!({ "error": msg }))).into_response()
}

/// Validate the bot service JWT. Returns `Some(response)` on failure (401).
pub fn require_bot_jwt(headers: &HeaderMap, config: &PlayersConfig) -> Option<Response> {
    let auth = headers
        .get(header::AUTHORIZATION)
        .and_then(|v| v.to_str().ok())
        .unwrap_or("");
    let token = match rustlens_botjwt::extract_bearer(auth) {
        Ok(t) => t,
        Err(e) => return Some(err_json(StatusCode::UNAUTHORIZED, &e.to_string())),
    };
    match rustlens_botjwt::validate(
        token,
        &config.bot_service_jwt_secret,
        &config.bot_jwt_issuer,
        &config.bot_jwt_audience,
    ) {
        Ok(_) => None,
        Err(e) => Some(err_json(StatusCode::UNAUTHORIZED, &e.to_string())),
    }
}

/// Parse a JSON request body, returning a boxed `400 invalid json body` response
/// on failure (boxed to keep the `Ok` hot path small — clippy `result_large_err`).
pub fn parse_json<T: DeserializeOwned>(body: &Bytes) -> Result<T, Box<Response>> {
    serde_json::from_slice(body)
        .map_err(|_| Box::new(err_json(StatusCode::BAD_REQUEST, "invalid json body")))
}

/// `^\d{17,20}$` (Discord snowflake).
pub fn is_snowflake(s: &str) -> bool {
    (17..=20).contains(&s.len()) && s.bytes().all(|b| b.is_ascii_digit())
}

/// `^\d{17}$` (Steam64).
pub fn is_steam64(s: &str) -> bool {
    s.len() == 17 && s.bytes().all(|b| b.is_ascii_digit())
}
