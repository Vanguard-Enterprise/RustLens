//! Admin-dashboard read route handlers (`/api/app/admin/*`).
//!
//! Each handler runs the full admin gate (app-secret → session → permission)
//! then serves the JSON shape the frontend admin dashboard expects. Query layer
//! lives in `rustlens_db::admin`.

use std::sync::Arc;

use axum::Json;
use axum::extract::{Path, RawQuery, State};
use axum::http::{HeaderMap, StatusCode};
use axum::response::{IntoResponse, Response};

use rustlens_db::admin::{self, AdminQueryError, AuditQuery, UsersQuery};
use rustlens_db::rbac;

use crate::appauth;
use crate::state::AppState;

type Shared = State<Arc<AppState>>;

fn error_json(status: StatusCode, msg: &str) -> Response {
    (status, Json(serde_json::json!({ "error": msg }))).into_response()
}

/// `GET /api/app/admin/permissions` — static permission catalog (requires
/// `roles.read`). Returned in Go catalog order.
pub async fn list_permissions(State(st): Shared, headers: HeaderMap) -> Response {
    if let Err(resp) = appauth::require_admin(&headers, &st, rbac::ROLES_READ).await {
        return *resp;
    }
    Json(serde_json::json!({ "items": rbac::ALL_PERMISSIONS })).into_response()
}

/// `GET /api/app/admin/roles` — admin roles with member counts + permissions
/// (requires `roles.read`).
pub async fn list_roles(State(st): Shared, headers: HeaderMap) -> Response {
    if let Err(resp) = appauth::require_admin(&headers, &st, rbac::ROLES_READ).await {
        return *resp;
    }
    match admin::list_admin_roles(&st.pool).await {
        Ok(items) => Json(serde_json::json!({ "items": items })).into_response(),
        Err(e) => {
            tracing::error!("admin list_roles: {e:#}");
            error_json(StatusCode::INTERNAL_SERVER_ERROR, "failed to list roles")
        }
    }
}

/// `GET /api/app/admin/users` — non-admin users by default (requires
/// `users.read`).
pub async fn list_users(State(st): Shared, headers: HeaderMap, RawQuery(raw): RawQuery) -> Response {
    if let Err(resp) = appauth::require_admin(&headers, &st, rbac::USERS_READ).await {
        return *resp;
    }
    let q = parse_users_query(raw.as_deref());
    match admin::list_admin_users(&st.pool, &q).await {
        Ok(page) => Json(page).into_response(),
        Err(e) => users_error(e, "list_users"),
    }
}

/// `GET /api/app/admin/administrators` — all users incl. admins (requires
/// `users.read`).
pub async fn list_administrators(
    State(st): Shared,
    headers: HeaderMap,
    RawQuery(raw): RawQuery,
) -> Response {
    if let Err(resp) = appauth::require_admin(&headers, &st, rbac::USERS_READ).await {
        return *resp;
    }
    let q = parse_users_query(raw.as_deref());
    match admin::list_administrators(&st.pool, &q).await {
        Ok(page) => Json(page).into_response(),
        Err(e) => users_error(e, "list_administrators"),
    }
}

/// `GET /api/app/admin/audit` — cursor-paginated audit log (requires
/// `audit.read`).
pub async fn list_audit(State(st): Shared, headers: HeaderMap, RawQuery(raw): RawQuery) -> Response {
    if let Err(resp) = appauth::require_admin(&headers, &st, rbac::AUDIT_READ).await {
        return *resp;
    }
    let q = parse_audit_query(raw.as_deref());
    match admin::list_admin_audit(&st.pool, &q).await {
        Ok(page) => Json(page).into_response(),
        Err(AdminQueryError::BadRequest(msg)) => error_json(StatusCode::BAD_REQUEST, &msg),
        Err(AdminQueryError::Db(e)) => {
            tracing::error!("admin list_audit: {e:#}");
            error_json(StatusCode::INTERNAL_SERVER_ERROR, "internal server error")
        }
    }
}

/// `GET /api/app/admin/users/{id}` — user-detail aggregate (requires `users.read`).
pub async fn get_user(State(st): Shared, headers: HeaderMap, Path(id): Path<String>) -> Response {
    if let Err(resp) = appauth::require_admin(&headers, &st, rbac::USERS_READ).await {
        return *resp;
    }
    if id.is_empty() {
        return error_json(StatusCode::BAD_REQUEST, "missing id");
    }
    match admin::get_admin_user(&st.pool, &id).await {
        Ok(Some(detail)) => Json(detail).into_response(),
        Ok(None) => error_json(StatusCode::NOT_FOUND, "not found"),
        Err(e) => {
            tracing::error!("admin get_user: {e:#}");
            error_json(StatusCode::INTERNAL_SERVER_ERROR, "internal server error")
        }
    }
}

/// `GET /api/app/admin/users/{id}/audit` — per-user audit (requires
/// `users.audit.read`). `limit` default 50 (cap 500), `before` RFC3339 cursor.
pub async fn get_user_audit(
    State(st): Shared,
    headers: HeaderMap,
    Path(id): Path<String>,
    RawQuery(raw): RawQuery,
) -> Response {
    if let Err(resp) = appauth::require_admin(&headers, &st, rbac::USERS_AUDIT_READ).await {
        return *resp;
    }
    if id.is_empty() {
        return error_json(StatusCode::BAD_REQUEST, "missing id");
    }
    let (limit, before) = parse_user_audit_query(raw.as_deref());
    let items = admin::fetch_user_audit(&st.pool, &id, limit, before.as_deref()).await;
    Json(serde_json::json!({ "items": items })).into_response()
}

/// `GET /api/app/admin/audit/{id}` — single hydrated audit event (requires
/// `audit.read`).
pub async fn get_audit_event(
    State(st): Shared,
    headers: HeaderMap,
    Path(id): Path<String>,
) -> Response {
    if let Err(resp) = appauth::require_admin(&headers, &st, rbac::AUDIT_READ).await {
        return *resp;
    }
    if id.is_empty() {
        return error_json(StatusCode::BAD_REQUEST, "missing id");
    }
    match admin::get_admin_audit_event(&st.pool, &id).await {
        Ok(Some(item)) => Json(item).into_response(),
        Ok(None) => error_json(StatusCode::NOT_FOUND, "not found"),
        Err(e) => {
            tracing::error!("admin get_audit_event: {e:#}");
            error_json(StatusCode::INTERNAL_SERVER_ERROR, "internal server error")
        }
    }
}

/// `GET /api/app/admin/audit/event-types` — distinct audit types (requires
/// `audit.read`).
pub async fn list_audit_event_types(State(st): Shared, headers: HeaderMap) -> Response {
    if let Err(resp) = appauth::require_admin(&headers, &st, rbac::AUDIT_READ).await {
        return *resp;
    }
    match admin::list_audit_event_types(&st.pool).await {
        Ok(types) => Json(serde_json::json!({ "types": types })).into_response(),
        Err(e) => {
            tracing::error!("admin list_audit_event_types: {e:#}");
            error_json(StatusCode::INTERNAL_SERVER_ERROR, "internal server error")
        }
    }
}

fn parse_user_audit_query(raw: Option<&str>) -> (i32, Option<String>) {
    let p = pairs(raw);
    let mut limit = 50;
    if let Some(v) = first(&p, "limit").filter(|s| !s.is_empty()) {
        parse_limit(v, &mut limit);
    }
    if !(1..=500).contains(&limit) {
        limit = 50;
    }
    let before = first(&p, "before").map(str::to_string);
    (limit, before)
}

fn users_error(e: AdminQueryError, ctx: &str) -> Response {
    match e {
        AdminQueryError::BadRequest(msg) => error_json(StatusCode::BAD_REQUEST, &msg),
        AdminQueryError::Db(err) => {
            tracing::error!("admin {ctx}: {err:#}");
            error_json(StatusCode::INTERNAL_SERVER_ERROR, "internal server error")
        }
    }
}

/// Parse digits-only like the Go `parseLimit`: leaves `out` unchanged on any
/// non-digit or a value over 10000.
fn parse_limit(s: &str, out: &mut i32) {
    let mut n: i64 = 0;
    for c in s.chars() {
        if !c.is_ascii_digit() {
            return;
        }
        n = n * 10 + (c as i64 - '0' as i64);
        if n > 10000 {
            return;
        }
    }
    *out = n as i32;
}

fn pairs(raw: Option<&str>) -> Vec<(String, String)> {
    raw.map(|q| url::form_urlencoded::parse(q.as_bytes()).into_owned().collect())
        .unwrap_or_default()
}

/// First value for `key` (matches Go `url.Values.Get`).
fn first<'a>(pairs: &'a [(String, String)], key: &str) -> Option<&'a str> {
    pairs.iter().find(|(k, _)| k == key).map(|(_, v)| v.as_str())
}

fn parse_users_query(raw: Option<&str>) -> UsersQuery {
    let p = pairs(raw);
    let opt = |k: &str| first(&p, k).map(str::to_string);

    let mut page = 1;
    if let Some(v) = first(&p, "page").filter(|s| !s.is_empty()) {
        parse_limit(v, &mut page);
        if page < 1 {
            page = 1;
        }
    }
    let mut page_size = 50;
    if let Some(v) = first(&p, "pageSize").filter(|s| !s.is_empty()) {
        parse_limit(v, &mut page_size);
    }
    if !(1..=200).contains(&page_size) {
        page_size = 50;
    }

    UsersQuery {
        q: opt("q"),
        role: p
            .iter()
            .filter(|(k, _)| k == "role")
            .map(|(_, v)| v.clone())
            .collect(),
        status: opt("status"),
        discord: opt("discord"),
        steam: opt("steam"),
        user_app: opt("userApp"),
        entitlement: opt("entitlement"),
        sort: opt("sort"),
        page,
        page_size,
        joined_since: opt("joinedSince"),
        joined_before: opt("joinedBefore"),
    }
}

fn parse_audit_query(raw: Option<&str>) -> AuditQuery {
    let p = pairs(raw);
    let opt = |k: &str| first(&p, k).map(str::to_string);

    let mut page_size = 50;
    if let Some(v) = first(&p, "pageSize").filter(|s| !s.is_empty()) {
        parse_limit(v, &mut page_size);
    }
    // Backward-compat: ?limit= alias only when pageSize wasn't effective.
    if page_size == 50 {
        if let Some(v) = first(&p, "limit").filter(|s| !s.is_empty()) {
            parse_limit(v, &mut page_size);
        }
    }
    if page_size <= 0 || page_size > 200 {
        page_size = 50;
    }

    AuditQuery {
        target_user_id: opt("targetUserId"),
        actor_user_id: opt("actorUserId"),
        event_type: p
            .iter()
            .filter(|(k, _)| k == "eventType")
            .map(|(_, v)| v.clone())
            .collect(),
        result: opt("result"),
        since: opt("since"),
        before: opt("before"),
        page_size,
    }
}
