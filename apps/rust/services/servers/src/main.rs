//! RustLens servers service.
//!
//! Tracked BattleMetrics servers, live player presence, server analytics, and
//! the outbound-proxy infra (with the BM poller + daily analytics refresher).
//! Talks to the same Postgres as the Go backend (schema owned by Go). PORT 8092.

mod auth;
mod player_states;
mod poller;
mod redis_lock;
mod state;
mod tracked;

use std::sync::Arc;
use std::time::Duration;

use anyhow::Context;
use axum::{
    Router,
    routing::{get, post, put},
};
use rustlens_config::ServersConfig;

use state::AppState;

/// BattleMetrics HTTP client timeout (Go's outbound fallback).
const BM_HTTP_TIMEOUT: Duration = Duration::from_secs(20);

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

    // Outbound proxy provider (BattleMetrics only) + BM client.
    let cipher = match config.proxy_encryption_key.as_deref() {
        Some(key) => Some(
            rustlens_crypto::Cipher::from_base64_key(key).context("PROXY_ENCRYPTION_KEY")?,
        ),
        None => None,
    };
    let endpoints = rustlens_outbound::load_initial_endpoints(
        &pool,
        cipher.as_ref(),
        config.proxy_enabled,
        &config.proxy_endpoints,
    )
    .await
    .context("loading outbound proxies")?;
    let provider = rustlens_outbound::Provider::new(!endpoints.is_empty(), &endpoints, BM_HTTP_TIMEOUT)
        .context("building outbound provider")?;
    let bm = rustlens_battlemetrics::Client::new(
        Arc::new(provider) as Arc<dyn rustlens_outbound::OutboundSender>,
    );

    // Redis for the worker locks (fails to single-pod mode when unavailable).
    let redis = match redis::Client::open(config.redis_url.as_str()) {
        Ok(client) => match client.get_multiplexed_async_connection().await {
            Ok(conn) => Some(conn),
            Err(e) => {
                tracing::warn!("redis unavailable, workers run single-pod: {e}");
                None
            }
        },
        Err(e) => {
            tracing::warn!("invalid REDIS_URL, workers run single-pod: {e}");
            None
        }
    };

    let state = Arc::new(AppState { config, pool, bm, redis });

    // Spawn the BattleMetrics rolling poller.
    tokio::spawn(poller::run(state.clone()));

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
        // ---- player presence ----
        .route(
            "/api/internal/bot/setup/player-states/servers/{serverID}",
            get(player_states::list_online_by_server),
        )
        .route(
            "/api/internal/bot/setup/player-states/servers/{serverID}/search",
            get(player_states::search_states_on_server),
        )
        .route(
            "/api/internal/bot/setup/player-states/servers/{serverID}/players/{playerID}",
            get(player_states::get_state),
        )
        .route(
            "/api/internal/bot/setup/player-states/players/{playerID}",
            get(player_states::list_states_by_player),
        )
        .route(
            "/api/internal/bot/setup/guilds/{guildID}/online-summary",
            get(player_states::list_online_summary),
        )
        .route(
            "/api/internal/bot/setup/guilds/{guildID}/tracked-server-log-events",
            get(player_states::list_tracked_server_log_events),
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
