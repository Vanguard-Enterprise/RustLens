//! Identity-domain queries against the shared Postgres tables.
//!
//! These mirror the Go backend's `appauth.Repo` exactly (same SQL, same
//! resolution rules) so the Rust identity service and the Go backend behave
//! identically against the same `app_users` / `admin_*` / `user_entitlements`
//! tables.

use chrono::{DateTime, Utc};
use serde::Serialize;
use sqlx::{PgPool, Row};

/// The Discord profile fields used when upserting an app user.
#[derive(Debug, Clone)]
pub struct DiscordProfile {
    pub id: String,
    pub username: String,
    pub global_name: String,
    pub avatar_hash: String,
}

/// Mirrors the Go `AppUser` JSON shape consumed by the frontend.
#[derive(Debug, Clone, Serialize)]
pub struct AppUser {
    pub id: String,
    #[serde(rename = "discordId")]
    pub discord_id: String,
    #[serde(rename = "discordUsername")]
    pub discord_username: String,
    #[serde(rename = "discordGlobalName", skip_serializing_if = "Option::is_none")]
    pub discord_global_name: Option<String>,
    #[serde(rename = "discordAvatar", skip_serializing_if = "Option::is_none")]
    pub discord_avatar: Option<String>,
    #[serde(rename = "displayName")]
    pub display_name: String,
    #[serde(rename = "avatarUrl")]
    pub avatar_url: String,
    pub roles: Vec<String>,
    #[serde(rename = "createdAt")]
    pub created_at: DateTime<Utc>,
    #[serde(rename = "updatedAt")]
    pub updated_at: DateTime<Utc>,
    #[serde(rename = "lastLoginAt")]
    pub last_login_at: DateTime<Utc>,
    #[serde(rename = "sessionEpoch")]
    pub session_epoch: i64,
}

/// One Steam account linked to the acting user, mirroring the frontend
/// `LinkedSteamAccount` TS type (and the Go `appauth.LinkedSteamAccount`).
#[derive(Debug, Clone, Serialize)]
pub struct LinkedSteamAccount {
    #[serde(rename = "steamId")]
    pub steam_id: String,
    #[serde(rename = "bmId")]
    pub bm_id: String,
    #[serde(rename = "bmName")]
    pub bm_name: String,
    #[serde(rename = "avatarImage")]
    pub avatar_image: String,
    /// Link type: `soft` or `hard`. Serialized as `type` to match the frontend.
    #[serde(rename = "type")]
    pub link_type: String,
    pub source: String,
}

/// The acting user's Discord connected-account summary, mirroring the Go
/// `store.ConnectedAccount` / `connectedAccountToMap` shape returned by
/// `GET /api/app/me/connections`. `profile_email` is always empty (the Go
/// store never populates it either).
#[derive(Debug, Clone, Serialize)]
pub struct ConnectedAccount {
    pub provider: String,
    #[serde(rename = "providerAccountId")]
    pub provider_account_id: String,
    pub username: String,
    #[serde(rename = "displayName")]
    pub display_name: String,
    #[serde(rename = "avatarUrl")]
    pub avatar_url: String,
    #[serde(rename = "profileEmail")]
    pub profile_email: String,
    #[serde(rename = "createdAt")]
    pub created_at: DateTime<Utc>,
    #[serde(rename = "updatedAt")]
    pub updated_at: DateTime<Utc>,
}

/// One guild-membership record for the acting user, mirroring the frontend
/// `UserGuildMembership` TS type. `left_at` serializes as `null` when absent.
#[derive(Debug, Clone, Serialize)]
pub struct UserGuildMembership {
    #[serde(rename = "guildId")]
    pub guild_id: String,
    #[serde(rename = "isActive")]
    pub is_active: bool,
    #[serde(rename = "firstSeenAt")]
    pub first_seen_at: DateTime<Utc>,
    #[serde(rename = "lastSeenAt")]
    pub last_seen_at: DateTime<Utc>,
    #[serde(rename = "leftAt")]
    pub left_at: Option<DateTime<Utc>>,
}

/// Safe dashboard summary of the Discord linked-roles connection, mirroring the
/// frontend `DiscordRoleConnectionSummary` type. Populated by the role-connection
/// work (spec Item 4); `None` until then, in which case the whole
/// `discordRoleConnection` field is omitted (matching Go's `omitempty`). The
/// inner nullable fields serialize as `null`, never omitted, to match the TS
/// `string | null` shape.
#[derive(Debug, Clone, Serialize)]
pub struct DiscordRoleConnectionSummary {
    pub linked: bool,
    #[serde(rename = "discordId")]
    pub discord_id: Option<String>,
    #[serde(rename = "linkedAt")]
    pub linked_at: Option<DateTime<Utc>>,
    #[serde(rename = "lastPushedAt")]
    pub last_pushed_at: Option<DateTime<Utc>>,
    #[serde(rename = "lastPushError")]
    pub last_push_error: Option<String>,
    pub revoked: bool,
}

/// Mirrors the Go `CurrentUser` JSON shape for `GET /api/app/me`
/// (`appauth.CurrentUser`). `steam_accounts` and `guilds` are populated from the
/// shared identity tables (see `load_linked_steam_accounts` / `load_user_guilds`);
/// the live Go `/me` handler leaves them empty, so this Rust implementation is a
/// strict superset that stays JSON-compatible (the frontend types declare the
/// arrays present).
#[derive(Debug, Clone, Serialize)]
pub struct CurrentUser {
    pub user: AppUser,
    #[serde(rename = "steamAccounts")]
    pub steam_accounts: Vec<LinkedSteamAccount>,
    pub guilds: Vec<UserGuildMembership>,
    #[serde(rename = "adminRoles")]
    pub admin_roles: Vec<String>,
    pub permissions: Vec<String>,
    pub entitlements: Vec<String>,
    #[serde(
        rename = "discordRoleConnection",
        skip_serializing_if = "Option::is_none"
    )]
    pub discord_role_connection: Option<DiscordRoleConnectionSummary>,
}

/// Outcome of the session revocation check.
#[derive(Debug, PartialEq, Eq)]
pub enum Revocation {
    /// Session is valid.
    Valid,
    /// Session is revoked: user missing, epoch bumped, suspended, or deleted.
    Revoked,
}

/// Upsert an app user by Discord ID and return its id. `new_id` is used only on
/// insert (format `usr_<24 hex>`, generated by the caller). Mirrors
/// `appauth.Repo.FindOrCreateByDiscordID`.
pub async fn find_or_create_by_discord_id(
    pool: &PgPool,
    new_id: &str,
    p: &DiscordProfile,
) -> Result<String, sqlx::Error> {
    const SQL: &str = r#"
        INSERT INTO app_users (id, discord_id, discord_username, discord_global_name, discord_avatar, display_name, roles)
        VALUES ($1, $2, $3, NULLIF($4, ''), NULLIF($5, ''), COALESCE(NULLIF($4, ''), $3), '["user"]'::jsonb)
        ON CONFLICT (discord_id) DO UPDATE
            SET discord_username    = EXCLUDED.discord_username,
                discord_global_name = EXCLUDED.discord_global_name,
                discord_avatar      = EXCLUDED.discord_avatar,
                display_name        = COALESCE(NULLIF(app_users.display_name, ''), NULLIF(EXCLUDED.discord_global_name, ''), EXCLUDED.discord_username),
                updated_at          = NOW()
        RETURNING id
    "#;
    let id: String = sqlx::query_scalar(SQL)
        .bind(new_id)
        .bind(&p.id)
        .bind(&p.username)
        .bind(&p.global_name)
        .bind(&p.avatar_hash)
        .fetch_one(pool)
        .await?;
    Ok(id)
}

/// Same upsert as `find_or_create_by_discord_id`, but also reports whether the
/// row was freshly inserted (`xmax = 0`) vs an existing row that was refreshed.
/// Mirrors `EnsureAppUser`'s use of `appauth.Repo.FindOrCreateByDiscordID`,
/// which returns `(id, created, err)`. Kept as a separate function (rather than
/// changing the return type of `find_or_create_by_discord_id`) so the OAuth
/// callback call site is untouched.
pub async fn find_or_create_by_discord_id_ensure(
    pool: &PgPool,
    new_id: &str,
    p: &DiscordProfile,
) -> Result<(String, bool), sqlx::Error> {
    const SQL: &str = r#"
        INSERT INTO app_users (id, discord_id, discord_username, discord_global_name, discord_avatar, display_name, roles)
        VALUES ($1, $2, $3, NULLIF($4, ''), NULLIF($5, ''), COALESCE(NULLIF($4, ''), $3), '["user"]'::jsonb)
        ON CONFLICT (discord_id) DO UPDATE
            SET discord_username    = EXCLUDED.discord_username,
                discord_global_name = EXCLUDED.discord_global_name,
                discord_avatar      = EXCLUDED.discord_avatar,
                display_name        = COALESCE(NULLIF(app_users.display_name, ''), NULLIF(EXCLUDED.discord_global_name, ''), EXCLUDED.discord_username),
                updated_at          = NOW()
        RETURNING id, (xmax = 0) AS created"#;
    let row = sqlx::query(SQL)
        .bind(new_id)
        .bind(&p.id)
        .bind(&p.username)
        .bind(&p.global_name)
        .bind(&p.avatar_hash)
        .fetch_one(pool)
        .await?;
    Ok((row.try_get("id")?, row.try_get("created")?))
}

/// One guild membership to sync into `user_guilds` at login. Mirrors the Go
/// `store.SyncUserGuildInput`. `icon_hash` is empty when the guild has no icon.
#[derive(Debug, Clone)]
pub struct UserGuildInput {
    pub guild_id: String,
    pub name: String,
    pub icon_hash: String,
    pub approximate_member_count: Option<i32>,
    pub is_owner: bool,
}

/// Sync the user's current guild list into `user_guilds` in one transaction,
/// mirroring the Go `SyncUserGuildsForUser`:
///   1. upsert every guild the user is currently in as active (preserving
///      `first_seen_at`),
///   2. mark any previously-active guild not in the new list as inactive,
///      stamping `left_at` if not already set.
///
/// An empty `guilds` list is valid: statement (1) inserts nothing and statement
/// (2) deactivates all currently-active rows (`guild_id <> ALL('{}')` is true
/// for every row) — same as Go.
pub async fn sync_user_guilds(
    pool: &PgPool,
    discord_id: &str,
    guilds: &[UserGuildInput],
) -> Result<(), sqlx::Error> {
    let gids: Vec<String> = guilds.iter().map(|g| g.guild_id.clone()).collect();
    let names: Vec<String> = guilds.iter().map(|g| g.name.clone()).collect();
    let icons: Vec<String> = guilds.iter().map(|g| g.icon_hash.clone()).collect();
    let members: Vec<Option<i32>> =
        guilds.iter().map(|g| g.approximate_member_count).collect();
    let owners: Vec<bool> = guilds.iter().map(|g| g.is_owner).collect();

    let mut tx = pool.begin().await?;

    sqlx::query(
        r#"
        INSERT INTO user_guilds (
            discord_id, guild_id, is_active, first_seen_at, last_seen_at, left_at,
            name, icon_hash, approximate_member_count, is_owner
        )
        SELECT $1, t.gid, TRUE, NOW(), NOW(), NULL, t.name, t.icon, t.members, t.is_owner
        FROM unnest($2::text[], $3::text[], $4::text[], $5::int[], $6::bool[])
            AS t(gid, name, icon, members, is_owner)
        ON CONFLICT (discord_id, guild_id) DO UPDATE
        SET is_active = TRUE,
            last_seen_at = NOW(),
            left_at = NULL,
            name = EXCLUDED.name,
            icon_hash = EXCLUDED.icon_hash,
            approximate_member_count = EXCLUDED.approximate_member_count,
            is_owner = EXCLUDED.is_owner
        "#,
    )
    .bind(discord_id)
    .bind(&gids)
    .bind(&names)
    .bind(&icons)
    .bind(&members)
    .bind(&owners)
    .execute(&mut *tx)
    .await?;

    sqlx::query(
        r#"
        UPDATE user_guilds
        SET is_active = FALSE,
            left_at = COALESCE(left_at, NOW())
        WHERE discord_id = $1
          AND is_active = TRUE
          AND guild_id <> ALL($2::text[])
        "#,
    )
    .bind(discord_id)
    .bind(&gids)
    .execute(&mut *tx)
    .await?;

    tx.commit().await
}

/// `UPDATE app_users SET last_login_at = NOW()`. Mirrors `TouchLastLogin`.
pub async fn touch_last_login(pool: &PgPool, user_id: &str) -> Result<(), sqlx::Error> {
    sqlx::query("UPDATE app_users SET last_login_at = NOW() WHERE id = $1")
        .bind(user_id)
        .execute(pool)
        .await?;
    Ok(())
}

/// Check whether a session is still valid against live DB state.
/// Mirrors `appauth.ValidateSession`'s DB portion: missing user, an epoch lower
/// than the DB's, or a suspended/deleted account all revoke the session.
pub async fn revocation_check(
    pool: &PgPool,
    user_id: &str,
    token_epoch: i64,
) -> Result<Revocation, sqlx::Error> {
    let row = sqlx::query(
        "SELECT session_epoch, suspended_at, deleted_at FROM app_users WHERE id = $1",
    )
    .bind(user_id)
    .fetch_optional(pool)
    .await?;

    let Some(row) = row else {
        // User no longer exists -> treat as revoked.
        return Ok(Revocation::Revoked);
    };
    let current_epoch: i32 = row.try_get("session_epoch")?;
    let suspended_at: Option<DateTime<Utc>> = row.try_get("suspended_at")?;
    let deleted_at: Option<DateTime<Utc>> = row.try_get("deleted_at")?;

    if token_epoch < current_epoch as i64 || suspended_at.is_some() || deleted_at.is_some() {
        return Ok(Revocation::Revoked);
    }
    Ok(Revocation::Valid)
}

/// Account status for the OAuth callback gate (suspended/deleted users may not
/// sign in). Returns a `/login?notice=` slug when blocked.
pub async fn blocked_notice(
    pool: &PgPool,
    user_id: &str,
) -> Result<Option<&'static str>, sqlx::Error> {
    let row = sqlx::query("SELECT suspended_at, deleted_at FROM app_users WHERE id = $1")
        .bind(user_id)
        .fetch_optional(pool)
        .await?;
    let Some(row) = row else {
        return Ok(None);
    };
    let suspended_at: Option<DateTime<Utc>> = row.try_get("suspended_at")?;
    let deleted_at: Option<DateTime<Utc>> = row.try_get("deleted_at")?;
    if deleted_at.is_some() {
        return Ok(Some("account-deleted"));
    }
    if suspended_at.is_some() {
        return Ok(Some("account-suspended"));
    }
    Ok(None)
}

/// Load an app user, computing the Discord CDN avatar URL fallback the same way
/// the Go backend does.
pub async fn load_app_user(
    pool: &PgPool,
    user_id: &str,
) -> Result<Option<AppUser>, sqlx::Error> {
    let row = sqlx::query(
        r#"SELECT id, discord_id, discord_username, discord_global_name, discord_avatar,
                  display_name, avatar_url, roles, created_at, updated_at, last_login_at, session_epoch
           FROM app_users WHERE id = $1"#,
    )
    .bind(user_id)
    .fetch_optional(pool)
    .await?;

    let Some(row) = row else {
        return Ok(None);
    };

    let discord_id: String = row.try_get("discord_id")?;
    let discord_avatar: Option<String> = row.try_get("discord_avatar")?;
    let mut avatar_url: String = row.try_get("avatar_url")?;
    if avatar_url.is_empty() {
        if let Some(hash) = discord_avatar.as_deref().filter(|h| !h.is_empty()) {
            if !discord_id.is_empty() {
                avatar_url = format!(
                    "https://cdn.discordapp.com/avatars/{discord_id}/{hash}.png"
                );
            }
        }
    }

    let roles_json: serde_json::Value = row.try_get("roles")?;
    let roles = json_string_array(&roles_json);

    Ok(Some(AppUser {
        id: row.try_get("id")?,
        discord_id,
        discord_username: row.try_get("discord_username")?,
        discord_global_name: row.try_get("discord_global_name")?,
        discord_avatar,
        display_name: row.try_get("display_name")?,
        avatar_url,
        roles,
        created_at: row.try_get("created_at")?,
        updated_at: row.try_get("updated_at")?,
        last_login_at: row.try_get("last_login_at")?,
        session_epoch: {
            let e: i32 = row.try_get("session_epoch")?;
            e as i64
        },
    }))
}

/// Admin role keys for a user, sorted. Mirrors `loadAdminRoleKeys`.
pub async fn load_admin_role_keys(
    pool: &PgPool,
    user_id: &str,
) -> Result<Vec<String>, sqlx::Error> {
    let rows = sqlx::query(
        r#"SELECT ar.key
           FROM admin_user_roles ur
           JOIN admin_roles ar ON ar.id = ur.role_id
           WHERE ur.user_id = $1
           ORDER BY ar.key"#,
    )
    .bind(user_id)
    .fetch_all(pool)
    .await?;
    rows.into_iter().map(|r| r.try_get::<String, _>("key")).collect()
}

/// Resolve permissions for a user. Mirrors `loadPermissions`: a `superadmin`
/// role grants ALL permissions; otherwise the union of the user's roles'
/// permissions; no admin roles => empty.
pub async fn load_permissions(
    pool: &PgPool,
    user_id: &str,
    admin_roles: &[String],
) -> Result<Vec<String>, sqlx::Error> {
    if admin_roles.iter().any(|r| r == "superadmin") {
        let rows = sqlx::query("SELECT key FROM admin_permissions ORDER BY key")
            .fetch_all(pool)
            .await?;
        return rows.into_iter().map(|r| r.try_get::<String, _>("key")).collect();
    }
    if admin_roles.is_empty() {
        return Ok(Vec::new());
    }
    let rows = sqlx::query(
        r#"SELECT DISTINCT rp.permission_key
           FROM admin_user_roles ur
           JOIN admin_role_permissions rp ON rp.role_id = ur.role_id
           WHERE ur.user_id = $1
           ORDER BY rp.permission_key"#,
    )
    .bind(user_id)
    .fetch_all(pool)
    .await?;
    rows.into_iter()
        .map(|r| r.try_get::<String, _>("permission_key"))
        .collect()
}

/// Non-expired entitlement keys for a user, sorted. Mirrors `loadEntitlements`.
pub async fn load_entitlements(
    pool: &PgPool,
    user_id: &str,
) -> Result<Vec<String>, sqlx::Error> {
    let rows = sqlx::query(
        r#"SELECT key
           FROM user_entitlements
           WHERE user_id = $1
             AND (expires_at IS NULL OR expires_at > NOW())
           ORDER BY key"#,
    )
    .bind(user_id)
    .fetch_all(pool)
    .await?;
    rows.into_iter().map(|r| r.try_get::<String, _>("key")).collect()
}

/// Linked Steam accounts for the acting user, keyed on the user's `discord_id`.
/// Mirrors the Go `steam_discord_links → steam_accounts → bm_profile_cache
/// (bmName) → steam_account_cache (avatarImage)` join, deduped and ordered by
/// `steam_id`. Does not filter `hidden_discord_link_identities` (neither does
/// the Go `/me` path).
pub async fn load_linked_steam_accounts(
    pool: &PgPool,
    discord_id: &str,
) -> Result<Vec<LinkedSteamAccount>, sqlx::Error> {
    let rows = sqlx::query(
        r#"SELECT DISTINCT sa.steam_id,
                  COALESCE(sa.bm_id, ''),
                  COALESCE(bpc.bm_name, ''),
                  COALESCE(sac.avatar_image, ''),
                  sdl.type,
                  sdl.source
           FROM steam_discord_links sdl
           INNER JOIN steam_accounts sa ON sa.steam_id = sdl.steam_id
           LEFT JOIN bm_profile_cache bpc ON bpc.bm_id = sa.bm_id
           LEFT JOIN steam_account_cache sac ON sac.steam_id = sa.steam_id
           WHERE sdl.discord_id = $1
           ORDER BY sa.steam_id"#,
    )
    .bind(discord_id)
    .fetch_all(pool)
    .await?;
    rows.into_iter()
        .map(|r| {
            Ok(LinkedSteamAccount {
                steam_id: r.try_get(0)?,
                bm_id: r.try_get(1)?,
                bm_name: r.try_get(2)?,
                avatar_image: r.try_get(3)?,
                link_type: r.try_get(4)?,
                source: r.try_get(5)?,
            })
        })
        .collect()
}

/// Guild memberships for the acting user, keyed on the user's `discord_id`.
/// Mirrors the Go `user_guilds` read: both active and inactive rows, ordered
/// active-first then most-recently-seen.
pub async fn load_user_guilds(
    pool: &PgPool,
    discord_id: &str,
) -> Result<Vec<UserGuildMembership>, sqlx::Error> {
    let rows = sqlx::query(
        r#"SELECT guild_id, is_active, first_seen_at, last_seen_at, left_at
           FROM user_guilds
           WHERE discord_id = $1
           ORDER BY is_active DESC, last_seen_at DESC"#,
    )
    .bind(discord_id)
    .fetch_all(pool)
    .await?;
    rows.into_iter()
        .map(|r| {
            Ok(UserGuildMembership {
                guild_id: r.try_get("guild_id")?,
                is_active: r.try_get("is_active")?,
                first_seen_at: r.try_get("first_seen_at")?,
                last_seen_at: r.try_get("last_seen_at")?,
                left_at: r.try_get("left_at")?,
            })
        })
        .collect()
}

/// One row of `GET /api/app/me/guilds/installed`, mirroring the Go
/// `store.DashboardGuildRow` returned by `ListInstalledGuildsForUser`.
#[derive(Debug, Clone)]
pub struct InstalledGuildRow {
    pub guild_id: String,
    pub name: String,
    pub icon_hash: Option<String>,
    pub banner_hash: Option<String>,
    pub owner_discord_id: Option<String>,
    pub owner_username: Option<String>,
    pub owner_global_name: Option<String>,
    pub owner_avatar_hash: Option<String>,
    pub approximate_member_count: Option<i32>,
    pub tracked_server_count: i32,
}

/// Bot-installed guilds the acting Discord user is currently an active member
/// of. Mirrors `ListInstalledGuildsForUser` exactly (same join, same ordering).
pub async fn list_installed_guilds_for_user(
    pool: &PgPool,
    discord_id: &str,
) -> Result<Vec<InstalledGuildRow>, sqlx::Error> {
    let rows = sqlx::query(
        r#"SELECT bg.guild_id,
                  bg.name,
                  bg.icon_hash,
                  bg.banner_hash,
                  bg.owner_discord_id,
                  bg.owner_username,
                  bg.owner_global_name,
                  bg.owner_avatar_hash,
                  bg.approximate_member_count,
                  (SELECT COUNT(*) FROM guild_tracked_servers gts WHERE gts.guild_id = bg.guild_id)::int AS tracked_server_count
           FROM user_guilds ug
           JOIN bot_guilds bg ON bg.guild_id = ug.guild_id
           WHERE ug.discord_id = $1
             AND ug.is_active = TRUE
             AND bg.is_installed = TRUE
           ORDER BY bg.name ASC, bg.guild_id ASC"#,
    )
    .bind(discord_id)
    .fetch_all(pool)
    .await?;
    rows.into_iter()
        .map(|r| {
            Ok(InstalledGuildRow {
                guild_id: r.try_get("guild_id")?,
                name: r.try_get("name")?,
                icon_hash: r.try_get("icon_hash")?,
                banner_hash: r.try_get("banner_hash")?,
                owner_discord_id: r.try_get("owner_discord_id")?,
                owner_username: r.try_get("owner_username")?,
                owner_global_name: r.try_get("owner_global_name")?,
                owner_avatar_hash: r.try_get("owner_avatar_hash")?,
                approximate_member_count: r.try_get("approximate_member_count")?,
                tracked_server_count: r.try_get("tracked_server_count")?,
            })
        })
        .collect()
}

/// The raw `app_users.avatar_url` column, with **no** CDN-URL fallback
/// synthesis. Mirrors `store.PostgresStore.GetAppUserByID` /
/// `GetCurrentUserByID`'s `user.AvatarURL` as consumed by
/// `store.ConnectedAccount.AvatarURL` (`GET /api/app/me/connections`): unlike
/// `load_app_user` (used by `GET /api/app/me` via `appauth.Repo.GetByID`'s
/// session-minting path), the Go store layer never derives a Discord CDN URL
/// here, so this column is empty in practice (nothing in the Go codebase ever
/// writes it).
pub async fn get_raw_avatar_url(pool: &PgPool, user_id: &str) -> Result<String, sqlx::Error> {
    let v: Option<String> = sqlx::query_scalar("SELECT avatar_url FROM app_users WHERE id = $1")
        .bind(user_id)
        .fetch_optional(pool)
        .await?;
    Ok(v.unwrap_or_default())
}

/// Whether `discord_id` is recorded as an active member of `guild_id` in
/// `user_guilds`. Mirrors `store.IsGuildMember` (including its empty-input
/// short-circuit).
pub async fn is_guild_member(
    pool: &PgPool,
    guild_id: &str,
    discord_id: &str,
) -> Result<bool, sqlx::Error> {
    if guild_id.is_empty() || discord_id.is_empty() {
        return Ok(false);
    }
    let exists: bool = sqlx::query_scalar(
        r#"SELECT EXISTS(
             SELECT 1
             FROM user_guilds
             WHERE guild_id = $1
               AND discord_id = $2
               AND is_active = TRUE
           )"#,
    )
    .bind(guild_id)
    .bind(discord_id)
    .fetch_one(pool)
    .await?;
    Ok(exists)
}

/// Load the full `CurrentUser` aggregate for `/api/app/me`. Steam accounts and
/// guilds are loaded from the shared identity tables keyed on the user's
/// `discord_id`; an empty `discord_id` short-circuits to empty vecs (matching
/// the Go aggregate's behavior for users with no Discord connection).
pub async fn load_current_user(
    pool: &PgPool,
    user_id: &str,
) -> Result<Option<CurrentUser>, sqlx::Error> {
    let Some(user) = load_app_user(pool, user_id).await? else {
        return Ok(None);
    };
    let admin_roles = load_admin_role_keys(pool, user_id).await?;
    let permissions = load_permissions(pool, user_id, &admin_roles).await?;
    let entitlements = load_entitlements(pool, user_id).await?;

    let (steam_accounts, guilds) = if user.discord_id.is_empty() {
        (Vec::new(), Vec::new())
    } else {
        (
            load_linked_steam_accounts(pool, &user.discord_id).await?,
            load_user_guilds(pool, &user.discord_id).await?,
        )
    };

    Ok(Some(CurrentUser {
        user,
        steam_accounts,
        guilds,
        admin_roles,
        permissions,
        entitlements,
        // Populated by the role-connection work (spec Item 4).
        discord_role_connection: None,
    }))
}

fn json_string_array(value: &serde_json::Value) -> Vec<String> {
    value
        .as_array()
        .map(|arr| {
            arr.iter()
                .filter_map(|v| v.as_str().map(str::to_string))
                .collect()
        })
        .unwrap_or_default()
}

#[cfg(test)]
mod tests {
    use super::*;

    fn sample_user() -> AppUser {
        let t: DateTime<Utc> = "2026-01-02T03:04:05Z".parse().unwrap();
        AppUser {
            id: "usr_x".into(),
            discord_id: "987".into(),
            discord_username: "frank".into(),
            discord_global_name: Some("Frank".into()),
            discord_avatar: Some("avh".into()),
            display_name: "Frank".into(),
            avatar_url: "https://cdn/a.png".into(),
            roles: vec!["user".into()],
            created_at: t,
            updated_at: t,
            last_login_at: t,
            session_epoch: 0,
        }
    }

    /// The `LinkedSteamAccount.link_type` field must serialize as `type` and
    /// `UserGuildMembership.left_at` as `leftAt: null` when absent — both are
    /// part of the frontend contract.
    #[test]
    fn linked_steam_and_guild_field_renames() {
        let acct = LinkedSteamAccount {
            steam_id: "76561190000000001".into(),
            bm_id: "bm-1".into(),
            bm_name: "Name".into(),
            avatar_image: "https://av/1.png".into(),
            link_type: "hard".into(),
            source: "battlemetrics".into(),
        };
        let v = serde_json::to_value(&acct).unwrap();
        assert_eq!(v["type"], "hard");
        assert!(v.get("link_type").is_none());
        assert_eq!(v["steamId"], "76561190000000001");

        let g = UserGuildMembership {
            guild_id: "g1".into(),
            is_active: true,
            first_seen_at: "2026-01-02T03:04:05Z".parse().unwrap(),
            last_seen_at: "2026-01-02T03:04:05Z".parse().unwrap(),
            left_at: None,
        };
        let gv = serde_json::to_value(&g).unwrap();
        assert!(gv.get("leftAt").is_some(), "leftAt must be present");
        assert!(gv["leftAt"].is_null(), "leftAt must serialize as null, not omitted");
        assert_eq!(gv["isActive"], true);
    }

    /// `discordRoleConnection` is omitted entirely when `None` (matches Go
    /// `omitempty`) and present when populated.
    #[test]
    fn current_user_omits_role_connection_when_none() {
        let cu = CurrentUser {
            user: sample_user(),
            steam_accounts: vec![],
            guilds: vec![],
            admin_roles: vec![],
            permissions: vec![],
            entitlements: vec![],
            discord_role_connection: None,
        };
        let v = serde_json::to_value(&cu).unwrap();
        assert!(v.get("discordRoleConnection").is_none());
        // Required arrays are always present (frontend declares them non-optional).
        assert!(v["steamAccounts"].is_array());
        assert!(v["guilds"].is_array());

        let cu2 = CurrentUser {
            discord_role_connection: Some(DiscordRoleConnectionSummary {
                linked: true,
                discord_id: Some("987".into()),
                linked_at: None,
                last_pushed_at: None,
                last_push_error: None,
                revoked: false,
            }),
            ..cu
        };
        let v2 = serde_json::to_value(&cu2).unwrap();
        assert_eq!(v2["discordRoleConnection"]["linked"], true);
        // Inner nullable fields serialize as null, not omitted.
        assert!(v2["discordRoleConnection"].get("linkedAt").is_some());
        assert!(v2["discordRoleConnection"]["linkedAt"].is_null());
    }
}
