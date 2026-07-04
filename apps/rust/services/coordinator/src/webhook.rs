//! Discord webhook-events (`POST /api/discord/webhook-events`). Top-level route,
//! Ed25519-signature-authed in the handler (over `timestamp || raw-body`, ±5-min
//! replay guard). Persists user installs into `bot_user_installs`. Mirrors Go's
//! DiscordWebhookEvents. Success is always `204` with an empty body.

use std::sync::Arc;

use axum::body::Bytes;
use axum::extract::State;
use axum::http::{HeaderMap, StatusCode};
use axum::response::{IntoResponse, Response};
use chrono::Utc;
use ed25519_dalek::{Signature, Verifier, VerifyingKey};
use serde::Deserialize;
use serde_json::Value;

use rustlens_db::bot_user_installs::{self, UpsertInput};

use crate::auth::{err_json, is_snowflake};
use crate::state::AppState;

type Shared = State<Arc<AppState>>;

const REPLAY_WINDOW_SECS: i64 = 300; // ±5 minutes

fn no_content() -> Response {
    StatusCode::NO_CONTENT.into_response()
}

fn header<'a>(headers: &'a HeaderMap, name: &str) -> &'a str {
    headers.get(name).and_then(|v| v.to_str().ok()).unwrap_or("")
}

#[derive(Deserialize)]
struct WebhookPayload {
    #[serde(default, rename = "type")]
    typ: i64,
    #[serde(default)]
    event: Option<WebhookEvent>,
}

#[derive(Deserialize)]
struct WebhookEvent {
    #[serde(default, rename = "type")]
    typ: String,
    #[serde(default)]
    data: Value,
}

#[derive(Deserialize)]
struct WebhookUser {
    #[serde(default)]
    id: String,
    #[serde(default)]
    username: String,
    #[serde(default, rename = "global_name")]
    global_name: Option<String>,
    #[serde(default)]
    avatar: Option<String>,
}

#[derive(Deserialize)]
struct AppAuthorizedData {
    #[serde(default, rename = "integration_type")]
    integration_type: Option<i64>,
    #[serde(default)]
    scopes: Vec<String>,
    user: WebhookUser,
}

#[derive(Deserialize)]
struct AppDeauthorizedData {
    user: WebhookUser,
}

pub async fn discord_webhook_events(State(st): Shared, headers: HeaderMap, body: Bytes) -> Response {
    // 1-2. Public key configured + valid (hex, 32 bytes).
    let pubkey_hex = st.config.discord_app_public_key.trim();
    if pubkey_hex.is_empty() {
        return err_json(StatusCode::SERVICE_UNAVAILABLE, "discord webhook public key not configured");
    }
    let vk = match hex::decode(pubkey_hex).ok().and_then(|b| <[u8; 32]>::try_from(b).ok()) {
        Some(bytes) => match VerifyingKey::from_bytes(&bytes) {
            Ok(k) => k,
            Err(_) => {
                tracing::error!("discord webhook: invalid DISCORD_APP_PUBLIC_KEY");
                return err_json(StatusCode::SERVICE_UNAVAILABLE, "discord webhook public key invalid");
            }
        },
        None => {
            tracing::error!("discord webhook: invalid DISCORD_APP_PUBLIC_KEY");
            return err_json(StatusCode::SERVICE_UNAVAILABLE, "discord webhook public key invalid");
        }
    };

    // 3. Signature headers.
    let sig_hex = header(&headers, "X-Signature-Ed25519");
    let ts = header(&headers, "X-Signature-Timestamp");
    if sig_hex.is_empty() || ts.is_empty() {
        return err_json(StatusCode::UNAUTHORIZED, "missing signature headers");
    }
    // 4. Decode signature (64 bytes).
    let sig = match hex::decode(sig_hex).ok().and_then(|b| <[u8; 64]>::try_from(b).ok()) {
        Some(bytes) => Signature::from_bytes(&bytes),
        None => return err_json(StatusCode::UNAUTHORIZED, "invalid signature encoding"),
    };
    // 5. Parse timestamp.
    let ts_secs: i64 = match ts.parse() {
        Ok(n) => n,
        Err(_) => return err_json(StatusCode::UNAUTHORIZED, "invalid signature timestamp"),
    };
    // 6. Replay window (±5 min).
    let drift = Utc::now().timestamp() - ts_secs;
    if !(-REPLAY_WINDOW_SECS..=REPLAY_WINDOW_SECS).contains(&drift) {
        return err_json(StatusCode::UNAUTHORIZED, "signature timestamp out of range");
    }
    // 8. Verify over `timestamp-header || raw-body`.
    let mut signed = ts.as_bytes().to_vec();
    signed.extend_from_slice(&body);
    if vk.verify(&signed, &sig).is_err() {
        return err_json(StatusCode::UNAUTHORIZED, "invalid signature");
    }

    // Parse the (now-authenticated) payload.
    let payload: WebhookPayload = match serde_json::from_slice(&body) {
        Ok(p) => p,
        Err(_) => return err_json(StatusCode::BAD_REQUEST, "invalid json"),
    };

    // PING (0) → 204 (registration handshake). Unknown/unsupported → 204.
    if payload.typ == 0 {
        return no_content();
    }
    let Some(event) = payload.event else {
        return no_content();
    };
    if payload.typ != 1 {
        return no_content();
    }

    match event.typ.as_str() {
        "APPLICATION_AUTHORIZED" => {
            let data: AppAuthorizedData = match serde_json::from_value(event.data) {
                Ok(d) => d,
                Err(_) => return err_json(StatusCode::BAD_REQUEST, "invalid authorized data"),
            };
            // Only user installs (integration_type == 1); guild installs arrive via
            // the bot gateway.
            if data.integration_type != Some(1) {
                return no_content();
            }
            if !is_snowflake(&data.user.id) {
                return err_json(StatusCode::BAD_REQUEST, "user.id must be a Discord snowflake");
            }
            let input = UpsertInput {
                discord_id: data.user.id,
                username: data.user.username,
                global_name: data.user.global_name,
                avatar_hash: data.user.avatar,
                scopes: data.scopes,
            };
            if let Err(e) = bot_user_installs::upsert_install(&st.pool, &input).await {
                tracing::error!("coordinator/webhook upsert install: {e}");
                return err_json(StatusCode::INTERNAL_SERVER_ERROR, "failed to upsert user install");
            }
            no_content()
        }
        "APPLICATION_DEAUTHORIZED" => {
            let data: AppDeauthorizedData = match serde_json::from_value(event.data) {
                Ok(d) => d,
                Err(_) => return err_json(StatusCode::BAD_REQUEST, "invalid deauthorized data"),
            };
            if !is_snowflake(&data.user.id) {
                return err_json(StatusCode::BAD_REQUEST, "user.id must be a Discord snowflake");
            }
            if let Err(e) = bot_user_installs::mark_removed(&st.pool, &data.user.id).await {
                tracing::error!("coordinator/webhook mark removed: {e}");
                return err_json(StatusCode::INTERNAL_SERVER_ERROR, "failed to mark user install removed");
            }
            no_content()
        }
        _ => no_content(),
    }
}
