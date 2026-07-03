//! Server player-count history cache for the servers service. Mirrors
//! `internal/store/server_analytics.go`. Schema owned by Go.

use chrono::{DateTime, Utc};
use sqlx::{PgPool, Row};

/// One stored daily player-count point.
#[derive(Debug, Clone, Copy)]
pub struct ServerPlayerCountPoint {
    pub timestamp_unix: i64,
    pub min: i32,
    pub value: f64,
    pub max: i32,
}

/// A cache entry: the stored points + when they were last fetched.
#[derive(Debug, Clone)]
pub struct CacheEntry {
    pub points: Vec<ServerPlayerCountPoint>,
    pub fetched_at: DateTime<Utc>,
}

/// Load the cached history for a server (`None` = never fetched).
pub async fn get_history(pool: &PgPool, server_bm_id: &str) -> Result<Option<CacheEntry>, sqlx::Error> {
    let fetched_at: Option<DateTime<Utc>> =
        sqlx::query_scalar("SELECT fetched_at FROM server_player_count_history_fetches WHERE server_bm_id = $1")
            .bind(server_bm_id)
            .fetch_optional(pool)
            .await?;
    let Some(fetched_at) = fetched_at else {
        return Ok(None);
    };
    let rows = sqlx::query(
        "SELECT timestamp_unix, min, value, max FROM server_player_count_history WHERE server_bm_id = $1 ORDER BY timestamp_unix ASC",
    )
    .bind(server_bm_id)
    .fetch_all(pool)
    .await?;
    let points = rows
        .iter()
        .map(|row| {
            Ok(ServerPlayerCountPoint {
                timestamp_unix: row.try_get("timestamp_unix")?,
                min: row.try_get("min")?,
                value: row.try_get("value")?,
                max: row.try_get("max")?,
            })
        })
        .collect::<Result<Vec<_>, sqlx::Error>>()?;
    Ok(Some(CacheEntry { points, fetched_at }))
}

/// Upsert the daily history points + record the fetch (one transaction). Points
/// with `timestamp_unix == 0` are skipped.
pub async fn upsert_history(
    pool: &PgPool,
    server_bm_id: &str,
    points: &[ServerPlayerCountPoint],
) -> Result<(), sqlx::Error> {
    let mut tx = pool.begin().await?;
    for p in points {
        if p.timestamp_unix == 0 {
            continue;
        }
        sqlx::query(
            r#"INSERT INTO server_player_count_history (server_bm_id, timestamp_unix, min, value, max, observed_at)
               VALUES ($1, $2, $3, $4, $5, NOW())
               ON CONFLICT (server_bm_id, timestamp_unix) DO UPDATE
               SET min = EXCLUDED.min, value = EXCLUDED.value, max = EXCLUDED.max, observed_at = NOW()"#,
        )
        .bind(server_bm_id)
        .bind(p.timestamp_unix)
        .bind(p.min)
        .bind(p.value)
        .bind(p.max)
        .execute(&mut *tx)
        .await?;
    }
    sqlx::query(
        r#"INSERT INTO server_player_count_history_fetches (server_bm_id, fetched_at)
           VALUES ($1, NOW())
           ON CONFLICT (server_bm_id) DO UPDATE SET fetched_at = NOW()"#,
    )
    .bind(server_bm_id)
    .execute(&mut *tx)
    .await?;
    tx.commit().await?;
    Ok(())
}
