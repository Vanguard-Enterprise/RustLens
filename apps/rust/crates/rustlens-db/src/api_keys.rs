//! Admin API-key persistence, mirroring the Go `internal/store/api_keys.go`
//! store. The `api_keys` table is owned by the Go `EnsureSchema` (shared DB); we
//! only read/write it. Secret hashing is the caller's responsibility (see
//! `rustlens_httpauth::api_key`): this layer stores/compares the raw `sha256`
//! bytes exactly as Go does, so keys minted by either backend validate against
//! the other.

use chrono::{DateTime, Utc};
use sqlx::{PgPool, Postgres, QueryBuilder, Row};

/// A row in the `api_keys` table (mirrors Go `store.APIKey`).
#[derive(Debug, Clone)]
pub struct ApiKey {
    pub id: String,
    pub name: String,
    pub prefix: String,
    pub last4: String,
    pub secret_hash: Vec<u8>,
    pub created_by_user_id: String,
    pub permissions: Vec<String>,
    pub expires_at: Option<DateTime<Utc>>,
    pub revoked_at: Option<DateTime<Utc>>,
    pub revoked_reason: Option<String>,
    pub last_used_at: Option<DateTime<Utc>>,
    pub last_used_ip: Option<String>,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

/// Input for [`create`] (mirrors Go `store.APIKeyCreate`).
pub struct ApiKeyCreate<'a> {
    pub name: &'a str,
    pub prefix: &'a str,
    pub last4: &'a str,
    pub secret_hash: &'a [u8],
    pub created_by_user_id: &'a str,
    pub permissions: &'a [String],
    pub expires_at: Option<DateTime<Utc>>,
}

/// Returned by [`get_by_prefix`] / [`get_by_id`] when no row matches. Distinct
/// from a real DB error so callers can map it to 404 / 401.
#[derive(Debug, thiserror::Error)]
pub enum ApiKeyError {
    #[error("api key not found")]
    NotFound,
    #[error(transparent)]
    Db(#[from] sqlx::Error),
}

const SELECT_COLS: &str = "id::text AS id, name, prefix, last4, secret_hash, created_by_user_id, \
     permissions, expires_at, revoked_at, revoked_reason, \
     last_used_at, last_used_ip::text AS last_used_ip, created_at, updated_at";

fn scan_key(row: &sqlx::postgres::PgRow) -> Result<ApiKey, sqlx::Error> {
    Ok(ApiKey {
        id: row.try_get("id")?,
        name: row.try_get("name")?,
        prefix: row.try_get("prefix")?,
        last4: row.try_get("last4")?,
        secret_hash: row.try_get("secret_hash")?,
        created_by_user_id: row.try_get("created_by_user_id")?,
        permissions: row.try_get("permissions")?,
        expires_at: row.try_get("expires_at")?,
        revoked_at: row.try_get("revoked_at")?,
        revoked_reason: row.try_get("revoked_reason")?,
        last_used_at: row.try_get("last_used_at")?,
        last_used_ip: row.try_get("last_used_ip")?,
        created_at: row.try_get("created_at")?,
        updated_at: row.try_get("updated_at")?,
    })
}

/// Insert a new API key. Returns the full row (id/created_at/updated_at from the
/// DB, the rest echoed from the input) so the create action can build its
/// one-time response.
pub async fn create(pool: &PgPool, input: ApiKeyCreate<'_>) -> Result<ApiKey, sqlx::Error> {
    let row = sqlx::query(
        r#"INSERT INTO api_keys
               (name, prefix, last4, secret_hash, created_by_user_id, permissions, expires_at)
           VALUES ($1,$2,$3,$4,$5,$6,$7)
           RETURNING id::text AS id, created_at, updated_at"#,
    )
    .bind(input.name)
    .bind(input.prefix)
    .bind(input.last4)
    .bind(input.secret_hash)
    .bind(input.created_by_user_id)
    .bind(input.permissions)
    .bind(input.expires_at)
    .fetch_one(pool)
    .await?;

    Ok(ApiKey {
        id: row.try_get("id")?,
        name: input.name.to_string(),
        prefix: input.prefix.to_string(),
        last4: input.last4.to_string(),
        secret_hash: input.secret_hash.to_vec(),
        created_by_user_id: input.created_by_user_id.to_string(),
        permissions: input.permissions.to_vec(),
        expires_at: input.expires_at,
        revoked_at: None,
        revoked_reason: None,
        last_used_at: None,
        last_used_ip: None,
        created_at: row.try_get("created_at")?,
        updated_at: row.try_get("updated_at")?,
    })
}

/// Look up a key by its 16-char prefix (the API-key auth hot path).
pub async fn get_by_prefix(pool: &PgPool, prefix: &str) -> Result<ApiKey, ApiKeyError> {
    scan_one(pool, "prefix = $1", prefix).await
}

/// Look up a key by its UUID (as text). Used by the rotate action.
pub async fn get_by_id(pool: &PgPool, id: &str) -> Result<ApiKey, ApiKeyError> {
    scan_one(pool, "id::text = $1", id).await
}

async fn scan_one(pool: &PgPool, where_clause: &str, arg: &str) -> Result<ApiKey, ApiKeyError> {
    let sql = format!("SELECT {SELECT_COLS} FROM api_keys WHERE {where_clause}");
    match sqlx::query(&sql).bind(arg).fetch_optional(pool).await? {
        Some(row) => Ok(scan_key(&row)?),
        None => Err(ApiKeyError::NotFound),
    }
}

/// Mark a key revoked. Idempotent: no-op when already revoked (matches Go).
pub async fn revoke(pool: &PgPool, id: &str, reason: &str) -> Result<(), sqlx::Error> {
    sqlx::query(
        r#"UPDATE api_keys
              SET revoked_at = NOW(), revoked_reason = $2, updated_at = NOW()
            WHERE id::text = $1 AND revoked_at IS NULL"#,
    )
    .bind(id)
    .bind(reason)
    .execute(pool)
    .await?;
    Ok(())
}

/// Replace secret material in place. Preserves id/name/permissions/creator/
/// expiry/created_at. Errors [`ApiKeyError::NotFound`] when no un-revoked row
/// matches (mirrors Go `Rotate` returning `ErrAPIKeyNotFound` on 0 rows).
pub async fn rotate(
    pool: &PgPool,
    id: &str,
    new_prefix: &str,
    new_last4: &str,
    new_hash: &[u8],
) -> Result<(), ApiKeyError> {
    let res = sqlx::query(
        r#"UPDATE api_keys
              SET prefix = $2, last4 = $3, secret_hash = $4, updated_at = NOW()
            WHERE id::text = $1 AND revoked_at IS NULL"#,
    )
    .bind(id)
    .bind(new_prefix)
    .bind(new_last4)
    .bind(new_hash)
    .execute(pool)
    .await?;
    if res.rows_affected() == 0 {
        return Err(ApiKeyError::NotFound);
    }
    Ok(())
}

/// Record the most recent successful auth. Empty `ip` stores NULL (matches Go).
pub async fn update_last_used(pool: &PgPool, id: &str, ip: &str) -> Result<(), sqlx::Error> {
    let ip_arg: Option<&str> = if ip.is_empty() { None } else { Some(ip) };
    sqlx::query(
        r#"UPDATE api_keys
              SET last_used_at = NOW(), last_used_ip = $2
            WHERE id::text = $1"#,
    )
    .bind(id)
    .bind(ip_arg)
    .execute(pool)
    .await?;
    Ok(())
}

/// `app_users` status for the API-key auth creator check (Go `LoadUserStatus`).
/// Errors on a missing row (matches Go's `QueryRow.Scan`), which the middleware
/// maps to a 500.
pub struct UserStatus {
    pub active: bool,
    pub suspended: bool,
    pub deleted: bool,
}

pub async fn load_user_status(pool: &PgPool, user_id: &str) -> Result<UserStatus, sqlx::Error> {
    let row = sqlx::query("SELECT suspended_at, deleted_at FROM app_users WHERE id = $1")
        .bind(user_id)
        .fetch_one(pool)
        .await?;
    let suspended_at: Option<DateTime<Utc>> = row.try_get("suspended_at")?;
    let deleted_at: Option<DateTime<Utc>> = row.try_get("deleted_at")?;
    let suspended = suspended_at.is_some();
    let deleted = deleted_at.is_some();
    Ok(UserStatus {
        active: !suspended && !deleted,
        suspended,
        deleted,
    })
}

/// Query params for [`list`] (mirrors Go `store.APIKeyListQuery`).
pub struct ListQuery {
    pub q: String,
    pub include_revoked: bool,
    pub page: i64,
    pub page_size: i64,
}

/// A list row plus the creator's display name (Go `store.APIKeyListItem`).
pub struct ListItem {
    pub key: ApiKey,
    pub created_by_display_name: String,
}

/// Paginated key list. Clamps `page` (min 1) and `page_size` (default 20, cap
/// 100) internally exactly like the Go store; the handler still echoes the raw
/// requested page/pageSize in its envelope.
pub async fn list(pool: &PgPool, q: &ListQuery) -> Result<(Vec<ListItem>, i64), sqlx::Error> {
    let page = q.page.max(1);
    let mut size = if q.page_size <= 0 { 20 } else { q.page_size };
    if size > 100 {
        size = 100;
    }
    let offset = (page - 1) * size;

    // COUNT.
    let mut cb: QueryBuilder<Postgres> =
        QueryBuilder::new("SELECT COUNT(*) FROM api_keys k WHERE 1=1");
    if !q.include_revoked {
        cb.push(" AND k.revoked_at IS NULL");
    }
    if !q.q.is_empty() {
        cb.push(" AND (k.name ILIKE ");
        cb.push_bind(format!("%{}%", q.q));
        cb.push(" OR k.prefix ILIKE ");
        cb.push_bind(format!("%{}%", q.q));
        cb.push(")");
    }
    let total: i64 = cb.build_query_scalar().fetch_one(pool).await?;

    // Page.
    let mut lb: QueryBuilder<Postgres> = QueryBuilder::new(
        "SELECT k.id::text AS id, k.name, k.prefix, k.last4, k.secret_hash, k.created_by_user_id, \
         k.permissions, k.expires_at, k.revoked_at, k.revoked_reason, \
         k.last_used_at, k.last_used_ip::text AS last_used_ip, k.created_at, k.updated_at, \
         COALESCE(u.display_name, u.discord_username, '') AS created_by_display_name \
         FROM api_keys k LEFT JOIN app_users u ON u.id = k.created_by_user_id WHERE 1=1",
    );
    if !q.include_revoked {
        lb.push(" AND k.revoked_at IS NULL");
    }
    if !q.q.is_empty() {
        lb.push(" AND (k.name ILIKE ");
        lb.push_bind(format!("%{}%", q.q));
        lb.push(" OR k.prefix ILIKE ");
        lb.push_bind(format!("%{}%", q.q));
        lb.push(")");
    }
    lb.push(" ORDER BY k.created_at DESC LIMIT ");
    lb.push_bind(size);
    lb.push(" OFFSET ");
    lb.push_bind(offset);

    let rows = lb.build().fetch_all(pool).await?;
    let mut out = Vec::with_capacity(rows.len());
    for row in &rows {
        out.push(ListItem {
            key: scan_key(row)?,
            created_by_display_name: row.try_get("created_by_display_name")?,
        });
    }
    Ok((out, total))
}

/// The actor's resolved permission set, mirroring the Go RBAC
/// `Store.PermissionsForUser`: superadmin implies the full static catalog;
/// everyone else gets the distinct union of their roles' permission keys. Used
/// by the `api_keys.create` subset guard.
pub async fn permissions_for_user(pool: &PgPool, user_id: &str) -> Result<Vec<String>, sqlx::Error> {
    let roles = crate::identity::load_admin_role_keys(pool, user_id).await?;
    if roles.iter().any(|r| r == crate::rbac::ROLE_SUPERADMIN) {
        return Ok(crate::rbac::ALL_PERMISSIONS
            .iter()
            .map(|p| p.key.to_string())
            .collect());
    }
    if roles.is_empty() {
        return Ok(Vec::new());
    }
    let rows = sqlx::query(
        r#"SELECT DISTINCT rp.permission_key
             FROM admin_user_roles ur
             JOIN admin_role_permissions rp ON rp.role_id = ur.role_id
            WHERE ur.user_id = $1"#,
    )
    .bind(user_id)
    .fetch_all(pool)
    .await?;
    rows.into_iter()
        .map(|r| r.try_get::<String, _>("permission_key"))
        .collect()
}
