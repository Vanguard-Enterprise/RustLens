//! Shared application state for the guilds service handlers.

use rustlens_config::GuildsConfig;
use rustlens_db::PgPool;

/// Injected into every handler via `axum::extract::State`, and shared with the
/// background scheduler.
pub struct AppState {
    pub config: GuildsConfig,
    pub pool: PgPool,
    /// Redis connection for the scheduler worker lock (`None` → single-pod, no
    /// distributed locking).
    pub redis: Option<redis::aio::MultiplexedConnection>,
}
