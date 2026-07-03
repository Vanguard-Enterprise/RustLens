//! Shared application state for the guilds service handlers.

use rustlens_config::GuildsConfig;
use rustlens_db::PgPool;

/// Injected into every handler via `axum::extract::State`.
pub struct AppState {
    pub config: GuildsConfig,
    pub pool: PgPool,
}
