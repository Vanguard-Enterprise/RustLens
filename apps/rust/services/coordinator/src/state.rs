//! Shared application state for the coordinator service.

use rustlens_config::CoordinatorConfig;

pub struct AppState {
    pub config: CoordinatorConfig,
    /// Redis connection for the shard lease store + rate-limit counters + the
    /// cleanup worker lock (`None` → shard/rate-limit routes error, cleanup runs
    /// single-pod).
    pub redis: Option<redis::aio::MultiplexedConnection>,
}
