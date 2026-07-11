//! Shared application state for the servers service handlers.

use std::sync::Arc;

use rustlens_config::ServersConfig;
use rustlens_db::PgPool;

/// Injected into every handler via `axum::extract::State`, and shared with the
/// background workers.
pub struct AppState {
    pub config: ServersConfig,
    pub pool: PgPool,
    /// BattleMetrics client (proxied via the outbound provider). Used by the
    /// poller + the analytics refresher.
    pub bm: rustlens_battlemetrics::Client,
    /// The outbound provider, kept so proxy mutations can live-reload it.
    pub outbound: Arc<rustlens_outbound::Provider>,
    /// AES-GCM cipher for `outbound_proxies` endpoints (`None` → DB proxy
    /// management disabled: `PROXY_ENCRYPTION_KEY` unset).
    pub cipher: Option<rustlens_crypto::Cipher>,
    /// Redis connection for the worker locks (`None` → run without distributed
    /// locking, single-pod).
    pub redis: Option<redis::aio::MultiplexedConnection>,
}
