//! `bot_user_installs` seen-heartbeat, owned by the `guilds` service. Mirrors
//! `TouchBotUserInstallSeen` in `internal/store/bot_user_installs.go`. Schema
//! owned by Go.

use sqlx::PgPool;

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
