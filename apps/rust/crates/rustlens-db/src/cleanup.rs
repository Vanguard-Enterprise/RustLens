//! Retention-sweep purges for the coordinator's cleanup worker. Mirrors
//! `internal/store/cleanup.go` + the command-usage purge. Deletes stale rows from
//! volatile tables owned by other domains (shared DB, delete-only). Schema owned
//! by Go. Each returns the number of rows deleted.

use chrono::{DateTime, Utc};
use sqlx::PgPool;

/// `PurgeWipeReactsBefore`: `scheduled_unix` is a BIGINT epoch-**seconds** column,
/// compared against the cutoff's Unix seconds.
pub async fn purge_wipe_reacts_before(pool: &PgPool, cutoff_unix: i64) -> Result<u64, sqlx::Error> {
    let res = sqlx::query("DELETE FROM wipe_reacts WHERE scheduled_unix < $1")
        .bind(cutoff_unix)
        .execute(pool)
        .await?;
    Ok(res.rows_affected())
}

/// `PurgeTrackAnalyticsSessionsCacheBefore`: `fetched_at` is TIMESTAMPTZ.
pub async fn purge_track_analytics_sessions_cache_before(
    pool: &PgPool,
    cutoff: DateTime<Utc>,
) -> Result<u64, sqlx::Error> {
    let res = sqlx::query("DELETE FROM track_analytics_sessions_cache WHERE fetched_at < $1")
        .bind(cutoff)
        .execute(pool)
        .await?;
    Ok(res.rows_affected())
}

/// `PurgeCommandUsageBefore`: `created_at` is TIMESTAMPTZ. Deletes cascade to
/// `command_usage_targets` (FK) and null out `parent_usage_id` (self-FK) via the DB.
pub async fn purge_command_usage_before(
    pool: &PgPool,
    cutoff: DateTime<Utc>,
) -> Result<u64, sqlx::Error> {
    let res = sqlx::query("DELETE FROM command_usage WHERE created_at < $1")
        .bind(cutoff)
        .execute(pool)
        .await?;
    Ok(res.rows_affected())
}
