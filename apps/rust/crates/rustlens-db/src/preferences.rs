//! Per-user notification preferences (`user_notification_preferences`).
//! Mirrors the Go `appauth` preferences repo. Both flags default to `TRUE`
//! (opt-out); the row is lazily created on first read.

use serde::Serialize;
use sqlx::{PgPool, Row};

/// The per-user notification toggle state (frontend `NotificationPreferences`).
#[derive(Debug, Clone, Serialize)]
pub struct NotificationPreferences {
    #[serde(rename = "notifyInApp")]
    pub notify_in_app: bool,
    #[serde(rename = "notifyDiscordAnnouncements")]
    pub notify_discord_announcements: bool,
}

/// Load the user's preferences, lazily creating a default row if none exists.
/// Mirrors `LoadNotificationPreferences`.
pub async fn load_notification_preferences(
    pool: &PgPool,
    user_id: &str,
) -> Result<NotificationPreferences, sqlx::Error> {
    let row = sqlx::query(
        r#"INSERT INTO user_notification_preferences (user_id)
           VALUES ($1)
           ON CONFLICT (user_id) DO UPDATE SET user_id = EXCLUDED.user_id
           RETURNING notify_in_app, notify_discord_announcements"#,
    )
    .bind(user_id)
    .fetch_one(pool)
    .await?;
    Ok(NotificationPreferences {
        notify_in_app: row.try_get(0)?,
        notify_discord_announcements: row.try_get(1)?,
    })
}

/// Apply a partial update (nil fields unchanged) and return the resulting row.
/// Mirrors `UpsertNotificationPreferences`; the `::boolean` casts are required so
/// a `None` bind is a typed NULL that `COALESCE` can fall through.
pub async fn upsert_notification_preferences(
    pool: &PgPool,
    user_id: &str,
    notify_in_app: Option<bool>,
    notify_discord_announcements: Option<bool>,
) -> Result<NotificationPreferences, sqlx::Error> {
    let row = sqlx::query(
        r#"INSERT INTO user_notification_preferences (user_id, notify_in_app, notify_discord_announcements, updated_at)
           VALUES ($1, COALESCE($2::boolean, TRUE), COALESCE($3::boolean, TRUE), NOW())
           ON CONFLICT (user_id) DO UPDATE SET
               notify_in_app                = COALESCE($2::boolean, user_notification_preferences.notify_in_app),
               notify_discord_announcements = COALESCE($3::boolean, user_notification_preferences.notify_discord_announcements),
               updated_at                   = NOW()
           RETURNING notify_in_app, notify_discord_announcements"#,
    )
    .bind(user_id)
    .bind(notify_in_app)
    .bind(notify_discord_announcements)
    .fetch_one(pool)
    .await?;
    Ok(NotificationPreferences {
        notify_in_app: row.try_get(0)?,
        notify_discord_announcements: row.try_get(1)?,
    })
}
