//! RustLens servers service.
//!
//! Tracked BattleMetrics servers, live player presence, server analytics, and
//! the outbound-proxy infra (with the BM poller + daily analytics refresher).
//! Talks to the same Postgres as the Go backend (schema owned by Go). PORT 8092.

mod auth;
mod state;
mod tracked;

use std::sync::Arc;

use anyhow::Context;
use axum::{
    Router,
    routing::{get, post, put},
};
use rustlens_config::ServersConfig;

use state::AppState;

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    tracing_subscriber::fmt()
        .with_env_filter(
            tracing_subscriber::EnvFilter::try_from_default_env()
                .unwrap_or_else(|_| "servers=info,tower_http=info".into()),
        )
        .init();

    let config = ServersConfig::from_env().context("loading configuration")?;
    let pool = rustlens_db::connect(&config.postgres_url, 10)
        .await
        .context("connecting to Postgres")?;

    let state = Arc::new(AppState { config, pool });

    let app = Router::new()
        .route("/api/healthz", get(healthz))
        // ---- global tracked servers ----
        .route(
            "/api/internal/bot/setup/tracked-servers",
            get(tracked::list_tracked_servers).post(tracked::create_tracked_server),
        )
        .route(
            "/api/internal/bot/setup/tracked-servers/{bmID}",
            put(tracked::update_tracked_server).delete(tracked::delete_tracked_server),
        )
        // ---- guild tracked servers ----
        .route(
            "/api/internal/bot/setup/guilds/{guildID}/tracked-servers",
            get(tracked::list_guild_tracked_servers).put(tracked::replace_guild_tracked_servers),
        )
        .route(
            "/api/internal/bot/setup/guilds/{guildID}/tracked-servers/{bmID}",
            post(tracked::add_guild_tracked_server).delete(tracked::remove_guild_tracked_server),
        )
        .with_state(state.clone());

    let addr = format!("0.0.0.0:{}", state.config.port);
    let listener = tokio::net::TcpListener::bind(&addr)
        .await
        .with_context(|| format!("binding {addr}"))?;
    tracing::info!("servers service listening on {addr}");
    axum::serve(listener, app).await.context("serving")?;
    Ok(())
}

async fn healthz() -> &'static str {
    "ok"
}
