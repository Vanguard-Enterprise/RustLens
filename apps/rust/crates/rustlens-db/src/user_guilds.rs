//! `user_guilds` membership state, owned by the `guilds` service. Mirrors
//! `CreateUserGuildLink`/`MarkUserGuildLeft`/`ListUserGuildLinksByDiscordID` +
//! `BulkUpsertUserGuilds` in `internal/store/steam.go`. Schema owned by Go.

use chrono::{DateTime, Utc};
use serde::Serialize;
use sqlx::{PgPool, Row};

/// One membership row (`{discordId,guildId,isActive,firstSeenAt,lastSeenAt,leftAt}`).
#[derive(Debug, Clone, Serialize)]
pub struct UserGuildLink {
    #[serde(rename = "discordId")]
    pub discord_id: String,
    #[serde(rename = "guildId")]
    pub guild_id: String,
    #[serde(rename = "isActive")]
    pub is_active: bool,
    #[serde(rename = "firstSeenAt")]
    pub first_seen_at: DateTime<Utc>,
    #[serde(rename = "lastSeenAt")]
    pub last_seen_at: DateTime<Utc>,
    #[serde(rename = "leftAt")]
    pub left_at: Option<DateTime<Utc>>,
}

const RETURNING: &str = "discord_id, guild_id, is_active, first_seen_at, last_seen_at, left_at";

fn row_to_link(row: &sqlx::postgres::PgRow) -> Result<UserGuildLink, sqlx::Error> {
    Ok(UserGuildLink {
        discord_id: row.try_get("discord_id")?,
        guild_id: row.try_get("guild_id")?,
        is_active: row.try_get("is_active")?,
        first_seen_at: row.try_get("first_seen_at")?,
        last_seen_at: row.try_get("last_seen_at")?,
        left_at: row.try_get("left_at")?,
    })
}

/// `CreateUserGuildLink`: upsert the pair active (`first_seen_at` preserved on
/// conflict).
pub async fn create(pool: &PgPool, discord_id: &str, guild_id: &str) -> Result<UserGuildLink, sqlx::Error> {
    let row = sqlx::query(&format!(
        r#"INSERT INTO user_guilds (discord_id, guild_id, is_active, first_seen_at, last_seen_at, left_at)
           VALUES ($1, $2, TRUE, NOW(), NOW(), NULL)
           ON CONFLICT (discord_id, guild_id) DO UPDATE
           SET is_active = TRUE, last_seen_at = NOW(), left_at = NULL
           RETURNING {RETURNING}"#
    ))
    .bind(discord_id)
    .bind(guild_id)
    .fetch_one(pool)
    .await?;
    row_to_link(&row)
}

/// `MarkUserGuildLeft`: upsert the pair left (inserted directly in left state
/// when absent).
pub async fn mark_left(pool: &PgPool, discord_id: &str, guild_id: &str) -> Result<UserGuildLink, sqlx::Error> {
    let row = sqlx::query(&format!(
        r#"INSERT INTO user_guilds (discord_id, guild_id, is_active, first_seen_at, last_seen_at, left_at)
           VALUES ($1, $2, FALSE, NOW(), NOW(), NOW())
           ON CONFLICT (discord_id, guild_id) DO UPDATE
           SET is_active = FALSE, last_seen_at = NOW(), left_at = NOW()
           RETURNING {RETURNING}"#
    ))
    .bind(discord_id)
    .bind(guild_id)
    .fetch_one(pool)
    .await?;
    row_to_link(&row)
}

/// `ListUserGuildLinksByDiscordID`, ordered `guild_id ASC`.
pub async fn list_by_discord(pool: &PgPool, discord_id: &str) -> Result<Vec<UserGuildLink>, sqlx::Error> {
    let rows = sqlx::query(&format!(
        "SELECT {RETURNING} FROM user_guilds WHERE discord_id = $1 ORDER BY guild_id ASC"
    ))
    .bind(discord_id)
    .fetch_all(pool)
    .await?;
    rows.iter().map(row_to_link).collect()
}

/// `BulkUpsertUserGuilds`: upsert each `(discord_id, guild_id)` active. No-op on
/// an empty slice (mirrors the Go early return).
pub async fn bulk_upsert(pool: &PgPool, guild_id: &str, discord_ids: &[String]) -> Result<(), sqlx::Error> {
    if discord_ids.is_empty() {
        return Ok(());
    }
    sqlx::query(
        r#"INSERT INTO user_guilds (discord_id, guild_id, is_active, first_seen_at, last_seen_at, left_at)
           SELECT did, $1, TRUE, NOW(), NOW(), NULL FROM unnest($2::text[]) AS did
           ON CONFLICT (discord_id, guild_id) DO UPDATE
           SET is_active = TRUE, last_seen_at = NOW(), left_at = NULL"#,
    )
    .bind(guild_id)
    .bind(discord_ids)
    .execute(pool)
    .await?;
    Ok(())
}
