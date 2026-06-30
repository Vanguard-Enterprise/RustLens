//! Shared application state for the identity service handlers.

use rustlens_config::IdentityConfig;
use rustlens_db::PgPool;

/// Injected into every handler via `axum::extract::State`.
pub struct AppState {
    pub config: IdentityConfig,
    pub pool: PgPool,
    pub http: reqwest::Client,
}
