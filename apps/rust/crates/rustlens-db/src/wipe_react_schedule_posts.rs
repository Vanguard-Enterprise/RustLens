//! Backend-owned queue of due wipe-react schedule posts
//! (`wipe_react_schedule_posts`), owned by the `guilds` service. Mirrors
//! `internal/store/wipe_react_schedule_posts.go`. Schema owned by Go.

use chrono::{DateTime, Utc};
use serde::Serialize;
use sqlx::{PgPool, Row};

#[derive(Debug, thiserror::Error)]
pub enum SchedulePostError {
    #[error("wipe react schedule post not found")]
    NotFound,
    #[error(transparent)]
    Db(#[from] sqlx::Error),
}

/// A claimed post joined with its schedule's display fields. The post's
/// `claimed_by`/`claimed_at`/`message_id`/`posted_channel_id`/`completed_at`/
/// `created_at` are intentionally not serialized.
#[derive(Debug, Clone, Serialize)]
pub struct SchedulePost {
    pub id: i64,
    #[serde(rename = "scheduleId")]
    pub schedule_id: i64,
    #[serde(rename = "guildId")]
    pub guild_id: String,
    #[serde(rename = "channelId")]
    pub channel_id: String,
    #[serde(rename = "wipeUnix")]
    pub wipe_unix: i64,
    #[serde(rename = "postUnix")]
    pub post_unix: i64,
    #[serde(rename = "timeZone")]
    pub time_zone: String,
    pub name: String,
    pub description: String,
    #[serde(rename = "askLateReason")]
    pub ask_late_reason: bool,
    #[serde(rename = "askDeclinedReason")]
    pub ask_declined_reason: bool,
    #[serde(rename = "createdBy")]
    pub created_by: String,
}

fn row_to_post(row: &sqlx::postgres::PgRow) -> Result<SchedulePost, sqlx::Error> {
    Ok(SchedulePost {
        id: row.try_get("id")?,
        schedule_id: row.try_get("schedule_id")?,
        guild_id: row.try_get("guild_id")?,
        channel_id: row.try_get("channel_id")?,
        wipe_unix: row.try_get("wipe_unix")?,
        post_unix: row.try_get("post_unix")?,
        time_zone: row.try_get("time_zone")?,
        name: row.try_get("name")?,
        description: row.try_get("description")?,
        ask_late_reason: row.try_get("ask_late_reason")?,
        ask_declined_reason: row.try_get("ask_declined_reason")?,
        created_by: row.try_get("created_by")?,
    })
}

/// `EnqueueWipeReactSchedulePost`: dedupes on `UNIQUE(schedule_id, wipe_unix)`.
pub async fn enqueue(
    pool: &PgPool,
    schedule_id: i64,
    guild_id: &str,
    channel_id: &str,
    wipe_unix: i64,
    post_unix: i64,
) -> Result<(), sqlx::Error> {
    sqlx::query(
        r#"INSERT INTO wipe_react_schedule_posts (schedule_id, guild_id, channel_id, wipe_unix, post_unix)
           VALUES ($1, $2, $3, $4, $5)
           ON CONFLICT (schedule_id, wipe_unix) DO NOTHING"#,
    )
    .bind(schedule_id)
    .bind(guild_id)
    .bind(channel_id)
    .bind(wipe_unix)
    .bind(post_unix)
    .execute(pool)
    .await?;
    Ok(())
}

/// `ClaimNextWipeReactSchedulePost`: the row-locking CTE. Eligible = same guild,
/// not completed, `post_unix <= now`, and never-claimed or claimed before
/// `now - 5m` (stale reclaim). `NotFound` when there is nothing to claim.
pub async fn claim_next(
    pool: &PgPool,
    guild_id: &str,
    worker_id: &str,
    now_unix: i64,
    stale_before: DateTime<Utc>,
) -> Result<SchedulePost, SchedulePostError> {
    let row = sqlx::query(
        r#"WITH next_post AS (
               SELECT p.id
               FROM wipe_react_schedule_posts p
               WHERE p.guild_id = $1
                 AND p.completed_at IS NULL
                 AND p.post_unix <= $2
                 AND (p.claimed_at IS NULL OR p.claimed_at < $3)
               ORDER BY p.post_unix ASC, p.id ASC
               FOR UPDATE SKIP LOCKED
               LIMIT 1
           )
           UPDATE wipe_react_schedule_posts p
           SET claimed_by = $4, claimed_at = NOW()
           FROM next_post, wipe_react_schedules s
           WHERE p.id = next_post.id AND s.id = p.schedule_id
           RETURNING p.id, p.schedule_id, p.guild_id, p.channel_id, p.wipe_unix, p.post_unix,
               s.time_zone, s.name, s.description, s.ask_late_reason, s.ask_declined_reason, s.created_by"#,
    )
    .bind(guild_id)
    .bind(now_unix)
    .bind(stale_before)
    .bind(worker_id)
    .fetch_optional(pool)
    .await?;
    match row {
        Some(r) => Ok(row_to_post(&r)?),
        None => Err(SchedulePostError::NotFound),
    }
}

/// `CompleteWipeReactSchedulePost`: mark done (only if still owned + not
/// completed) then bump the schedule's `last_posted_for_unix` via `GREATEST`.
pub async fn complete(
    pool: &PgPool,
    guild_id: &str,
    post_id: i64,
    worker_id: &str,
    message_id: &str,
    channel_id: &str,
) -> Result<(), SchedulePostError> {
    let mut tx = pool.begin().await?;
    let row = sqlx::query(
        r#"UPDATE wipe_react_schedule_posts
           SET message_id = $4, posted_channel_id = $5, completed_at = NOW()
           WHERE guild_id = $1 AND id = $2 AND claimed_by = $3 AND completed_at IS NULL
           RETURNING schedule_id, wipe_unix"#,
    )
    .bind(guild_id)
    .bind(post_id)
    .bind(worker_id)
    .bind(message_id)
    .bind(channel_id)
    .fetch_optional(&mut *tx)
    .await?;
    let Some(row) = row else {
        return Err(SchedulePostError::NotFound);
    };
    let schedule_id: i64 = row.try_get("schedule_id")?;
    let wipe_unix: i64 = row.try_get("wipe_unix")?;
    sqlx::query(
        r#"UPDATE wipe_react_schedules
           SET last_posted_for_unix = GREATEST(last_posted_for_unix, $3), updated_at = NOW()
           WHERE guild_id = $1 AND id = $2"#,
    )
    .bind(guild_id)
    .bind(schedule_id)
    .bind(wipe_unix)
    .execute(&mut *tx)
    .await?;
    tx.commit().await?;
    Ok(())
}

/// `ReleaseWipeReactSchedulePost`: un-claim (only if still owned + not completed).
pub async fn release(
    pool: &PgPool,
    guild_id: &str,
    post_id: i64,
    worker_id: &str,
) -> Result<(), SchedulePostError> {
    let res = sqlx::query(
        r#"UPDATE wipe_react_schedule_posts
           SET claimed_by = '', claimed_at = NULL
           WHERE guild_id = $1 AND id = $2 AND claimed_by = $3 AND completed_at IS NULL"#,
    )
    .bind(guild_id)
    .bind(post_id)
    .bind(worker_id)
    .execute(pool)
    .await?;
    if res.rows_affected() == 0 {
        return Err(SchedulePostError::NotFound);
    }
    Ok(())
}
