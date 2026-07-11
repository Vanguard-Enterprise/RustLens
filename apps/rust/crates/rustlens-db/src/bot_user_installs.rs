//! `bot_user_installs` seen-heartbeat, owned by the `guilds` service. Mirrors
//! `TouchBotUserInstallSeen` in `internal/store/bot_user_installs.go`. Schema
//! owned by Go.

use chrono::{DateTime, SecondsFormat, Utc};
use serde::Serialize;
use sqlx::{PgPool, Row};

fn rfc3339_secs(dt: DateTime<Utc>) -> String {
    dt.to_rfc3339_opts(SecondsFormat::Secs, true)
}

/// Input for `bot-user-installs/seen`. `username == ""` preserves the stored
/// username; `None` global_name/avatar_hash preserve theirs.
#[derive(Debug, Clone, Default)]
pub struct SeenInput {
    pub discord_id: String,
    pub username: String,
    pub global_name: Option<String>,
    pub avatar_hash: Option<String>,
}

/// `TouchBotUserInstallSeen`: insert-active-if-missing, else bump `last_seen_at`
/// and conditionally backfill metadata. Critically, `is_installed` is NOT forced
/// to TRUE on conflict (only on fresh insert) — a de-authorized row stays left.
pub async fn touch_seen(pool: &PgPool, input: &SeenInput) -> Result<(), sqlx::Error> {
    sqlx::query(
        r#"INSERT INTO bot_user_installs (
               discord_id, username, global_name, avatar_hash,
               is_installed, installed_at, last_seen_at, metadata_updated_at
           )
           VALUES ($1, $2, $3, $4, TRUE, NOW(), NOW(), NOW())
           ON CONFLICT (discord_id) DO UPDATE SET
               username = CASE
                   WHEN EXCLUDED.username <> '' THEN EXCLUDED.username
                   ELSE bot_user_installs.username
               END,
               global_name  = COALESCE(EXCLUDED.global_name, bot_user_installs.global_name),
               avatar_hash  = COALESCE(EXCLUDED.avatar_hash, bot_user_installs.avatar_hash),
               last_seen_at = NOW(),
               metadata_updated_at = CASE
                   WHEN EXCLUDED.username <> ''
                     OR EXCLUDED.global_name IS NOT NULL
                     OR EXCLUDED.avatar_hash IS NOT NULL
                   THEN NOW()
                   ELSE bot_user_installs.metadata_updated_at
               END"#,
    )
    .bind(&input.discord_id)
    .bind(&input.username)
    .bind(&input.global_name)
    .bind(&input.avatar_hash)
    .execute(pool)
    .await?;
    Ok(())
}

/// Full install upsert from a Discord `APPLICATION_AUTHORIZED` webhook event.
/// Unlike `touch_seen`, this overwrites metadata + forces `is_installed=TRUE` and
/// clears `left_at` (re-authorization). A nil `scopes` is stored as `{}`.
#[derive(Debug, Clone, Default)]
pub struct UpsertInput {
    pub discord_id: String,
    pub username: String,
    pub global_name: Option<String>,
    pub avatar_hash: Option<String>,
    pub scopes: Vec<String>,
}

/// `UpsertBotUserInstall`.
pub async fn upsert_install(pool: &PgPool, input: &UpsertInput) -> Result<(), sqlx::Error> {
    sqlx::query(
        r#"INSERT INTO bot_user_installs (
               discord_id, username, global_name, avatar_hash, scopes,
               is_installed, installed_at, left_at, metadata_updated_at
           )
           VALUES ($1, $2, $3, $4, $5, TRUE, NOW(), NULL, NOW())
           ON CONFLICT (discord_id) DO UPDATE SET
               username            = EXCLUDED.username,
               global_name         = EXCLUDED.global_name,
               avatar_hash         = EXCLUDED.avatar_hash,
               scopes              = EXCLUDED.scopes,
               is_installed        = TRUE,
               left_at             = NULL,
               metadata_updated_at = NOW()"#,
    )
    .bind(&input.discord_id)
    .bind(&input.username)
    .bind(&input.global_name)
    .bind(&input.avatar_hash)
    .bind(&input.scopes)
    .execute(pool)
    .await?;
    Ok(())
}

/// `MarkBotUserInstallRemoved`: no-op (0 rows) when the user was never recorded.
pub async fn mark_removed(pool: &PgPool, discord_id: &str) -> Result<(), sqlx::Error> {
    sqlx::query("UPDATE bot_user_installs SET is_installed = FALSE, left_at = NOW() WHERE discord_id = $1")
        .bind(discord_id)
        .execute(pool)
        .await?;
    Ok(())
}

// ---- admin list (GET /api/app/admin/user-installs) ----

/// One row returned by the admin `user-installs` list. Mirrors Go's
/// `adminUserInstallItem`: nullable fields emit explicit `null`, `installedAt`
/// is a pre-formatted RFC3339 **seconds** string (matching Go's pre-formatting).
#[derive(Debug, Clone, Serialize)]
pub struct AdminUserInstallItem {
    #[serde(rename = "discordId")]
    pub discord_id: String,
    pub username: String,
    #[serde(rename = "globalName")]
    pub global_name: Option<String>,
    #[serde(rename = "avatarHash")]
    pub avatar_hash: Option<String>,
    pub scopes: Vec<String>,
    #[serde(rename = "isInstalled")]
    pub is_installed: bool,
    #[serde(rename = "installedAt")]
    pub installed_at: String,
    #[serde(rename = "leftAt")]
    pub left_at: Option<String>,
    #[serde(rename = "lastSeenAt")]
    pub last_seen_at: Option<String>,
}

/// Filter for the admin list. `q` is a free-text search applied to username
/// (substring), global_name (substring), and discord_id (prefix), all
/// case-insensitive. Empty disables the WHERE clause. Mirrors Go's
/// `ListAdminUserInstallsOpts`.
#[derive(Debug, Clone, Default)]
pub struct AdminListFilter {
    pub q: String,
}

const ADMIN_LIST_WHERE: &str = r#"
    WHERE ($1 = ''
           OR username ILIKE '%' || $1 || '%'
           OR global_name ILIKE '%' || $1 || '%'
           OR discord_id ILIKE $1 || '%')
"#;

/// `ListAdminUserInstalls`: one paginated page of `bot_user_installs` rows.
/// Total is computed with a separate `COUNT(*)` query against the same WHERE
/// clause so off-the-end pages still report the correct total.
pub async fn list_admin(
    pool: &PgPool,
    filter: &AdminListFilter,
    limit: i64,
    offset: i64,
) -> Result<(Vec<AdminUserInstallItem>, i64), sqlx::Error> {
    let total: i64 = sqlx::query_scalar(&format!(
        "SELECT COUNT(*) FROM bot_user_installs {ADMIN_LIST_WHERE}"
    ))
    .bind(&filter.q)
    .fetch_one(pool)
    .await?;

    let rows = sqlx::query(&format!(
        r#"SELECT discord_id, username, global_name, avatar_hash, scopes,
                  is_installed, installed_at, left_at, last_seen_at
           FROM bot_user_installs
           {ADMIN_LIST_WHERE}
           ORDER BY installed_at DESC, discord_id ASC
           LIMIT $2 OFFSET $3"#
    ))
    .bind(&filter.q)
    .bind(limit)
    .bind(offset)
    .fetch_all(pool)
    .await?;

    let items = rows
        .iter()
        .map(|row| {
            let installed_at: DateTime<Utc> = row.try_get("installed_at")?;
            let left_at: Option<DateTime<Utc>> = row.try_get("left_at")?;
            let last_seen_at: Option<DateTime<Utc>> = row.try_get("last_seen_at")?;
            Ok(AdminUserInstallItem {
                discord_id: row.try_get("discord_id")?,
                username: row.try_get("username")?,
                global_name: row.try_get("global_name")?,
                avatar_hash: row.try_get("avatar_hash")?,
                scopes: row.try_get("scopes")?,
                is_installed: row.try_get("is_installed")?,
                installed_at: rfc3339_secs(installed_at),
                left_at: left_at.map(rfc3339_secs),
                last_seen_at: last_seen_at.map(rfc3339_secs),
            })
        })
        .collect::<Result<Vec<_>, sqlx::Error>>()?;
    Ok((items, total))
}
