//! Bot-internal identity queries (`/api/internal/bot/*`, JWT-authed).
//! Command-usage ingest, user bans, and the website-admin / PRO lookups.
//! Mirrors the Go `store` + `appauth` bot-facing handlers.

use sqlx::PgPool;

/// Resolve a Discord ID to an `app_users.id`, or `None` if there's no row.
/// Mirrors `appauth.Repo.FindByDiscordID`.
pub async fn find_by_discord_id(
    pool: &PgPool,
    discord_id: &str,
) -> Result<Option<String>, sqlx::Error> {
    sqlx::query_scalar("SELECT id FROM app_users WHERE discord_id = $1")
        .bind(discord_id)
        .fetch_optional(pool)
        .await
}

/// A command-usage target to insert (already validated by the handler).
#[derive(Debug, Clone)]
pub struct CommandUsageTargetInsert {
    pub kind: String,
    pub snowflake: String,
    pub display_name: Option<String>,
    pub avatar_hash: Option<String>,
    pub relation: String,
}

/// A command-usage row to insert (already validated by the handler).
#[derive(Debug, Clone)]
pub struct CommandUsageInsert {
    pub app_user_id: String,
    pub discord_id: String,
    pub guild_id: Option<String>,
    pub channel_id: Option<String>,
    pub interaction_type: String,
    pub command: String,
    pub subcommand: Option<String>,
    pub target: Option<String>,
    pub options: serde_json::Value,
    pub outcome: String,
    pub error_code: Option<String>,
    pub latency_ms: i32,
    pub correlation_id: Option<String>,
    pub parent_correlation_id: Option<String>,
    pub channel_kind: Option<String>,
    pub channel_name: Option<String>,
    pub targets: Vec<CommandUsageTargetInsert>,
}

/// Insert one command-usage row + its targets in a transaction, resolving
/// `parent_correlation_id` to a `parent_usage_id` (same app user, newest match).
/// Mirrors the Go `InsertCommandUsage`.
pub async fn insert_command_usage(
    pool: &PgPool,
    row: &CommandUsageInsert,
) -> Result<(), sqlx::Error> {
    let mut tx = pool.begin().await?;

    let parent_usage_id: Option<i64> = match row
        .parent_correlation_id
        .as_deref()
        .filter(|s| !s.is_empty())
    {
        Some(pc) => sqlx::query_scalar(
            "SELECT id FROM command_usage WHERE app_user_id = $1 AND correlation_id = $2 ORDER BY id DESC LIMIT 1",
        )
        .bind(&row.app_user_id)
        .bind(pc)
        .fetch_optional(&mut *tx)
        .await?,
        None => None,
    };

    let usage_id: i64 = sqlx::query_scalar(
        r#"INSERT INTO command_usage (
               app_user_id, discord_id, guild_id, channel_id,
               interaction_type, command, subcommand, target,
               options, outcome, error_code, latency_ms,
               correlation_id, parent_usage_id, channel_kind, channel_name
           ) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
           RETURNING id"#,
    )
    .bind(&row.app_user_id)
    .bind(&row.discord_id)
    .bind(&row.guild_id)
    .bind(&row.channel_id)
    .bind(&row.interaction_type)
    .bind(&row.command)
    .bind(&row.subcommand)
    .bind(&row.target)
    .bind(&row.options)
    .bind(&row.outcome)
    .bind(&row.error_code)
    .bind(row.latency_ms)
    .bind(&row.correlation_id)
    .bind(parent_usage_id)
    .bind(&row.channel_kind)
    .bind(&row.channel_name)
    .fetch_one(&mut *tx)
    .await?;

    for t in &row.targets {
        sqlx::query(
            "INSERT INTO command_usage_targets (command_usage_id, kind, snowflake, display_name, avatar_hash, relation) VALUES ($1,$2,$3,$4,$5,$6)",
        )
        .bind(usage_id)
        .bind(&t.kind)
        .bind(&t.snowflake)
        .bind(&t.display_name)
        .bind(&t.avatar_hash)
        .bind(&t.relation)
        .execute(&mut *tx)
        .await?;
    }

    tx.commit().await
}

/// The user's ban categories + suspension state (Go `GetUserBan`). Missing row →
/// empty categories.
pub async fn get_user_ban(
    pool: &PgPool,
    discord_id: &str,
) -> Result<(Vec<String>, bool), sqlx::Error> {
    let categories: Vec<String> = sqlx::query_scalar("SELECT categories FROM user_bans WHERE discord_id = $1")
        .bind(discord_id)
        .fetch_optional(pool)
        .await?
        .unwrap_or_default();
    let suspended: bool = sqlx::query_scalar("SELECT (suspended_at IS NOT NULL) FROM app_users WHERE discord_id = $1")
        .bind(discord_id)
        .fetch_optional(pool)
        .await?
        .unwrap_or(false);
    Ok((categories, suspended))
}

/// Upsert a user's ban categories, or delete the row when empty (Go
/// `UpsertUserBan`). Returns the resulting categories.
pub async fn upsert_user_ban(
    pool: &PgPool,
    discord_id: &str,
    categories: &[String],
    updated_by: &str,
) -> Result<Vec<String>, sqlx::Error> {
    if categories.is_empty() {
        sqlx::query("DELETE FROM user_bans WHERE discord_id = $1")
            .bind(discord_id)
            .execute(pool)
            .await?;
        return Ok(Vec::new());
    }
    sqlx::query(
        r#"INSERT INTO user_bans (discord_id, categories, updated_by, updated_at)
           VALUES ($1, $2, $3, NOW())
           ON CONFLICT (discord_id) DO UPDATE
           SET categories = EXCLUDED.categories, updated_by = EXCLUDED.updated_by, updated_at = NOW()"#,
    )
    .bind(discord_id)
    .bind(categories)
    .bind(updated_by)
    .execute(pool)
    .await?;
    Ok(categories.to_vec())
}

/// Whether the Discord user maps to an app user holding any admin permission
/// (superadmin implies all). Mirrors `IsDiscordWebsiteAdmin`.
pub async fn is_discord_website_admin(
    pool: &PgPool,
    discord_id: &str,
) -> Result<bool, sqlx::Error> {
    sqlx::query_scalar(
        r#"SELECT EXISTS (
               SELECT 1
               FROM app_users u
               JOIN admin_user_roles aur ON aur.user_id = u.id
               JOIN admin_roles r ON r.id = aur.role_id
               LEFT JOIN admin_role_permissions rp ON rp.role_id = r.id
               WHERE u.discord_id = $1
                 AND (r.key = 'superadmin' OR rp.permission_key IS NOT NULL)
           )"#,
    )
    .bind(discord_id)
    .fetch_one(pool)
    .await
}

/// Whether the Discord user has an active entitlement `key`. Mirrors
/// `DiscordUserHasEntitlement`.
pub async fn discord_user_has_entitlement(
    pool: &PgPool,
    discord_id: &str,
    key: &str,
) -> Result<bool, sqlx::Error> {
    sqlx::query_scalar(
        r#"SELECT EXISTS (
               SELECT 1
               FROM app_users u
               JOIN user_entitlements ue ON ue.user_id = u.id
               WHERE u.discord_id = $1 AND ue.key = $2
                 AND (ue.expires_at IS NULL OR ue.expires_at > NOW())
           )"#,
    )
    .bind(discord_id)
    .bind(key)
    .fetch_one(pool)
    .await
}
