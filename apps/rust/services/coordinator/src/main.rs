//! RustLens coordinator service.
//!
//! Bot shard coordination (Redis leases + round-robin allocation), owner stats,
//! rate-limit consume, Discord webhook events, and the cleanup retention sweep.
//! Shares the same Postgres + Redis as the Go backend (schema owned by Go).
//! PORT 8094.

mod auth;
mod leases;
mod owner_stats;
mod ratelimit;
mod shards;
mod state;

use std::sync::Arc;

use anyhow::Context;
use axum::{
    Router,
    routing::{get, post},
};
use rustlens_config::CoordinatorConfig;

use state::AppState;

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    tracing_subscriber::fmt()
        .with_env_filter(
            tracing_subscriber::EnvFilter::try_from_default_env()
                .unwrap_or_else(|_| "coordinator=info,tower_http=info".into()),
        )
        .init();

    let config = CoordinatorConfig::from_env().context("loading configuration")?;
    let pool = rustlens_db::connect(&config.postgres_url, 10)
        .await
        .context("connecting to Postgres")?;

    // Redis holds the shard lease state + rate-limit counters (required for the
    // shard routes; falls back to single-pod for the cleanup worker).
    let redis = match redis::Client::open(config.redis_url.as_str()) {
        Ok(client) => match client.get_multiplexed_async_connection().await {
            Ok(conn) => Some(conn),
            Err(e) => {
                tracing::warn!("redis unavailable: shard/rate-limit routes will error: {e}");
                None
            }
        },
        Err(e) => {
            tracing::warn!("invalid REDIS_URL: shard/rate-limit routes will error: {e}");
            None
        }
    };

    let state = Arc::new(AppState { config, pool, redis });

    let app = Router::new()
        .route("/api/healthz", get(healthz))
        // ---- shard coordination (bot JWT) ----
        .route("/api/internal/bot/shards/register", post(shards::register_shard_worker))
        .route("/api/internal/bot/shards/heartbeat", post(shards::heartbeat_shard_worker))
        .route("/api/internal/bot/shards/release", post(shards::release_shard_worker))
        .route("/api/internal/bot/shards/assignments", get(shards::get_shard_assignments))
        // ---- owner stats (bot JWT) ----
        .route("/api/internal/bot/owner/stats", get(owner_stats::get_owner_stats))
        // ---- rate-limit consume (bot JWT) ----
        .route(
            "/api/internal/bot/setup/rate-limit/consume",
            post(ratelimit::consume_rate_limit),
        )
        .with_state(state.clone());

    let addr = format!("0.0.0.0:{}", state.config.port);
    let listener = tokio::net::TcpListener::bind(&addr)
        .await
        .with_context(|| format!("binding {addr}"))?;
    tracing::info!("coordinator service listening on {addr}");
    axum::serve(listener, app).await.context("serving")?;
    Ok(())
}

async fn healthz() -> &'static str {
    "ok"
}
