//! `track_analytics_sessions_cache` read/write for the players offline-analytics
//! route. Mirrors `internal/store/track_analytics.go`. Owned by the future
//! `servers` service; players reads + upserts it during the transition.

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use sqlx::types::Json;
use sqlx::{PgPool, Row};

/// One cached play session (`{start, stop}` unix seconds).
#[derive(Debug, Clone, Copy, Serialize, Deserialize)]
pub struct TrackAnalyticsSession {
    #[serde(rename = "start")]
    pub start_unix: i64,
    #[serde(rename = "stop")]
    pub stop_unix: i64,
}

/// A cache entry: the sessions blob + when it was fetched.
#[derive(Debug, Clone)]
pub struct TrackAnalyticsCacheEntry {
    pub sessions: Vec<TrackAnalyticsSession>,
    pub fetched_at: DateTime<Utc>,
}

/// Load the cached sessions for `(player, server)` (`None` = no row).
pub async fn get_track_analytics_cache(
    pool: &PgPool,
    player_bm_id: &str,
    server_bm_id: &str,
) -> Result<Option<TrackAnalyticsCacheEntry>, sqlx::Error> {
    let row = sqlx::query(
        "SELECT sessions, fetched_at FROM track_analytics_sessions_cache WHERE player_bm_id = $1 AND server_bm_id = $2",
    )
    .bind(player_bm_id)
    .bind(server_bm_id)
    .fetch_optional(pool)
    .await?;
    row.map(|row| {
        let sessions: Json<Vec<TrackAnalyticsSession>> = row.try_get("sessions")?;
        Ok(TrackAnalyticsCacheEntry {
            sessions: sessions.0,
            fetched_at: row.try_get("fetched_at")?,
        })
    })
    .transpose()
}

/// Upsert the cached sessions for `(player, server)`, stamping `fetched_at = NOW()`.
pub async fn upsert_track_analytics_cache(
    pool: &PgPool,
    player_bm_id: &str,
    server_bm_id: &str,
    sessions: &[TrackAnalyticsSession],
) -> Result<(), sqlx::Error> {
    sqlx::query(
        r#"INSERT INTO track_analytics_sessions_cache (player_bm_id, server_bm_id, sessions, fetched_at)
           VALUES ($1, $2, $3, NOW())
           ON CONFLICT (player_bm_id, server_bm_id) DO UPDATE
           SET sessions = EXCLUDED.sessions, fetched_at = NOW()"#,
    )
    .bind(player_bm_id)
    .bind(server_bm_id)
    .bind(Json(sessions))
    .execute(pool)
    .await?;
    Ok(())
}
