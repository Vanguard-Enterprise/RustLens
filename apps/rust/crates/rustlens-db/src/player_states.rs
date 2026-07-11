//! Player presence (`player_states`) reads + the poller's `sync_player_states`
//! diff, owned by the `servers` service. Mirrors `internal/store/player_states.go`,
//! `online_summary.go`, `tracked_log_events.go`. Schema owned by Go.
//!
//! The `PlayerState` struct + `list_player_states_by_player` (already in
//! `players_steam`) are reused for the by-player route.

use chrono::{DateTime, Duration, Utc};
use serde::Serialize;
use sqlx::{PgPool, Row};

pub use crate::players_steam::PlayerState;

/// One presence-search hit (`{serverBmId,serverName,playerBmId,steamId,bmName,isOnline}`).
#[derive(Debug, Clone, Serialize)]
pub struct SearchResult {
    #[serde(rename = "serverBmId")]
    pub server_bm_id: String,
    #[serde(rename = "serverName")]
    pub server_name: String,
    #[serde(rename = "playerBmId")]
    pub player_bm_id: String,
    #[serde(rename = "steamId")]
    pub steam_id: String,
    #[serde(rename = "bmName")]
    pub bm_name: String,
    #[serde(rename = "isOnline")]
    pub is_online: bool,
}

/// One guild online-summary row (one per Discord user, most-recently-online server).
#[derive(Debug, Clone, Serialize)]
pub struct OnlineSummaryItem {
    #[serde(rename = "guildId")]
    pub guild_id: String,
    #[serde(rename = "discordId")]
    pub discord_id: String,
    #[serde(rename = "steamId")]
    pub steam_id: String,
    #[serde(rename = "playerBmId")]
    pub player_bm_id: String,
    #[serde(rename = "bmName")]
    pub bm_name: String,
    #[serde(rename = "serverBmId")]
    pub server_bm_id: String,
    #[serde(rename = "serverName")]
    pub server_name: String,
    #[serde(rename = "onlineUnix")]
    pub online_unix: i64,
}

/// One tracked-server log-event snapshot row (incl. offline).
#[derive(Debug, Clone, Serialize)]
pub struct LogEvent {
    #[serde(rename = "serverBmId")]
    pub server_bm_id: String,
    #[serde(rename = "serverName")]
    pub server_name: String,
    #[serde(rename = "playerBmId")]
    pub player_bm_id: String,
    #[serde(rename = "steamId")]
    pub steam_id: String,
    #[serde(rename = "discordId")]
    pub discord_id: String,
    #[serde(rename = "bmName")]
    pub bm_name: String,
    #[serde(rename = "avatarImage")]
    pub avatar_image: String,
    #[serde(rename = "isOnline")]
    pub is_online: bool,
    #[serde(rename = "occurredUnix")]
    pub occurred_unix: i64,
}

/// A presence input for `sync_player_states`.
#[derive(Debug, Clone)]
pub struct PresenceInput {
    pub player_bm_id: String,
    pub bm_name: String,
}

const STATE_COLS: &str = "ps.server_bm_id, ts.name AS server_name, ps.player_bm_id, ps.is_online, ps.online_since, ps.offline_since, ps.updated_at";

fn state_row(row: &sqlx::postgres::PgRow) -> Result<PlayerState, sqlx::Error> {
    Ok(PlayerState {
        server_bm_id: row.try_get("server_bm_id")?,
        server_name: row.try_get("server_name")?,
        player_bm_id: row.try_get("player_bm_id")?,
        is_online: row.try_get("is_online")?,
        online_since: row.try_get("online_since")?,
        offline_since: row.try_get("offline_since")?,
        updated_at: row.try_get("updated_at")?,
    })
}

/// Online rows for a server (online-first, latest-online first).
pub async fn list_online_by_server(pool: &PgPool, server_bm_id: &str) -> Result<Vec<PlayerState>, sqlx::Error> {
    let rows = sqlx::query(&format!(
        r#"SELECT {STATE_COLS}
           FROM player_states ps
           JOIN tracked_servers ts ON ts.bm_id = ps.server_bm_id
           WHERE ps.server_bm_id = $1 AND ps.is_online = TRUE
           ORDER BY ps.online_since DESC NULLS LAST, ps.updated_at DESC, ps.player_bm_id ASC"#
    ))
    .bind(server_bm_id)
    .fetch_all(pool)
    .await?;
    rows.iter().map(state_row).collect()
}

/// A single presence row (`None` when absent).
pub async fn get_state(pool: &PgPool, server_bm_id: &str, player_bm_id: &str) -> Result<Option<PlayerState>, sqlx::Error> {
    let row = sqlx::query(&format!(
        r#"SELECT {STATE_COLS}
           FROM player_states ps
           JOIN tracked_servers ts ON ts.bm_id = ps.server_bm_id
           WHERE ps.server_bm_id = $1 AND ps.player_bm_id = $2"#
    ))
    .bind(server_bm_id)
    .bind(player_bm_id)
    .fetch_optional(pool)
    .await?;
    row.as_ref().map(state_row).transpose()
}

/// ILIKE `bm_profile_cache.bm_name` search on one server (INNER join
/// `steam_accounts`). Online by default; `include_offline` adds ≤24h-offline.
pub async fn search_by_name(
    pool: &PgPool,
    server_bm_id: &str,
    q: &str,
    limit: i64,
    include_offline: bool,
) -> Result<Vec<SearchResult>, sqlx::Error> {
    let cutoff = Utc::now() - Duration::hours(24);
    let rows = sqlx::query(
        r#"SELECT ps.server_bm_id, ts.name, ps.player_bm_id, sa.steam_id,
                  COALESCE(bpc.bm_name, '') AS bm_name, ps.is_online
           FROM player_states ps
           JOIN tracked_servers ts ON ts.bm_id = ps.server_bm_id
           JOIN steam_accounts sa ON sa.bm_id = ps.player_bm_id
           LEFT JOIN bm_profile_cache bpc ON bpc.bm_id = sa.bm_id
           WHERE ps.server_bm_id = $1
             AND (ps.is_online = TRUE OR ($4 = TRUE AND ps.offline_since IS NOT NULL AND ps.offline_since >= $5))
             AND COALESCE(bpc.bm_name, '') ILIKE '%' || $2 || '%'
           ORDER BY ps.is_online DESC, COALESCE(bpc.bm_name, '') ASC, sa.steam_id ASC
           LIMIT $3"#,
    )
    .bind(server_bm_id)
    .bind(q)
    .bind(limit)
    .bind(include_offline)
    .bind(cutoff)
    .fetch_all(pool)
    .await?;
    rows.iter()
        .map(|row| {
            Ok(SearchResult {
                server_bm_id: row.try_get("server_bm_id")?,
                server_name: row.try_get("name")?,
                player_bm_id: row.try_get("player_bm_id")?,
                steam_id: row.try_get("steam_id")?,
                bm_name: row.try_get("bm_name")?,
                is_online: row.try_get("is_online")?,
            })
        })
        .collect()
}

/// Guild online summary — one row per Discord user (most-recently-online server),
/// across bot-wide `tracked_servers`.
pub async fn list_online_summary(pool: &PgPool, guild_id: &str) -> Result<Vec<OnlineSummaryItem>, sqlx::Error> {
    let rows = sqlx::query(
        r#"SELECT guild_id, discord_id, steam_id, bm_id, bm_name, server_bm_id, server_name, online_unix
           FROM (
               SELECT gsl.guild_id, gsl.discord_id, gsl.steam_id, sa.bm_id,
                   COALESCE(bpc.bm_name, '') AS bm_name, ps.server_bm_id, ts.name AS server_name,
                   EXTRACT(EPOCH FROM ps.online_since)::BIGINT AS online_unix,
                   ROW_NUMBER() OVER (PARTITION BY gsl.discord_id
                       ORDER BY ps.online_since DESC, ps.updated_at DESC, ps.server_bm_id ASC) AS summary_rank
               FROM guild_steam_discord_links gsl
               JOIN steam_accounts sa ON sa.steam_id = gsl.steam_id
               LEFT JOIN bm_profile_cache bpc ON bpc.bm_id = sa.bm_id
               JOIN player_states ps ON ps.player_bm_id = sa.bm_id
               JOIN tracked_servers ts ON ts.bm_id = ps.server_bm_id
               WHERE gsl.guild_id = $1 AND sa.bm_id IS NOT NULL AND sa.bm_id <> ''
                 AND ps.is_online = TRUE AND ps.online_since IS NOT NULL
           ) summary
           WHERE summary_rank = 1
           ORDER BY server_name ASC, online_unix DESC, discord_id ASC, bm_id ASC"#,
    )
    .bind(guild_id)
    .fetch_all(pool)
    .await?;
    rows.iter()
        .map(|row| {
            Ok(OnlineSummaryItem {
                guild_id: row.try_get("guild_id")?,
                discord_id: row.try_get("discord_id")?,
                steam_id: row.try_get("steam_id")?,
                player_bm_id: row.try_get("bm_id")?,
                bm_name: row.try_get("bm_name")?,
                server_bm_id: row.try_get("server_bm_id")?,
                server_name: row.try_get("server_name")?,
                online_unix: row.try_get("online_unix")?,
            })
        })
        .collect()
}

/// Guild tracked-server log-event snapshot (guild-scoped selection + links; incl. offline).
pub async fn list_log_events(pool: &PgPool, guild_id: &str) -> Result<Vec<LogEvent>, sqlx::Error> {
    let rows = sqlx::query(
        r#"SELECT ps.server_bm_id, ts.name AS server_name, ps.player_bm_id, sa.steam_id, gsl.discord_id,
                  COALESCE(bpc.bm_name, '') AS bm_name, COALESCE(sac.avatar_image, '') AS avatar_image,
                  ps.is_online,
                  EXTRACT(EPOCH FROM COALESCE(ps.online_since, ps.offline_since, ps.updated_at))::BIGINT AS occurred_unix
           FROM player_states ps
           JOIN tracked_servers ts ON ts.bm_id = ps.server_bm_id
           JOIN guild_tracked_servers gts ON gts.server_bm_id = ps.server_bm_id AND gts.guild_id = $1
           JOIN steam_accounts sa ON sa.bm_id = ps.player_bm_id
           JOIN guild_steam_discord_links gsl ON gsl.steam_id = sa.steam_id AND gsl.guild_id = $1
           LEFT JOIN steam_account_cache sac ON sac.steam_id = sa.steam_id
           LEFT JOIN bm_profile_cache bpc ON bpc.bm_id = sa.bm_id
           ORDER BY ts.name ASC, gsl.discord_id ASC, sa.steam_id ASC, ps.player_bm_id ASC"#,
    )
    .bind(guild_id)
    .fetch_all(pool)
    .await?;
    rows.iter()
        .map(|row| {
            Ok(LogEvent {
                server_bm_id: row.try_get("server_bm_id")?,
                server_name: row.try_get("server_name")?,
                player_bm_id: row.try_get("player_bm_id")?,
                steam_id: row.try_get("steam_id")?,
                discord_id: row.try_get("discord_id")?,
                bm_name: row.try_get("bm_name")?,
                avatar_image: row.try_get("avatar_image")?,
                is_online: row.try_get("is_online")?,
                occurred_unix: row.try_get("occurred_unix")?,
            })
        })
        .collect()
}

/// Count online rows for a server.
pub async fn count_online(pool: &PgPool, server_bm_id: &str) -> Result<i64, sqlx::Error> {
    sqlx::query_scalar("SELECT COUNT(*) FROM player_states WHERE server_bm_id = $1 AND is_online")
        .bind(server_bm_id)
        .fetch_one(pool)
        .await
}

/// Sync a server's fetched player list into `player_states` (the poller diff).
/// One transaction, `FOR UPDATE` load, never deletes; `observed_at` is reused for
/// every timestamp. Mirrors `SyncPlayerStates` byte-for-byte.
pub async fn sync_player_states(
    pool: &PgPool,
    server_bm_id: &str,
    players: &[PresenceInput],
    observed_at: DateTime<Utc>,
) -> Result<(), sqlx::Error> {
    let mut tx = pool.begin().await?;

    let existing_rows =
        sqlx::query("SELECT player_bm_id, is_online FROM player_states WHERE server_bm_id = $1 FOR UPDATE")
            .bind(server_bm_id)
            .fetch_all(&mut *tx)
            .await?;
    // player_bm_id -> is_online
    let mut existing: std::collections::HashMap<String, bool> = existing_rows
        .iter()
        .map(|r| (r.get::<String, _>("player_bm_id"), r.get::<bool, _>("is_online")))
        .collect();

    let mut seen: std::collections::HashSet<&str> = std::collections::HashSet::new();
    for player in players {
        if player.player_bm_id.is_empty() {
            continue;
        }
        seen.insert(player.player_bm_id.as_str());

        if !player.bm_name.is_empty() {
            sqlx::query(
                r#"INSERT INTO bm_profile_cache (bm_id, bm_name, updated_at)
                   VALUES ($1, $2, 'epoch'::timestamptz)
                   ON CONFLICT (bm_id) DO UPDATE SET bm_name = EXCLUDED.bm_name
                   WHERE bm_profile_cache.bm_name IS DISTINCT FROM EXCLUDED.bm_name"#,
            )
            .bind(&player.player_bm_id)
            .bind(&player.bm_name)
            .execute(&mut *tx)
            .await?;
        }

        match existing.get(&player.player_bm_id).copied() {
            None => {
                sqlx::query(
                    r#"INSERT INTO player_states (server_bm_id, player_bm_id, is_online, online_since, offline_since, updated_at)
                       VALUES ($1, $2, TRUE, $3, NULL, $3)"#,
                )
                .bind(server_bm_id)
                .bind(&player.player_bm_id)
                .bind(observed_at)
                .execute(&mut *tx)
                .await?;
            }
            Some(true) => {
                sqlx::query("UPDATE player_states SET updated_at = $3 WHERE server_bm_id = $1 AND player_bm_id = $2")
                    .bind(server_bm_id)
                    .bind(&player.player_bm_id)
                    .bind(observed_at)
                    .execute(&mut *tx)
                    .await?;
            }
            Some(false) => {
                sqlx::query(
                    r#"UPDATE player_states SET is_online = TRUE, online_since = $3, offline_since = NULL, updated_at = $3
                       WHERE server_bm_id = $1 AND player_bm_id = $2"#,
                )
                .bind(server_bm_id)
                .bind(&player.player_bm_id)
                .bind(observed_at)
                .execute(&mut *tx)
                .await?;
            }
        }
        existing.insert(player.player_bm_id.clone(), true);
    }

    // Mark online rows that were not seen this poll as offline.
    for (player_bm_id, is_online) in &existing {
        if !*is_online || seen.contains(player_bm_id.as_str()) {
            continue;
        }
        sqlx::query(
            r#"UPDATE player_states SET is_online = FALSE, offline_since = $3, updated_at = $3
               WHERE server_bm_id = $1 AND player_bm_id = $2"#,
        )
        .bind(server_bm_id)
        .bind(player_bm_id)
        .bind(observed_at)
        .execute(&mut *tx)
        .await?;
    }

    tx.commit().await?;
    Ok(())
}
