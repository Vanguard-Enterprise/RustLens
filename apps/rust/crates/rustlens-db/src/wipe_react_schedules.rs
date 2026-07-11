//! Recurring wipe-react schedules (`wipe_react_schedules`), owned by the `guilds`
//! service. Mirrors `internal/store/wipe_react_schedules.go`. Schema owned by Go.
//! Max 3 schedules per guild (enforced via a tx COUNT before insert).

use serde::Serialize;
use serde_json::Value;
use sqlx::{PgPool, Row};

pub const MAX_SCHEDULES_PER_GUILD: i64 = 3;

#[derive(Debug, thiserror::Error)]
pub enum ScheduleError {
    #[error("wipe react schedule not found")]
    NotFound,
    #[error("wipe react schedule limit reached")]
    LimitReached,
    #[error(transparent)]
    Db(#[from] sqlx::Error),
}

/// One schedule. `days` is always a (possibly empty) array, never null.
#[derive(Debug, Clone, Serialize)]
pub struct WipeReactSchedule {
    pub id: i64,
    #[serde(rename = "guildId")]
    pub guild_id: String,
    pub days: Vec<String>,
    #[serde(rename = "includesForceWipe")]
    pub includes_force_wipe: bool,
    pub time: String,
    #[serde(rename = "timeZone")]
    pub time_zone: String,
    pub repeat: String,
    #[serde(rename = "postBefore")]
    pub post_before: String,
    pub name: String,
    pub description: String,
    #[serde(rename = "askLateReason")]
    pub ask_late_reason: bool,
    #[serde(rename = "askDeclinedReason")]
    pub ask_declined_reason: bool,
    #[serde(rename = "channelId")]
    pub channel_id: String,
    #[serde(rename = "lastPostedForUnix")]
    pub last_posted_for_unix: i64,
    #[serde(rename = "createdBy")]
    pub created_by: String,
}

/// Create/update input (the request payload, post-validation).
#[derive(Debug, Clone, Default)]
pub struct ScheduleInput {
    pub days: Vec<String>,
    pub includes_force_wipe: bool,
    pub time: String,
    pub time_zone: String,
    pub repeat: String,
    pub post_before: String,
    pub name: String,
    pub description: String,
    pub ask_late_reason: bool,
    pub ask_declined_reason: bool,
    pub channel_id: String,
    pub created_by: String,
}

const COLS: &str = "id, guild_id, days, includes_force_wipe, time, time_zone, repeat, post_before, \
                    name, description, ask_late_reason, ask_declined_reason, channel_id, \
                    last_posted_for_unix, created_by";

fn days_from_jsonb(v: Value) -> Vec<String> {
    match v {
        Value::Array(a) => a.into_iter().filter_map(|x| x.as_str().map(str::to_string)).collect(),
        _ => Vec::new(),
    }
}

fn days_to_json(days: &[String]) -> String {
    serde_json::to_string(days).unwrap_or_else(|_| "[]".to_string())
}

fn row_to_schedule(row: &sqlx::postgres::PgRow) -> Result<WipeReactSchedule, sqlx::Error> {
    Ok(WipeReactSchedule {
        id: row.try_get("id")?,
        guild_id: row.try_get("guild_id")?,
        days: days_from_jsonb(row.try_get("days")?),
        includes_force_wipe: row.try_get("includes_force_wipe")?,
        time: row.try_get("time")?,
        time_zone: row.try_get("time_zone")?,
        repeat: row.try_get("repeat")?,
        post_before: row.try_get("post_before")?,
        name: row.try_get("name")?,
        description: row.try_get("description")?,
        ask_late_reason: row.try_get("ask_late_reason")?,
        ask_declined_reason: row.try_get("ask_declined_reason")?,
        channel_id: row.try_get("channel_id")?,
        last_posted_for_unix: row.try_get("last_posted_for_unix")?,
        created_by: row.try_get("created_by")?,
    })
}

/// Per-guild list, ordered `id ASC`.
pub async fn list(pool: &PgPool, guild_id: &str) -> Result<Vec<WipeReactSchedule>, sqlx::Error> {
    let rows = sqlx::query(&format!(
        "SELECT {COLS} FROM wipe_react_schedules WHERE guild_id = $1 ORDER BY id ASC"
    ))
    .bind(guild_id)
    .fetch_all(pool)
    .await?;
    rows.iter().map(row_to_schedule).collect()
}

/// All schedules across guilds (poller), ordered `guild_id ASC, id ASC`.
pub async fn list_all(pool: &PgPool) -> Result<Vec<WipeReactSchedule>, sqlx::Error> {
    let rows = sqlx::query(&format!(
        "SELECT {COLS} FROM wipe_react_schedules ORDER BY guild_id ASC, id ASC"
    ))
    .fetch_all(pool)
    .await?;
    rows.iter().map(row_to_schedule).collect()
}

pub async fn get(pool: &PgPool, guild_id: &str, id: i64) -> Result<WipeReactSchedule, ScheduleError> {
    let row = sqlx::query(&format!(
        "SELECT {COLS} FROM wipe_react_schedules WHERE guild_id = $1 AND id = $2"
    ))
    .bind(guild_id)
    .bind(id)
    .fetch_optional(pool)
    .await?;
    match row {
        Some(r) => Ok(row_to_schedule(&r)?),
        None => Err(ScheduleError::NotFound),
    }
}

/// `CreateWipeReactSchedule`: tx COUNT (>= 3 → `LimitReached`) then insert. The
/// returned record echoes the input with the assigned id + `last_posted_for_unix`
/// of 0 (mirrors Go; not a DB re-read).
pub async fn create(
    pool: &PgPool,
    guild_id: &str,
    input: &ScheduleInput,
) -> Result<WipeReactSchedule, ScheduleError> {
    let mut tx = pool.begin().await?;
    let count: i64 =
        sqlx::query_scalar("SELECT COUNT(*) FROM wipe_react_schedules WHERE guild_id = $1")
            .bind(guild_id)
            .fetch_one(&mut *tx)
            .await?;
    if count >= MAX_SCHEDULES_PER_GUILD {
        return Err(ScheduleError::LimitReached);
    }
    let id: i64 = sqlx::query_scalar(
        r#"INSERT INTO wipe_react_schedules (
               guild_id, days, includes_force_wipe, time, time_zone, repeat, post_before,
               name, description, ask_late_reason, ask_declined_reason, channel_id,
               last_posted_for_unix, created_by
           )
           VALUES ($1, $2::jsonb, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, 0, $13)
           RETURNING id"#,
    )
    .bind(guild_id)
    .bind(days_to_json(&input.days))
    .bind(input.includes_force_wipe)
    .bind(&input.time)
    .bind(&input.time_zone)
    .bind(&input.repeat)
    .bind(&input.post_before)
    .bind(&input.name)
    .bind(&input.description)
    .bind(input.ask_late_reason)
    .bind(input.ask_declined_reason)
    .bind(&input.channel_id)
    .bind(&input.created_by)
    .fetch_one(&mut *tx)
    .await?;
    tx.commit().await?;
    Ok(WipeReactSchedule {
        id,
        guild_id: guild_id.to_string(),
        days: input.days.clone(),
        includes_force_wipe: input.includes_force_wipe,
        time: input.time.clone(),
        time_zone: input.time_zone.clone(),
        repeat: input.repeat.clone(),
        post_before: input.post_before.clone(),
        name: input.name.clone(),
        description: input.description.clone(),
        ask_late_reason: input.ask_late_reason,
        ask_declined_reason: input.ask_declined_reason,
        channel_id: input.channel_id.clone(),
        last_posted_for_unix: 0,
        created_by: input.created_by.clone(),
    })
}

/// `UpdateWipeReactSchedule`: does not touch `last_posted_for_unix`/`created_by`/
/// `created_at`. 0 rows → `NotFound`. Re-reads and returns fresh state.
pub async fn update(
    pool: &PgPool,
    guild_id: &str,
    id: i64,
    input: &ScheduleInput,
) -> Result<WipeReactSchedule, ScheduleError> {
    let res = sqlx::query(
        r#"UPDATE wipe_react_schedules
           SET days = $3::jsonb, includes_force_wipe = $4, time = $5, time_zone = $6,
               repeat = $7, post_before = $8, name = $9, description = $10,
               ask_late_reason = $11, ask_declined_reason = $12, channel_id = $13,
               updated_at = NOW()
           WHERE guild_id = $1 AND id = $2"#,
    )
    .bind(guild_id)
    .bind(id)
    .bind(days_to_json(&input.days))
    .bind(input.includes_force_wipe)
    .bind(&input.time)
    .bind(&input.time_zone)
    .bind(&input.repeat)
    .bind(&input.post_before)
    .bind(&input.name)
    .bind(&input.description)
    .bind(input.ask_late_reason)
    .bind(input.ask_declined_reason)
    .bind(&input.channel_id)
    .execute(pool)
    .await?;
    if res.rows_affected() == 0 {
        return Err(ScheduleError::NotFound);
    }
    get(pool, guild_id, id).await
}

pub async fn delete(pool: &PgPool, guild_id: &str, id: i64) -> Result<(), ScheduleError> {
    let res = sqlx::query("DELETE FROM wipe_react_schedules WHERE guild_id = $1 AND id = $2")
        .bind(guild_id)
        .bind(id)
        .execute(pool)
        .await?;
    if res.rows_affected() == 0 {
        return Err(ScheduleError::NotFound);
    }
    Ok(())
}
