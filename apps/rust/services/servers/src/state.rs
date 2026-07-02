//! Shared application state for the servers service handlers.

use rustlens_config::ServersConfig;
use rustlens_db::PgPool;

/// Injected into every handler via `axum::extract::State`, and shared with the
/// background workers.
pub struct AppState {
    pub config: ServersConfig,
    pub pool: PgPool,
    /// BattleMetrics client (proxied via the outbound provider). Used by the
    /// poller (and later the analytics refresher).
    pub bm: rustlens_battlemetrics::Client,
    /// Redis connection for the worker locks (`None` → run without distributed
    /// locking, single-pod).
    pub redis: Option<redis::aio::MultiplexedConnection>,
}
