//! Shared application state for the players service handlers.

use rustlens_config::PlayersConfig;
use rustlens_db::PgPool;

/// Injected into every handler via `axum::extract::State`.
pub struct AppState {
    pub config: PlayersConfig,
    pub pool: PgPool,
}
