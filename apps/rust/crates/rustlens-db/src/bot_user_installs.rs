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
