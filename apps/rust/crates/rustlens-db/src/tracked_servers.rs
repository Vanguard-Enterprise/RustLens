//! Tracked BattleMetrics servers + guild selections, owned by the `servers`
//! service. Mirrors `internal/store/tracked_servers.go` and
//! `guild_tracked_servers.go`. Schema owned by Go — read/write only.

use chrono::{DateTime, Utc};
use serde::Serialize;
use sqlx::{PgPool, Postgres, QueryBuilder, Row};

#[derive(Debug, thiserror::Error)]
pub enum TrackedServerError {
    #[error("tracked server already exists")]
    AlreadyExists,
    #[error("tracked server not found")]
    NotFound,
    #[error("tracked server already enabled for guild")]
    GuildAlreadyEnabled,
    #[error("tracked server not enabled for guild")]
    GuildNotEnabled,
    #[error(transparent)]
    Db(#[from] sqlx::Error),
}

/// A global tracked server. Serializes as the bot `trackedServerToMap`
/// (`lastPolledAt` is `null` when unpolled — the admin map omits it instead).
#[derive(Debug, Clone, Serialize)]
pub struct TrackedServer {
    #[serde(rename = "bmId")]
    pub bm_id: String,
    pub name: String,
    #[serde(rename = "isVitality")]
    pub is_vitality: bool,
    #[serde(rename = "isAtlas")]
    pub is_atlas: bool,
    #[serde(rename = "isVital")]
    pub is_vital: bool,
    #[serde(rename = "isModded")]
    pub is_modded: bool,
    #[serde(rename = "isOfficial")]
    pub is_official: bool,
    #[serde(rename = "isBuild")]
    pub is_build: bool,
    #[serde(rename = "highFrequency")]
    pub high_frequency: bool,
    #[serde(rename = "lastPolledAt")]
    pub last_polled_at: Option<DateTime<Utc>>,
    #[serde(rename = "lastPollError")]
    pub last_poll_error: String,
}

/// Server category/frequency flags supplied on create/update.
#[derive(Debug, Clone, Copy, Default)]
pub struct ServerFlags {
    pub is_vitality: bool,
    pub is_atlas: bool,
    pub is_vital: bool,
    pub is_modded: bool,
    pub is_official: bool,
    pub is_build: bool,
    pub high_frequency: bool,
}

/// A guild's selected tracked server (`{guildId,bmId,name}`).
#[derive(Debug, Clone, Serialize)]
pub struct GuildTrackedServer {
    #[serde(rename = "guildId")]
    pub guild_id: String,
    #[serde(rename = "bmId")]
    pub bm_id: String,
    pub name: String,
}

const COLS: &str = "bm_id, name, is_vitality, is_atlas, is_vital, is_modded, is_official, is_build, high_frequency, last_polled_at, last_poll_error";

fn row_to_server(row: &sqlx::postgres::PgRow) -> Result<TrackedServer, sqlx::Error> {
    Ok(TrackedServer {
        bm_id: row.try_get("bm_id")?,
        name: row.try_get("name")?,
        is_vitality: row.try_get("is_vitality")?,
        is_atlas: row.try_get("is_atlas")?,
        is_vital: row.try_get("is_vital")?,
        is_modded: row.try_get("is_modded")?,
        is_official: row.try_get("is_official")?,
        is_build: row.try_get("is_build")?,
        high_frequency: row.try_get("high_frequency")?,
        last_polled_at: row.try_get("last_polled_at")?,
        last_poll_error: row.try_get("last_poll_error")?,
    })
}

pub async fn list(pool: &PgPool) -> Result<Vec<TrackedServer>, sqlx::Error> {
    let rows = sqlx::query(&format!(
        "SELECT {COLS} FROM tracked_servers ORDER BY name ASC, bm_id ASC"
    ))
    .fetch_all(pool)
    .await?;
    rows.iter().map(row_to_server).collect()
}

pub async fn get(pool: &PgPool, bm_id: &str) -> Result<Option<TrackedServer>, sqlx::Error> {
    let row = sqlx::query(&format!("SELECT {COLS} FROM tracked_servers WHERE bm_id = $1"))
        .bind(bm_id)
        .fetch_optional(pool)
        .await?;
    row.as_ref().map(row_to_server).transpose()
}

pub async fn create(
    pool: &PgPool,
    bm_id: &str,
    name: &str,
    flags: ServerFlags,
) -> Result<TrackedServer, TrackedServerError> {
    let row = sqlx::query(&format!(
        r#"INSERT INTO tracked_servers
               (bm_id, name, is_vitality, is_atlas, is_vital, is_modded, is_official, is_build, high_frequency)
           VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
           ON CONFLICT (bm_id) DO NOTHING
           RETURNING {COLS}"#
    ))
    .bind(bm_id)
    .bind(name)
    .bind(flags.is_vitality)
    .bind(flags.is_atlas)
    .bind(flags.is_vital)
    .bind(flags.is_modded)
    .bind(flags.is_official)
    .bind(flags.is_build)
    .bind(flags.high_frequency)
    .fetch_optional(pool)
    .await?;
    match row {
        Some(row) => Ok(row_to_server(&row)?),
        None => Err(TrackedServerError::AlreadyExists),
    }
}

/// Update a tracked server. When `next_bm_id != current_bm_id` this performs the
/// rename-remap transaction (insert-new → repoint children → delete-old),
/// carrying `last_polled_at`/`last_poll_error`. Mirrors `UpdateTrackedServer`.
pub async fn update(
    pool: &PgPool,
    current_bm_id: &str,
    next_bm_id: &str,
    name: &str,
    flags: ServerFlags,
) -> Result<TrackedServer, TrackedServerError> {
    if current_bm_id == next_bm_id {
        let row = sqlx::query(&format!(
            r#"UPDATE tracked_servers
               SET name=$2, is_vitality=$3, is_atlas=$4, is_vital=$5, is_modded=$6,
                   is_official=$7, is_build=$8, high_frequency=$9
               WHERE bm_id=$1
               RETURNING {COLS}"#
        ))
        .bind(current_bm_id)
        .bind(name)
        .bind(flags.is_vitality)
        .bind(flags.is_atlas)
        .bind(flags.is_vital)
        .bind(flags.is_modded)
        .bind(flags.is_official)
        .bind(flags.is_build)
        .bind(flags.high_frequency)
        .fetch_optional(pool)
        .await?;
        return match row {
            Some(row) => Ok(row_to_server(&row)?),
            None => Err(TrackedServerError::NotFound),
        };
    }

    // Rename: one transaction.
    let mut tx = pool.begin().await?;
    let existing = sqlx::query(&format!(
        "SELECT {COLS} FROM tracked_servers WHERE bm_id = $1"
    ))
    .bind(current_bm_id)
    .fetch_optional(&mut *tx)
    .await?;
    let Some(existing) = existing else {
        return Err(TrackedServerError::NotFound);
    };
    let last_polled_at: Option<DateTime<Utc>> = existing.try_get("last_polled_at")?;
    let last_poll_error: String = existing.try_get("last_poll_error")?;

    let insert = sqlx::query(
        r#"INSERT INTO tracked_servers
               (bm_id, name, is_vitality, is_atlas, is_vital, is_modded, is_official, is_build,
                high_frequency, last_polled_at, last_poll_error)
           VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)"#,
    )
    .bind(next_bm_id)
    .bind(name)
    .bind(flags.is_vitality)
    .bind(flags.is_atlas)
    .bind(flags.is_vital)
    .bind(flags.is_modded)
    .bind(flags.is_official)
    .bind(flags.is_build)
    .bind(flags.high_frequency)
    .bind(last_polled_at)
    .bind(&last_poll_error)
    .execute(&mut *tx)
    .await;
    if let Err(sqlx::Error::Database(e)) = &insert {
        if e.code().as_deref() == Some("23505") {
            return Err(TrackedServerError::AlreadyExists);
        }
    }
    insert?;

    for table in [
        "guild_tracked_servers",
        "player_states",
        "track_groups",
        "server_player_count_history",
        "server_player_count_history_fetches",
        "track_analytics_sessions_cache",
    ] {
        sqlx::query(&format!(
            "UPDATE {table} SET server_bm_id = $2 WHERE server_bm_id = $1"
        ))
        .bind(current_bm_id)
        .bind(next_bm_id)
        .execute(&mut *tx)
        .await?;
    }
    sqlx::query("DELETE FROM tracked_servers WHERE bm_id = $1")
        .bind(current_bm_id)
        .execute(&mut *tx)
        .await?;
    tx.commit().await?;

    Ok(TrackedServer {
        bm_id: next_bm_id.to_string(),
        name: name.to_string(),
        is_vitality: flags.is_vitality,
        is_atlas: flags.is_atlas,
        is_vital: flags.is_vital,
        is_modded: flags.is_modded,
        is_official: flags.is_official,
        is_build: flags.is_build,
        high_frequency: flags.high_frequency,
        last_polled_at,
        last_poll_error,
    })
}

pub async fn delete(pool: &PgPool, bm_id: &str) -> Result<(), TrackedServerError> {
    let res = sqlx::query("DELETE FROM tracked_servers WHERE bm_id = $1")
        .bind(bm_id)
        .execute(pool)
        .await?;
    if res.rows_affected() == 0 {
        return Err(TrackedServerError::NotFound);
    }
    Ok(())
}

// ---- guild-scoped ----

pub async fn list_guild(pool: &PgPool, guild_id: &str) -> Result<Vec<GuildTrackedServer>, sqlx::Error> {
    let rows = sqlx::query(
        r#"SELECT gts.guild_id, gts.server_bm_id, ts.name
           FROM guild_tracked_servers gts
           JOIN tracked_servers ts ON ts.bm_id = gts.server_bm_id
           WHERE gts.guild_id = $1
           ORDER BY ts.name ASC, gts.server_bm_id ASC"#,
    )
    .bind(guild_id)
    .fetch_all(pool)
    .await?;
    rows.iter()
        .map(|row| {
            Ok(GuildTrackedServer {
                guild_id: row.try_get("guild_id")?,
                bm_id: row.try_get("server_bm_id")?,
                name: row.try_get("name")?,
            })
        })
        .collect()
}

/// Replace a guild's selected servers (delete-all then insert each, one tx).
pub async fn replace_guild(pool: &PgPool, guild_id: &str, bm_ids: &[String]) -> Result<(), sqlx::Error> {
    let mut tx = pool.begin().await?;
    sqlx::query("DELETE FROM guild_tracked_servers WHERE guild_id = $1")
        .bind(guild_id)
        .execute(&mut *tx)
        .await?;
    for bm_id in bm_ids {
        sqlx::query("INSERT INTO guild_tracked_servers (guild_id, server_bm_id) VALUES ($1, $2)")
            .bind(guild_id)
            .bind(bm_id)
            .execute(&mut *tx)
            .await?;
    }
    tx.commit().await?;
    Ok(())
}

pub async fn add_guild(pool: &PgPool, guild_id: &str, bm_id: &str) -> Result<(), TrackedServerError> {
    let exists: bool =
        sqlx::query_scalar("SELECT EXISTS (SELECT 1 FROM tracked_servers WHERE bm_id = $1)")
            .bind(bm_id)
            .fetch_one(pool)
            .await?;
    if !exists {
        return Err(TrackedServerError::NotFound);
    }
    let res = sqlx::query(
        "INSERT INTO guild_tracked_servers (guild_id, server_bm_id) VALUES ($1, $2) ON CONFLICT (guild_id, server_bm_id) DO NOTHING",
    )
    .bind(guild_id)
    .bind(bm_id)
    .execute(pool)
    .await?;
    if res.rows_affected() == 0 {
        return Err(TrackedServerError::GuildAlreadyEnabled);
    }
    Ok(())
}

pub async fn remove_guild(pool: &PgPool, guild_id: &str, bm_id: &str) -> Result<(), TrackedServerError> {
    let res = sqlx::query("DELETE FROM guild_tracked_servers WHERE guild_id = $1 AND server_bm_id = $2")
        .bind(guild_id)
        .bind(bm_id)
        .execute(pool)
        .await?;
    if res.rows_affected() == 0 {
        return Err(TrackedServerError::GuildNotEnabled);
    }
    Ok(())
}

// ---- admin list-page (used by S-E) ----

/// Filters for the admin paginated list.
#[derive(Debug, Default)]
pub struct TrackedServerFilter {
    pub q: Option<String>,
    pub high_frequency: Option<bool>,
    pub has_error: Option<bool>,
    pub is_vitality: Option<bool>,
    pub is_atlas: Option<bool>,
    pub is_vital: Option<bool>,
    pub is_modded: Option<bool>,
    pub is_official: Option<bool>,
    pub is_build: Option<bool>,
}

fn order_clause(sort_by: &str, sort_order: &str) -> String {
    let dir = if sort_order.eq_ignore_ascii_case("desc") { "DESC" } else { "ASC" };
    match sort_by {
        "bmId" => format!("ORDER BY bm_id {dir}"),
        "highFrequency" => format!("ORDER BY high_frequency {dir}, name ASC, bm_id ASC"),
        "lastPolledAt" => {
            let nulls = if dir == "ASC" { "NULLS FIRST" } else { "NULLS LAST" };
            format!("ORDER BY last_polled_at {dir} {nulls}, name ASC, bm_id ASC")
        }
        "pollStatus" => format!("ORDER BY (last_poll_error <> '') {dir}, name ASC, bm_id ASC"),
        "name" | "" => format!("ORDER BY name {dir}, bm_id ASC"),
        _ => "ORDER BY name ASC, bm_id ASC".to_string(),
    }
}

fn apply_filter<'a>(qb: &mut QueryBuilder<'a, Postgres>, f: &'a TrackedServerFilter) {
    qb.push(" WHERE 1=1");
    if let Some(q) = f.q.as_deref().map(str::trim).filter(|s| !s.is_empty()) {
        qb.push(" AND (name ILIKE ").push_bind(format!("%{q}%"));
        qb.push(" OR bm_id ILIKE ").push_bind(format!("%{q}%")).push(")");
    }
    if let Some(v) = f.high_frequency {
        qb.push(" AND high_frequency = ").push_bind(v);
    }
    if let Some(v) = f.has_error {
        qb.push(if v { " AND last_poll_error <> ''" } else { " AND last_poll_error = ''" });
    }
    for (col, val) in [
        ("is_vitality", f.is_vitality),
        ("is_atlas", f.is_atlas),
        ("is_vital", f.is_vital),
        ("is_modded", f.is_modded),
        ("is_official", f.is_official),
        ("is_build", f.is_build),
    ] {
        if let Some(v) = val {
            qb.push(format!(" AND {col} = ")).push_bind(v);
        }
    }
}

/// Paginated admin list + total count.
pub async fn list_page(
    pool: &PgPool,
    filter: &TrackedServerFilter,
    sort_by: &str,
    sort_order: &str,
    limit: i64,
    offset: i64,
) -> Result<(Vec<TrackedServer>, i64), sqlx::Error> {
    let mut count_qb: QueryBuilder<Postgres> = QueryBuilder::new("SELECT COUNT(*) FROM tracked_servers");
    apply_filter(&mut count_qb, filter);
    let total: i64 = count_qb.build_query_scalar().fetch_one(pool).await?;

    let mut qb: QueryBuilder<Postgres> = QueryBuilder::new(format!("SELECT {COLS} FROM tracked_servers"));
    apply_filter(&mut qb, filter);
    qb.push(format!(" {} ", order_clause(sort_by, sort_order)));
    qb.push("LIMIT ").push_bind(limit).push(" OFFSET ").push_bind(offset);
    let rows = qb.build().fetch_all(pool).await?;
    let servers = rows.iter().map(row_to_server).collect::<Result<Vec<_>, _>>()?;
    Ok((servers, total))
}

pub async fn set_high_frequency(pool: &PgPool, bm_id: &str, high_frequency: bool) -> Result<(), TrackedServerError> {
    let res = sqlx::query("UPDATE tracked_servers SET high_frequency = $2 WHERE bm_id = $1")
        .bind(bm_id)
        .bind(high_frequency)
        .execute(pool)
        .await?;
    if res.rows_affected() == 0 {
        return Err(TrackedServerError::NotFound);
    }
    Ok(())
}

// ---- poller helpers (used by S-C) ----

/// Least-recently-polled servers for one frequency group (`NULLS FIRST`).
pub async fn list_to_poll(
    pool: &PgPool,
    high_frequency: bool,
    limit: i64,
) -> Result<Vec<TrackedServer>, sqlx::Error> {
    if limit <= 0 {
        return Ok(Vec::new());
    }
    let rows = sqlx::query(&format!(
        r#"SELECT {COLS} FROM tracked_servers
           WHERE high_frequency = $1
           ORDER BY last_polled_at NULLS FIRST, bm_id ASC
           LIMIT $2"#
    ))
    .bind(high_frequency)
    .bind(limit)
    .fetch_all(pool)
    .await?;
    rows.iter().map(row_to_server).collect()
}

/// Record a poll result (advances `last_polled_at` even on error).
pub async fn record_poll_result(
    pool: &PgPool,
    bm_id: &str,
    polled_at: DateTime<Utc>,
    poll_error: &str,
) -> Result<(), sqlx::Error> {
    sqlx::query("UPDATE tracked_servers SET last_polled_at = $2, last_poll_error = $3 WHERE bm_id = $1")
        .bind(bm_id)
        .bind(polled_at)
        .bind(poll_error)
        .execute(pool)
        .await?;
    Ok(())
}

/// Increment + return the empty-poll counter.
pub async fn increment_empty_polls(pool: &PgPool, bm_id: &str) -> Result<i32, sqlx::Error> {
    sqlx::query_scalar(
        "UPDATE tracked_servers SET empty_player_poll_count = empty_player_poll_count + 1 WHERE bm_id = $1 RETURNING empty_player_poll_count",
    )
    .bind(bm_id)
    .fetch_one(pool)
    .await
}

/// Reset the empty-poll counter (no-op when already 0).
pub async fn reset_empty_polls(pool: &PgPool, bm_id: &str) -> Result<(), sqlx::Error> {
    sqlx::query("UPDATE tracked_servers SET empty_player_poll_count = 0 WHERE bm_id = $1 AND empty_player_poll_count <> 0")
        .bind(bm_id)
        .execute(pool)
        .await?;
    Ok(())
}
