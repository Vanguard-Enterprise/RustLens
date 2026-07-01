//! RustLens identity service.
//!
//! Discord-OAuth login that mints the shared `rustlens_session` cookie, plus
//! `/api/app/me`. Talks to the same Postgres as the Go backend.

mod admin;
mod appauth;
mod bot;
mod cookies;
mod discord;
mod dispatcher;
mod events_handler;
mod handlers;
mod ratelimit;
mod role_connection;
mod state;

use std::sync::Arc;

use anyhow::Context;
use axum::{
    Router,
    routing::{delete, get, post},
};
use rustlens_config::IdentityConfig;

use state::AppState;

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    tracing_subscriber::fmt()
        .with_env_filter(
            tracing_subscriber::EnvFilter::try_from_default_env()
                .unwrap_or_else(|_| "identity=info,tower_http=info".into()),
        )
        .init();

    let config = IdentityConfig::from_env().context("loading configuration")?;
    let pool = rustlens_db::connect(&config.postgres_url, 10)
        .await
        .context("connecting to Postgres")?;

    let hub = Arc::new(rustlens_events::Hub::new());
    let events = rustlens_events::RedisBridge::start(hub.clone(), &config.redis_url);

    // Build the role-connection cipher (linked-roles feature) when configured.
    let cipher = match config.proxy_encryption_key.as_deref() {
        Some(key) => match rustlens_crypto::Cipher::from_base64_key(key) {
            Ok(c) => Some(c),
            Err(e) => {
                tracing::error!("invalid PROXY_ENCRYPTION_KEY, linked-roles disabled: {e}");
                None
            }
        },
        None => None,
    };

    let state = Arc::new(AppState {
        config,
        pool,
        http: reqwest::Client::new(),
        hub,
        events,
        cipher,
        rate_limiter: ratelimit::RateLimiter::new(),
        idempotency: ratelimit::IdempotencyCache::new(),
    });

    // Register the linked-roles metadata schema once at startup (best-effort).
    if state.config.role_connection_enabled() && state.config.discord_bot_token.is_some() {
        match role_connection::register_schema(&state).await {
            Ok(()) => tracing::info!("linked-roles metadata schema registered"),
            Err(e) => tracing::warn!("linked-roles metadata schema registration failed: {e:#}"),
        }
    }

    // Spawn the linked-roles reconciler (Phase 4b): re-push metadata on startup
    // and every 6h, advisory-locked so only one pod runs at a time.
    if state.config.role_connection_enabled() {
        tokio::spawn(role_connection::reconcile_loop(state.clone()));
    }

    let app = Router::new()
        .route("/api/healthz", get(healthz))
        .route("/api/auth/discord", get(handlers::initiate))
        .route("/api/auth/discord/callback", get(handlers::callback))
        .route("/api/app/me", get(handlers::me))
        .route(
            "/api/app/me/preferences",
            get(handlers::get_preferences).patch(handlers::patch_preferences),
        )
        .route("/api/app/auth/logout", post(handlers::logout))
        .route("/api/app/admin/permissions", get(admin::list_permissions))
        .route("/api/app/admin/roles", get(admin::list_roles))
        .route("/api/app/admin/users", get(admin::list_users))
        .route("/api/app/admin/administrators", get(admin::list_administrators))
        .route("/api/app/admin/audit", get(admin::list_audit))
        .route("/api/app/admin/audit/event-types", get(admin::list_audit_event_types))
        .route("/api/app/admin/audit/{id}", get(admin::get_audit_event))
        .route("/api/app/admin/users/{id}", get(admin::get_user))
        .route("/api/app/admin/users/{id}/audit", get(admin::get_user_audit))
        .route(
            "/api/app/admin/users/{id}/command-usage",
            get(admin::get_user_command_usage),
        )
        .route(
            "/api/app/admin/users/{id}/command-usage/filter-options",
            get(admin::get_user_command_usage_filter_options),
        )
        .route(
            "/api/app/admin/users/{id}/command-usage/{usageId}/targets",
            get(admin::get_command_usage_targets),
        )
        .route(
            "/api/app/admin/users/{id}/command-usage/{usageId}/children",
            get(admin::get_command_usage_children),
        )
        .route("/api/app/admin/actions/{type}", post(dispatcher::dispatch))
        .route("/api/auth/discord/role-connection", get(role_connection::initiate))
        .route(
            "/api/auth/discord/role-connection/callback",
            get(role_connection::callback),
        )
        .route(
            "/api/app/me/discord-role-connection",
            delete(role_connection::disconnect),
        )
        .route("/api/events/stream", get(events_handler::stream))
        // Bot-internal (JWT-authed) identity routes.
        .route(
            "/api/internal/bot/setup/command-usage",
            post(bot::record_command_usage),
        )
        .route(
            "/api/internal/bot/setup/bans/{discordID}",
            get(bot::get_user_ban).put(bot::update_user_ban),
        )
        .route(
            "/api/internal/bot/setup/discord/{discordID}/is-website-admin",
            get(bot::is_website_admin),
        )
        .route(
            "/api/internal/bot/setup/discord/{discordID}/has-pro-role",
            get(bot::has_pro_role),
        )
        .with_state(state.clone());

    let addr = format!("0.0.0.0:{}", state.config.port);
    let listener = tokio::net::TcpListener::bind(&addr)
        .await
        .with_context(|| format!("binding {addr}"))?;
    tracing::info!("identity service listening on {addr}");
    axum::serve(listener, app).await.context("serving")?;
    Ok(())
}

async fn healthz() -> &'static str {
    "ok"
}
