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
    let _otel_guard = rustlens_otel::init("rustlens-servers", "servers=info,tower_http=info");

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

    // Wire the outbound provider's auto-disable callback (Go
    // `cmd/server/main.go` ~line 129, `SetAutoDisableCallback`): persist the
    // disable, then reload the provider from the DB (falling back to the
    // legacy env proxies when no DB rows are enabled) via the same `reload`
    // helper the proxy admin/owner routes use. A failed persist or reload is
    // logged and left for the next auto-disable/admin reload to retry, same
    // as Go.
    {
        let cb_state = state.clone();
        state.outbound.set_auto_disable_callback(Arc::new(move |redacted: String| {
            let state = cb_state.clone();
            Box::pin(async move {
                let disabled = match rustlens_db::outbound_proxies::disable_by_redacted_endpoint(
                    &state.pool,
                    &redacted,
                )
                .await
                {
                    Ok(disabled) => disabled,
                    Err(e) => {
                        tracing::error!("servers: auto-disable proxy {redacted:?} persist failed: {e}");
                        return;
                    }
                };
                if !disabled {
                    return;
                }
                if let Err(e) = proxies::reload(&state).await {
                    tracing::error!("servers: reload proxies after auto-disable {redacted:?} failed: {e}");
                    return;
                }
                tracing::warn!(
                    "servers: auto-disabled outbound proxy {redacted:?} after >=50% errors in 10m"
                );
            })
        }));
    }

    // Spawn the background workers: the poller + the daily analytics refresher.
    tokio::spawn(poller::run(state.clone()));
    tokio::spawn(server_analytics::run_refresher(state.clone()));

    let app = build_router().with_state(state.clone());

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

/// Builds the full servers router (state left unresolved so tests can
/// construct it without a live pool). Adding the routes is what triggers any
/// matchit conflict panic, so `router_builds` below constructs this at test
/// time and fails `cargo test` instead of failing at service boot.
fn build_router() -> Router<Arc<AppState>> {
    let router = Router::new().route("/api/healthz", get(healthz));

    // ---- global tracked servers ----
    let router = router.route(
        "/api/internal/bot/servers/tracked-servers",
        get(tracked::list_tracked_servers).post(tracked::create_tracked_server),
    );
    let router = router.route(
        "/api/internal/bot/servers/tracked-servers/{bmID}",
        put(tracked::update_tracked_server).delete(tracked::delete_tracked_server),
    );
    // ---- server analytics ----
    let router = router.route(
        "/api/internal/bot/servers/tracked-servers/{bmID}/player-count-history",
        get(server_analytics::get_player_count_analytics),
    );

    // ---- guild tracked servers ----
    let router = router.route(
        "/api/internal/bot/servers/guilds/{guildID}/tracked-servers",
        get(tracked::list_guild_tracked_servers).put(tracked::replace_guild_tracked_servers),
    );
    let router = router.route(
        "/api/internal/bot/servers/guilds/{guildID}/tracked-servers/{bmID}",
        post(tracked::add_guild_tracked_server).delete(tracked::remove_guild_tracked_server),
    );
    let router = router.route(
        "/api/internal/bot/servers/guilds/{guildID}/online-summary",
        get(player_states::list_online_summary),
    );
    let router = router.route(
        "/api/internal/bot/servers/guilds/{guildID}/tracked-server-log-events",
        get(player_states::list_tracked_server_log_events),
    );

    // ---- player presence ----
    let router = router.route(
        "/api/internal/bot/servers/player-states/servers/{serverID}",
        get(player_states::list_online_by_server),
    );
    let router = router.route(
        "/api/internal/bot/servers/player-states/servers/{serverID}/search",
        get(player_states::search_states_on_server),
    );
    let router = router.route(
        "/api/internal/bot/servers/player-states/servers/{serverID}/players/{playerID}",
        get(player_states::get_state),
    );
    let router = router.route(
        "/api/internal/bot/servers/player-states/players/{playerID}",
        get(player_states::list_states_by_player),
    );

    // ---- owner proxy routes (bot JWT) ----
    let router = router.route(
        "/api/internal/bot/servers/owner/proxies",
        get(proxies::list_owner_proxies).post(proxies::create_owner_proxy),
    );
    let router = router.route(
        "/api/internal/bot/servers/owner/proxies/{proxyID}",
        axum::routing::patch(proxies::update_owner_proxy).delete(proxies::delete_owner_proxy),
    );

    // ---- admin proxy routes (app-secret + session + permission); not part of
    // the bot-internal reorg ----
    router
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
}

#[cfg(test)]
mod tests {
    use super::build_router;

    /// Constructing the router adds every route to matchit, so a bare-param /
    /// literal-segment conflict panics here in `cargo test` rather than at
    /// service boot.
    #[test]
    fn router_builds() {
        let _ = build_router();
    }
}
