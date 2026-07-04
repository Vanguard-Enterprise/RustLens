//! Owner-stats Postgres counts (`GET /api/internal/bot/owner/stats`), owned by
//! the coordinator service. Mirrors `internal/store/owner_stats.go`. Seven plain
//! COUNT aggregates; the first error aborts. Schema owned by Go.

use sqlx::PgPool;

#[derive(Debug, Clone, Copy, Default)]
pub struct OwnerStatsCounts {
    pub configured_guilds: i64,
    pub steam_account_count: i64,
    pub hard_link_count: i64,
    pub unique_linked_users: i64,
    pub tracked_server_count: i64,
    pub online_tracked_players: i64,
    pub active_wipe_reacts: i64,
}

async fn count(pool: &PgPool, sql: &str) -> Result<i64, sqlx::Error> {
    sqlx::query_scalar(sql).fetch_one(pool).await
}

/// `OwnerStats(nowUnix)`. `now_unix` bounds the active-wipe-reacts count.
pub async fn owner_stats(pool: &PgPool, now_unix: i64) -> Result<OwnerStatsCounts, sqlx::Error> {
    Ok(OwnerStatsCounts {
        configured_guilds: count(pool, "SELECT COUNT(*) FROM guild_settings").await?,
        steam_account_count: count(pool, "SELECT COUNT(*) FROM steam_accounts").await?,
        hard_link_count: count(pool, "SELECT COUNT(*) FROM steam_discord_links WHERE type = 'hard'")
            .await?,
        unique_linked_users: count(
            pool,
            "SELECT COUNT(DISTINCT discord_id) FROM guild_steam_discord_links",
        )
        .await?,
        tracked_server_count: count(pool, "SELECT COUNT(*) FROM tracked_servers").await?,
        online_tracked_players: count(pool, "SELECT COUNT(*) FROM player_states WHERE is_online")
            .await?,
        active_wipe_reacts: sqlx::query_scalar(
            "SELECT COUNT(*) FROM wipe_reacts WHERE scheduled_unix > $1",
        )
        .bind(now_unix)
        .fetch_one(pool)
        .await?,
    })
}
