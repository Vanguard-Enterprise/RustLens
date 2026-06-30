//! Shared application state for the identity service handlers.

use std::sync::Arc;

use rustlens_config::IdentityConfig;
use rustlens_db::PgPool;
use rustlens_events::{Hub, RedisBridge};

/// Injected into every handler via `axum::extract::State`.
pub struct AppState {
    pub config: IdentityConfig,
    pub pool: PgPool,
    pub http: reqwest::Client,
    /// In-process per-user event hub backing `/api/events/stream`.
    pub hub: Arc<Hub>,
    /// Cross-pod publisher (local hub + Redis fan-out). Used by admin mutations
    /// (spec Item 3b) to push `roles.changed` / `session.revoked` events.
    #[allow(dead_code)] // wired here; consumed by the action dispatcher (Item 3b)
    pub events: Arc<RedisBridge>,
}
