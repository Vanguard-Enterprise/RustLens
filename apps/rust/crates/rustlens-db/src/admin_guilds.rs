//! Admin guild list + detail reads, owned by the `guilds` service. Mirrors
//! `ListAdminGuilds` (`internal/store/bot_guilds.go`) + the inline queries in
//! `admin_guild_detail_handlers.go`. Reads across ~12 tables (read-only). Schema
//! owned by Go.

use chrono::{DateTime, SecondsFormat, Utc};
use serde::Serialize;
use sqlx::{PgPool, Postgres, QueryBuilder, Row};

fn rfc3339_secs(dt: DateTime<Utc>) -> String {
    dt.to_rfc3339_opts(SecondsFormat::Secs, true)
}

/// List/detail guild card. `installedAt`/`leftAt` are pre-formatted RFC3339
/// **seconds** strings (matching Go's pre-formatting); nullable fields emit
/// explicit `null`.
#[derive(Debug, Clone, Serialize)]
pub struct AdminGuildItem {
    #[serde(rename = "guildId")]
    pub guild_id: String,
    pub name: String,
    #[serde(rename = "iconHash")]
    pub icon_hash: Option<String>,
    #[serde(rename = "ownerDiscordId")]
    pub owner_discord_id: Option<String>,
    #[serde(rename = "ownerUsername")]
    pub owner_username: Option<String>,
    #[serde(rename = "ownerGlobalName")]
    pub owner_global_name: Option<String>,
    #[serde(rename = "ownerAvatarHash")]
    pub owner_avatar_hash: Option<String>,
    #[serde(rename = "approximateMemberCount")]
    pub approximate_member_count: Option<i32>,
    #[serde(rename = "installedAt")]
    pub installed_at: String,
    #[serde(rename = "isInstalled")]
    pub is_installed: bool,
    #[serde(rename = "leftAt")]
    pub left_at: Option<String>,
    #[serde(rename = "isConfigured")]
    pub is_configured: bool,
    #[serde(rename = "trackedServerCount")]
    pub tracked_server_count: i32,
}

const ITEM_SELECT: &str = "SELECT bg.guild_id, bg.name, bg.icon_hash, bg.owner_discord_id, \
    bg.owner_username, bg.owner_global_name, bg.owner_avatar_hash, bg.approximate_member_count, \
    bg.installed_at, bg.is_installed, bg.left_at, (gs.guild_id IS NOT NULL) AS is_configured, \
    COALESCE(gts.cnt, 0) AS tracked_server_count \
    FROM bot_guilds bg \
    LEFT JOIN guild_settings gs ON gs.guild_id = bg.guild_id \
    LEFT JOIN (SELECT guild_id, COUNT(*)::int AS cnt FROM guild_tracked_servers GROUP BY guild_id) gts \
        ON gts.guild_id = bg.guild_id";

fn row_to_item(row: &sqlx::postgres::PgRow) -> Result<AdminGuildItem, sqlx::Error> {
    let installed_at: DateTime<Utc> = row.try_get("installed_at")?;
    let left_at: Option<DateTime<Utc>> = row.try_get("left_at")?;
    Ok(AdminGuildItem {
        guild_id: row.try_get("guild_id")?,
        name: row.try_get("name")?,
        icon_hash: row.try_get("icon_hash")?,
        owner_discord_id: row.try_get("owner_discord_id")?,
        owner_username: row.try_get("owner_username")?,
        owner_global_name: row.try_get("owner_global_name")?,
        owner_avatar_hash: row.try_get("owner_avatar_hash")?,
        approximate_member_count: row.try_get("approximate_member_count")?,
        installed_at: rfc3339_secs(installed_at),
        is_installed: row.try_get("is_installed")?,
        left_at: left_at.map(rfc3339_secs),
        is_configured: row.try_get("is_configured")?,
        tracked_server_count: row.try_get("tracked_server_count")?,
    })
}

/// Validated list filter (sort already constrained to the allowed set).
#[derive(Debug, Clone, Default)]
pub struct GuildListFilter {
    pub q: Option<String>,
    pub joined_since: Option<DateTime<Utc>>,
    pub joined_before: Option<DateTime<Utc>>,
    pub sort: String,
}

fn apply_where(qb: &mut QueryBuilder<Postgres>, f: &GuildListFilter) {
    let mut started = false;
    let prefix = |qb: &mut QueryBuilder<Postgres>, started: &mut bool| {
        qb.push(if *started { " AND " } else { " WHERE " });
        *started = true;
    };
    if let Some(q) = f.q.as_deref().map(str::trim).filter(|s| !s.is_empty()) {
        prefix(qb, &mut started);
        qb.push("(bg.name ILIKE '%' || ")
            .push_bind(q.to_string())
            .push(" || '%' OR bg.guild_id ILIKE ")
            .push_bind(q.to_string())
            .push(" || '%' OR bg.owner_username ILIKE '%' || ")
            .push_bind(q.to_string())
            .push(" || '%')");
    }
    if let Some(since) = f.joined_since {
        prefix(qb, &mut started);
        qb.push("bg.installed_at >= ").push_bind(since);
    }
    if let Some(before) = f.joined_before {
        prefix(qb, &mut started);
        qb.push("bg.installed_at < ").push_bind(before);
    }
}

fn order_by(sort: &str) -> &'static str {
    match sort {
        "-name" => "bg.name DESC, bg.guild_id ASC",
        "members" => "bg.approximate_member_count ASC NULLS LAST, bg.guild_id ASC",
        "-members" => "bg.approximate_member_count DESC NULLS LAST, bg.guild_id ASC",
        "joined" => "bg.installed_at ASC, bg.guild_id ASC",
        "-joined" => "bg.installed_at DESC, bg.guild_id ASC",
        // "name" and "" both fall here.
        _ => "bg.name ASC, bg.guild_id ASC",
    }
}

/// `ListAdminGuilds`: `(items, total)`.
pub async fn list_page(
    pool: &PgPool,
    filter: &GuildListFilter,
    limit: i64,
    offset: i64,
) -> Result<(Vec<AdminGuildItem>, i64), sqlx::Error> {
    let mut count_qb: QueryBuilder<Postgres> = QueryBuilder::new("SELECT COUNT(*) FROM bot_guilds bg");
    apply_where(&mut count_qb, filter);
    let total: i64 = count_qb.build_query_scalar().fetch_one(pool).await?;

    let mut qb: QueryBuilder<Postgres> = QueryBuilder::new(ITEM_SELECT);
    apply_where(&mut qb, filter);
    qb.push(format!(" ORDER BY {} ", order_by(&filter.sort)));
    qb.push("LIMIT ").push_bind(limit).push(" OFFSET ").push_bind(offset);
    let rows = qb.build().fetch_all(pool).await?;
    let items = rows.iter().map(row_to_item).collect::<Result<Vec<_>, _>>()?;
    Ok((items, total))
}

/// `getAdminGuildItem`: `None` when the `bot_guilds` row is absent (→ 404).
pub async fn get_item(pool: &PgPool, guild_id: &str) -> Result<Option<AdminGuildItem>, sqlx::Error> {
    let row = sqlx::query(&format!("{ITEM_SELECT} WHERE bg.guild_id = $1"))
        .bind(guild_id)
        .fetch_optional(pool)
        .await?;
    row.as_ref().map(row_to_item).transpose()
}

/// Eight scalar guild counts.
#[derive(Debug, Clone, Copy, Default)]
pub struct AdminGuildStats {
    pub member_count: i64,
    pub active_member_count: i64,
    pub linked_member_count: i64,
    pub tracked_server_count: i64,
    pub track_group_count: i64,
    pub active_wipe_react_count: i64,
    pub wipe_react_count: i64,
    pub schedule_count: i64,
}

pub async fn stats(pool: &PgPool, guild_id: &str) -> Result<AdminGuildStats, sqlx::Error> {
    let row = sqlx::query(
        r#"SELECT
               (SELECT COUNT(*) FROM user_guilds WHERE guild_id = $1),
               (SELECT COUNT(*) FROM user_guilds WHERE guild_id = $1 AND is_active = TRUE AND left_at IS NULL),
               (SELECT COUNT(DISTINCT discord_id) FROM guild_steam_discord_links WHERE guild_id = $1),
               (SELECT COUNT(*) FROM guild_tracked_servers WHERE guild_id = $1),
               (SELECT COUNT(*) FROM track_groups WHERE guild_id = $1),
               (SELECT COUNT(*) FROM wipe_reacts WHERE guild_id = $1 AND scheduled_unix > EXTRACT(EPOCH FROM NOW())::bigint),
               (SELECT COUNT(*) FROM wipe_reacts WHERE guild_id = $1),
               (SELECT COUNT(*) FROM wipe_react_schedules WHERE guild_id = $1)"#,
    )
    .bind(guild_id)
    .fetch_one(pool)
    .await?;
    Ok(AdminGuildStats {
        member_count: row.try_get(0)?,
        active_member_count: row.try_get(1)?,
        linked_member_count: row.try_get(2)?,
        tracked_server_count: row.try_get(3)?,
        track_group_count: row.try_get(4)?,
        active_wipe_react_count: row.try_get(5)?,
        wipe_react_count: row.try_get(6)?,
        schedule_count: row.try_get(7)?,
    })
}

#[derive(Debug, Clone, Serialize)]
pub struct AdminGuildMemberItem {
    #[serde(rename = "discordId")]
    pub discord_id: String,
    #[serde(rename = "appUserId")]
    pub app_user_id: Option<String>,
    #[serde(rename = "displayName")]
    pub display_name: Option<String>,
    #[serde(rename = "discordUsername")]
    pub discord_username: Option<String>,
    #[serde(rename = "globalName")]
    pub global_name: Option<String>,
    #[serde(rename = "avatarHash")]
    pub avatar_hash: Option<String>,
    #[serde(rename = "isActive")]
    pub is_active: bool,
    #[serde(rename = "isOwner")]
    pub is_owner: bool,
    #[serde(rename = "firstSeenAt")]
    pub first_seen_at: DateTime<Utc>,
    #[serde(rename = "lastSeenAt")]
    pub last_seen_at: DateTime<Utc>,
    #[serde(rename = "leftAt")]
    pub left_at: Option<DateTime<Utc>>,
    #[serde(rename = "steamIds")]
    pub steam_ids: Vec<String>,
}

pub async fn members(
    pool: &PgPool,
    guild_id: &str,
    limit: i64,
    offset: i64,
) -> Result<Vec<AdminGuildMemberItem>, sqlx::Error> {
    let rows = sqlx::query(
        r#"SELECT ug.discord_id, u.id,
               COALESCE(NULLIF(u.display_name,''), NULLIF(u.discord_global_name,''), NULLIF(u.discord_username,'')) AS display_name,
               u.discord_username, u.discord_global_name, u.discord_avatar,
               ug.is_active, ug.is_owner, ug.first_seen_at, ug.last_seen_at, ug.left_at,
               COALESCE(array_agg(DISTINCT gsl.steam_id ORDER BY gsl.steam_id) FILTER (WHERE gsl.steam_id IS NOT NULL), ARRAY[]::text[]) AS steam_ids
           FROM user_guilds ug
           LEFT JOIN app_users u ON u.discord_id = ug.discord_id
           LEFT JOIN guild_steam_discord_links gsl ON gsl.guild_id = ug.guild_id AND gsl.discord_id = ug.discord_id
           WHERE ug.guild_id = $1
           GROUP BY ug.discord_id, u.id, u.display_name, u.discord_global_name, u.discord_username,
                    u.discord_avatar, ug.is_active, ug.is_owner, ug.first_seen_at, ug.last_seen_at, ug.left_at
           ORDER BY (ug.is_active AND ug.left_at IS NULL) DESC, ug.last_seen_at DESC, ug.discord_id ASC
           LIMIT $2 OFFSET $3"#,
    )
    .bind(guild_id)
    .bind(limit)
    .bind(offset)
    .fetch_all(pool)
    .await?;
    rows.iter()
        .map(|r| {
            Ok(AdminGuildMemberItem {
                discord_id: r.try_get("discord_id")?,
                app_user_id: r.try_get("id")?,
                display_name: r.try_get("display_name")?,
                discord_username: r.try_get("discord_username")?,
                global_name: r.try_get("discord_global_name")?,
                avatar_hash: r.try_get("discord_avatar")?,
                is_active: r.try_get("is_active")?,
                is_owner: r.try_get("is_owner")?,
                first_seen_at: r.try_get("first_seen_at")?,
                last_seen_at: r.try_get("last_seen_at")?,
                left_at: r.try_get("left_at")?,
                steam_ids: r.try_get("steam_ids")?,
            })
        })
        .collect()
}

#[derive(Debug, Clone, Serialize)]
pub struct AdminGuildSteamLinkItem {
    #[serde(rename = "discordId")]
    pub discord_id: String,
    #[serde(rename = "steamId")]
    pub steam_id: String,
    #[serde(rename = "appUserId")]
    pub app_user_id: Option<String>,
    #[serde(rename = "displayName")]
    pub display_name: Option<String>,
    #[serde(rename = "discordUsername")]
    pub discord_username: Option<String>,
    #[serde(rename = "createdAt")]
    pub created_at: DateTime<Utc>,
}

pub async fn links(
    pool: &PgPool,
    guild_id: &str,
    limit: i64,
    offset: i64,
) -> Result<Vec<AdminGuildSteamLinkItem>, sqlx::Error> {
    let rows = sqlx::query(
        r#"SELECT gsl.discord_id, gsl.steam_id, u.id,
               COALESCE(NULLIF(u.display_name,''), NULLIF(u.discord_global_name,''), NULLIF(u.discord_username,'')) AS display_name,
               u.discord_username, gsl.created_at
           FROM guild_steam_discord_links gsl
           LEFT JOIN app_users u ON u.discord_id = gsl.discord_id
           WHERE gsl.guild_id = $1
           ORDER BY gsl.created_at DESC, gsl.discord_id ASC, gsl.steam_id ASC
           LIMIT $2 OFFSET $3"#,
    )
    .bind(guild_id)
    .bind(limit)
    .bind(offset)
    .fetch_all(pool)
    .await?;
    rows.iter()
        .map(|r| {
            Ok(AdminGuildSteamLinkItem {
                discord_id: r.try_get("discord_id")?,
                steam_id: r.try_get("steam_id")?,
                app_user_id: r.try_get("id")?,
                display_name: r.try_get("display_name")?,
                discord_username: r.try_get("discord_username")?,
                created_at: r.try_get("created_at")?,
            })
        })
        .collect()
}

#[derive(Debug, Clone, Serialize)]
pub struct AdminGuildTrackGroupItem {
    pub id: i64,
    pub name: String,
    #[serde(rename = "serverBmId")]
    pub server_bm_id: String,
    #[serde(rename = "serverName")]
    pub server_name: Option<String>,
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
    #[serde(rename = "memberCount")]
    pub member_count: i32,
    #[serde(rename = "createdBy")]
    pub created_by: String,
    #[serde(rename = "createdAt")]
    pub created_at: DateTime<Utc>,
    #[serde(rename = "updatedAt")]
    pub updated_at: DateTime<Utc>,
}

pub async fn track_groups(
    pool: &PgPool,
    guild_id: &str,
    limit: i64,
    offset: i64,
) -> Result<Vec<AdminGuildTrackGroupItem>, sqlx::Error> {
    let rows = sqlx::query(
        r#"SELECT tg.id, tg.name, tg.server_bm_id, ts.name AS server_name,
               tg.channel_id, tg.message_id, tg.send_updates, tg.ping_alert_role_on_empty,
               tg.online_count, COALESCE(m.member_count, 0)::int AS member_count,
               tg.created_by, tg.created_at, tg.updated_at
           FROM track_groups tg
           LEFT JOIN tracked_servers ts ON ts.bm_id = tg.server_bm_id
           LEFT JOIN (SELECT track_group_id, COUNT(*) AS member_count FROM track_group_members GROUP BY track_group_id) m
               ON m.track_group_id = tg.id
           WHERE tg.guild_id = $1
           ORDER BY tg.name ASC, tg.id ASC
           LIMIT $2 OFFSET $3"#,
    )
    .bind(guild_id)
    .bind(limit)
    .bind(offset)
    .fetch_all(pool)
    .await?;
    rows.iter()
        .map(|r| {
            Ok(AdminGuildTrackGroupItem {
                id: r.try_get("id")?,
                name: r.try_get("name")?,
                server_bm_id: r.try_get("server_bm_id")?,
                server_name: r.try_get("server_name")?,
                channel_id: r.try_get("channel_id")?,
                message_id: r.try_get("message_id")?,
                send_updates: r.try_get("send_updates")?,
                ping_alert_role_on_empty: r.try_get("ping_alert_role_on_empty")?,
                online_count: r.try_get("online_count")?,
                member_count: r.try_get("member_count")?,
                created_by: r.try_get("created_by")?,
                created_at: r.try_get("created_at")?,
                updated_at: r.try_get("updated_at")?,
            })
        })
        .collect()
}

#[derive(Debug, Clone, Serialize)]
pub struct AdminGuildUsageItem {
    pub id: i64,
    #[serde(rename = "appUserId")]
    pub app_user_id: String,
    #[serde(rename = "discordId")]
    pub discord_id: String,
    #[serde(rename = "channelId")]
    pub channel_id: Option<String>,
    #[serde(rename = "channelName")]
    pub channel_name: Option<String>,
    #[serde(rename = "interactionType")]
    pub interaction_type: String,
    pub command: String,
    pub subcommand: Option<String>,
    pub outcome: String,
    #[serde(rename = "errorCode")]
    pub error_code: Option<String>,
    #[serde(rename = "latencyMs")]
    pub latency_ms: i32,
    #[serde(rename = "createdAt")]
    pub created_at: DateTime<Utc>,
    #[serde(rename = "displayName")]
    pub display_name: Option<String>,
    #[serde(rename = "discordUsername")]
    pub discord_username: Option<String>,
}

pub async fn recent_usage(
    pool: &PgPool,
    guild_id: &str,
    limit: i64,
    offset: i64,
) -> Result<Vec<AdminGuildUsageItem>, sqlx::Error> {
    let rows = sqlx::query(
        r#"SELECT cu.id, cu.app_user_id, cu.discord_id, cu.channel_id, cu.channel_name,
               cu.interaction_type, cu.command, cu.subcommand, cu.outcome, cu.error_code,
               cu.latency_ms, cu.created_at,
               COALESCE(NULLIF(u.display_name,''), NULLIF(u.discord_global_name,''), NULLIF(u.discord_username,'')) AS display_name,
               u.discord_username
           FROM command_usage cu
           LEFT JOIN app_users u ON u.id = cu.app_user_id
           WHERE cu.guild_id = $1
           ORDER BY cu.created_at DESC, cu.id DESC
           LIMIT $2 OFFSET $3"#,
    )
    .bind(guild_id)
    .bind(limit)
    .bind(offset)
    .fetch_all(pool)
    .await?;
    rows.iter()
        .map(|r| {
            Ok(AdminGuildUsageItem {
                id: r.try_get("id")?,
                app_user_id: r.try_get("app_user_id")?,
                discord_id: r.try_get("discord_id")?,
                channel_id: r.try_get("channel_id")?,
                channel_name: r.try_get("channel_name")?,
                interaction_type: r.try_get("interaction_type")?,
                command: r.try_get("command")?,
                subcommand: r.try_get("subcommand")?,
                outcome: r.try_get("outcome")?,
                error_code: r.try_get("error_code")?,
                latency_ms: r.try_get("latency_ms")?,
                created_at: r.try_get("created_at")?,
                display_name: r.try_get("display_name")?,
                discord_username: r.try_get("discord_username")?,
            })
        })
        .collect()
}

pub async fn count_steam_links(pool: &PgPool, guild_id: &str) -> Result<i64, sqlx::Error> {
    sqlx::query_scalar("SELECT COUNT(*) FROM guild_steam_discord_links WHERE guild_id = $1")
        .bind(guild_id)
        .fetch_one(pool)
        .await
}

pub async fn count_usage(pool: &PgPool, guild_id: &str) -> Result<i64, sqlx::Error> {
    sqlx::query_scalar("SELECT COUNT(*) FROM command_usage WHERE guild_id = $1")
        .bind(guild_id)
        .fetch_one(pool)
        .await
}

#[derive(Debug, Clone, Serialize)]
pub struct TopCommand {
    pub command: String,
    pub count: i64,
}

#[derive(Debug, Clone, Serialize)]
pub struct AdminGuildUsageSummary {
    #[serde(rename = "totalCount")]
    pub total_count: i64,
    #[serde(rename = "errorCount")]
    pub error_count: i64,
    #[serde(rename = "errorRate")]
    pub error_rate: f64,
    #[serde(rename = "topCommands")]
    pub top_commands: Vec<TopCommand>,
}

pub async fn usage_summary(pool: &PgPool, guild_id: &str) -> Result<AdminGuildUsageSummary, sqlx::Error> {
    let totals = sqlx::query(
        "SELECT COUNT(*), COUNT(*) FILTER (WHERE outcome = 'error') \
         FROM command_usage WHERE guild_id = $1 AND created_at >= NOW() - INTERVAL '30 days'",
    )
    .bind(guild_id)
    .fetch_one(pool)
    .await?;
    let total_count: i64 = totals.try_get(0)?;
    let error_count: i64 = totals.try_get(1)?;
    let error_rate = if total_count > 0 {
        error_count as f64 / total_count as f64
    } else {
        0.0
    };
    let rows = sqlx::query(
        "SELECT command, COUNT(*) AS count FROM command_usage \
         WHERE guild_id = $1 AND created_at >= NOW() - INTERVAL '30 days' \
         GROUP BY command ORDER BY count DESC, command ASC LIMIT 5",
    )
    .bind(guild_id)
    .fetch_all(pool)
    .await?;
    let top_commands = rows
        .iter()
        .map(|r| Ok(TopCommand { command: r.try_get("command")?, count: r.try_get("count")? }))
        .collect::<Result<Vec<_>, sqlx::Error>>()?;
    Ok(AdminGuildUsageSummary { total_count, error_count, error_rate, top_commands })
}
