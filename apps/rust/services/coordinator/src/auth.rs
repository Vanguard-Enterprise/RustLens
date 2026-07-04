//! Shared HTTP helpers: bot-JWT gate (returns the claims for the `worker_id`
//! fallback), JSON errors, request parsing, snowflake validator.

use axum::Json;
use axum::body::Bytes;
use axum::http::{HeaderMap, StatusCode, header};
use axum::response::{IntoResponse, Response};
use serde::de::DeserializeOwned;
use serde_json::json;

use rustlens_botjwt::BotClaims;
use rustlens_config::CoordinatorConfig;

pub fn err_json(status: StatusCode, msg: &str) -> Response {
    (status, Json(json!({ "error": msg }))).into_response()
}

/// Validate the bot service JWT and return its claims (for the `worker_id`
/// fallback). Boxed error keeps the `Ok` path small (clippy `result_large_err`).
pub fn require_bot_jwt(
    headers: &HeaderMap,
    config: &CoordinatorConfig,
) -> Result<BotClaims, Box<Response>> {
    let auth = headers
        .get(header::AUTHORIZATION)
        .and_then(|v| v.to_str().ok())
        .unwrap_or("");
    let token = rustlens_botjwt::extract_bearer(auth)
        .map_err(|e| Box::new(err_json(StatusCode::UNAUTHORIZED, &e.to_string())))?;
    rustlens_botjwt::validate(
        token,
        &config.bot_service_jwt_secret,
        &config.bot_jwt_issuer,
        &config.bot_jwt_audience,
    )
    .map_err(|e| Box::new(err_json(StatusCode::UNAUTHORIZED, &e.to_string())))
}

/// Parse a JSON body → `400 invalid request body` (boxed) on failure.
pub fn parse_json<T: DeserializeOwned>(body: &Bytes) -> Result<T, Box<Response>> {
    serde_json::from_slice(body)
        .map_err(|_| Box::new(err_json(StatusCode::BAD_REQUEST, "invalid request body")))
}

/// `^\d{17,20}$` (Discord snowflake).
pub fn is_snowflake(s: &str) -> bool {
    (17..=20).contains(&s.len()) && s.bytes().all(|b| b.is_ascii_digit())
}

/// `validateSnowflakeValue`: non-empty AND a valid snowflake.
pub fn is_snowflake_value(v: &str) -> bool {
    !v.is_empty() && is_snowflake(v)
}
