//! Admin mutation actions (the `Execute` side of the Go action dispatcher) and
//! the audit-event writer. The HTTP dispatcher (validation, permission, reason,
//! confirmation, response envelope) and event publishing live in the identity
//! service; this module owns the SQL effects and returns the data the service
//! needs to publish events and write audit rows.

use sqlx::{PgPool, Row};

use crate::admin::has_permission;
use crate::rbac;

/// Execution error from an action, mapped to HTTP status + code exactly like the
/// Go `mapExecError` sentinels. The `detail` is appended to the code in the
/// `message` field, matching Go's wrapped `fmt.Errorf("%w: ...")` form.
#[derive(Debug, thiserror::Error)]
pub enum ActionError {
    #[error("not_found: {0}")]
    NotFound(String),
    #[error("conflict: {0}")]
    Conflict(String),
    #[error("permission_denied: {0}")]
    PermissionDenied(String),
    #[error("validation_failed: {0}")]
    Validation(String),
    #[error(transparent)]
    Db(#[from] sqlx::Error),
}

impl ActionError {
    /// `(http_status, error_code, message)` for the JSON error response, matching
    /// the Go dispatcher.
    pub fn parts(&self) -> (u16, &'static str, String) {
        match self {
            ActionError::NotFound(_) => (404, "not_found", self.to_string()),
            ActionError::Conflict(_) => (409, "conflict", self.to_string()),
            ActionError::PermissionDenied(_) => (403, "permission_denied", self.to_string()),
            ActionError::Validation(_) => (400, "validation_failed", self.to_string()),
            ActionError::Db(e) => (500, "execution_failed", e.to_string()),
        }
    }
}

// ---------------------------------------------------------------------------
// Guards
// ---------------------------------------------------------------------------

/// Reject acting on one's own account (Go `GuardActingOnSelf`).
pub fn guard_acting_on_self(actor_id: &str, target_id: &str) -> Result<(), ActionError> {
    if !actor_id.is_empty() && actor_id == target_id {
        return Err(ActionError::Conflict(
            "cannot perform this action on your own account".into(),
        ));
    }
    Ok(())
}

/// Reject when the target is the last superadmin (Go `GuardLastSuperadminByUser`).
pub async fn guard_last_superadmin(pool: &PgPool, user_id: &str) -> Result<(), ActionError> {
    let has: bool = sqlx::query_scalar(
        r#"SELECT EXISTS(
               SELECT 1 FROM admin_user_roles aur
               JOIN admin_roles r ON r.id = aur.role_id
               WHERE aur.user_id = $1 AND r.key = 'superadmin'
           )"#,
    )
    .bind(user_id)
    .fetch_one(pool)
    .await?;
    if !has {
        return Ok(());
    }
    let total: i64 = sqlx::query_scalar(
        r#"SELECT COUNT(DISTINCT aur.user_id)
           FROM admin_user_roles aur
           JOIN admin_roles r ON r.id = aur.role_id
           WHERE r.key = 'superadmin'"#,
    )
    .fetch_one(pool)
    .await?;
    if total <= 1 {
        return Err(ActionError::Conflict(
            "cannot perform this action on the last superadmin".into(),
        ));
    }
    Ok(())
}

/// `ErrNotFound` if the role is missing, `ErrPermissionDenied` if it is a system
/// role (Go `AssertNotSystemRole`). `role_id` is a UUID string.
pub async fn assert_not_system_role(pool: &PgPool, role_id: &str) -> Result<(), ActionError> {
    let is_system: Option<bool> =
        sqlx::query_scalar("SELECT is_system FROM admin_roles WHERE id::text = $1")
            .bind(role_id)
            .fetch_optional(pool)
            .await?;
    match is_system {
        None => Err(ActionError::NotFound(format!("role {role_id}"))),
        Some(true) => Err(ActionError::PermissionDenied("cannot modify system role".into())),
        Some(false) => Ok(()),
    }
}

// ---------------------------------------------------------------------------
// roles.*
// ---------------------------------------------------------------------------

/// Insert a custom role + its permissions. Returns the new role's UUID (as text).
pub async fn roles_create(
    pool: &PgPool,
    key: &str,
    name: &str,
    description: &str,
    permissions: &[String],
) -> Result<String, ActionError> {
    if rbac::SYSTEM_ROLE_KEYS.contains(&key) {
        return Err(ActionError::Conflict(format!(
            "key {key} reserved for system role"
        )));
    }
    let mut tx = pool.begin().await?;
    let new_id: String = match sqlx::query_scalar(
        "INSERT INTO admin_roles (key, name, description, is_system) VALUES ($1,$2,$3,FALSE) RETURNING id::text",
    )
    .bind(key)
    .bind(name)
    .bind(description)
    .fetch_one(&mut *tx)
    .await
    {
        Ok(id) => id,
        Err(sqlx::Error::Database(e)) if e.code().as_deref() == Some("23505") => {
            return Err(ActionError::Conflict(format!("key {key} already exists")));
        }
        Err(e) => return Err(e.into()),
    };
    for k in permissions {
        sqlx::query("INSERT INTO admin_role_permissions (role_id, permission_key) VALUES ($1::uuid, $2)")
            .bind(&new_id)
            .bind(k)
            .execute(&mut *tx)
            .await?;
    }
    tx.commit().await?;
    Ok(new_id)
}

/// Update a non-system role's name/description.
pub async fn roles_update(
    pool: &PgPool,
    id: &str,
    name: &str,
    description: &str,
) -> Result<(), ActionError> {
    assert_not_system_role(pool, id).await?;
    let res = sqlx::query(
        "UPDATE admin_roles SET name=$2, description=$3, updated_at=NOW() WHERE id::text=$1 AND is_system=FALSE",
    )
    .bind(id)
    .bind(name)
    .bind(description)
    .execute(pool)
    .await?;
    if res.rows_affected() == 0 {
        return Err(ActionError::NotFound(format!("role {id}")));
    }
    Ok(())
}

/// Delete a non-system role (cascades to permissions + grants via FK).
pub async fn roles_delete(pool: &PgPool, id: &str) -> Result<(), ActionError> {
    assert_not_system_role(pool, id).await?;
    let res = sqlx::query("DELETE FROM admin_roles WHERE id::text=$1 AND is_system=FALSE")
        .bind(id)
        .execute(pool)
        .await?;
    if res.rows_affected() == 0 {
        return Err(ActionError::NotFound(format!("role {id}")));
    }
    Ok(())
}

/// Replace a non-system role's permission set. Returns the user IDs holding the
/// role (for the caller to publish `roles.changed`).
pub async fn roles_set_permissions(
    pool: &PgPool,
    id: &str,
    permissions: &[String],
) -> Result<Vec<String>, ActionError> {
    assert_not_system_role(pool, id).await?;
    let mut tx = pool.begin().await?;
    sqlx::query("DELETE FROM admin_role_permissions WHERE role_id::text=$1")
        .bind(id)
        .execute(&mut *tx)
        .await?;
    for k in permissions {
        sqlx::query("INSERT INTO admin_role_permissions (role_id, permission_key) VALUES ($1::uuid, $2)")
            .bind(id)
            .bind(k)
            .execute(&mut *tx)
            .await?;
    }
    tx.commit().await?;

    // Best-effort: affected users for event fan-out (errors swallowed like Go).
    let users: Vec<String> = sqlx::query("SELECT user_id FROM admin_user_roles WHERE role_id::text = $1")
        .bind(id)
        .fetch_all(pool)
        .await
        .map(|rows| {
            rows.into_iter()
                .filter_map(|r| r.try_get::<String, _>("user_id").ok())
                .collect()
        })
        .unwrap_or_default();
    Ok(users)
}

// ---------------------------------------------------------------------------
// users.roles.*
// ---------------------------------------------------------------------------

const ENTITLEMENT_PRO: &str = "pro";

fn opt_actor(actor_id: &str) -> Option<&str> {
    if actor_id.is_empty() {
        None
    } else {
        Some(actor_id)
    }
}

/// Grant a role or the `pro` entitlement to a user. Superadmin grants require the
/// actor to hold `admins.grant`.
pub async fn users_grant_role(
    pool: &PgPool,
    user_id: &str,
    role_key: &str,
    actor_id: &str,
) -> Result<(), ActionError> {
    if role_key == ENTITLEMENT_PRO {
        sqlx::query(
            r#"INSERT INTO user_entitlements (user_id, key, granted_by)
               VALUES ($1, $2, $3)
               ON CONFLICT (user_id, key) DO UPDATE
               SET granted_by = EXCLUDED.granted_by, granted_at = NOW(), expires_at = NULL"#,
        )
        .bind(user_id)
        .bind(role_key)
        .bind(opt_actor(actor_id))
        .execute(pool)
        .await?;
        return Ok(());
    }

    let role_id: Option<String> = sqlx::query_scalar("SELECT id::text FROM admin_roles WHERE key=$1")
        .bind(role_key)
        .fetch_optional(pool)
        .await?;
    let Some(role_id) = role_id else {
        return Err(ActionError::NotFound(format!("role {role_key}")));
    };

    if role_key == rbac::ROLE_SUPERADMIN && !has_permission(pool, actor_id, rbac::ADMINS_GRANT).await? {
        return Err(ActionError::PermissionDenied(format!(
            "grant of superadmin requires {}",
            rbac::ADMINS_GRANT
        )));
    }

    sqlx::query(
        "INSERT INTO admin_user_roles (user_id, role_id, granted_by) VALUES ($1, $2::uuid, $3) ON CONFLICT DO NOTHING",
    )
    .bind(user_id)
    .bind(&role_id)
    .bind(opt_actor(actor_id))
    .execute(pool)
    .await?;
    Ok(())
}

/// Revoke a role or the `pro` entitlement. Superadmin revokes require
/// `admins.grant` and refuse to remove the last superadmin.
pub async fn users_revoke_role(
    pool: &PgPool,
    user_id: &str,
    role_key: &str,
    actor_id: &str,
) -> Result<(), ActionError> {
    if role_key == ENTITLEMENT_PRO {
        sqlx::query("DELETE FROM user_entitlements WHERE user_id = $1 AND key = $2")
            .bind(user_id)
            .bind(role_key)
            .execute(pool)
            .await?;
        return Ok(());
    }

    let role_id: Option<String> = sqlx::query_scalar("SELECT id::text FROM admin_roles WHERE key=$1")
        .bind(role_key)
        .fetch_optional(pool)
        .await?;
    let Some(role_id) = role_id else {
        return Err(ActionError::NotFound(format!("role {role_key}")));
    };

    if role_key == rbac::ROLE_SUPERADMIN {
        if !has_permission(pool, actor_id, rbac::ADMINS_GRANT).await? {
            return Err(ActionError::PermissionDenied(format!(
                "revoke of superadmin requires {}",
                rbac::ADMINS_GRANT
            )));
        }
        let count: i64 = sqlx::query_scalar(
            "SELECT COUNT(*) FROM admin_user_roles aur JOIN admin_roles r ON r.id = aur.role_id WHERE r.key = $1",
        )
        .bind(rbac::ROLE_SUPERADMIN)
        .fetch_one(pool)
        .await?;
        if count <= 1 {
            return Err(ActionError::Conflict("cannot revoke the last superadmin".into()));
        }
    }

    sqlx::query("DELETE FROM admin_user_roles WHERE user_id=$1 AND role_id=$2::uuid")
        .bind(user_id)
        .bind(&role_id)
        .execute(pool)
        .await?;
    Ok(())
}

// ---------------------------------------------------------------------------
// users.suspend / unsuspend / force_logout / delete
// ---------------------------------------------------------------------------

/// Suspend a user and bump their session epoch. `reason` is the dispatcher's
/// required reason.
pub async fn users_suspend(
    pool: &PgPool,
    user_id: &str,
    actor_id: &str,
    reason: &str,
) -> Result<(), ActionError> {
    guard_acting_on_self(actor_id, user_id)?;
    guard_last_superadmin(pool, user_id).await?;

    let mut tx = pool.begin().await?;
    let updated: Option<String> = sqlx::query_scalar(
        r#"UPDATE app_users
              SET suspended_at = now(), suspended_by = $2, suspend_reason = $3, updated_at = now()
            WHERE id = $1 AND suspended_at IS NULL AND deleted_at IS NULL
            RETURNING id"#,
    )
    .bind(user_id)
    .bind(actor_id)
    .bind(if reason.is_empty() { None } else { Some(reason) })
    .fetch_optional(&mut *tx)
    .await?;
    if updated.is_none() {
        return Err(ActionError::Conflict(
            "user missing, already suspended, or deleted".into(),
        ));
    }
    sqlx::query("UPDATE app_users SET session_epoch = session_epoch + 1 WHERE id = $1")
        .bind(user_id)
        .execute(&mut *tx)
        .await?;
    tx.commit().await?;
    Ok(())
}

/// Clear a user's suspension. No event (matches Go).
pub async fn users_unsuspend(pool: &PgPool, user_id: &str) -> Result<(), ActionError> {
    let updated: Option<String> = sqlx::query_scalar(
        r#"UPDATE app_users
              SET suspended_at = NULL, suspended_by = NULL, suspend_reason = NULL, updated_at = now()
            WHERE id = $1 AND suspended_at IS NOT NULL
            RETURNING id"#,
    )
    .bind(user_id)
    .fetch_optional(pool)
    .await?;
    if updated.is_none() {
        return Err(ActionError::Conflict("user missing or not suspended".into()));
    }
    Ok(())
}

/// Bump a user's session epoch to invalidate all their sessions.
pub async fn users_force_logout(
    pool: &PgPool,
    user_id: &str,
    actor_id: &str,
) -> Result<(), ActionError> {
    guard_acting_on_self(actor_id, user_id)?;
    let updated: Option<String> = sqlx::query_scalar(
        "UPDATE app_users SET session_epoch = session_epoch + 1, updated_at = now() WHERE id = $1 RETURNING id",
    )
    .bind(user_id)
    .fetch_optional(pool)
    .await?;
    if updated.is_none() {
        return Err(ActionError::NotFound("user not found".into()));
    }
    Ok(())
}

/// Hard-delete a user (cascades via FK).
pub async fn users_delete(
    pool: &PgPool,
    user_id: &str,
    actor_id: &str,
) -> Result<(), ActionError> {
    guard_acting_on_self(actor_id, user_id)?;
    guard_last_superadmin(pool, user_id).await?;

    let exists: Option<String> = sqlx::query_scalar("SELECT id FROM app_users WHERE id=$1")
        .bind(user_id)
        .fetch_optional(pool)
        .await?;
    if exists.is_none() {
        return Err(ActionError::NotFound(format!("user {user_id}")));
    }
    sqlx::query("DELETE FROM app_users WHERE id=$1")
        .bind(user_id)
        .execute(pool)
        .await?;
    Ok(())
}

// ---------------------------------------------------------------------------
// Audit
// ---------------------------------------------------------------------------

/// An audit row to write (mirrors the Go `AuditEntryInput` fields this service
/// populates). `via_api_key_*` are set when the actor authenticated with an
/// admin API key rather than a session (Go `APIKeyAuthFromContext`).
pub struct AuditEntry<'a> {
    pub action_type: &'a str,
    pub actor_user_id: &'a str,
    pub actor_role_snapshot: &'a [String],
    pub resource_type: &'a str,
    pub resource_id: &'a str,
    pub target_user_id: Option<&'a str>,
    pub params: &'a serde_json::Value,
    pub reason: Option<&'a str>,
    pub result: &'a str,
    pub error_message: Option<&'a str>,
    pub user_agent: Option<&'a str>,
    pub via_api_key_id: Option<&'a str>,
    pub via_api_key_name: Option<&'a str>,
}

fn null_if_empty(s: &str) -> Option<&str> {
    if s.is_empty() {
        None
    } else {
        Some(s)
    }
}

/// Insert one `audit_events` row. Mirrors the Go `audit.Store.Record`.
pub async fn record_audit(pool: &PgPool, e: &AuditEntry<'_>) -> Result<(), sqlx::Error> {
    sqlx::query(
        r#"INSERT INTO audit_events (
               action_type, actor_user_id, actor_role_snapshot,
               resource_type, resource_id, target_user_id,
               params, reason, result, error_message, ip_address, user_agent,
               via_api_key_id, via_api_key_name
           ) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,NULL,$11,$12::uuid,$13)"#,
    )
    .bind(e.action_type)
    .bind(e.actor_user_id)
    .bind(e.actor_role_snapshot)
    .bind(null_if_empty(e.resource_type))
    .bind(null_if_empty(e.resource_id))
    .bind(e.target_user_id)
    .bind(e.params)
    .bind(e.reason)
    .bind(e.result)
    .bind(e.error_message)
    .bind(e.user_agent)
    .bind(e.via_api_key_id)
    .bind(e.via_api_key_name)
    .execute(pool)
    .await?;
    Ok(())
}
