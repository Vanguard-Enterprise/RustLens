//! Shared application state for the servers service handlers.

use rustlens_config::ServersConfig;
use rustlens_db::PgPool;

/// Injected into every handler via `axum::extract::State`.
pub struct AppState {
    pub config: ServersConfig,
    pub pool: PgPool,
}
