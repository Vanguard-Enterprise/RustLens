//! Track groups + members (`track_groups`, `track_group_members`), owned by the
//! `guilds` service. Mirrors `internal/store/track_groups.go`. Schema owned by Go.
//! The online-state query reads across servers/players/linking tables (read-only).

use chrono::{DateTime, Utc};
use serde::Serialize;
use sqlx::{PgPool, Row};

#[derive(Debug, thiserror::Error)]
pub enum TrackGroupError {
    #[error("track group not found")]
    NotFound,
    #[error("track group with that name already exists")]
    NameTaken,
    #[error(transparent)]
    Db(#[from] sqlx::Error),
}

fn map_db_err(e: sqlx::Error) -> TrackGroupError {
    if let sqlx::Error::Database(db) = &e {
        if db.code().as_deref() == Some("23505") {
            return TrackGroupError::NameTaken;
        }
    }
    TrackGroupError::Db(e)
}

#[derive(Debug, Clone, Serialize)]
pub struct TrackGroup {
    pub id: i64,
    #[serde(rename = "guildId")]
    pub guild_id: String,
    pub name: String,
    #[serde(rename = "serverBmId")]
    pub server_bm_id: String,
    #[serde(rename = "channelId")]
    pub channel_id: String,
    #[serde(rename = "messageId")]
    pub message_id: String,
    #[serde(rename = "sendUpdates")]
    pub send_updates: bool,
    #[serde(rename = "pingAlertRoleOnEmpty")]
    pub ping_alert_role_on_empty: bool,
    #[serde(rename = "onlineCount")]
    pub online_count: i32,
    #[serde(rename = "createdBy")]
    pub created_by: String,
    #[serde(rename = "createdAt")]
    pub created_at: DateTime<Utc>,
    #[serde(rename = "updatedAt")]
    pub updated_at: DateTime<Utc>,
}

const COLS: &str = "id, guild_id, name, server_bm_id, channel_id, message_id, send_updates, \
                    ping_alert_role_on_empty, online_count, created_by, created_at, updated_at";

fn row_to_group(row: &sqlx::postgres::PgRow) -> Result<TrackGroup, sqlx::Error> {
    Ok(TrackGroup {
        id: row.try_get("id")?,
        guild_id: row.try_get("guild_id")?,
        name: row.try_get("name")?,
        server_bm_id: row.try_get("server_bm_id")?,
        channel_id: row.try_get("channel_id")?,
        message_id: row.try_get("message_id")?,
        send_updates: row.try_get("send_updates")?,
        ping_alert_role_on_empty: row.try_get("ping_alert_role_on_empty")?,
        online_count: row.try_get("online_count")?,
        created_by: row.try_get("created_by")?,
        created_at: row.try_get("created_at")?,
        updated_at: row.try_get("updated_at")?,
    })
}

/// Create input (post-validation; guild_id comes from the path).
#[derive(Debug, Clone, Default)]
pub struct CreateInput {
    pub name: String,
    pub server_bm_id: String,
    pub channel_id: String,
    pub message_id: String,
    pub send_updates: bool,
    pub ping_alert_role_on_empty: bool,
    pub online_count: i32,
    pub created_by: String,
}

/// Optional-pointer patch (only provided fields applied via COALESCE).
#[derive(Debug, Clone, Default)]
pub struct UpdatePatch {
    pub channel_id: Option<String>,
    pub message_id: Option<String>,
    pub send_updates: Option<bool>,
    pub ping_alert_role_on_empty: Option<bool>,
    pub online_count: Option<i32>,
}

pub async fn list(pool: &PgPool, guild_id: &str) -> Result<Vec<TrackGroup>, sqlx::Error> {
    let rows = sqlx::query(&format!(
        "SELECT {COLS} FROM track_groups WHERE guild_id = $1 ORDER BY name ASC, id ASC"
    ))
    .bind(guild_id)
    .fetch_all(pool)
    .await?;
    rows.iter().map(row_to_group).collect()
}

pub async fn get(pool: &PgPool, guild_id: &str, id: i64) -> Result<TrackGroup, TrackGroupError> {
    let row = sqlx::query(&format!(
        "SELECT {COLS} FROM track_groups WHERE guild_id = $1 AND id = $2"
    ))
    .bind(guild_id)
    .bind(id)
    .fetch_optional(pool)
    .await?;
    match row {
        Some(r) => Ok(row_to_group(&r)?),
        None => Err(TrackGroupError::NotFound),
    }
}

/// `CreateTrackGroup`: 23505 on `UNIQUE(guild_id,name)` → `NameTaken` (409). An
/// unknown `server_bm_id` fails the FK → generic DB error (500 in the handler).
pub async fn create(
    pool: &PgPool,
    guild_id: &str,
    input: &CreateInput,
) -> Result<TrackGroup, TrackGroupError> {
    let row = sqlx::query(&format!(
        r#"INSERT INTO track_groups
               (guild_id, name, server_bm_id, channel_id, message_id, send_updates,
                ping_alert_role_on_empty, online_count, created_by)
           VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
           RETURNING {COLS}"#
    ))
    .bind(guild_id)
    .bind(&input.name)
    .bind(&input.server_bm_id)
    .bind(&input.channel_id)
    .bind(&input.message_id)
    .bind(input.send_updates)
    .bind(input.ping_alert_role_on_empty)
    .bind(input.online_count)
    .bind(&input.created_by)
    .fetch_one(pool)
    .await
    .map_err(map_db_err)?;
    Ok(row_to_group(&row)?)
}

pub async fn update(
    pool: &PgPool,
    guild_id: &str,
    id: i64,
    patch: &UpdatePatch,
) -> Result<TrackGroup, TrackGroupError> {
    let row = sqlx::query(&format!(
        r#"UPDATE track_groups
           SET channel_id = COALESCE($3, channel_id),
               message_id = COALESCE($4, message_id),
               send_updates = COALESCE($5, send_updates),
               ping_alert_role_on_empty = COALESCE($6, ping_alert_role_on_empty),
               online_count = COALESCE($7, online_count),
               updated_at = NOW()
           WHERE guild_id = $1 AND id = $2
           RETURNING {COLS}"#
    ))
    .bind(guild_id)
    .bind(id)
    .bind(&patch.channel_id)
    .bind(&patch.message_id)
    .bind(patch.send_updates)
    .bind(patch.ping_alert_role_on_empty)
    .bind(patch.online_count)
    .fetch_optional(pool)
    .await?;
    match row {
        Some(r) => Ok(row_to_group(&r)?),
        None => Err(TrackGroupError::NotFound),
    }
}

pub async fn delete(pool: &PgPool, guild_id: &str, id: i64) -> Result<(), TrackGroupError> {
    let res = sqlx::query("DELETE FROM track_groups WHERE guild_id = $1 AND id = $2")
        .bind(guild_id)
        .bind(id)
        .execute(pool)
        .await?;
    if res.rows_affected() == 0 {
        return Err(TrackGroupError::NotFound);
    }
    Ok(())
}

// ---- track analytics ----

/// One tracked group member with a resolvable BattleMetrics ID, used by the
/// players track-group-analytics route. Mirrors Go's `TrackAnalyticsMember`.
#[derive(Debug, Clone)]
pub struct TrackAnalyticsMember {
    pub steam_id: String,
    pub bm_id: String,
}

/// `ListTrackAnalyticsMembers`: members of the group that have a resolvable
/// BattleMetrics ID (either on the member row or the linked steam account).
/// Members without one are skipped.
pub async fn list_track_analytics_members(
    pool: &PgPool,
    group_id: i64,
) -> Result<Vec<TrackAnalyticsMember>, sqlx::Error> {
    let rows = sqlx::query(
        r#"SELECT tgm.steam_id, COALESCE(NULLIF(tgm.bm_id, ''), COALESCE(sa.bm_id, '')) AS bm_id
           FROM track_group_members tgm
           LEFT JOIN steam_accounts sa ON sa.steam_id = tgm.steam_id
           WHERE tgm.track_group_id = $1"#,
    )
    .bind(group_id)
    .fetch_all(pool)
    .await?;
    let mut out = Vec::with_capacity(rows.len());
    for row in &rows {
        let steam_id: String = row.try_get("steam_id")?;
        let bm_id: String = row.try_get("bm_id")?;
        if bm_id.is_empty() {
            continue;
        }
        out.push(TrackAnalyticsMember { steam_id, bm_id });
    }
    Ok(out)
}

// ---- members ----

#[derive(Debug, Clone, Serialize)]
pub struct TrackGroupMember {
    #[serde(rename = "trackGroupId")]
    pub track_group_id: i64,
    #[serde(rename = "steamId")]
    pub steam_id: String,
    #[serde(rename = "bmId")]
    pub bm_id: String,
    #[serde(rename = "createdAt")]
    pub created_at: DateTime<Utc>,
}

pub async fn list_members(pool: &PgPool, group_id: i64) -> Result<Vec<TrackGroupMember>, sqlx::Error> {
    let rows = sqlx::query(
        "SELECT track_group_id, steam_id, bm_id, created_at FROM track_group_members \
         WHERE track_group_id = $1 ORDER BY steam_id ASC",
    )
    .bind(group_id)
    .fetch_all(pool)
    .await?;
    rows.iter()
        .map(|r| {
            Ok(TrackGroupMember {
                track_group_id: r.try_get("track_group_id")?,
                steam_id: r.try_get("steam_id")?,
                bm_id: r.try_get("bm_id")?,
                created_at: r.try_get("created_at")?,
            })
        })
        .collect()
}

/// `AddTrackGroupMembers` (tx): skip members with empty `steam_id`; upsert each,
/// preserving a stored non-empty `bm_id` when the incoming one is empty. Returns
/// the steam IDs that were newly inserted (`xmax = 0`).
pub async fn add_members(
    pool: &PgPool,
    group_id: i64,
    members: &[(String, String)],
) -> Result<Vec<String>, sqlx::Error> {
    let mut tx = pool.begin().await?;
    let mut added = Vec::new();
    for (steam_id, bm_id) in members {
        if steam_id.is_empty() {
            continue;
        }
        let inserted: bool = sqlx::query_scalar(
            r#"INSERT INTO track_group_members (track_group_id, steam_id, bm_id)
               VALUES ($1, $2, $3)
               ON CONFLICT (track_group_id, steam_id) DO UPDATE
               SET bm_id = CASE WHEN EXCLUDED.bm_id <> '' THEN EXCLUDED.bm_id ELSE track_group_members.bm_id END
               RETURNING (xmax = 0)"#,
        )
        .bind(group_id)
        .bind(steam_id)
        .bind(bm_id)
        .fetch_one(&mut *tx)
        .await?;
        if inserted {
            added.push(steam_id.clone());
        }
    }
    tx.commit().await?;
    Ok(added)
}

/// `RemoveTrackGroupMembers`: identifiers of length 17 are Steam IDs, otherwise
/// BM IDs; empty/whitespace dropped. Empty input short-circuits to 0. Returns
/// rows deleted.
pub async fn remove_members(
    pool: &PgPool,
    group_id: i64,
    identifiers: &[String],
) -> Result<u64, sqlx::Error> {
    let mut steam_ids = Vec::new();
    let mut bm_ids = Vec::new();
    for raw in identifiers {
        let id = raw.trim();
        if id.is_empty() {
            continue;
        }
        if id.len() == 17 {
            steam_ids.push(id.to_string());
        } else {
            bm_ids.push(id.to_string());
        }
    }
    if steam_ids.is_empty() && bm_ids.is_empty() {
        return Ok(0);
    }
    let res = sqlx::query(
        "DELETE FROM track_group_members \
         WHERE track_group_id = $1 AND (steam_id = ANY($2::text[]) OR bm_id = ANY($3::text[]))",
    )
    .bind(group_id)
    .bind(&steam_ids)
    .bind(&bm_ids)
    .execute(pool)
    .await?;
    Ok(res.rows_affected())
}

// ---- online-state ----

#[derive(Debug, Clone, Serialize)]
pub struct OnlineRow {
    #[serde(rename = "steamId")]
    pub steam_id: String,
    #[serde(rename = "bmId")]
    pub bm_id: String,
    #[serde(rename = "bmName")]
    pub bm_name: String,
    #[serde(rename = "discordId")]
    pub discord_id: String,
    #[serde(rename = "avatarImage")]
    pub avatar_image: String,
    #[serde(rename = "isOnline")]
    pub is_online: bool,
    /// online_since epoch when online, else offline_since epoch (unix seconds).
    #[serde(rename = "onlineUnix")]
    pub online_unix: i64,
}

/// `ListTrackGroupOnlineState`: cross-domain read across steam_accounts,
/// steam_account_cache, bm_profile_cache, player_states, guild_steam_discord_links.
pub async fn list_online_state(pool: &PgPool, group_id: i64) -> Result<Vec<OnlineRow>, sqlx::Error> {
    let rows = sqlx::query(
        r#"SELECT
               tgm.steam_id,
               COALESCE(NULLIF(tgm.bm_id, ''), COALESCE(sa.bm_id, '')) AS bm_id,
               COALESCE(bpc.bm_name, '') AS bm_name,
               COALESCE(gsl.discord_id, '') AS discord_id,
               COALESCE(sac.avatar_image, '') AS avatar_image,
               COALESCE(ps.is_online, FALSE) AS is_online,
               COALESCE(EXTRACT(EPOCH FROM ps.online_since)::BIGINT, 0) AS online_unix,
               COALESCE(EXTRACT(EPOCH FROM ps.offline_since)::BIGINT, 0) AS offline_unix
           FROM track_groups tg
           JOIN track_group_members tgm ON tgm.track_group_id = tg.id
           LEFT JOIN steam_accounts sa ON sa.steam_id = tgm.steam_id
           LEFT JOIN steam_account_cache sac ON sac.steam_id = tgm.steam_id
           LEFT JOIN bm_profile_cache bpc ON bpc.bm_id = COALESCE(NULLIF(tgm.bm_id, ''), sa.bm_id)
           LEFT JOIN LATERAL (
               SELECT discord_id
               FROM guild_steam_discord_links
               WHERE guild_id = tg.guild_id AND steam_id = tgm.steam_id
               ORDER BY discord_id ASC
               LIMIT 1
           ) gsl ON TRUE
           LEFT JOIN player_states ps
               ON ps.server_bm_id = tg.server_bm_id
               AND ps.player_bm_id = COALESCE(NULLIF(tgm.bm_id, ''), sa.bm_id)
           WHERE tg.id = $1
           ORDER BY is_online DESC, bm_name ASC, tgm.steam_id ASC"#,
    )
    .bind(group_id)
    .fetch_all(pool)
    .await?;
    rows.iter()
        .map(|r| {
            let is_online: bool = r.try_get("is_online")?;
            let online_unix: i64 = r.try_get("online_unix")?;
            let offline_unix: i64 = r.try_get("offline_unix")?;
            Ok(OnlineRow {
                steam_id: r.try_get("steam_id")?,
                bm_id: r.try_get("bm_id")?,
                bm_name: r.try_get("bm_name")?,
                discord_id: r.try_get("discord_id")?,
                avatar_image: r.try_get("avatar_image")?,
                is_online,
                // Not online → emit the offline-streak start instead.
                online_unix: if is_online { online_unix } else { offline_unix },
            })
        })
        .collect()
}
