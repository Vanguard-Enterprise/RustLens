//! `bot_guilds` metadata sync (upsert/leave/reconcile) + `members_synced_at`,
//! owned by the `guilds` service. Mirrors `internal/store/bot_guilds.go` (write
//! side; the admin list/detail reads land in `admin_guilds`). Schema owned by Go.

use chrono::{DateTime, Utc};
use sqlx::{PgPool, Row};

/// Metadata supplied on `bot-guilds/upsert`. Owner fields + name are stored
/// verbatim (unvalidated in Go); nullable fields map to SQL NULL when `None`.
#[derive(Debug, Clone, Default)]
pub struct BotGuildMetadata {
    pub guild_id: String,
    pub name: String,
    pub icon_hash: Option<String>,
    pub banner_hash: Option<String>,
    pub owner_discord_id: Option<String>,
    pub owner_username: Option<String>,
    pub owner_global_name: Option<String>,
    pub owner_avatar_hash: Option<String>,
    pub approximate_member_count: Option<i32>,
}

/// `UpsertBotGuildMetadata`: force `is_installed=TRUE`, `left_at=NULL`, refresh
/// `metadata_updated_at`. `installed_at` is preserved on conflict (not in the SET),
/// and `members_synced_at` is untouched.
pub async fn upsert_metadata(pool: &PgPool, m: &BotGuildMetadata) -> Result<(), sqlx::Error> {
    sqlx::query(
        r#"INSERT INTO bot_guilds (
               guild_id, name, icon_hash, banner_hash,
               owner_discord_id, owner_username, owner_global_name, owner_avatar_hash,
               approximate_member_count, is_installed, installed_at, left_at, metadata_updated_at
           )
           VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, TRUE, NOW(), NULL, NOW())
           ON CONFLICT (guild_id) DO UPDATE SET
               name                     = EXCLUDED.name,
               icon_hash                = EXCLUDED.icon_hash,
               banner_hash              = EXCLUDED.banner_hash,
               owner_discord_id         = EXCLUDED.owner_discord_id,
               owner_username           = EXCLUDED.owner_username,
               owner_global_name        = EXCLUDED.owner_global_name,
               owner_avatar_hash        = EXCLUDED.owner_avatar_hash,
               approximate_member_count = EXCLUDED.approximate_member_count,
               is_installed             = TRUE,
               left_at                  = NULL,
               metadata_updated_at      = NOW()"#,
    )
    .bind(&m.guild_id)
    .bind(&m.name)
    .bind(&m.icon_hash)
    .bind(&m.banner_hash)
    .bind(&m.owner_discord_id)
    .bind(&m.owner_username)
    .bind(&m.owner_global_name)
    .bind(&m.owner_avatar_hash)
    .bind(m.approximate_member_count)
    .execute(pool)
    .await?;
    Ok(())
}

/// `MarkBotGuildLeft`: no-op (0 rows) when the guild is absent — not an error.
pub async fn mark_left(pool: &PgPool, guild_id: &str) -> Result<(), sqlx::Error> {
    sqlx::query("UPDATE bot_guilds SET is_installed = FALSE, left_at = NOW() WHERE guild_id = $1")
        .bind(guild_id)
        .execute(pool)
        .await?;
    Ok(())
}

/// `ReconcileShardBotGuilds`: mark every installed guild in this shard's range
/// whose ID is not in `present` as left. Empty `present` clears the whole shard
/// range. Does not re-activate. `(guild_id >> 22) % shard_count == shard_id` is
/// Discord's standard sharding formula.
pub async fn reconcile(
    pool: &PgPool,
    shard_id: i64,
    shard_count: i64,
    present: &[String],
) -> Result<(), sqlx::Error> {
    sqlx::query(
        r#"UPDATE bot_guilds
           SET is_installed = FALSE, left_at = NOW()
           WHERE is_installed = TRUE
             AND ((guild_id::bigint >> 22) % $1::bigint) = $2::bigint
             AND guild_id <> ALL($3::text[])"#,
    )
    .bind(shard_count)
    .bind(shard_id)
    .bind(present)
    .execute(pool)
    .await?;
    Ok(())
}

/// `StampMembersSynced`.
pub async fn stamp_members_synced(pool: &PgPool, guild_id: &str) -> Result<(), sqlx::Error> {
    sqlx::query("UPDATE bot_guilds SET members_synced_at = NOW() WHERE guild_id = $1")
        .bind(guild_id)
        .execute(pool)
        .await?;
    Ok(())
}

/// `MembersSyncedAt`: `None` when the column is NULL or the guild is absent.
pub async fn members_synced_at(pool: &PgPool, guild_id: &str) -> Result<Option<DateTime<Utc>>, sqlx::Error> {
    let row = sqlx::query("SELECT members_synced_at FROM bot_guilds WHERE guild_id = $1")
        .bind(guild_id)
        .fetch_optional(pool)
        .await?;
    match row {
        Some(r) => Ok(r.try_get("members_synced_at")?),
        None => Ok(None),
    }
}
