//! `platform_sync_state` read/write for the players service (platformsync.io
//! staleness tracking). Mirrors `internal/store/platform_sync.go`. Only the
//! queried `(platform, id)` pair is ever recorded.

use chrono::{DateTime, Utc};
use sqlx::{PgPool, Row};

/// A `platform_sync_state` row.
#[derive(Debug, Clone)]
pub struct PlatformSyncState {
    pub platform: String,
    pub platform_id: String,
    pub last_platform_sync_at: DateTime<Utc>,
}

/// Load the last-sync state for a `(platform, id)` pair (`None` = never synced).
pub async fn get_platform_sync_state(
    pool: &PgPool,
    platform: &str,
    platform_id: &str,
) -> Result<Option<PlatformSyncState>, sqlx::Error> {
    let row = sqlx::query(
        "SELECT platform, platform_id, last_platform_sync_at FROM platform_sync_state WHERE platform = $1 AND platform_id = $2",
    )
    .bind(platform)
    .bind(platform_id)
    .fetch_optional(pool)
    .await?;
    row.map(|row| {
        Ok(PlatformSyncState {
            platform: row.try_get("platform")?,
            platform_id: row.try_get("platform_id")?,
            last_platform_sync_at: row.try_get("last_platform_sync_at")?,
        })
    })
    .transpose()
}

/// Record `NOW()` as the last-sync time for a `(platform, id)` pair.
pub async fn upsert_platform_sync_state(
    pool: &PgPool,
    platform: &str,
    platform_id: &str,
) -> Result<PlatformSyncState, sqlx::Error> {
    let row = sqlx::query(
        r#"INSERT INTO platform_sync_state (platform, platform_id, last_platform_sync_at)
           VALUES ($1, $2, NOW())
           ON CONFLICT (platform, platform_id) DO UPDATE SET last_platform_sync_at = NOW()
           RETURNING platform, platform_id, last_platform_sync_at"#,
    )
    .bind(platform)
    .bind(platform_id)
    .fetch_one(pool)
    .await?;
    Ok(PlatformSyncState {
        platform: row.try_get("platform")?,
        platform_id: row.try_get("platform_id")?,
        last_platform_sync_at: row.try_get("last_platform_sync_at")?,
    })
}
