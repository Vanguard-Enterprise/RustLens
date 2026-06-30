//! RustLens identity service.
//!
//! Discord-OAuth login that mints the shared `rustlens_session` cookie, plus
//! `/api/app/me`. Talks to the same Postgres as the Go backend.

mod admin;
mod appauth;
mod cookies;
mod discord;
mod events_handler;
mod handlers;
mod state;

use std::sync::Arc;

use anyhow::Context;
use axum::{Router, routing::get};
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

    let state = Arc::new(AppState {
        config,
        pool,
        http: reqwest::Client::new(),
        hub,
        events,
    });

    let app = Router::new()
        .route("/api/healthz", get(healthz))
        .route("/api/auth/discord", get(handlers::initiate))
        .route("/api/auth/discord/callback", get(handlers::callback))
        .route("/api/app/me", get(handlers::me))
        .route("/api/app/admin/permissions", get(admin::list_permissions))
        .route("/api/app/admin/roles", get(admin::list_roles))
        .route("/api/app/admin/users", get(admin::list_users))
        .route("/api/app/admin/administrators", get(admin::list_administrators))
        .route("/api/app/admin/audit", get(admin::list_audit))
        .route("/api/events/stream", get(events_handler::stream))
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
