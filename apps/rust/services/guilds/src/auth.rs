//! Shared HTTP helpers: bot-JWT gate, JSON errors, request parsing, snowflake
//! validators. Mirrors the Go `setup`-group conventions.

use axum::Json;
use axum::body::Bytes;
use axum::http::{HeaderMap, StatusCode, header};
use axum::response::{IntoResponse, Response};
use serde::de::DeserializeOwned;
use serde_json::json;

use rustlens_config::GuildsConfig;

pub fn err_json(status: StatusCode, msg: &str) -> Response {
    (status, Json(json!({ "error": msg }))).into_response()
}

/// Validate the bot service JWT. Returns `Some(response)` (401) on failure.
pub fn require_bot_jwt(headers: &HeaderMap, config: &GuildsConfig) -> Option<Response> {
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

/// Parse a JSON body, returning a boxed `400 invalid request body` response on
/// failure (boxed to keep the `Ok` path small — clippy `result_large_err`).
/// Note: Go emits `invalid request body` here, not `invalid json body`.
pub fn parse_json<T: DeserializeOwned>(body: &Bytes) -> Result<T, Box<Response>> {
    serde_json::from_slice(body)
        .map_err(|_| Box::new(err_json(StatusCode::BAD_REQUEST, "invalid request body")))
}

/// `^\d{17,20}$` (Discord snowflake).
pub fn is_snowflake(s: &str) -> bool {
    (17..=20).contains(&s.len()) && s.bytes().all(|b| b.is_ascii_digit())
}

/// `validateSnowflakeValue`: non-empty AND a valid snowflake (for body fields).
pub fn is_snowflake_value(v: &str) -> bool {
    !v.is_empty() && is_snowflake(v)
}

/// `validateGuildID`: empty → `guildID is required`; bad → `... must be a valid
/// Discord snowflake`. Returns `Some(response)` on failure.
pub fn validate_guild_id(guild_id: &str) -> Option<Response> {
    validate_snowflake_path("guildID", guild_id)
}

/// `validateSnowflakePathParam(name)`: empty → `<name> is required`; bad →
/// `<name> must be a valid Discord snowflake`.
pub fn validate_snowflake_path(name: &str, value: &str) -> Option<Response> {
    if value.is_empty() {
        return Some(err_json(StatusCode::BAD_REQUEST, &format!("{name} is required")));
    }
    if !is_snowflake(value) {
        return Some(err_json(
            StatusCode::BAD_REQUEST,
            &format!("{name} must be a valid Discord snowflake"),
        ));
    }
    None
}
