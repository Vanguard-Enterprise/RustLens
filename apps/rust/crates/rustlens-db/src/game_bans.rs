//! Steam game-ban cache read/write + active-guild-member discord IDs for the
//! players service game-ban scan. Mirrors `internal/store/steam.go` (game-ban
//! cache) + `bot_guilds.go` (`ListActiveUserGuildDiscordIDs`).

use chrono::{DateTime, Utc};
use sqlx::{PgPool, Row};

/// The game-ban cache slice of a `steam_account_cache` row.
#[derive(Debug, Clone)]
pub struct SteamGameBanCache {
    pub steam_id: String,
    pub game_banned: bool,
    pub game_banned_at: Option<DateTime<Utc>>,
    pub game_ban_checked_at: Option<DateTime<Utc>>,
}

/// Load the game-ban cache rows for a set of Steam IDs.
pub async fn list_steam_game_ban_cache(
    pool: &PgPool,
    steam_ids: &[String],
) -> Result<Vec<SteamGameBanCache>, sqlx::Error> {
    let rows = sqlx::query(
        "SELECT steam_id, game_banned, game_banned_at, game_ban_checked_at FROM steam_account_cache WHERE steam_id = ANY($1)",
    )
    .bind(steam_ids)
    .fetch_all(pool)
    .await?;
    rows.iter()
        .map(|row| {
            Ok(SteamGameBanCache {
                steam_id: row.try_get("steam_id")?,
                game_banned: row.try_get("game_banned")?,
                game_banned_at: row.try_get("game_banned_at")?,
                game_ban_checked_at: row.try_get("game_ban_checked_at")?,
            })
        })
        .collect()
}

/// Upsert only the game-ban columns of a `steam_account_cache` row (requires the
/// `steam_accounts` parent to exist, per the FK). Mirrors
/// `UpsertSteamAccountGameBanCache`.
pub async fn upsert_steam_account_game_ban_cache(
    pool: &PgPool,
    steam_id: &str,
    game_banned: bool,
    game_banned_at: Option<DateTime<Utc>>,
    checked_at: DateTime<Utc>,
) -> Result<(), sqlx::Error> {
    sqlx::query(
        r#"INSERT INTO steam_account_cache (steam_id, game_banned, game_banned_at, game_ban_checked_at)
           VALUES ($1, $2, $3, $4)
           ON CONFLICT (steam_id) DO UPDATE
           SET game_banned = EXCLUDED.game_banned,
               game_banned_at = EXCLUDED.game_banned_at,
               game_ban_checked_at = EXCLUDED.game_ban_checked_at"#,
    )
    .bind(steam_id)
    .bind(game_banned)
    .bind(game_banned_at)
    .bind(checked_at)
    .execute(pool)
    .await?;
    Ok(())
}

/// List distinct active (not-left) member Discord IDs for a guild
/// (`ListActiveUserGuildDiscordIDs`). Cross-domain read of `user_guilds`.
pub async fn list_active_user_guild_discord_ids(
    pool: &PgPool,
    guild_id: &str,
) -> Result<Vec<String>, sqlx::Error> {
    let rows = sqlx::query(
        "SELECT discord_id FROM user_guilds WHERE guild_id = $1 AND is_active = TRUE AND left_at IS NULL ORDER BY discord_id ASC",
    )
    .bind(guild_id)
    .fetch_all(pool)
    .await?;
    rows.iter().map(|row| row.try_get("discord_id")).collect()
}
