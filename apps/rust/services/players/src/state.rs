//! Shared application state for the players service handlers.

use std::collections::HashSet;
use std::sync::{Arc, Mutex};

use rustlens_config::PlayersConfig;
use rustlens_db::PgPool;

/// Per-Steam-ID in-flight guard for the detached backfill (Go's
/// `singleflight.Group`): coalesces concurrent backfills for the same Steam ID
/// to one, releasing the key when the task completes (no result caching).
#[derive(Clone, Default)]
pub struct Backfill {
    inflight: Arc<Mutex<HashSet<String>>>,
}

impl Backfill {
    /// Claim `key`; returns `true` if the caller should run the backfill (no
    /// other backfill is in flight for this Steam ID).
    pub fn try_begin(&self, key: &str) -> bool {
        self.inflight.lock().unwrap().insert(key.to_string())
    }
    /// Release `key` after the backfill completes.
    pub fn end(&self, key: &str) {
        self.inflight.lock().unwrap().remove(key);
    }
}

/// Injected into every handler via `axum::extract::State`.
pub struct AppState {
    pub config: PlayersConfig,
    pub pool: PgPool,
    /// Steam Web API client (direct).
    pub steam: rustlens_steam::steamapi::Client,
    /// Steam↔BM resolver chain (bmsearch primary + bmmatch fallback), direct.
    pub resolver: Arc<rustlens_steam::Chain>,
    /// BattleMetrics client (proxied via the outbound provider).
    pub bm: rustlens_battlemetrics::Client,
    /// platformsync.io client (direct).
    pub platform_sync: rustlens_steam::platformsync::Client,
    /// Redis connection for app-route rate limiting (`None` → fails open).
    pub redis: Option<redis::aio::MultiplexedConnection>,
    /// Detached backfill in-flight guard.
    pub backfill: Backfill,
}
