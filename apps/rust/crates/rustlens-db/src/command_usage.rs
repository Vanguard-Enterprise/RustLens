//! Admin command-usage read queries (`command_usage` + `command_usage_targets`).
//! Mirrors the Go `store/command_usage.go` admin read path: a filtered,
//! target-embedded list with a 90-day summary, filter options, per-record
//! targets, and follow-up children. Targets are snapshotted at capture time and
//! enriched at read from live `app_users` / `bot_guilds`.

use std::collections::{HashMap, HashSet};

use chrono::{DateTime, Duration, Utc};
use serde::{Deserialize, Serialize};
use sqlx::postgres::PgRow;
use sqlx::{PgPool, Postgres, QueryBuilder, Row};

/// Inline target embed cap (Go `InlineTargetCap`).
const INLINE_TARGET_CAP: i64 = 100;

/// One command-usage target. Deserialized from the row's `targets_json` (which
/// has no `appUserId`); `app_user_id` is filled during enrichment.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CommandUsageTargetRow {
    pub kind: String,
    pub snowflake: String,
    #[serde(rename = "displayName")]
    pub display_name: Option<String>,
    #[serde(rename = "avatarHash")]
    pub avatar_hash: Option<String>,
    pub relation: String,
    #[serde(rename = "appUserId", default, skip_serializing_if = "Option::is_none")]
    pub app_user_id: Option<String>,
}

/// One command-usage row (frontend `AdminCommandUsageItem` base fields). Nullable
/// fields without `omitempty` serialize as `null`.
#[derive(Debug, Clone, Serialize)]
pub struct CommandUsageRow {
    pub id: i64,
    #[serde(rename = "appUserId")]
    pub app_user_id: String,
    #[serde(rename = "discordId")]
    pub discord_id: String,
    #[serde(rename = "guildId")]
    pub guild_id: Option<String>,
    #[serde(rename = "channelId")]
    pub channel_id: Option<String>,
    #[serde(rename = "interactionType")]
    pub interaction_type: String,
    pub command: String,
    pub subcommand: Option<String>,
    pub target: Option<String>,
    pub options: serde_json::Value,
    pub outcome: String,
    #[serde(rename = "errorCode")]
    pub error_code: Option<String>,
    #[serde(rename = "latencyMs")]
    pub latency_ms: i32,
    #[serde(rename = "correlationId", skip_serializing_if = "Option::is_none")]
    pub correlation_id: Option<String>,
    #[serde(rename = "parentUsageId", skip_serializing_if = "Option::is_none")]
    pub parent_usage_id: Option<i64>,
    #[serde(rename = "channelKind", skip_serializing_if = "Option::is_none")]
    pub channel_kind: Option<String>,
    #[serde(rename = "channelName", skip_serializing_if = "Option::is_none")]
    pub channel_name: Option<String>,
    #[serde(rename = "guildName", skip_serializing_if = "Option::is_none")]
    pub guild_name: Option<String>,
    #[serde(rename = "guildIcon", skip_serializing_if = "Option::is_none")]
    pub guild_icon: Option<String>,
    #[serde(rename = "createdAt")]
    pub created_at: DateTime<Utc>,
}

/// A command-usage row with its embedded targets (frontend `AdminCommandUsageItem`).
#[derive(Debug, Clone, Serialize)]
pub struct CommandUsageRowWithTargets {
    #[serde(flatten)]
    pub row: CommandUsageRow,
    pub targets: Vec<CommandUsageTargetRow>,
    #[serde(rename = "targetTotal", skip_serializing_if = "Option::is_none")]
    pub target_total: Option<i64>,
    #[serde(rename = "childCount")]
    pub child_count: i64,
}

#[derive(Debug, Clone, Serialize)]
pub struct CommandUsageTopCommand {
    pub command: String,
    pub count: i64,
}

/// The 90-day summary block on the list response (`errorRate` computed here).
#[derive(Debug, Clone, Serialize)]
pub struct CommandUsageSummary {
    #[serde(rename = "totalCount")]
    pub total_count: i64,
    #[serde(rename = "errorCount")]
    pub error_count: i64,
    #[serde(rename = "errorRate")]
    pub error_rate: f64,
    #[serde(rename = "topCommands")]
    pub top_commands: Vec<CommandUsageTopCommand>,
}

/// The `{items,total,page,pageSize,summary}` list response.
#[derive(Debug, Clone, Serialize)]
pub struct CommandUsagePage {
    pub items: Vec<CommandUsageRowWithTargets>,
    pub total: i64,
    pub page: i32,
    #[serde(rename = "pageSize")]
    pub page_size: i32,
    pub summary: CommandUsageSummary,
}

#[derive(Debug, Clone, Serialize)]
pub struct CommandUsageFilterGuild {
    #[serde(rename = "guildId")]
    pub guild_id: String,
    pub name: String,
    #[serde(rename = "iconHash")]
    pub icon_hash: Option<String>,
}

#[derive(Debug, Clone, Serialize)]
pub struct CommandUsageFilterOptions {
    pub commands: Vec<String>,
    pub guilds: Vec<CommandUsageFilterGuild>,
}

/// Filters for the list route. `window_days` is only ever `Some(1|7|30|90)`
/// (the handler validates); `None` means no time bound (matching Go's default 0).
#[derive(Debug, Clone, Default)]
pub struct CommandUsageListFilters {
    pub q: Option<String>,
    pub command: Option<String>,
    pub guild_id: Option<String>,
    pub outcomes: Vec<String>,
    pub interaction_types: Vec<String>,
    pub window_days: Option<i64>,
}

/// Shared SELECT list for the list/children queries (through `target_total`).
const CU_SELECT: &str = r#"SELECT
    cu.id, cu.app_user_id, cu.discord_id, cu.guild_id, cu.channel_id,
    cu.interaction_type, cu.command, cu.subcommand, cu.target,
    cu.options, cu.outcome, cu.error_code, cu.latency_ms,
    cu.correlation_id, cu.parent_usage_id, cu.channel_kind, cu.channel_name, cu.created_at,
    bg.name AS guild_name, bg.icon_hash AS guild_icon,
    COALESCE(
        (SELECT jsonb_agg(jsonb_build_object(
            'kind', t.kind, 'snowflake', t.snowflake, 'displayName', t.display_name,
            'avatarHash', t.avatar_hash, 'relation', t.relation) ORDER BY t.id)
         FROM (SELECT id, kind, snowflake, display_name, avatar_hash, relation
               FROM command_usage_targets WHERE command_usage_id = cu.id
               ORDER BY id ASC LIMIT 100) t),
        '[]'::jsonb
    ) AS targets_json,
    (SELECT COUNT(*) FROM command_usage_targets WHERE command_usage_id = cu.id) AS target_total"#;

fn scan_cu_row(row: &PgRow, with_child_count: bool) -> Result<CommandUsageRowWithTargets, sqlx::Error> {
    let targets_json: serde_json::Value = row.try_get(20)?;
    let targets: Vec<CommandUsageTargetRow> = serde_json::from_value(targets_json).unwrap_or_default();
    let target_total: i64 = row.try_get(21)?;
    let child_count: i64 = if with_child_count { row.try_get(22)? } else { 0 };
    Ok(CommandUsageRowWithTargets {
        row: CommandUsageRow {
            id: row.try_get(0)?,
            app_user_id: row.try_get(1)?,
            discord_id: row.try_get(2)?,
            guild_id: row.try_get(3)?,
            channel_id: row.try_get(4)?,
            interaction_type: row.try_get(5)?,
            command: row.try_get(6)?,
            subcommand: row.try_get(7)?,
            target: row.try_get(8)?,
            options: row
                .try_get::<Option<serde_json::Value>, _>(9)?
                .unwrap_or(serde_json::Value::Null),
            outcome: row.try_get(10)?,
            error_code: row.try_get(11)?,
            latency_ms: row.try_get(12)?,
            correlation_id: row.try_get(13)?,
            parent_usage_id: row.try_get(14)?,
            channel_kind: row.try_get(15)?,
            channel_name: row.try_get(16)?,
            created_at: row.try_get(17)?,
            guild_name: row.try_get(18)?,
            guild_icon: row.try_get(19)?,
        },
        targets,
        target_total: (target_total > INLINE_TARGET_CAP).then_some(target_total),
        child_count,
    })
}

fn push_cu_where(qb: &mut QueryBuilder<'_, Postgres>, user_id: &str, f: &CommandUsageListFilters) {
    qb.push(" WHERE cu.app_user_id = ");
    qb.push_bind(user_id.to_string());
    qb.push(" AND cu.parent_usage_id IS NULL");
    if let Some(days) = f.window_days {
        qb.push(" AND cu.created_at >= ");
        qb.push_bind(Utc::now() - Duration::days(days));
    }
    if let Some(c) = f.command.as_deref().filter(|s| !s.is_empty()) {
        qb.push(" AND cu.command = ");
        qb.push_bind(c.to_string());
    }
    if let Some(g) = f.guild_id.as_deref().filter(|s| !s.is_empty()) {
        qb.push(" AND cu.guild_id = ");
        qb.push_bind(g.to_string());
    }
    if !f.outcomes.is_empty() {
        qb.push(" AND cu.outcome = ANY(");
        qb.push_bind(f.outcomes.clone());
        qb.push(")");
    }
    if !f.interaction_types.is_empty() {
        qb.push(" AND cu.interaction_type = ANY(");
        qb.push_bind(f.interaction_types.clone());
        qb.push(")");
    }
    if let Some(q) = f.q.as_deref().filter(|s| !s.is_empty()) {
        let pat = format!("%{q}%");
        qb.push(" AND (cu.command ILIKE ");
        qb.push_bind(pat.clone());
        qb.push(" OR cu.subcommand ILIKE ");
        qb.push_bind(pat.clone());
        qb.push(" OR cu.target ILIKE ");
        qb.push_bind(pat.clone());
        qb.push(" OR EXISTS (SELECT 1 FROM command_usage_targets t WHERE t.command_usage_id = cu.id AND (t.display_name ILIKE ");
        qb.push_bind(pat.clone());
        qb.push(" OR t.snowflake ILIKE ");
        qb.push_bind(pat);
        qb.push(")))");
    }
}

/// The filtered, target-embedded top-level list + 90-day summary. Mirrors the Go
/// `ListUserCommandUsageFiltered` + `SummarizeUserCommandUsage`.
pub async fn list_user_command_usage(
    pool: &PgPool,
    user_id: &str,
    f: &CommandUsageListFilters,
    page: i32,
    page_size: i32,
) -> Result<CommandUsagePage, sqlx::Error> {
    let mut cqb: QueryBuilder<Postgres> = QueryBuilder::new("SELECT COUNT(*) FROM command_usage cu");
    push_cu_where(&mut cqb, user_id, f);
    let total: i64 = cqb.build_query_scalar().fetch_one(pool).await?;

    let offset = (page - 1) * page_size;
    let mut qb: QueryBuilder<Postgres> = QueryBuilder::new(CU_SELECT);
    qb.push(", (SELECT COUNT(*) FROM command_usage child WHERE child.parent_usage_id = cu.id) AS child_count FROM command_usage cu LEFT JOIN bot_guilds bg ON bg.guild_id = cu.guild_id");
    push_cu_where(&mut qb, user_id, f);
    qb.push(" ORDER BY cu.created_at DESC, cu.id DESC LIMIT ");
    qb.push_bind(page_size as i64);
    qb.push(" OFFSET ");
    qb.push_bind(offset as i64);

    let rows = qb.build().fetch_all(pool).await?;
    let mut items = rows
        .iter()
        .map(|r| scan_cu_row(r, true))
        .collect::<Result<Vec<_>, _>>()?;
    enrich_targets(pool, &mut items).await?;

    let summary = summarize_user_command_usage(pool, user_id, 90).await?;
    Ok(CommandUsagePage { items, total, page, page_size, summary })
}

/// Follow-up (child) rows for a parent usage record, oldest first. Targets are
/// embedded but NOT enriched, and `childCount` is 0 (matches Go).
pub async fn list_user_command_usage_children(
    pool: &PgPool,
    user_id: &str,
    parent_usage_id: i64,
) -> Result<Vec<CommandUsageRowWithTargets>, sqlx::Error> {
    let sql = format!(
        "{CU_SELECT} FROM command_usage cu LEFT JOIN bot_guilds bg ON bg.guild_id = cu.guild_id \
         WHERE cu.parent_usage_id = $1 AND cu.app_user_id = $2 ORDER BY cu.created_at ASC, cu.id ASC"
    );
    let rows = sqlx::query(&sql)
        .bind(parent_usage_id)
        .bind(user_id)
        .fetch_all(pool)
        .await?;
    rows.iter().map(|r| scan_cu_row(r, false)).collect()
}

/// Full target list for one usage record, scoped to its owning user. Raw
/// snapshot (unenriched). Mirrors `ListCommandUsageTargetsForUser`.
pub async fn list_command_usage_targets_for_user(
    pool: &PgPool,
    user_id: &str,
    usage_id: i64,
) -> Result<Vec<CommandUsageTargetRow>, sqlx::Error> {
    let rows = sqlx::query(
        r#"SELECT t.kind, t.snowflake, t.display_name, t.avatar_hash, t.relation
           FROM command_usage_targets t
           JOIN command_usage cu ON cu.id = t.command_usage_id
           WHERE cu.id = $1 AND cu.app_user_id = $2
           ORDER BY t.id ASC"#,
    )
    .bind(usage_id)
    .bind(user_id)
    .fetch_all(pool)
    .await?;
    rows.into_iter()
        .map(|r| {
            Ok(CommandUsageTargetRow {
                kind: r.try_get(0)?,
                snowflake: r.try_get(1)?,
                display_name: r.try_get(2)?,
                avatar_hash: r.try_get(3)?,
                relation: r.try_get(4)?,
                app_user_id: None,
            })
        })
        .collect()
}

/// Distinct commands + guilds seen in the user's window (for filter dropdowns).
pub async fn command_usage_filter_options(
    pool: &PgPool,
    user_id: &str,
    window_days: i64,
) -> Result<CommandUsageFilterOptions, sqlx::Error> {
    let since = Utc::now() - Duration::days(window_days);
    let commands: Vec<String> = sqlx::query_scalar(
        "SELECT DISTINCT command FROM command_usage WHERE app_user_id = $1 AND created_at >= $2 ORDER BY command ASC",
    )
    .bind(user_id)
    .bind(since)
    .fetch_all(pool)
    .await?;

    let grows = sqlx::query(
        r#"SELECT DISTINCT cu.guild_id, COALESCE(bg.name, '') AS name, bg.icon_hash
           FROM command_usage cu
           LEFT JOIN bot_guilds bg ON bg.guild_id = cu.guild_id
           WHERE cu.app_user_id = $1 AND cu.created_at >= $2 AND cu.guild_id IS NOT NULL
           ORDER BY name ASC, cu.guild_id ASC"#,
    )
    .bind(user_id)
    .bind(since)
    .fetch_all(pool)
    .await?;
    let guilds = grows
        .into_iter()
        .map(|r| {
            Ok(CommandUsageFilterGuild {
                guild_id: r.try_get(0)?,
                name: r.try_get(1)?,
                icon_hash: r.try_get(2)?,
            })
        })
        .collect::<Result<Vec<_>, sqlx::Error>>()?;
    Ok(CommandUsageFilterOptions { commands, guilds })
}

/// The N-day usage summary (counts + top-5 commands + computed error rate).
pub async fn summarize_user_command_usage(
    pool: &PgPool,
    user_id: &str,
    window_days: i64,
) -> Result<CommandUsageSummary, sqlx::Error> {
    let since = Utc::now() - Duration::days(window_days);
    let (total, errors): (i64, i64) = sqlx::query_as(
        "SELECT COUNT(*), COUNT(*) FILTER (WHERE outcome = 'error') FROM command_usage WHERE app_user_id = $1 AND created_at >= $2",
    )
    .bind(user_id)
    .bind(since)
    .fetch_one(pool)
    .await?;

    let rows = sqlx::query(
        "SELECT command, COUNT(*) AS c FROM command_usage WHERE app_user_id = $1 AND created_at >= $2 GROUP BY command ORDER BY c DESC, command ASC LIMIT 5",
    )
    .bind(user_id)
    .bind(since)
    .fetch_all(pool)
    .await?;
    let top_commands = rows
        .into_iter()
        .map(|r| {
            Ok(CommandUsageTopCommand {
                command: r.try_get(0)?,
                count: r.try_get::<i64, _>("c")?,
            })
        })
        .collect::<Result<Vec<_>, sqlx::Error>>()?;

    let error_rate = if total > 0 {
        errors as f64 / total as f64
    } else {
        0.0
    };
    Ok(CommandUsageSummary {
        total_count: total,
        error_count: errors,
        error_rate,
        top_commands,
    })
}

/// Apply live-data enrichment to user/guild targets across the page (live wins
/// over snapshot; only present avatar/icon overrides). Mirrors the Go pass.
async fn enrich_targets(
    pool: &PgPool,
    items: &mut [CommandUsageRowWithTargets],
) -> Result<(), sqlx::Error> {
    let mut user_ids: HashSet<String> = HashSet::new();
    let mut guild_ids: HashSet<String> = HashSet::new();
    for it in items.iter() {
        for t in &it.targets {
            match t.kind.as_str() {
                "user" => {
                    user_ids.insert(t.snowflake.clone());
                }
                "guild" => {
                    guild_ids.insert(t.snowflake.clone());
                }
                _ => {}
            }
        }
    }

    // discord_id -> (app_user_id, display_name, avatar)
    let mut user_map: HashMap<String, (String, String, Option<String>)> = HashMap::new();
    if !user_ids.is_empty() {
        let ids: Vec<String> = user_ids.into_iter().collect();
        let rows = sqlx::query(
            "SELECT discord_id, id, COALESCE(discord_global_name, discord_username), discord_avatar FROM app_users WHERE discord_id = ANY($1)",
        )
        .bind(&ids)
        .fetch_all(pool)
        .await?;
        for r in rows {
            user_map.insert(r.try_get(0)?, (r.try_get(1)?, r.try_get(2)?, r.try_get(3)?));
        }
    }

    // guild_id -> (name, icon)
    let mut guild_map: HashMap<String, (String, Option<String>)> = HashMap::new();
    if !guild_ids.is_empty() {
        let ids: Vec<String> = guild_ids.into_iter().collect();
        let rows = sqlx::query("SELECT guild_id, name, icon_hash FROM bot_guilds WHERE guild_id = ANY($1)")
            .bind(&ids)
            .fetch_all(pool)
            .await?;
        for r in rows {
            guild_map.insert(r.try_get(0)?, (r.try_get(1)?, r.try_get(2)?));
        }
    }

    for it in items.iter_mut() {
        for t in it.targets.iter_mut() {
            match t.kind.as_str() {
                "user" => {
                    if let Some((app_user_id, display, avatar)) = user_map.get(&t.snowflake) {
                        t.display_name = Some(display.clone());
                        if avatar.is_some() {
                            t.avatar_hash = avatar.clone();
                        }
                        t.app_user_id = Some(app_user_id.clone());
                    }
                }
                "guild" => {
                    if let Some((name, icon)) = guild_map.get(&t.snowflake) {
                        t.display_name = Some(name.clone());
                        if icon.is_some() {
                            t.avatar_hash = icon.clone();
                        }
                    }
                }
                _ => {}
            }
        }
    }
    Ok(())
}
