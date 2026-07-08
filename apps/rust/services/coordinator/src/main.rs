//! RustLens coordinator service.
//!
//! Bot shard coordination (Redis leases + round-robin allocation), owner stats,
//! rate-limit consume, Discord webhook events, and the cleanup retention sweep.
//! Shares the same Postgres + Redis as the Go backend (schema owned by Go).
//! PORT 8094.

mod auth;
mod cleanup;
mod leases;
mod owner_stats;
mod ratelimit;
mod redis_lock;
mod shards;
mod state;
mod webhook;

use std::sync::Arc;

use anyhow::Context;
use axum::{
    Router,
    routing::{MethodRouter, get, post},
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

    // Spawn the background retention-sweep worker.
    tokio::spawn(cleanup::run(state.clone()));

    let app = build_router().with_state(state.clone());

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

/// Registers a bot-internal route under BOTH the legacy flat path and the new
/// `coordinator/` domain-prefix path (D1 domain-prefix reorg). The flat paths
/// are dropped wholesale in Phase E once every domain's bot deploy has cut
/// over; until then Rust serves both so the bot deploy is the only cutover
/// moment and reverting the bot is a full rollback.
///
/// `MethodRouter` is `Clone`, so the same handler(s) back both paths. Callers
/// pass `old` and `new` explicitly rather than deriving one from the other,
/// keeping the reorg auditable (see `players::main::dual_route` for the
/// pattern this mirrors).
///
/// Coordinator has no bare-param routes at all (every segment is a literal),
/// so unlike players/servers/guilds there is no `bm/`-style matchit
/// bare-param-vs-literal conflict to avoid here.
fn dual_route(
    router: Router<Arc<AppState>>,
    old_path: &str,
    new_path: &str,
    method: MethodRouter<Arc<AppState>>,
) -> Router<Arc<AppState>> {
    router.route(old_path, method.clone()).route(new_path, method)
}

/// Builds the full coordinator router (state left unresolved so tests can
/// construct it without a live pool/Redis connection). Adding the routes is
/// what triggers any matchit conflict panic, so `router_builds` below
/// constructs this at test time and fails `cargo test` instead of failing at
/// service boot.
fn build_router() -> Router<Arc<AppState>> {
    let router = Router::new()
        .route("/api/healthz", get(healthz))
        // ---- Discord webhook events (Ed25519 signature in-handler); not part
        // of the bot-internal reorg ----
        .route("/api/discord/webhook-events", post(webhook::discord_webhook_events));

    // ---- shard coordination (bot JWT) ----
    let router = dual_route(
        router,
        "/api/internal/bot/shards/register",
        "/api/internal/bot/coordinator/shards/register",
        post(shards::register_shard_worker),
    );
    let router = dual_route(
        router,
        "/api/internal/bot/shards/heartbeat",
        "/api/internal/bot/coordinator/shards/heartbeat",
        post(shards::heartbeat_shard_worker),
    );
    let router = dual_route(
        router,
        "/api/internal/bot/shards/release",
        "/api/internal/bot/coordinator/shards/release",
        post(shards::release_shard_worker),
    );
    let router = dual_route(
        router,
        "/api/internal/bot/shards/assignments",
        "/api/internal/bot/coordinator/shards/assignments",
        get(shards::get_shard_assignments),
    );

    // ---- owner stats (bot JWT) ----
    let router = dual_route(
        router,
        "/api/internal/bot/owner/stats",
        "/api/internal/bot/coordinator/stats",
        get(owner_stats::get_owner_stats),
    );

    // ---- rate-limit consume (bot JWT) ----
    dual_route(
        router,
        "/api/internal/bot/setup/rate-limit/consume",
        "/api/internal/bot/coordinator/rate-limit/consume",
        post(ratelimit::consume_rate_limit),
    )
}

#[cfg(test)]
mod tests {
    use super::build_router;

    /// Constructing the router adds every route to matchit, so any bare-param
    /// / literal-segment conflict panics here in `cargo test` rather than at
    /// service boot. This guards the dual-registration reorg (coordinator has
    /// no bare-param routes today, but the test is kept for parity with the
    /// other services and to catch future additions).
    #[test]
    fn router_builds() {
        let _ = build_router();
    }
}
