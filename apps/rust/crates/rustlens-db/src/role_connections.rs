//! Storage for Discord linked-roles ("role connection") OAuth tokens and the
//! dashboard summary. Mirrors the Go `store/role_connections.go`.

use chrono::{DateTime, Utc};
use sqlx::{PgPool, Row};

use crate::identity::DiscordRoleConnectionSummary;

/// A stored role-connection row (encrypted tokens at rest).
#[derive(Debug, Clone)]
pub struct RoleConnection {
    pub user_id: String,
    pub discord_id: String,
    pub access_token_ct: Vec<u8>,
    pub access_token_nonce: Vec<u8>,
    pub refresh_token_ct: Vec<u8>,
    pub refresh_token_nonce: Vec<u8>,
    pub access_expires_at: DateTime<Utc>,
    pub scopes: String,
    pub linked_at: DateTime<Utc>,
    pub last_pushed_at: Option<DateTime<Utc>>,
    pub last_pushed_hash: Option<String>,
    pub last_push_error: Option<String>,
    pub revoked_at: Option<DateTime<Utc>>,
}

/// Fields needed to upsert a role connection (token + expiry).
pub struct NewRoleConnection<'a> {
    pub user_id: &'a str,
    pub discord_id: &'a str,
    pub access_token_ct: &'a [u8],
    pub access_token_nonce: &'a [u8],
    pub refresh_token_ct: &'a [u8],
    pub refresh_token_nonce: &'a [u8],
    pub access_expires_at: DateTime<Utc>,
    pub scopes: &'a str,
}

/// Load the full row, or `None` if the user has no connection.
pub async fn get_role_connection(
    pool: &PgPool,
    user_id: &str,
) -> Result<Option<RoleConnection>, sqlx::Error> {
    let row = sqlx::query(
        r#"SELECT user_id, discord_id, access_token_ct, access_token_nonce,
                  refresh_token_ct, refresh_token_nonce, access_expires_at, scopes,
                  linked_at, last_pushed_at, last_pushed_hash, last_push_error, revoked_at
           FROM discord_role_connections WHERE user_id = $1"#,
    )
    .bind(user_id)
    .fetch_optional(pool)
    .await?;

    let Some(row) = row else {
        return Ok(None);
    };
    Ok(Some(RoleConnection {
        user_id: row.try_get("user_id")?,
        discord_id: row.try_get("discord_id")?,
        access_token_ct: row.try_get("access_token_ct")?,
        access_token_nonce: row.try_get("access_token_nonce")?,
        refresh_token_ct: row.try_get("refresh_token_ct")?,
        refresh_token_nonce: row.try_get("refresh_token_nonce")?,
        access_expires_at: row.try_get("access_expires_at")?,
        scopes: row.try_get("scopes")?,
        linked_at: row.try_get("linked_at")?,
        last_pushed_at: row.try_get("last_pushed_at")?,
        last_pushed_hash: row.try_get("last_pushed_hash")?,
        last_push_error: row.try_get("last_push_error")?,
        revoked_at: row.try_get("revoked_at")?,
    }))
}

/// The safe dashboard summary (returned in `/api/app/me`). A missing row yields
/// `{ linked: false }` (matching Go).
pub async fn get_role_connection_summary(
    pool: &PgPool,
    user_id: &str,
) -> Result<DiscordRoleConnectionSummary, sqlx::Error> {
    let row = get_role_connection(pool, user_id).await?;
    let Some(row) = row else {
        return Ok(DiscordRoleConnectionSummary {
            linked: false,
            discord_id: None,
            linked_at: None,
            last_pushed_at: None,
            last_push_error: None,
            revoked: false,
        });
    };
    Ok(DiscordRoleConnectionSummary {
        linked: row.revoked_at.is_none(),
        discord_id: Some(row.discord_id),
        linked_at: Some(row.linked_at),
        last_pushed_at: row.last_pushed_at,
        last_push_error: row.last_push_error,
        revoked: row.revoked_at.is_some(),
    })
}

/// Insert or replace a user's role connection, clearing any prior revocation.
pub async fn upsert_role_connection(
    pool: &PgPool,
    in_: &NewRoleConnection<'_>,
) -> Result<(), sqlx::Error> {
    sqlx::query(
        r#"INSERT INTO discord_role_connections (
               user_id, discord_id,
               access_token_ct, access_token_nonce,
               refresh_token_ct, refresh_token_nonce,
               access_expires_at, scopes, linked_at, revoked_at
           ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, now(), NULL)
           ON CONFLICT (user_id) DO UPDATE SET
               discord_id          = EXCLUDED.discord_id,
               access_token_ct     = EXCLUDED.access_token_ct,
               access_token_nonce  = EXCLUDED.access_token_nonce,
               refresh_token_ct    = EXCLUDED.refresh_token_ct,
               refresh_token_nonce = EXCLUDED.refresh_token_nonce,
               access_expires_at   = EXCLUDED.access_expires_at,
               scopes              = EXCLUDED.scopes,
               revoked_at          = NULL"#,
    )
    .bind(in_.user_id)
    .bind(in_.discord_id)
    .bind(in_.access_token_ct)
    .bind(in_.access_token_nonce)
    .bind(in_.refresh_token_ct)
    .bind(in_.refresh_token_nonce)
    .bind(in_.access_expires_at)
    .bind(in_.scopes)
    .execute(pool)
    .await?;
    Ok(())
}

/// Delete a user's role connection.
pub async fn delete_role_connection(pool: &PgPool, user_id: &str) -> Result<(), sqlx::Error> {
    sqlx::query("DELETE FROM discord_role_connections WHERE user_id = $1")
        .bind(user_id)
        .execute(pool)
        .await?;
    Ok(())
}

/// Record a successful metadata push (clears any prior error).
pub async fn record_pushed(pool: &PgPool, user_id: &str, hash: &str) -> Result<(), sqlx::Error> {
    sqlx::query(
        "UPDATE discord_role_connections SET last_pushed_at = now(), last_pushed_hash = $2, last_push_error = NULL WHERE user_id = $1",
    )
    .bind(user_id)
    .bind(hash)
    .execute(pool)
    .await?;
    Ok(())
}

/// Record a failed metadata push.
pub async fn record_push_error(pool: &PgPool, user_id: &str, err: &str) -> Result<(), sqlx::Error> {
    sqlx::query("UPDATE discord_role_connections SET last_push_error = $2 WHERE user_id = $1")
        .bind(user_id)
        .bind(err)
        .execute(pool)
        .await?;
    Ok(())
}

/// The `granted_at` of the user's active `pro` entitlement, if any. Drives the
/// `is_pro` / `pro_since` metadata.
pub async fn get_user_pro_granted_at(
    pool: &PgPool,
    user_id: &str,
) -> Result<Option<DateTime<Utc>>, sqlx::Error> {
    let at: Option<DateTime<Utc>> = sqlx::query_scalar(
        r#"SELECT granted_at FROM user_entitlements
           WHERE user_id = $1 AND key = 'pro'
             AND (expires_at IS NULL OR expires_at > NOW())"#,
    )
    .bind(user_id)
    .fetch_optional(pool)
    .await?;
    Ok(at)
}

/// The display name for the role-connection platform username: prefers
/// `display_name`, falls back to `discord_username`.
pub async fn get_app_user_display_name(
    pool: &PgPool,
    user_id: &str,
) -> Result<String, sqlx::Error> {
    let name: Option<String> = sqlx::query_scalar(
        "SELECT COALESCE(NULLIF(display_name, ''), discord_username) FROM app_users WHERE id = $1",
    )
    .bind(user_id)
    .fetch_optional(pool)
    .await?;
    Ok(name.unwrap_or_default())
}
