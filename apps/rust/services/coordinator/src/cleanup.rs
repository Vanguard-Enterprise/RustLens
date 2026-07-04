//! The hourly retention-sweep worker. Mirrors Go's `internal/cleanup/service.go`:
//! runs one sweep immediately on start, then every `CLEANUP_INTERVAL_SECONDS`,
//! deleting stale rows from `wipe_reacts` (7d) / `track_analytics_sessions_cache`
//! (48h) / `command_usage` (90d). Redis-locked across replicas (per the
//! servers/guilds-phase decision); single-pod when Redis is absent. Each sweep
//! logs-and-continues on failure and never aborts the others.

use std::sync::Arc;
use std::time::Duration;

use chrono::{Duration as ChronoDuration, Utc};

use rustlens_db::cleanup;

use crate::redis_lock;
use crate::state::AppState;

const CADENCE_KEY: &str = "rustlens:cleanup:cadence";
const LOCK_KEY: &str = "rustlens:cleanup:lock";

/// Run the cleanup loop until the process exits. Disabled when interval is 0.
pub async fn run(state: Arc<AppState>) {
    let secs = state.config.cleanup_interval_seconds;
    if secs == 0 {
        tracing::info!("cleanup service disabled (interval 0)");
        return;
    }
    let owner = redis_lock::worker_owner();
    let mut ticker = tokio::time::interval(Duration::from_secs(secs));
    ticker.set_missed_tick_behavior(tokio::time::MissedTickBehavior::Delay);
    tracing::info!("cleanup service started (interval {secs}s)");
    loop {
        ticker.tick().await; // first tick fires immediately
        run_cycle(&state, &owner).await;
    }
}

async fn run_cycle(state: &Arc<AppState>, owner: &str) {
    let interval = Duration::from_secs(state.config.cleanup_interval_seconds);
    let Some(conn) = &state.redis else {
        // No Redis: single-pod mode.
        do_sweep(state).await;
        return;
    };
    let mut conn = conn.clone();
    // Cadence key throttles to one sweep per interval across pods.
    let margin = (interval / 10).max(Duration::from_millis(250));
    let cadence_ttl = interval
        .checked_sub(margin)
        .filter(|d| !d.is_zero())
        .unwrap_or(interval / 2);
    if !redis_lock::try_acquire(&mut conn, CADENCE_KEY, owner, cadence_ttl.as_millis() as u64).await {
        return;
    }
    // Lock guards against concurrent sweeps; sweeps are quick so a 10-min TTL is
    // ample crash-safety.
    let lock_ttl = Duration::from_secs(600).min(interval);
    if !redis_lock::try_acquire(&mut conn, LOCK_KEY, owner, lock_ttl.as_millis() as u64).await {
        return;
    }
    do_sweep(state).await;
    redis_lock::release(&mut conn, LOCK_KEY, owner).await;
}

async fn do_sweep(state: &Arc<AppState>) {
    let now = Utc::now();
    let cfg = &state.config;

    // 1. Wipe reacts (7d): scheduled_unix is epoch seconds.
    let wr_cutoff = (now - ChronoDuration::seconds(cfg.cleanup_wipe_react_retention_seconds)).timestamp();
    match cleanup::purge_wipe_reacts_before(&state.pool, wr_cutoff).await {
        Ok(n) if n > 0 => tracing::info!("cleanup: purged {n} wipe reacts"),
        Ok(_) => {}
        Err(e) => tracing::error!("cleanup: purge wipe reacts failed: {e}"),
    }

    // 2. Track-analytics session cache (48h).
    let ac_cutoff = now - ChronoDuration::seconds(cfg.cleanup_analytics_cache_retention_seconds);
    match cleanup::purge_track_analytics_sessions_cache_before(&state.pool, ac_cutoff).await {
        Ok(n) if n > 0 => tracing::info!("cleanup: purged {n} analytics-cache rows"),
        Ok(_) => {}
        Err(e) => tracing::error!("cleanup: purge analytics cache failed: {e}"),
    }

    // 3. Command usage (90d).
    let cu_cutoff = now - ChronoDuration::seconds(cfg.cleanup_command_usage_retention_seconds);
    match cleanup::purge_command_usage_before(&state.pool, cu_cutoff).await {
        Ok(n) if n > 0 => tracing::info!("cleanup: purged {n} command-usage rows"),
        Ok(_) => {}
        Err(e) => tracing::error!("cleanup: purge command usage failed: {e}"),
    }
}
