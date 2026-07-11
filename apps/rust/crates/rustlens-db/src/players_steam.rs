//! Steam-account + cache + name-history + tracked-server/player-state reads for
//! the players service. Mirrors `internal/store/steam.go`, `tracked_servers.go`,
//! and `player_states.go`. Schema owned by Go — read/write only. `get_*` return
//! `Ok(None)` for the Go `ErrSteamAccountNotFound` (no-rows) case.

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use sqlx::types::Json;
use sqlx::{PgPool, Row};

pub use crate::players_links::UPSERT_STEAM_ACCOUNT_SQL;

/// A resolved steam account (joined view). Serializes as the Go `steamAccountToMap`.
#[derive(Debug, Clone, Serialize)]
pub struct SteamAccount {
    #[serde(rename = "steamId")]
    pub steam_id: String,
    #[serde(rename = "bmId")]
    pub bm_id: Option<String>,
    #[serde(rename = "bmName")]
    pub bm_name: String,
    #[serde(rename = "avatarImage")]
    pub avatar_image: String,
    #[serde(rename = "firstSeenAt")]
    pub first_seen_at: Option<DateTime<Utc>>,
    #[serde(rename = "createdAt")]
    pub created_at: DateTime<Utc>,
    #[serde(rename = "updatedAt")]
    pub updated_at: DateTime<Utc>,
}

/// A top-owned-game entry (`top_games` JSONB element).
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct AnalyticsGame {
    #[serde(rename = "appId")]
    pub app_id: i32,
    pub name: String,
    pub hours: i64,
}

/// A top-played-server entry (`top_servers` JSONB element).
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct AnalyticsServer {
    #[serde(rename = "bmId")]
    pub bm_id: String,
    pub name: String,
    pub hours: i64,
}

/// The Steam-side cache row (`steam_account_cache`).
#[derive(Debug, Clone, Default)]
pub struct SteamAccountCache {
    pub steam_id: String,
    pub persona_name: String,
    pub avatar_image: String,
    pub rust_hours: Option<i64>,
    pub total_games: Option<i32>,
    pub top_games: Vec<AnalyticsGame>,
    pub game_banned: bool,
    pub game_banned_at: Option<DateTime<Utc>>,
    pub game_ban_checked_at: Option<DateTime<Utc>>,
    pub updated_at: DateTime<Utc>,
}

/// The BattleMetrics-side cache row (`bm_profile_cache`).
#[derive(Debug, Clone, Default)]
pub struct BmProfileCache {
    pub bm_id: String,
    pub bm_name: String,
    pub first_seen_at: Option<DateTime<Utc>>,
    pub vitality_hours: i64,
    pub atlas_hours: i64,
    pub vital_hours: i64,
    pub modded_hours: i64,
    pub official_hours: i64,
    pub build_hours: i64,
    pub bm_hours: i64,
    pub bm_servers_played: i32,
    pub top_servers: Vec<AnalyticsServer>,
    pub updated_at: DateTime<Utc>,
}

/// A tracked server (category flags used for profile hour buckets).
#[derive(Debug, Clone)]
pub struct TrackedServer {
    pub bm_id: String,
    pub name: String,
    pub is_vitality: bool,
    pub is_atlas: bool,
    pub is_vital: bool,
    pub is_modded: bool,
    pub is_official: bool,
    pub is_build: bool,
    pub high_frequency: bool,
}

/// A player presence state (profile online-status).
#[derive(Debug, Clone)]
pub struct PlayerState {
    pub server_bm_id: String,
    pub server_name: String,
    pub player_bm_id: String,
    pub is_online: bool,
    pub online_since: Option<DateTime<Utc>>,
    pub offline_since: Option<DateTime<Utc>>,
    pub updated_at: DateTime<Utc>,
}

/// A `bm_name_history` observation (name + when last seen).
#[derive(Debug, Clone)]
pub struct NameHistory {
    pub name: String,
    pub last_seen_at: DateTime<Utc>,
}

const ACCOUNT_SELECT: &str = r#"
    SELECT sa.steam_id, sa.bm_id,
        COALESCE(bpc.bm_name, '') AS bm_name,
        COALESCE(sac.avatar_image, '') AS avatar_image,
        bpc.first_seen_at,
        sa.created_at,
        COALESCE(sac.updated_at, sa.created_at) AS updated_at
    FROM steam_accounts sa
    LEFT JOIN steam_account_cache sac ON sac.steam_id = sa.steam_id
    LEFT JOIN bm_profile_cache bpc ON bpc.bm_id = sa.bm_id
"#;

fn account_row(row: &sqlx::postgres::PgRow) -> Result<SteamAccount, sqlx::Error> {
    Ok(SteamAccount {
        steam_id: row.try_get("steam_id")?,
        bm_id: row.try_get("bm_id")?,
        bm_name: row.try_get("bm_name")?,
        avatar_image: row.try_get("avatar_image")?,
        first_seen_at: row.try_get("first_seen_at")?,
        created_at: row.try_get("created_at")?,
        updated_at: row.try_get("updated_at")?,
    })
}

pub async fn get_steam_account_by_steam_id(
    pool: &PgPool,
    steam_id: &str,
) -> Result<Option<SteamAccount>, sqlx::Error> {
    let row = sqlx::query(&format!("{ACCOUNT_SELECT} WHERE sa.steam_id = $1"))
        .bind(steam_id)
        .fetch_optional(pool)
        .await?;
    row.as_ref().map(account_row).transpose()
}

pub async fn get_steam_account_by_bm_id(
    pool: &PgPool,
    bm_id: &str,
) -> Result<Option<SteamAccount>, sqlx::Error> {
    let row = sqlx::query(&format!("{ACCOUNT_SELECT} WHERE sa.bm_id = $1"))
        .bind(bm_id)
        .fetch_optional(pool)
        .await?;
    row.as_ref().map(account_row).transpose()
}

/// Upsert `steam_accounts` (bm_id-conflict-tolerant). Returns the minimal row
/// (`bm_name`/`avatar_image` empty, `updated_at = created_at`).
pub async fn upsert_steam_account(
    pool: &PgPool,
    steam_id: &str,
    bm_id: Option<&str>,
) -> Result<SteamAccount, sqlx::Error> {
    let row = sqlx::query(&format!(
        "{UPSERT_STEAM_ACCOUNT_SQL} RETURNING steam_id, bm_id, created_at"
    ))
    .bind(steam_id)
    .bind(bm_id)
    .fetch_one(pool)
    .await?;
    let created_at: DateTime<Utc> = row.try_get("created_at")?;
    Ok(SteamAccount {
        steam_id: row.try_get("steam_id")?,
        bm_id: row.try_get("bm_id")?,
        bm_name: String::new(),
        avatar_image: String::new(),
        first_seen_at: None,
        created_at,
        updated_at: created_at,
    })
}

fn steam_cache_row(row: &sqlx::postgres::PgRow) -> Result<SteamAccountCache, sqlx::Error> {
    let top_games: Json<Vec<AnalyticsGame>> = row.try_get("top_games")?;
    Ok(SteamAccountCache {
        steam_id: row.try_get("steam_id")?,
        persona_name: row.try_get("persona_name")?,
        avatar_image: row.try_get("avatar_image")?,
        rust_hours: row.try_get("rust_hours")?,
        total_games: row.try_get("total_games")?,
        top_games: top_games.0,
        game_banned: row.try_get("game_banned")?,
        game_banned_at: row.try_get("game_banned_at")?,
        game_ban_checked_at: row.try_get("game_ban_checked_at")?,
        updated_at: row.try_get("updated_at")?,
    })
}

const STEAM_CACHE_COLS: &str = "steam_id, persona_name, avatar_image, rust_hours, total_games, top_games, game_banned, game_banned_at, game_ban_checked_at, updated_at";

pub async fn get_steam_account_cache(
    pool: &PgPool,
    steam_id: &str,
) -> Result<Option<SteamAccountCache>, sqlx::Error> {
    let row = sqlx::query(&format!(
        "SELECT {STEAM_CACHE_COLS} FROM steam_account_cache WHERE steam_id = $1"
    ))
    .bind(steam_id)
    .fetch_optional(pool)
    .await?;
    row.as_ref().map(steam_cache_row).transpose()
}

/// Upsert the Steam-side cache (7 columns; game-ban columns preserved). Persona
/// name is never clobbered by an empty value.
pub async fn upsert_steam_account_cache(
    pool: &PgPool,
    cache: &SteamAccountCache,
) -> Result<SteamAccountCache, sqlx::Error> {
    let row = sqlx::query(&format!(
        r#"INSERT INTO steam_account_cache
               (steam_id, persona_name, avatar_image, rust_hours, total_games, top_games, updated_at)
           VALUES ($1, $2, $3, $4, $5, $6, NOW())
           ON CONFLICT (steam_id) DO UPDATE
           SET persona_name = CASE WHEN EXCLUDED.persona_name <> '' THEN EXCLUDED.persona_name
                                   ELSE steam_account_cache.persona_name END,
               avatar_image = EXCLUDED.avatar_image,
               rust_hours = EXCLUDED.rust_hours,
               total_games = EXCLUDED.total_games,
               top_games = EXCLUDED.top_games,
               updated_at = NOW()
           RETURNING {STEAM_CACHE_COLS}"#
    ))
    .bind(&cache.steam_id)
    .bind(&cache.persona_name)
    .bind(&cache.avatar_image)
    .bind(cache.rust_hours)
    .bind(cache.total_games)
    .bind(Json(&cache.top_games))
    .fetch_one(pool)
    .await?;
    steam_cache_row(&row)
}

fn bm_cache_row(row: &sqlx::postgres::PgRow) -> Result<BmProfileCache, sqlx::Error> {
    let top_servers: Json<Vec<AnalyticsServer>> = row.try_get("top_servers")?;
    Ok(BmProfileCache {
        bm_id: row.try_get("bm_id")?,
        bm_name: row.try_get("bm_name")?,
        first_seen_at: row.try_get("first_seen_at")?,
        vitality_hours: row.try_get("vitality_hours")?,
        atlas_hours: row.try_get("atlas_hours")?,
        vital_hours: row.try_get("vital_hours")?,
        modded_hours: row.try_get("modded_hours")?,
        official_hours: row.try_get("official_hours")?,
        build_hours: row.try_get("build_hours")?,
        bm_hours: row.try_get("bm_hours")?,
        bm_servers_played: row.try_get("bm_servers_played")?,
        top_servers: top_servers.0,
        updated_at: row.try_get("updated_at")?,
    })
}

const BM_CACHE_COLS: &str = "bm_id, bm_name, first_seen_at, vitality_hours, atlas_hours, vital_hours, modded_hours, official_hours, build_hours, bm_hours, bm_servers_played, top_servers, updated_at";

pub async fn get_bm_profile_cache(
    pool: &PgPool,
    bm_id: &str,
) -> Result<Option<BmProfileCache>, sqlx::Error> {
    let row = sqlx::query(&format!(
        "SELECT {BM_CACHE_COLS} FROM bm_profile_cache WHERE bm_id = $1"
    ))
    .bind(bm_id)
    .fetch_optional(pool)
    .await?;
    row.as_ref().map(bm_cache_row).transpose()
}

pub async fn upsert_bm_profile_cache(
    pool: &PgPool,
    cache: &BmProfileCache,
) -> Result<BmProfileCache, sqlx::Error> {
    let row = sqlx::query(&format!(
        r#"INSERT INTO bm_profile_cache
               (bm_id, bm_name, first_seen_at, vitality_hours, atlas_hours, vital_hours,
                modded_hours, official_hours, build_hours, bm_hours, bm_servers_played,
                top_servers, updated_at)
           VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12, NOW())
           ON CONFLICT (bm_id) DO UPDATE
           SET bm_name = EXCLUDED.bm_name, first_seen_at = EXCLUDED.first_seen_at,
               vitality_hours = EXCLUDED.vitality_hours, atlas_hours = EXCLUDED.atlas_hours,
               vital_hours = EXCLUDED.vital_hours, modded_hours = EXCLUDED.modded_hours,
               official_hours = EXCLUDED.official_hours, build_hours = EXCLUDED.build_hours,
               bm_hours = EXCLUDED.bm_hours, bm_servers_played = EXCLUDED.bm_servers_played,
               top_servers = EXCLUDED.top_servers, updated_at = NOW()
           RETURNING {BM_CACHE_COLS}"#
    ))
    .bind(&cache.bm_id)
    .bind(&cache.bm_name)
    .bind(cache.first_seen_at)
    .bind(cache.vitality_hours)
    .bind(cache.atlas_hours)
    .bind(cache.vital_hours)
    .bind(cache.modded_hours)
    .bind(cache.official_hours)
    .bind(cache.build_hours)
    .bind(cache.bm_hours)
    .bind(cache.bm_servers_played)
    .bind(Json(&cache.top_servers))
    .fetch_one(pool)
    .await?;
    bm_cache_row(&row)
}

/// Merge name identifiers into `bm_name_history` (one transaction; skip empty
/// name / zero timestamp).
pub async fn upsert_bm_name_history(
    pool: &PgPool,
    bm_id: &str,
    names: &[NameHistory],
) -> Result<(), sqlx::Error> {
    if bm_id.is_empty() || names.is_empty() {
        return Ok(());
    }
    let mut tx = pool.begin().await?;
    for item in names {
        if item.name.is_empty() {
            continue;
        }
        sqlx::query(
            r#"INSERT INTO bm_name_history (bm_id, name, last_seen_at, observed_at)
               VALUES ($1, $2, $3, NOW())
               ON CONFLICT (bm_id, name, last_seen_at) DO UPDATE SET observed_at = NOW()"#,
        )
        .bind(bm_id)
        .bind(&item.name)
        .bind(item.last_seen_at)
        .execute(&mut *tx)
        .await?;
    }
    tx.commit().await?;
    Ok(())
}

/// List name history for a BM ID, ordered `last_seen_at ASC, name ASC`.
pub async fn list_bm_name_history(pool: &PgPool, bm_id: &str) -> Result<Vec<NameHistory>, sqlx::Error> {
    let rows = sqlx::query(
        "SELECT name, last_seen_at FROM bm_name_history WHERE bm_id = $1 ORDER BY last_seen_at ASC, name ASC",
    )
    .bind(bm_id)
    .fetch_all(pool)
    .await?;
    rows.iter()
        .map(|row| {
            Ok(NameHistory {
                name: row.try_get("name")?,
                last_seen_at: row.try_get("last_seen_at")?,
            })
        })
        .collect()
}

pub async fn list_tracked_servers(pool: &PgPool) -> Result<Vec<TrackedServer>, sqlx::Error> {
    let rows = sqlx::query(
        r#"SELECT bm_id, name, is_vitality, is_atlas, is_vital, is_modded, is_official, is_build, high_frequency
           FROM tracked_servers ORDER BY name ASC, bm_id ASC"#,
    )
    .fetch_all(pool)
    .await?;
    rows.iter()
        .map(|row| {
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
            })
        })
        .collect()
}

/// `CountSteamAccounts`: the current `steam_accounts` row count, cached for up
/// to an hour in `cached_counts`. The refresh uses `FOR UPDATE SKIP LOCKED` so
/// only one instance rescans the table per stale window; others read the
/// existing cached value.
pub async fn count_steam_accounts(pool: &PgPool) -> Result<i64, sqlx::Error> {
    sqlx::query(
        r#"WITH stale AS (
               SELECT name FROM cached_counts
               WHERE name = 'steam_accounts'
                 AND updated_at < now() - interval '1 hour'
               FOR UPDATE SKIP LOCKED
           )
           UPDATE cached_counts c
           SET value = (SELECT count(*) FROM steam_accounts),
               updated_at = now()
           FROM stale
           WHERE c.name = stale.name"#,
    )
    .execute(pool)
    .await?;

    sqlx::query_scalar("SELECT value FROM cached_counts WHERE name = 'steam_accounts'")
        .fetch_one(pool)
        .await
}

pub async fn list_player_states_by_player(
    pool: &PgPool,
    player_bm_id: &str,
) -> Result<Vec<PlayerState>, sqlx::Error> {
    let rows = sqlx::query(
        r#"SELECT ps.server_bm_id, ts.name AS server_name, ps.player_bm_id, ps.is_online,
                  ps.online_since, ps.offline_since, ps.updated_at
           FROM player_states ps
           JOIN tracked_servers ts ON ts.bm_id = ps.server_bm_id
           WHERE ps.player_bm_id = $1
           ORDER BY ps.is_online DESC, ps.online_since DESC NULLS LAST, ps.updated_at DESC, ps.server_bm_id ASC"#,
    )
    .bind(player_bm_id)
    .fetch_all(pool)
    .await?;
    rows.iter()
        .map(|row| {
            Ok(PlayerState {
                server_bm_id: row.try_get("server_bm_id")?,
                server_name: row.try_get("server_name")?,
                player_bm_id: row.try_get("player_bm_id")?,
                is_online: row.try_get("is_online")?,
                online_since: row.try_get("online_since")?,
                offline_since: row.try_get("offline_since")?,
                updated_at: row.try_get("updated_at")?,
            })
        })
        .collect()
}
