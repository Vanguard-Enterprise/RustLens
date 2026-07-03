//! Per-guild bot settings (`guild_settings`), owned by the `guilds` service.
//! Mirrors `GetGuildSettings`/`UpdateGuildSettings` in `internal/store`. Schema
//! owned by Go. Note the `limited` column exists in the live schema (and this
//! struct) even though it is absent from the backend CLAUDE.md doc table.

use serde::Serialize;
use sqlx::{PgPool, Row};

/// The full settings row (all channel/role columns are `TEXT NOT NULL DEFAULT ''`).
#[derive(Debug, Clone, Serialize)]
pub struct GuildSettings {
    #[serde(rename = "guildId")]
    pub guild_id: String,
    #[serde(rename = "wipeReactLogChannelId")]
    pub wipe_react_log_channel_id: String,
    #[serde(rename = "linkingLogChannelId")]
    pub linking_log_channel_id: String,
    #[serde(rename = "trackedServerLogChannelId")]
    pub tracked_server_log_channel_id: String,
    #[serde(rename = "trackedUsersCategoryId")]
    pub tracked_users_category_id: String,
    #[serde(rename = "adminRoleId")]
    pub admin_role_id: String,
    #[serde(rename = "alertRoleId")]
    pub alert_role_id: String,
    pub limited: bool,
}

impl GuildSettings {
    fn empty(guild_id: &str) -> Self {
        Self {
            guild_id: guild_id.to_string(),
            wipe_react_log_channel_id: String::new(),
            linking_log_channel_id: String::new(),
            tracked_server_log_channel_id: String::new(),
            tracked_users_category_id: String::new(),
            admin_role_id: String::new(),
            alert_role_id: String::new(),
            limited: false,
        }
    }
}

const COLS: &str = "wipe_react_log_channel_id, linking_log_channel_id, tracked_server_log_channel_id, \
                    tracked_users_category_id, admin_role_id, alert_role_id, limited";

fn row_to_settings(guild_id: &str, row: &sqlx::postgres::PgRow) -> Result<GuildSettings, sqlx::Error> {
    Ok(GuildSettings {
        guild_id: guild_id.to_string(),
        wipe_react_log_channel_id: row.try_get("wipe_react_log_channel_id")?,
        linking_log_channel_id: row.try_get("linking_log_channel_id")?,
        tracked_server_log_channel_id: row.try_get("tracked_server_log_channel_id")?,
        tracked_users_category_id: row.try_get("tracked_users_category_id")?,
        admin_role_id: row.try_get("admin_role_id")?,
        alert_role_id: row.try_get("alert_role_id")?,
        limited: row.try_get("limited")?,
    })
}

/// `GetGuildSettings`: a missing row returns a synthetic all-empty struct
/// (never an error, never inserts).
pub async fn get(pool: &PgPool, guild_id: &str) -> Result<GuildSettings, sqlx::Error> {
    let row = sqlx::query(&format!("SELECT {COLS} FROM guild_settings WHERE guild_id = $1"))
        .bind(guild_id)
        .fetch_optional(pool)
        .await?;
    match row {
        Some(r) => row_to_settings(guild_id, &r),
        None => Ok(GuildSettings::empty(guild_id)),
    }
}

/// Optional-pointer patch: `None` → SQL NULL → column preserved (COALESCE);
/// `Some("")` → cleared to `''`; `Some(v)` → set. `limited` follows the same rule.
#[derive(Debug, Clone, Default)]
pub struct GuildSettingsPatch {
    pub wipe_react_log_channel_id: Option<String>,
    pub linking_log_channel_id: Option<String>,
    pub tracked_server_log_channel_id: Option<String>,
    pub tracked_users_category_id: Option<String>,
    pub admin_role_id: Option<String>,
    pub alert_role_id: Option<String>,
    pub limited: Option<bool>,
}

/// `UpdateGuildSettings`: ensure-row → COALESCE update → re-read. Three
/// statements, no explicit transaction (mirrors Go).
pub async fn update(
    pool: &PgPool,
    guild_id: &str,
    patch: &GuildSettingsPatch,
) -> Result<GuildSettings, sqlx::Error> {
    sqlx::query("INSERT INTO guild_settings (guild_id) VALUES ($1) ON CONFLICT (guild_id) DO NOTHING")
        .bind(guild_id)
        .execute(pool)
        .await?;
    sqlx::query(
        r#"UPDATE guild_settings
           SET wipe_react_log_channel_id     = COALESCE($2, wipe_react_log_channel_id),
               linking_log_channel_id        = COALESCE($3, linking_log_channel_id),
               tracked_server_log_channel_id = COALESCE($4, tracked_server_log_channel_id),
               tracked_users_category_id     = COALESCE($5, tracked_users_category_id),
               admin_role_id                 = COALESCE($6, admin_role_id),
               alert_role_id                 = COALESCE($7, alert_role_id),
               limited                       = COALESCE($8, limited),
               updated_at                    = NOW()
           WHERE guild_id = $1"#,
    )
    .bind(guild_id)
    .bind(&patch.wipe_react_log_channel_id)
    .bind(&patch.linking_log_channel_id)
    .bind(&patch.tracked_server_log_channel_id)
    .bind(&patch.tracked_users_category_id)
    .bind(&patch.admin_role_id)
    .bind(&patch.alert_role_id)
    .bind(patch.limited)
    .execute(pool)
    .await?;
    get(pool, guild_id).await
}
