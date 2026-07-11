//! Persisted wipe reacts (`wipe_reacts`) + reaction mutations, owned by the
//! `guilds` service. Mirrors `internal/store/wipe_reacts.go`. Schema owned by Go.
//!
//! The three reaction id lists preserve Go's nil-vs-`[]` distinction end-to-end:
//! a jsonb `null` decodes to `None` (emits JSON `null`), a jsonb `[]` decodes to
//! `Some(vec![])` (emits `[]`). Reaction mutations always rebuild the arrays
//! non-`None`, so a changed reaction never emits `null`.

use serde::Serialize;
use serde_json::Value;
use sqlx::{PgPool, Row};

#[derive(Debug, thiserror::Error)]
pub enum WipeReactError {
    #[error("wipe react not found")]
    NotFound,
    #[error(transparent)]
    Db(#[from] sqlx::Error),
}

/// One wipe react. `*_user_ids` are `None` for a stored jsonb `null`.
#[derive(Debug, Clone, Serialize)]
pub struct WipeReact {
    #[serde(rename = "guildId")]
    pub guild_id: String,
    #[serde(rename = "channelId")]
    pub channel_id: String,
    #[serde(rename = "messageId")]
    pub message_id: String,
    #[serde(rename = "creatorId")]
    pub creator_id: String,
    pub name: String,
    pub description: String,
    #[serde(rename = "timeZone")]
    pub time_zone: String,
    #[serde(rename = "scheduledUnix")]
    pub scheduled_unix: i64,
    #[serde(rename = "askLateReason")]
    pub ask_late_reason: bool,
    #[serde(rename = "askDeclinedReason")]
    pub ask_declined_reason: bool,
    #[serde(rename = "acceptedUserIds")]
    pub accepted_user_ids: Option<Vec<String>>,
    #[serde(rename = "declinedUserIds")]
    pub declined_user_ids: Option<Vec<String>>,
    #[serde(rename = "lateUserIds")]
    pub late_user_ids: Option<Vec<String>>,
    #[serde(rename = "embedTimestamp")]
    pub embed_timestamp: String,
}

const COLS: &str = "guild_id, channel_id, message_id, creator_id, name, description, time_zone, \
                    scheduled_unix, ask_late_reason, ask_declined_reason, \
                    accepted_user_ids, declined_user_ids, late_user_ids, embed_timestamp";

/// jsonb `null` → `None`; jsonb `[...]` → `Some(vec)` (mirrors `decodeStringSlice`).
fn jsonb_to_opt_vec(v: Value) -> Option<Vec<String>> {
    match v {
        Value::Array(arr) => Some(
            arr.into_iter()
                .filter_map(|x| x.as_str().map(str::to_string))
                .collect(),
        ),
        _ => None,
    }
}

/// `None` → `"null"`; `Some(vec)` → `"[...]"`/`"[]"` (mirrors `encodeStringSlice`).
/// Bound as text and cast `::jsonb` in the SQL, exactly like Go.
fn opt_vec_to_json_text(o: &Option<Vec<String>>) -> String {
    serde_json::to_string(o).unwrap_or_else(|_| "null".to_string())
}

fn row_to_wipe_react(row: &sqlx::postgres::PgRow) -> Result<WipeReact, sqlx::Error> {
    Ok(WipeReact {
        guild_id: row.try_get("guild_id")?,
        channel_id: row.try_get("channel_id")?,
        message_id: row.try_get("message_id")?,
        creator_id: row.try_get("creator_id")?,
        name: row.try_get("name")?,
        description: row.try_get("description")?,
        time_zone: row.try_get("time_zone")?,
        scheduled_unix: row.try_get("scheduled_unix")?,
        ask_late_reason: row.try_get("ask_late_reason")?,
        ask_declined_reason: row.try_get("ask_declined_reason")?,
        accepted_user_ids: jsonb_to_opt_vec(row.try_get("accepted_user_ids")?),
        declined_user_ids: jsonb_to_opt_vec(row.try_get("declined_user_ids")?),
        late_user_ids: jsonb_to_opt_vec(row.try_get("late_user_ids")?),
        embed_timestamp: row.try_get("embed_timestamp")?,
    })
}

/// `ListActiveWipeReacts`: `scheduled_unix > now`, newest first.
pub async fn list_active(pool: &PgPool, guild_id: &str, now_unix: i64) -> Result<Vec<WipeReact>, sqlx::Error> {
    let rows = sqlx::query(&format!(
        "SELECT {COLS} FROM wipe_reacts WHERE guild_id = $1 AND scheduled_unix > $2 \
         ORDER BY scheduled_unix DESC, message_id DESC"
    ))
    .bind(guild_id)
    .bind(now_unix)
    .fetch_all(pool)
    .await?;
    rows.iter().map(row_to_wipe_react).collect()
}

/// `ListWipeReacts`: all for the guild, newest first.
pub async fn list_all(pool: &PgPool, guild_id: &str) -> Result<Vec<WipeReact>, sqlx::Error> {
    let rows = sqlx::query(&format!(
        "SELECT {COLS} FROM wipe_reacts WHERE guild_id = $1 ORDER BY scheduled_unix DESC, message_id DESC"
    ))
    .bind(guild_id)
    .fetch_all(pool)
    .await?;
    rows.iter().map(row_to_wipe_react).collect()
}

/// `ListPastWipeReacts`: `scheduled_unix <= now`, newest first, capped at `limit`
/// (backs the admin guild-detail `recentWipeReacts`).
pub async fn list_past(
    pool: &PgPool,
    guild_id: &str,
    now_unix: i64,
    limit: i64,
) -> Result<Vec<WipeReact>, sqlx::Error> {
    let rows = sqlx::query(&format!(
        "SELECT {COLS} FROM wipe_reacts WHERE guild_id = $1 AND scheduled_unix <= $2 \
         ORDER BY scheduled_unix DESC, message_id DESC LIMIT $3"
    ))
    .bind(guild_id)
    .bind(now_unix)
    .bind(limit)
    .fetch_all(pool)
    .await?;
    rows.iter().map(row_to_wipe_react).collect()
}

/// `GetWipeReact`: `NotFound` when absent.
pub async fn get(pool: &PgPool, guild_id: &str, message_id: &str) -> Result<WipeReact, WipeReactError> {
    let row = sqlx::query(&format!(
        "SELECT {COLS} FROM wipe_reacts WHERE guild_id = $1 AND message_id = $2"
    ))
    .bind(guild_id)
    .bind(message_id)
    .fetch_optional(pool)
    .await?;
    match row {
        Some(r) => Ok(row_to_wipe_react(&r)?),
        None => Err(WipeReactError::NotFound),
    }
}

/// `CreateWipeReact`: insert (no ON CONFLICT — a duplicate PK surfaces as a DB
/// error, mapped to 500 by the handler). Go echoes the input record; the handler
/// returns the same `WipeReact` it passed in, so this returns `()`.
pub async fn create(pool: &PgPool, w: &WipeReact) -> Result<(), sqlx::Error> {
    sqlx::query(
        r#"INSERT INTO wipe_reacts (
               message_id, guild_id, channel_id, creator_id, name, description, time_zone,
               scheduled_unix, ask_late_reason, ask_declined_reason,
               accepted_user_ids, declined_user_ids, late_user_ids, embed_timestamp
           )
           VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::jsonb, $12::jsonb, $13::jsonb, $14)"#,
    )
    .bind(&w.message_id)
    .bind(&w.guild_id)
    .bind(&w.channel_id)
    .bind(&w.creator_id)
    .bind(&w.name)
    .bind(&w.description)
    .bind(&w.time_zone)
    .bind(w.scheduled_unix)
    .bind(w.ask_late_reason)
    .bind(w.ask_declined_reason)
    .bind(opt_vec_to_json_text(&w.accepted_user_ids))
    .bind(opt_vec_to_json_text(&w.declined_user_ids))
    .bind(opt_vec_to_json_text(&w.late_user_ids))
    .bind(&w.embed_timestamp)
    .execute(pool)
    .await?;
    Ok(())
}

/// `UpdateWipeReact`: writes only name/description/time_zone/scheduled_unix/
/// ask_*_reason/embed_timestamp (channel_id/creator_id/reaction arrays immutable
/// here). 0 rows → `NotFound`. Re-reads and returns fresh state.
pub async fn update(pool: &PgPool, w: &WipeReact) -> Result<WipeReact, WipeReactError> {
    let res = sqlx::query(
        r#"UPDATE wipe_reacts
           SET name = $3, description = $4, time_zone = $5, scheduled_unix = $6,
               ask_late_reason = $7, ask_declined_reason = $8, embed_timestamp = $9,
               updated_at = NOW()
           WHERE guild_id = $1 AND message_id = $2"#,
    )
    .bind(&w.guild_id)
    .bind(&w.message_id)
    .bind(&w.name)
    .bind(&w.description)
    .bind(&w.time_zone)
    .bind(w.scheduled_unix)
    .bind(w.ask_late_reason)
    .bind(w.ask_declined_reason)
    .bind(&w.embed_timestamp)
    .execute(pool)
    .await?;
    if res.rows_affected() == 0 {
        return Err(WipeReactError::NotFound);
    }
    get(pool, &w.guild_id, &w.message_id).await
}

/// `DeleteWipeReact`: 0 rows → `NotFound`.
pub async fn delete(pool: &PgPool, guild_id: &str, message_id: &str) -> Result<(), WipeReactError> {
    let res = sqlx::query("DELETE FROM wipe_reacts WHERE guild_id = $1 AND message_id = $2")
        .bind(guild_id)
        .bind(message_id)
        .execute(pool)
        .await?;
    if res.rows_affected() == 0 {
        return Err(WipeReactError::NotFound);
    }
    Ok(())
}

/// Result of a reaction set/remove: whether it changed, the resulting record,
/// and the user's prior reaction type.
pub struct ReactionOutcome {
    pub changed: bool,
    pub wipe_react: WipeReact,
    /// Prior reaction (`accepted`/`declined`/`late`) or `None`.
    pub prior_type: Option<&'static str>,
}

/// accepted → declined → late precedence, first match wins (`findReactionType`).
fn find_reaction_type(w: &WipeReact, user_id: &str) -> Option<&'static str> {
    let has = |o: &Option<Vec<String>>| o.as_ref().is_some_and(|v| v.iter().any(|x| x == user_id));
    if has(&w.accepted_user_ids) {
        Some("accepted")
    } else if has(&w.declined_user_ids) {
        Some("declined")
    } else if has(&w.late_user_ids) {
        Some("late")
    } else {
        None
    }
}

fn without(o: &Option<Vec<String>>, user_id: &str) -> Vec<String> {
    o.as_ref()
        .map(|v| v.iter().filter(|x| *x != user_id).cloned().collect())
        .unwrap_or_default()
}

async fn write_reaction_arrays(
    tx: &mut sqlx::Transaction<'_, sqlx::Postgres>,
    w: &WipeReact,
) -> Result<(), sqlx::Error> {
    sqlx::query(
        r#"UPDATE wipe_reacts
           SET accepted_user_ids = $3::jsonb, declined_user_ids = $4::jsonb,
               late_user_ids = $5::jsonb, updated_at = NOW()
           WHERE guild_id = $1 AND message_id = $2"#,
    )
    .bind(&w.guild_id)
    .bind(&w.message_id)
    .bind(opt_vec_to_json_text(&w.accepted_user_ids))
    .bind(opt_vec_to_json_text(&w.declined_user_ids))
    .bind(opt_vec_to_json_text(&w.late_user_ids))
    .execute(&mut **tx)
    .await?;
    Ok(())
}

async fn load_for_update(
    tx: &mut sqlx::Transaction<'_, sqlx::Postgres>,
    guild_id: &str,
    message_id: &str,
) -> Result<WipeReact, WipeReactError> {
    let row = sqlx::query(&format!(
        "SELECT {COLS} FROM wipe_reacts WHERE guild_id = $1 AND message_id = $2 FOR UPDATE"
    ))
    .bind(guild_id)
    .bind(message_id)
    .fetch_optional(&mut **tx)
    .await?;
    match row {
        Some(r) => Ok(row_to_wipe_react(&r)?),
        None => Err(WipeReactError::NotFound),
    }
}

/// `SetWipeReactReaction`: tx + `FOR UPDATE`. Moves the user's reaction to
/// `reaction_type` (accepted/declined/late) if different, rebuilding all three
/// arrays non-`None`.
pub async fn set_reaction(
    pool: &PgPool,
    guild_id: &str,
    message_id: &str,
    user_id: &str,
    reaction_type: &str,
) -> Result<ReactionOutcome, WipeReactError> {
    let mut tx = pool.begin().await?;
    let mut w = load_for_update(&mut tx, guild_id, message_id).await?;
    let prior = find_reaction_type(&w, user_id);
    let changed = prior != Some(reaction_type);
    if changed {
        let mut accepted = without(&w.accepted_user_ids, user_id);
        let mut declined = without(&w.declined_user_ids, user_id);
        let mut late = without(&w.late_user_ids, user_id);
        match reaction_type {
            "accepted" => accepted.push(user_id.to_string()),
            "declined" => declined.push(user_id.to_string()),
            _ => late.push(user_id.to_string()),
        }
        w.accepted_user_ids = Some(accepted);
        w.declined_user_ids = Some(declined);
        w.late_user_ids = Some(late);
        write_reaction_arrays(&mut tx, &w).await?;
    }
    tx.commit().await?;
    Ok(ReactionOutcome { changed, wipe_react: w, prior_type: prior })
}

/// `RemoveWipeReactReaction`: tx + `FOR UPDATE`. Removes the user from all three
/// lists if present.
pub async fn remove_reaction(
    pool: &PgPool,
    guild_id: &str,
    message_id: &str,
    user_id: &str,
) -> Result<ReactionOutcome, WipeReactError> {
    let mut tx = pool.begin().await?;
    let mut w = load_for_update(&mut tx, guild_id, message_id).await?;
    let removed = find_reaction_type(&w, user_id);
    let changed = removed.is_some();
    if changed {
        w.accepted_user_ids = Some(without(&w.accepted_user_ids, user_id));
        w.declined_user_ids = Some(without(&w.declined_user_ids, user_id));
        w.late_user_ids = Some(without(&w.late_user_ids, user_id));
        write_reaction_arrays(&mut tx, &w).await?;
    }
    tx.commit().await?;
    Ok(ReactionOutcome { changed, wipe_react: w, prior_type: removed })
}
