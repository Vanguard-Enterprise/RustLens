//! RustLens servers service.
//!
//! Tracked BattleMetrics servers, live player presence, server analytics, and
//! the outbound-proxy infra (with the BM poller + daily analytics refresher).
//! Talks to the same Postgres as the Go backend (schema owned by Go). PORT 8092.

mod admin_servers;
mod auth;
mod bmservers;
mod player_states;
mod poller;
mod proxies;
mod redis_lock;
mod server_analytics;
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
    let provider = Arc::new(
        rustlens_outbound::Provider::new(!endpoints.is_empty(), &endpoints, BM_HTTP_TIMEOUT)
            .context("building outbound provider")?,
    );
    let bm = rustlens_battlemetrics::Client::new(
        provider.clone() as Arc<dyn rustlens_outbound::OutboundSender>,
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

    let state = Arc::new(AppState { config, pool, bm, outbound: provider, cipher, redis });

    // Spawn the background workers: the poller + the daily analytics refresher.
    tokio::spawn(poller::run(state.clone()));
    tokio::spawn(server_analytics::run_refresher(state.clone()));

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
        // ---- server analytics ----
        .route(
            "/api/internal/bot/setup/tracked-servers/{bmID}/player-count-history",
            get(server_analytics::get_player_count_analytics),
        )
        // ---- owner proxy routes (bot JWT) ----
        .route(
            "/api/internal/bot/owner/proxies",
            get(proxies::list_owner_proxies).post(proxies::create_owner_proxy),
        )
        .route(
            "/api/internal/bot/owner/proxies/{proxyID}",
            axum::routing::patch(proxies::update_owner_proxy).delete(proxies::delete_owner_proxy),
        )
        // ---- admin proxy routes (app-secret + session + permission) ----
        .route(
            "/api/app/admin/proxies",
            get(proxies::list_admin_proxies).post(proxies::create_admin_proxy),
        )
        .route("/api/app/admin/proxies/bulk", post(proxies::bulk_admin_proxies))
        .route(
            "/api/app/admin/proxies/{proxyID}",
            axum::routing::patch(proxies::update_admin_proxy).delete(proxies::delete_admin_proxy),
        )
        // ---- admin bm-servers routes (app-secret + session + permission) ----
        .route(
            "/api/app/admin/bm-servers",
            get(bmservers::list_admin_bm_servers).post(bmservers::create_admin_bm_server),
        )
        .route("/api/app/admin/bm-servers/bulk", post(bmservers::bulk_admin_bm_servers))
        .route(
            "/api/app/admin/bm-servers/{bmID}",
            put(bmservers::update_admin_bm_server).delete(bmservers::delete_admin_bm_server),
        )
        // ---- admin servers list (app-secret + session + bm_servers.read) ----
        .route("/api/app/admin/servers", get(admin_servers::list_admin_servers))
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
