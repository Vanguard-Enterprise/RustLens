//! Admin-dashboard read queries against the shared Postgres tables.
//!
//! Mirrors the Go backend's `/api/app/admin/*` read handlers so the Rust
//! identity service serves identical JSON to the admin dashboard. Response
//! structs match the frontend TS types in `apps/frontend/lib/rustlens-api.ts`.

use chrono::{DateTime, SecondsFormat, TimeZone, Utc};
use serde::Serialize;
use sqlx::{PgPool, Postgres, QueryBuilder, Row};

use crate::{identity, rbac};

/// Error from an admin read query: either a client input problem (→ HTTP 400)
/// or a database error (→ HTTP 500).
#[derive(Debug, thiserror::Error)]
pub enum AdminQueryError {
    #[error("{0}")]
    BadRequest(String),
    #[error(transparent)]
    Db(#[from] sqlx::Error),
}

/// Whether the user has `perm`. Mirrors the Go `HasPermission`: a `superadmin`
/// role grants everything; otherwise the permission must appear in the union of
/// the user's roles' `admin_role_permissions`.
pub async fn has_permission(
    pool: &PgPool,
    user_id: &str,
    perm: &str,
) -> Result<bool, sqlx::Error> {
    let roles = identity::load_admin_role_keys(pool, user_id).await?;
    if roles.iter().any(|r| r == rbac::ROLE_SUPERADMIN) {
        return Ok(true);
    }
    if roles.is_empty() {
        return Ok(false);
    }
    let exists: bool = sqlx::query_scalar(
        r#"SELECT EXISTS(
               SELECT 1 FROM admin_user_roles ur
               JOIN admin_role_permissions rp ON rp.role_id = ur.role_id
               WHERE ur.user_id = $1 AND rp.permission_key = $2
           )"#,
    )
    .bind(user_id)
    .bind(perm)
    .fetch_one(pool)
    .await?;
    Ok(exists)
}

/// Whether the Discord user (resolved via `app_users.discord_id`) has `perm`,
/// with superadmin implying everything. Single EXISTS query (Go's
/// `DiscordHasPermission`, used by the rate-limit bypass checks).
pub async fn discord_has_permission(
    pool: &PgPool,
    discord_id: &str,
    perm: &str,
) -> Result<bool, sqlx::Error> {
    sqlx::query_scalar(
        r#"SELECT EXISTS(
               SELECT 1 FROM app_users u
               JOIN admin_user_roles aur ON aur.user_id = u.id
               JOIN admin_roles r ON r.id = aur.role_id
               LEFT JOIN admin_role_permissions rp
                   ON rp.role_id = r.id AND rp.permission_key = $2
               WHERE u.discord_id = $1 AND (r.key = 'superadmin' OR rp.permission_key IS NOT NULL)
           )"#,
    )
    .bind(discord_id)
    .bind(perm)
    .fetch_one(pool)
    .await
}

/// Whether the user holds **any** admin permission (Go's
/// `len(PermissionsForUser) > 0`). Used by the players name-search admin gate.
pub async fn has_any_permission(pool: &PgPool, user_id: &str) -> Result<bool, sqlx::Error> {
    let roles = identity::load_admin_role_keys(pool, user_id).await?;
    if roles.iter().any(|r| r == rbac::ROLE_SUPERADMIN) {
        return Ok(true);
    }
    if roles.is_empty() {
        return Ok(false);
    }
    let exists: bool = sqlx::query_scalar(
        r#"SELECT EXISTS(
               SELECT 1 FROM admin_user_roles ur
               JOIN admin_role_permissions rp ON rp.role_id = ur.role_id
               WHERE ur.user_id = $1
           )"#,
    )
    .bind(user_id)
    .fetch_one(pool)
    .await?;
    Ok(exists)
}

async fn table_exists(pool: &PgPool, name: &str) -> bool {
    sqlx::query_scalar::<_, bool>(
        "SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_name = $1)",
    )
    .bind(name)
    .fetch_one(pool)
    .await
    .unwrap_or(false)
}

/// Mirrors the frontend `AdminRoleItem` type. `description` is the empty string
/// when unset (Go `COALESCE(description,'')`), which the TS `string | null`
/// accepts.
#[derive(Debug, Clone, Serialize)]
pub struct AdminRoleItem {
    pub id: String,
    pub key: String,
    pub name: String,
    pub description: String,
    #[serde(rename = "isSystem")]
    pub is_system: bool,
    pub permissions: Vec<String>,
    #[serde(rename = "memberCount")]
    pub member_count: i32,
}

/// List all admin roles with member counts and resolved permissions. A
/// `superadmin` role resolves to the full static catalog (in catalog order);
/// every other role resolves to its `admin_role_permissions` rows, sorted.
/// Mirrors the Go `ListAdminRoles`.
pub async fn list_admin_roles(pool: &PgPool) -> Result<Vec<AdminRoleItem>, sqlx::Error> {
    // admin_roles.id is UUID; cast to text so it scans as String and matches
    // the Go `Role.ID.String()` form.
    let rows = sqlx::query(
        r#"SELECT r.id::text, r.key, r.name, COALESCE(r.description, ''), r.is_system,
                  COUNT(ur.user_id)::int AS member_count
           FROM admin_roles r
           LEFT JOIN admin_user_roles ur ON ur.role_id = r.id
           GROUP BY r.id, r.key, r.name, r.description, r.is_system
           ORDER BY r.key"#,
    )
    .fetch_all(pool)
    .await?;

    let mut out = Vec::with_capacity(rows.len());
    for row in rows {
        let id: String = row.try_get(0)?;
        let key: String = row.try_get(1)?;
        let name: String = row.try_get(2)?;
        let description: String = row.try_get(3)?;
        let is_system: bool = row.try_get(4)?;
        let member_count: i32 = row.try_get("member_count")?;

        let permissions = if key == rbac::ROLE_SUPERADMIN {
            rbac::ALL_PERMISSIONS.iter().map(|p| p.key.to_string()).collect()
        } else {
            let prows = sqlx::query(
                "SELECT permission_key FROM admin_role_permissions WHERE role_id::text = $1 ORDER BY permission_key",
            )
            .bind(&id)
            .fetch_all(pool)
            .await?;
            prows
                .into_iter()
                .map(|r| r.try_get::<String, _>("permission_key"))
                .collect::<Result<Vec<_>, _>>()?
        };

        out.push(AdminRoleItem {
            id,
            key,
            name,
            description,
            is_system,
            permissions,
            member_count,
        });
    }
    Ok(out)
}

// ---------------------------------------------------------------------------
// Users / administrators list
// ---------------------------------------------------------------------------

/// Mirrors the frontend `adminAppUser` nested type.
#[derive(Debug, Clone, Serialize)]
pub struct AdminAppUser {
    pub id: String,
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
}

/// Mirrors the frontend `AdminUserItem` type. `discordGlobalName`/`discordAvatar`
/// and `suspendedAt`/`deletedAt` are omitted when empty/absent (Go `omitempty`).
#[derive(Debug, Clone, Serialize)]
pub struct AdminUserItem {
    pub user: AdminAppUser,
    #[serde(rename = "discordId")]
    pub discord_id: String,
    #[serde(rename = "discordUsername")]
    pub discord_username: String,
    #[serde(rename = "discordGlobalName", skip_serializing_if = "String::is_empty")]
    pub discord_global_name: String,
    #[serde(rename = "discordAvatar", skip_serializing_if = "String::is_empty")]
    pub discord_avatar: String,
    #[serde(rename = "guildCount")]
    pub guild_count: i32,
    #[serde(rename = "steamConnectionCount")]
    pub steam_connection_count: i32,
    #[serde(rename = "adminRoles")]
    pub admin_roles: Vec<String>,
    pub entitlements: Vec<String>,
    #[serde(rename = "hasUserAppInstalled")]
    pub has_user_app_installed: bool,
    #[serde(rename = "banCategories")]
    pub ban_categories: Vec<String>,
    #[serde(rename = "suspendedAt", skip_serializing_if = "Option::is_none")]
    pub suspended_at: Option<String>,
    #[serde(rename = "deletedAt", skip_serializing_if = "Option::is_none")]
    pub deleted_at: Option<String>,
}

/// `{items, total, page, pageSize}` page wrapper for the users/administrators
/// list routes.
#[derive(Debug, Clone, Serialize)]
pub struct AdminUsersPage {
    pub items: Vec<AdminUserItem>,
    pub total: i64,
    pub page: i32,
    #[serde(rename = "pageSize")]
    pub page_size: i32,
}

/// Parsed query for the users/administrators list routes. The handler is
/// responsible for clamping `page`/`page_size` (defaults 1 / 50, cap 200).
#[derive(Debug, Clone, Default)]
pub struct UsersQuery {
    pub q: Option<String>,
    pub role: Vec<String>,
    pub status: Option<String>,
    pub discord: Option<String>,
    pub steam: Option<String>,
    pub user_app: Option<String>,
    pub entitlement: Option<String>,
    pub sort: Option<String>,
    pub page: i32,
    pub page_size: i32,
    pub joined_since: Option<String>,
    pub joined_before: Option<String>,
}

/// Parse an RFC3339 or `YYYY-MM-DD` date bound. For `upper_bound` a date-only
/// value is advanced by 24h (mirrors the Go `parseDateBound`).
fn parse_date_bound(v: &str, upper_bound: bool) -> Result<DateTime<Utc>, AdminQueryError> {
    if let Ok(t) = DateTime::parse_from_rfc3339(v) {
        return Ok(t.with_timezone(&Utc));
    }
    if let Ok(d) = chrono::NaiveDate::parse_from_str(v, "%Y-%m-%d") {
        let dt = Utc.from_utc_datetime(&d.and_hms_opt(0, 0, 0).unwrap());
        return Ok(if upper_bound {
            dt + chrono::Duration::hours(24)
        } else {
            dt
        });
    }
    Err(AdminQueryError::BadRequest("invalid date".into()))
}

/// SQL `ORDER BY` fragment for the sort param (whitelisted). Returns an error
/// for an unknown sort field. Mirrors the Go `orderByClause`.
fn order_by_clause(sort: Option<&str>) -> Result<String, AdminQueryError> {
    let raw = sort.unwrap_or("");
    let desc = raw.starts_with('-');
    let field = raw.strip_prefix('-').unwrap_or(raw);
    let col = match field {
        "" | "createdAt" => "u.created_at",
        "lastLoginAt" => "u.last_login_at",
        "displayName" => "u.display_name",
        _ => return Err(AdminQueryError::BadRequest("invalid sort".into())),
    };
    // Default (empty field) is DESC, matching Go.
    if desc || field.is_empty() {
        Ok(format!("{col} DESC"))
    } else {
        Ok(format!("{col} ASC"))
    }
}

const MEMBERS_ONLY_CLAUSE: &str = "NOT EXISTS (
    SELECT 1
    FROM admin_user_roles aur
    JOIN admin_role_permissions arp ON arp.role_id = aur.role_id
    WHERE aur.user_id = u.id
)";

/// Append the dynamic WHERE clause (filters) onto `qb`. `members_only` adds the
/// non-admin restriction used by the users route when no role filter is set.
/// Mirrors `buildUsersQuery` + the members-only injection.
fn push_users_where(
    qb: &mut QueryBuilder<'_, Postgres>,
    f: &UsersQuery,
    members_only: bool,
) -> Result<(), AdminQueryError> {
    let mut first = true;
    let open = |qb: &mut QueryBuilder<'_, Postgres>, first: &mut bool| {
        if *first {
            qb.push(" WHERE ");
            *first = false;
        } else {
            qb.push(" AND ");
        }
    };

    // status (default "active")
    match f.status.as_deref().unwrap_or("active") {
        "active" => {
            open(qb, &mut first);
            qb.push("u.suspended_at IS NULL AND u.deleted_at IS NULL");
        }
        "suspended" => {
            open(qb, &mut first);
            qb.push("u.suspended_at IS NOT NULL AND u.deleted_at IS NULL");
        }
        "deleted" => {
            open(qb, &mut first);
            qb.push("u.deleted_at IS NOT NULL");
        }
        "any" => {}
        _ => return Err(AdminQueryError::BadRequest("invalid status".into())),
    }

    // q substring (ILIKE across 5 columns; bind the pattern per column)
    if let Some(s) = f.q.as_deref().map(str::trim).filter(|s| !s.is_empty()) {
        let pat = format!("%{s}%");
        open(qb, &mut first);
        qb.push("(u.display_name ILIKE ");
        qb.push_bind(pat.clone());
        qb.push(" OR u.id ILIKE ");
        qb.push_bind(pat.clone());
        qb.push(" OR u.discord_username ILIKE ");
        qb.push_bind(pat.clone());
        qb.push(" OR u.discord_global_name ILIKE ");
        qb.push_bind(pat.clone());
        qb.push(" OR u.discord_id ILIKE ");
        qb.push_bind(pat);
        qb.push(")");
    }

    // role filter (EXISTS over the user's admin roles)
    if !f.role.is_empty() {
        open(qb, &mut first);
        qb.push("EXISTS (SELECT 1 FROM admin_user_roles aur JOIN admin_roles r ON r.id = aur.role_id WHERE aur.user_id = u.id AND r.key = ANY(");
        qb.push_bind(f.role.clone());
        qb.push("))");
    }

    // discord linked/unlinked
    match f.discord.as_deref().unwrap_or("") {
        "" | "any" => {}
        "linked" => {
            open(qb, &mut first);
            qb.push("u.discord_id IS NOT NULL AND u.discord_id <> ''");
        }
        "unlinked" => {
            open(qb, &mut first);
            qb.push("(u.discord_id IS NULL OR u.discord_id = '')");
        }
        _ => return Err(AdminQueryError::BadRequest("invalid discord".into())),
    }

    // joined window
    if let Some(v) = f.joined_since.as_deref().map(str::trim).filter(|s| !s.is_empty()) {
        let t = parse_date_bound(v, false)
            .map_err(|_| AdminQueryError::BadRequest("invalid joinedSince".into()))?;
        open(qb, &mut first);
        qb.push("u.created_at >= ");
        qb.push_bind(t);
    }
    if let Some(v) = f.joined_before.as_deref().map(str::trim).filter(|s| !s.is_empty()) {
        let t = parse_date_bound(v, true)
            .map_err(|_| AdminQueryError::BadRequest("invalid joinedBefore".into()))?;
        open(qb, &mut first);
        qb.push("u.created_at < ");
        qb.push_bind(t);
    }

    // steam (no-op data model; "linked" matches nothing)
    match f.steam.as_deref().unwrap_or("") {
        "" | "any" | "unlinked" => {}
        "linked" => {
            open(qb, &mut first);
            qb.push("FALSE");
        }
        _ => return Err(AdminQueryError::BadRequest("invalid steam".into())),
    }

    // userApp install state
    match f.user_app.as_deref().unwrap_or("") {
        "" | "any" => {}
        "installed" => {
            open(qb, &mut first);
            qb.push("EXISTS (SELECT 1 FROM bot_user_installs bui WHERE bui.discord_id = u.discord_id AND bui.is_installed = TRUE)");
        }
        "not_installed" => {
            open(qb, &mut first);
            qb.push("NOT EXISTS (SELECT 1 FROM bot_user_installs bui WHERE bui.discord_id = u.discord_id AND bui.is_installed = TRUE)");
        }
        _ => return Err(AdminQueryError::BadRequest("invalid userApp".into())),
    }

    // entitlement
    if let Some(e) = f.entitlement.as_deref().map(str::trim).filter(|s| !s.is_empty()) {
        open(qb, &mut first);
        qb.push("EXISTS (SELECT 1 FROM user_entitlements ue WHERE ue.user_id = u.id AND ue.key = ");
        qb.push_bind(e.to_string());
        qb.push(" AND (ue.expires_at IS NULL OR ue.expires_at > NOW()))");
    }

    if members_only {
        open(qb, &mut first);
        qb.push(MEMBERS_ONLY_CLAUSE);
    }

    Ok(())
}

/// Shared SELECT column list for both list routes (19 columns), differing only
/// in how admin-role and entitlement key arrays are sourced.
const USERS_SELECT_HEAD: &str = r#"SELECT u.id,
       COALESCE(NULLIF(u.display_name, ''), NULLIF(u.discord_global_name, ''), NULLIF(u.discord_username, ''), '') AS display_name,
       COALESCE(NULLIF(u.avatar_url, ''),
           CASE WHEN NULLIF(u.discord_avatar, '') IS NOT NULL AND NULLIF(u.discord_id, '') IS NOT NULL
                THEN 'https://cdn.discordapp.com/avatars/' || u.discord_id || '/' || u.discord_avatar || '.png'
                ELSE '' END) AS avatar_url,
       u.roles::text,
       u.created_at, u.updated_at, u.last_login_at,
       u.suspended_at, u.deleted_at,
       COALESCE(u.discord_id, ''), COALESCE(u.discord_username, ''),
       COALESCE(u.discord_global_name, ''), COALESCE(u.discord_avatar, ''),
       COALESCE((SELECT COUNT(*) FROM user_guilds g WHERE g.discord_id = u.discord_id AND g.left_at IS NULL), 0),
       0 AS steam_count,"#;

fn map_user_row(row: &sqlx::postgres::PgRow) -> Result<AdminUserItem, sqlx::Error> {
    let roles_json: String = row.try_get(3)?;
    let roles: Vec<String> = if roles_json.is_empty() {
        Vec::new()
    } else {
        serde_json::from_str(&roles_json).unwrap_or_default()
    };
    let suspended_at: Option<DateTime<Utc>> = row.try_get(7)?;
    let deleted_at: Option<DateTime<Utc>> = row.try_get(8)?;
    let guild_count: i64 = row.try_get(13)?;
    let steam_count: i32 = row.try_get(14)?;
    let admin_roles: Vec<String> = row.try_get(15)?;
    let entitlements: Vec<String> = row.try_get(16)?;
    let ban_categories: Vec<String> = row.try_get(17)?;
    let fmt = |t: DateTime<Utc>| t.to_rfc3339_opts(SecondsFormat::Secs, true);

    Ok(AdminUserItem {
        user: AdminAppUser {
            id: row.try_get(0)?,
            display_name: row.try_get(1)?,
            avatar_url: row.try_get(2)?,
            roles,
            created_at: row.try_get(4)?,
            updated_at: row.try_get(5)?,
            last_login_at: row.try_get(6)?,
        },
        discord_id: row.try_get(9)?,
        discord_username: row.try_get(10)?,
        discord_global_name: row.try_get(11)?,
        discord_avatar: row.try_get(12)?,
        guild_count: guild_count as i32,
        steam_connection_count: steam_count,
        admin_roles,
        entitlements,
        has_user_app_installed: row.try_get(18)?,
        ban_categories,
        suspended_at: suspended_at.map(fmt),
        deleted_at: deleted_at.map(fmt),
    })
}

async fn count_users(
    pool: &PgPool,
    f: &UsersQuery,
    members_only: bool,
) -> Result<i64, AdminQueryError> {
    let mut qb: QueryBuilder<Postgres> = QueryBuilder::new("SELECT COUNT(*) FROM app_users u");
    push_users_where(&mut qb, f, members_only)?;
    let total: i64 = qb.build_query_scalar().fetch_one(pool).await?;
    Ok(total)
}

/// List non-admin users by default (members-only when no role filter), matching
/// the Go `ListAdminUsers`. Uses correlated subqueries for the role/entitlement
/// arrays.
pub async fn list_admin_users(
    pool: &PgPool,
    f: &UsersQuery,
) -> Result<AdminUsersPage, AdminQueryError> {
    let order_by = order_by_clause(f.sort.as_deref())?;
    let members_only = f.role.is_empty();
    let total = count_users(pool, f, members_only).await?;
    let offset = (f.page - 1) * f.page_size;

    let mut qb: QueryBuilder<Postgres> = QueryBuilder::new(USERS_SELECT_HEAD);
    qb.push(
        r#"
       COALESCE((SELECT array_agg(r.key ORDER BY r.key) FROM admin_user_roles aur JOIN admin_roles r ON r.id = aur.role_id WHERE aur.user_id = u.id), ARRAY[]::text[]) AS admin_role_keys,
       COALESCE((SELECT array_agg(ue.key ORDER BY ue.key) FROM user_entitlements ue WHERE ue.user_id = u.id AND (ue.expires_at IS NULL OR ue.expires_at > NOW())), ARRAY[]::text[]) AS entitlement_keys,
       COALESCE((SELECT ub.categories FROM user_bans ub WHERE ub.discord_id = u.discord_id), ARRAY[]::text[]) AS ban_categories,
       EXISTS (SELECT 1 FROM bot_user_installs bui WHERE bui.discord_id = u.discord_id AND bui.is_installed = TRUE) AS has_user_app_installed
       FROM app_users u"#,
    );
    push_users_where(&mut qb, f, members_only)?;
    qb.push(format!(" ORDER BY {order_by} LIMIT "));
    qb.push_bind(f.page_size as i64);
    qb.push(" OFFSET ");
    qb.push_bind(offset as i64);

    let rows = qb.build().fetch_all(pool).await?;
    let items = rows.iter().map(map_user_row).collect::<Result<Vec<_>, _>>()?;
    Ok(AdminUsersPage { items, total, page: f.page, page_size: f.page_size })
}

/// List ALL users (no members-only restriction), matching the Go
/// `ListAdministrators`. Uses LEFT JOINs + GROUP BY with array_agg for the
/// role/entitlement arrays.
pub async fn list_administrators(
    pool: &PgPool,
    f: &UsersQuery,
) -> Result<AdminUsersPage, AdminQueryError> {
    let order_by = order_by_clause(f.sort.as_deref())?;
    let total = count_users(pool, f, false).await?;
    let offset = (f.page - 1) * f.page_size;

    let mut qb: QueryBuilder<Postgres> = QueryBuilder::new(USERS_SELECT_HEAD);
    qb.push(
        r#"
       COALESCE(array_agg(DISTINCT r.key) FILTER (WHERE r.key IS NOT NULL), ARRAY[]::text[]),
       COALESCE(array_agg(DISTINCT ue.key) FILTER (WHERE ue.key IS NOT NULL), ARRAY[]::text[]),
       COALESCE((SELECT ub.categories FROM user_bans ub WHERE ub.discord_id = u.discord_id), ARRAY[]::text[]),
       EXISTS (SELECT 1 FROM bot_user_installs bui WHERE bui.discord_id = u.discord_id AND bui.is_installed = TRUE) AS has_user_app_installed
       FROM app_users u
       LEFT JOIN admin_user_roles aur ON aur.user_id = u.id
       LEFT JOIN admin_roles r ON r.id = aur.role_id
       LEFT JOIN user_entitlements ue ON ue.user_id = u.id AND (ue.expires_at IS NULL OR ue.expires_at > NOW())"#,
    );
    push_users_where(&mut qb, f, false)?;
    qb.push(format!(" GROUP BY u.id ORDER BY {order_by} LIMIT "));
    qb.push_bind(f.page_size as i64);
    qb.push(" OFFSET ");
    qb.push_bind(offset as i64);

    let rows = qb.build().fetch_all(pool).await?;
    let items = rows.iter().map(map_user_row).collect::<Result<Vec<_>, _>>()?;
    Ok(AdminUsersPage { items, total, page: f.page, page_size: f.page_size })
}

// ---------------------------------------------------------------------------
// Audit log
// ---------------------------------------------------------------------------

/// Mirrors the frontend `AuditEventItem` type.
#[derive(Debug, Clone, Serialize)]
pub struct AuditEventItem {
    pub id: String,
    #[serde(rename = "actorUserId")]
    pub actor_user_id: String,
    #[serde(rename = "targetUserId")]
    pub target_user_id: String,
    #[serde(rename = "eventType")]
    pub event_type: String,
    pub payload: serde_json::Value,
    #[serde(rename = "createdAt")]
    pub created_at: DateTime<Utc>,
    #[serde(rename = "actorDisplayName", skip_serializing_if = "Option::is_none")]
    pub actor_display_name: Option<String>,
    #[serde(rename = "actorAvatarUrl", skip_serializing_if = "Option::is_none")]
    pub actor_avatar_url: Option<String>,
    #[serde(rename = "targetDisplayName", skip_serializing_if = "Option::is_none")]
    pub target_display_name: Option<String>,
    #[serde(rename = "targetAvatarUrl", skip_serializing_if = "Option::is_none")]
    pub target_avatar_url: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub result: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub reason: Option<String>,
}

/// `{items, nextCursor}` wrapper for the audit list route.
#[derive(Debug, Clone, Serialize)]
pub struct AdminAuditPage {
    pub items: Vec<AuditEventItem>,
    #[serde(rename = "nextCursor")]
    pub next_cursor: Option<String>,
}

/// Parsed query for the audit route. The handler clamps `page_size`
/// (default 50, cap 200).
#[derive(Debug, Clone, Default)]
pub struct AuditQuery {
    pub target_user_id: Option<String>,
    pub actor_user_id: Option<String>,
    pub event_type: Vec<String>,
    /// "success" | "error"; any other value is ignored (cleared) by the handler.
    pub result: Option<String>,
    pub since: Option<String>,
    pub before: Option<String>,
    pub page_size: i32,
}

/// List admin audit events, newest first, cursor-paginated by `created_at`.
/// Unions the new `audit_events` table with the legacy `app_audit_events` table
/// when present (and no `result` filter, which only the new table supports).
/// Mirrors the Go `ListAdminAudit` + `hydrateAuditActors`.
pub async fn list_admin_audit(
    pool: &PgPool,
    f: &AuditQuery,
) -> Result<AdminAuditPage, AdminQueryError> {
    let result_filter = match f.result.as_deref() {
        Some("success") => Some("success"),
        Some("error") => Some("error"),
        _ => None,
    };
    let has_legacy = result_filter.is_none() && table_exists(pool, "app_audit_events").await;

    let mut qb: QueryBuilder<Postgres> = QueryBuilder::new("SELECT * FROM (");

    // New events sub-select.
    qb.push(
        r#"SELECT id::text,
                  COALESCE(actor_user_id::text, '') AS actor_user_id,
                  COALESCE(target_user_id::text, '') AS target_user_id,
                  action_type AS event_type,
                  COALESCE(params, '{}'::jsonb) AS payload,
                  NULLIF(result, '') AS result,
                  reason,
                  created_at
           FROM audit_events"#,
    );
    push_audit_where(&mut qb, f, result_filter, false);

    if has_legacy {
        qb.push(" UNION ALL ");
        qb.push(
            r#"SELECT id::text,
                      COALESCE(actor_user_id, '') AS actor_user_id,
                      COALESCE(target_user_id, '') AS target_user_id,
                      event_type,
                      COALESCE(payload, '{}'::jsonb) AS payload,
                      NULL::text AS result,
                      NULL::text AS reason,
                      created_at
               FROM app_audit_events"#,
        );
        push_audit_where(&mut qb, f, None, true);
    }

    qb.push(") u ORDER BY created_at DESC LIMIT ");
    qb.push_bind((f.page_size + 1) as i64);

    let rows = qb.build().fetch_all(pool).await?;
    let mut items: Vec<AuditEventItem> = Vec::with_capacity(rows.len());
    let mut ids: Vec<String> = Vec::new();
    for row in &rows {
        let payload: serde_json::Value = row
            .try_get::<serde_json::Value, _>("payload")
            .unwrap_or_else(|_| serde_json::json!({}));
        let item = AuditEventItem {
            id: row.try_get("id")?,
            actor_user_id: row.try_get("actor_user_id")?,
            target_user_id: row.try_get("target_user_id")?,
            event_type: row.try_get("event_type")?,
            payload,
            created_at: row.try_get("created_at")?,
            actor_display_name: None,
            actor_avatar_url: None,
            target_display_name: None,
            target_avatar_url: None,
            result: row.try_get("result")?,
            reason: row.try_get("reason")?,
        };
        if !item.actor_user_id.is_empty() {
            ids.push(item.actor_user_id.clone());
        }
        if !item.target_user_id.is_empty() {
            ids.push(item.target_user_id.clone());
        }
        items.push(item);
    }

    let mut next_cursor = None;
    if items.len() > f.page_size as usize {
        items.truncate(f.page_size as usize);
        if let Some(last) = items.last() {
            next_cursor = Some(last.created_at.to_rfc3339_opts(SecondsFormat::Nanos, true));
        }
    }

    hydrate_audit_actors(pool, &mut items, &ids).await;
    Ok(AdminAuditPage { items, next_cursor })
}

fn push_audit_where(
    qb: &mut QueryBuilder<'_, Postgres>,
    f: &AuditQuery,
    result_filter: Option<&str>,
    legacy: bool,
) {
    let mut first = true;
    let open = |qb: &mut QueryBuilder<'_, Postgres>, first: &mut bool| {
        if *first {
            qb.push(" WHERE ");
            *first = false;
        } else {
            qb.push(" AND ");
        }
    };

    if let Some(v) = f.target_user_id.as_deref().map(str::trim).filter(|s| !s.is_empty()) {
        open(qb, &mut first);
        if legacy {
            qb.push("target_user_id = ");
        } else {
            qb.push("target_user_id::text = ");
        }
        qb.push_bind(v.to_string());
    }
    if let Some(v) = f.actor_user_id.as_deref().map(str::trim).filter(|s| !s.is_empty()) {
        open(qb, &mut first);
        if legacy {
            qb.push("actor_user_id = ");
        } else {
            qb.push("actor_user_id::text = ");
        }
        qb.push_bind(v.to_string());
    }
    if !f.event_type.is_empty() {
        open(qb, &mut first);
        if legacy {
            qb.push("event_type = ANY(");
        } else {
            qb.push("action_type = ANY(");
        }
        qb.push_bind(f.event_type.clone());
        qb.push("::text[])");
    }
    if !legacy {
        if let Some(r) = result_filter {
            open(qb, &mut first);
            qb.push("result = ");
            qb.push_bind(r.to_string());
        }
    }
    if let Some(v) = f.since.as_deref().map(str::trim).filter(|s| !s.is_empty()) {
        open(qb, &mut first);
        qb.push("created_at >= ");
        qb.push_bind(v.to_string());
        qb.push("::timestamptz");
    }
    if let Some(v) = f.before.as_deref().map(str::trim).filter(|s| !s.is_empty()) {
        open(qb, &mut first);
        qb.push("created_at < ");
        qb.push_bind(v.to_string());
        qb.push("::timestamptz");
    }
}

async fn hydrate_audit_actors(pool: &PgPool, items: &mut [AuditEventItem], ids: &[String]) {
    if ids.is_empty() {
        return;
    }
    let rows = match sqlx::query(
        r#"SELECT id::text,
                  COALESCE(NULLIF(display_name, ''), NULLIF(discord_global_name, ''), NULLIF(discord_username, ''), '') AS name,
                  COALESCE(NULLIF(avatar_url, ''),
                      CASE WHEN NULLIF(discord_avatar, '') IS NOT NULL AND NULLIF(discord_id, '') IS NOT NULL
                           THEN 'https://cdn.discordapp.com/avatars/' || discord_id || '/' || discord_avatar || '.png'
                           ELSE NULL END,
                      '') AS avatar
           FROM app_users WHERE id::text = ANY($1)"#,
    )
    .bind(ids)
    .fetch_all(pool)
    .await
    {
        Ok(r) => r,
        Err(_) => return,
    };

    let mut lookup: std::collections::HashMap<String, (String, String)> = std::collections::HashMap::new();
    for row in rows {
        let id: String = match row.try_get("id") {
            Ok(v) => v,
            Err(_) => continue,
        };
        let name: String = row.try_get("name").unwrap_or_default();
        let avatar: String = row.try_get("avatar").unwrap_or_default();
        lookup.insert(id, (name, avatar));
    }
    let str_opt = |s: &str| if s.is_empty() { None } else { Some(s.to_string()) };
    for it in items.iter_mut() {
        if let Some((n, a)) = lookup.get(&it.actor_user_id) {
            it.actor_display_name = str_opt(n);
            it.actor_avatar_url = str_opt(a);
        }
        if let Some((n, a)) = lookup.get(&it.target_user_id) {
            it.target_display_name = str_opt(n);
            it.target_avatar_url = str_opt(a);
        }
    }
}

// ---------------------------------------------------------------------------
// User detail + per-user / single audit
// ---------------------------------------------------------------------------

/// One guild a user belongs to, for the admin user-detail view. Nullable fields
/// serialize as `null` (Go `*string`/`*int32`, no omitempty).
#[derive(Debug, Clone, Serialize)]
pub struct AdminUserGuild {
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
    #[serde(rename = "isInstalled")]
    pub is_installed: bool,
    #[serde(rename = "leftAt")]
    pub left_at: Option<String>,
}

/// The admin user-detail aggregate (frontend `AdminUserDetail`).
#[derive(Debug, Clone, Serialize)]
pub struct AdminUserDetail {
    pub user: AdminAppUser,
    #[serde(rename = "discordId")]
    pub discord_id: String,
    #[serde(rename = "discordUsername")]
    pub discord_username: String,
    #[serde(rename = "discordGlobalName", skip_serializing_if = "String::is_empty")]
    pub discord_global_name: String,
    #[serde(rename = "discordAvatar", skip_serializing_if = "String::is_empty")]
    pub discord_avatar: String,
    pub guilds: Vec<AdminUserGuild>,
    #[serde(rename = "adminRoles")]
    pub admin_roles: Vec<String>,
    pub entitlements: Vec<String>,
    #[serde(rename = "banCategories")]
    pub ban_categories: Vec<String>,
    #[serde(rename = "suspendedAt", skip_serializing_if = "Option::is_none")]
    pub suspended_at: Option<String>,
    #[serde(rename = "suspendReason", skip_serializing_if = "String::is_empty")]
    pub suspend_reason: String,
    #[serde(rename = "deletedAt", skip_serializing_if = "Option::is_none")]
    pub deleted_at: Option<String>,
    #[serde(rename = "recentAudit")]
    pub recent_audit: Vec<AuditEventItem>,
}

/// Load the admin user-detail aggregate, or `None` (404) if the user is missing.
/// Mirrors the Go `GetAdminUser` (raw `display_name`/`avatar_url`, no CDN
/// fallback; guilds prefer `bot_guilds` over `user_guilds`; 5 most-recent audit
/// rows, unhydrated).
pub async fn get_admin_user(
    pool: &PgPool,
    id: &str,
) -> Result<Option<AdminUserDetail>, sqlx::Error> {
    let row = sqlx::query(
        r#"SELECT id, display_name, avatar_url, roles::text,
                  created_at, updated_at, last_login_at,
                  suspended_at, COALESCE(suspend_reason,''), deleted_at,
                  COALESCE(discord_id, ''), COALESCE(discord_username, ''),
                  COALESCE(discord_global_name, ''), COALESCE(discord_avatar, ''),
                  COALESCE((SELECT ub.categories FROM user_bans ub WHERE ub.discord_id = app_users.discord_id), ARRAY[]::text[])
           FROM app_users WHERE id = $1"#,
    )
    .bind(id)
    .fetch_optional(pool)
    .await?;
    let Some(row) = row else {
        return Ok(None);
    };

    let fmt = |t: DateTime<Utc>| t.to_rfc3339_opts(SecondsFormat::Secs, true);
    let roles_json: String = row.try_get(3)?;
    let roles: Vec<String> = if roles_json.is_empty() {
        Vec::new()
    } else {
        serde_json::from_str(&roles_json).unwrap_or_default()
    };
    let suspended_at: Option<DateTime<Utc>> = row.try_get(7)?;
    let deleted_at: Option<DateTime<Utc>> = row.try_get(9)?;
    let discord_id: String = row.try_get(10)?;
    let ban_categories: Vec<String> = row.try_get(14)?;

    let user = AdminAppUser {
        id: row.try_get(0)?,
        display_name: row.try_get(1)?,
        avatar_url: row.try_get(2)?,
        roles,
        created_at: row.try_get(4)?,
        updated_at: row.try_get(5)?,
        last_login_at: row.try_get(6)?,
    };

    let admin_roles: Vec<String> = sqlx::query(
        r#"SELECT r.key FROM admin_roles r
           JOIN admin_user_roles aur ON aur.role_id = r.id
           WHERE aur.user_id = $1 ORDER BY r.key"#,
    )
    .bind(id)
    .fetch_all(pool)
    .await?
    .into_iter()
    .filter_map(|r| r.try_get::<String, _>("key").ok())
    .collect();

    let entitlements: Vec<String> = sqlx::query(
        r#"SELECT key FROM user_entitlements
           WHERE user_id = $1 AND (expires_at IS NULL OR expires_at > NOW())
           ORDER BY key"#,
    )
    .bind(id)
    .fetch_all(pool)
    .await?
    .into_iter()
    .filter_map(|r| r.try_get::<String, _>("key").ok())
    .collect();

    let mut guilds = Vec::new();
    if !discord_id.is_empty() {
        let grows = sqlx::query(
            r#"SELECT
                   ug.guild_id,
                   COALESCE(NULLIF(bg.name, ''), ug.name, ''),
                   COALESCE(bg.icon_hash, ug.icon_hash),
                   COALESCE(bg.owner_discord_id, CASE WHEN ug.is_owner THEN owner_app.discord_id END),
                   COALESCE(bg.owner_username, CASE WHEN ug.is_owner THEN owner_app.discord_username END),
                   COALESCE(bg.owner_global_name, CASE WHEN ug.is_owner THEN owner_app.discord_global_name END),
                   COALESCE(bg.owner_avatar_hash, CASE WHEN ug.is_owner THEN owner_app.discord_avatar END),
                   COALESCE(bg.approximate_member_count, ug.approximate_member_count),
                   COALESCE(bg.is_installed, FALSE),
                   bg.left_at
               FROM user_guilds ug
               LEFT JOIN bot_guilds bg ON bg.guild_id = ug.guild_id
               LEFT JOIN app_users owner_app ON owner_app.discord_id = ug.discord_id
               WHERE ug.discord_id = $1 AND ug.left_at IS NULL
               ORDER BY COALESCE(NULLIF(bg.name, ''), NULLIF(ug.name, ''), ug.guild_id)"#,
        )
        .bind(&discord_id)
        .fetch_all(pool)
        .await?;
        for r in grows {
            let left_at: Option<DateTime<Utc>> = r.try_get(9)?;
            guilds.push(AdminUserGuild {
                guild_id: r.try_get(0)?,
                name: r.try_get(1)?,
                icon_hash: r.try_get(2)?,
                owner_discord_id: r.try_get(3)?,
                owner_username: r.try_get(4)?,
                owner_global_name: r.try_get(5)?,
                owner_avatar_hash: r.try_get(6)?,
                approximate_member_count: r.try_get(7)?,
                is_installed: r.try_get(8)?,
                left_at: left_at.map(fmt),
            });
        }
    }

    let recent_audit = fetch_user_audit(pool, id, 5, None).await;

    Ok(Some(AdminUserDetail {
        user,
        discord_id,
        discord_username: row.try_get(11)?,
        discord_global_name: row.try_get(12)?,
        discord_avatar: row.try_get(13)?,
        guilds,
        admin_roles,
        entitlements,
        ban_categories,
        suspended_at: suspended_at.map(fmt),
        suspend_reason: row.try_get(8)?,
        deleted_at: deleted_at.map(fmt),
        recent_audit,
    }))
}

/// Audit rows targeting `user_id`, newest first, cursor-paginated by `before`
/// (RFC3339). NOT hydrated (matches Go `fetchUserAudit`). Errors yield an empty
/// list (best-effort, like Go).
pub async fn fetch_user_audit(
    pool: &PgPool,
    user_id: &str,
    limit: i32,
    before: Option<&str>,
) -> Vec<AuditEventItem> {
    let before_ts = before
        .and_then(|b| DateTime::parse_from_rfc3339(b).ok())
        .map(|t| t.with_timezone(&Utc));
    let has_legacy = table_exists(pool, "app_audit_events").await;

    let mut qb: QueryBuilder<Postgres> = QueryBuilder::new(
        "SELECT id::text, COALESCE(actor_user_id::text,''), COALESCE(target_user_id::text,''), action_type, COALESCE(params,'{}'::jsonb), created_at FROM audit_events WHERE target_user_id::text = ",
    );
    qb.push_bind(user_id.to_string());
    if let Some(b) = before_ts {
        qb.push(" AND created_at < ");
        qb.push_bind(b);
    }
    if has_legacy {
        qb.push(" UNION ALL SELECT id::text, COALESCE(actor_user_id,''), COALESCE(target_user_id,''), event_type, COALESCE(payload,'{}'::jsonb), created_at FROM app_audit_events WHERE target_user_id = ");
        qb.push_bind(user_id.to_string());
        if let Some(b) = before_ts {
            qb.push(" AND created_at < ");
            qb.push_bind(b);
        }
    }
    qb.push(" ORDER BY created_at DESC LIMIT ");
    qb.push_bind(limit as i64);

    let rows = match qb.build().fetch_all(pool).await {
        Ok(r) => r,
        Err(_) => return Vec::new(),
    };
    rows.into_iter()
        .filter_map(|r| {
            Some(AuditEventItem {
                id: r.try_get(0).ok()?,
                actor_user_id: r.try_get(1).ok()?,
                target_user_id: r.try_get(2).ok()?,
                event_type: r.try_get(3).ok()?,
                payload: r.try_get::<serde_json::Value, _>(4).unwrap_or_else(|_| serde_json::json!({})),
                created_at: r.try_get(5).ok()?,
                actor_display_name: None,
                actor_avatar_url: None,
                target_display_name: None,
                target_avatar_url: None,
                result: None,
                reason: None,
            })
        })
        .collect()
}

/// A single hydrated audit event by id, or `None` (404). Mirrors
/// `GetAdminAuditEvent`.
pub async fn get_admin_audit_event(
    pool: &PgPool,
    id: &str,
) -> Result<Option<AuditEventItem>, sqlx::Error> {
    let has_legacy = table_exists(pool, "app_audit_events").await;
    let new_q = r#"SELECT id::text, COALESCE(actor_user_id::text,'') AS actor_user_id,
                          COALESCE(target_user_id::text,'') AS target_user_id,
                          action_type AS event_type, COALESCE(params,'{}'::jsonb) AS payload,
                          NULLIF(result,'') AS result, reason, created_at
                   FROM audit_events WHERE id::text = $1"#;
    let legacy_q = r#"SELECT id::text, COALESCE(actor_user_id,'') AS actor_user_id,
                             COALESCE(target_user_id,'') AS target_user_id,
                             event_type, COALESCE(payload,'{}'::jsonb) AS payload,
                             NULL::text AS result, NULL::text AS reason, created_at
                      FROM app_audit_events WHERE id::text = $1"#;
    let sql = if has_legacy {
        format!("({new_q}) UNION ALL ({legacy_q}) LIMIT 1")
    } else {
        new_q.to_string()
    };

    let row = sqlx::query(&sql).bind(id).fetch_optional(pool).await?;
    let Some(row) = row else {
        return Ok(None);
    };
    let mut item = AuditEventItem {
        id: row.try_get("id")?,
        actor_user_id: row.try_get("actor_user_id")?,
        target_user_id: row.try_get("target_user_id")?,
        event_type: row.try_get("event_type")?,
        payload: row.try_get::<serde_json::Value, _>("payload").unwrap_or_else(|_| serde_json::json!({})),
        created_at: row.try_get("created_at")?,
        actor_display_name: None,
        actor_avatar_url: None,
        target_display_name: None,
        target_avatar_url: None,
        result: row.try_get("result")?,
        reason: row.try_get("reason")?,
    };
    let mut ids = Vec::new();
    if !item.actor_user_id.is_empty() {
        ids.push(item.actor_user_id.clone());
    }
    if !item.target_user_id.is_empty() {
        ids.push(item.target_user_id.clone());
    }
    hydrate_audit_actors(pool, std::slice::from_mut(&mut item), &ids).await;
    Ok(Some(item))
}

/// Distinct audit action/event types, sorted. Mirrors `loadAuditEventTypes`
/// (the Go 60s in-process cache is not replicated — the query is fast + admin-only).
pub async fn list_audit_event_types(pool: &PgPool) -> Result<Vec<String>, sqlx::Error> {
    let has_legacy = table_exists(pool, "app_audit_events").await;
    let mut sql = "SELECT DISTINCT action_type AS t FROM audit_events WHERE action_type IS NOT NULL AND action_type <> ''".to_string();
    if has_legacy {
        sql.push_str(" UNION SELECT DISTINCT event_type AS t FROM app_audit_events WHERE event_type IS NOT NULL AND event_type <> ''");
    }
    let mut out: Vec<String> = sqlx::query(&sql)
        .fetch_all(pool)
        .await?
        .into_iter()
        .filter_map(|r| r.try_get::<String, _>("t").ok())
        .collect();
    out.sort();
    Ok(out)
}
