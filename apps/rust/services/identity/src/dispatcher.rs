//! `POST /api/app/admin/actions/{type}` — the admin mutation dispatcher.
//!
//! Mirrors the Go `internal/admin/actions/Dispatcher.ServeHTTP` flow (rate
//! limiting and idempotency are intentionally deferred): app-secret + session
//! gate → permission → reason → validate → dangerous confirmation → execute →
//! publish events → audit → respond. SQL effects live in
//! `rustlens_db::admin_actions`; this module owns validation, the HTTP contract,
//! and event publishing.

use std::collections::HashSet;
use std::sync::Arc;

use axum::Json;
use axum::body::Bytes;
use axum::extract::{Path, State};
use axum::http::{HeaderMap, StatusCode, header};
use axum::response::{IntoResponse, Response};
use serde::Deserialize;
use serde_json::{Value, json};

use rustlens_db::admin_actions::{self as acts, ActionError, AuditEntry};
use rustlens_db::rbac;
use rustlens_events::{self as ev, Event, Publisher};

use crate::appauth;
use crate::state::AppState;

type Shared = State<Arc<AppState>>;

struct Spec {
    permission: &'static str,
    requires_reason: bool,
    dangerous: bool,
    resource_type: &'static str,
}

fn spec(action_type: &str) -> Option<Spec> {
    let s = match action_type {
        "roles.create" | "roles.update" | "roles.delete" | "roles.permissions.set" => Spec {
            permission: rbac::ADMINS_GRANT,
            requires_reason: true,
            dangerous: false,
            resource_type: "role",
        },
        "users.roles.grant" | "users.roles.revoke" => Spec {
            permission: rbac::USERS_ROLES_ASSIGN,
            requires_reason: true,
            dangerous: false,
            resource_type: "user",
        },
        "users.suspend" => Spec {
            permission: rbac::USERS_SUSPEND,
            requires_reason: true,
            dangerous: false,
            resource_type: "user",
        },
        "users.unsuspend" => Spec {
            permission: rbac::USERS_SUSPEND,
            requires_reason: false,
            dangerous: false,
            resource_type: "user",
        },
        "users.force_logout" => Spec {
            permission: rbac::USERS_SECURITY_RESET,
            requires_reason: true,
            dangerous: false,
            resource_type: "user",
        },
        "users.delete" => Spec {
            permission: rbac::USERS_DELETE,
            requires_reason: true,
            dangerous: true,
            resource_type: "user",
        },
        _ => return None,
    };
    Some(s)
}

#[derive(Debug, Deserialize, Default)]
struct Body {
    #[serde(default)]
    params: Value,
    #[serde(default)]
    reason: String,
    #[serde(default, rename = "typedConfirmation")]
    typed_confirmation: String,
}

fn err(status: StatusCode, body: Value) -> Response {
    (status, Json(body)).into_response()
}

/// Validation outcome: the resource id / target / dangerous-confirmation value.
struct Validated {
    resource_id: String,
    target: Option<String>,
    confirmation_expected: String,
}

pub async fn dispatch(
    State(st): Shared,
    Path(action_type): Path<String>,
    headers: HeaderMap,
    raw: Bytes,
) -> Response {
    // 1. Resolve action.
    let Some(spec) = spec(&action_type) else {
        return err(StatusCode::NOT_FOUND, json!({ "error": "action_not_found" }));
    };

    // 2. Auth: app-secret + session header.
    if let Err(resp) = appauth::require_app_secret(&headers, &st.config.app_api_internal_secret) {
        return *resp;
    }
    let session = match appauth::require_session(&headers, &st).await {
        Ok(s) => s,
        Err(resp) => return *resp,
    };

    // 2a. Rate limit (per actor+action token bucket).
    if let Err(retry) = st.rate_limiter.allow(
        &session.user_id,
        &action_type,
        crate::ratelimit::DEFAULT_MUTATING_CAPACITY,
        crate::ratelimit::DEFAULT_MUTATING_REFILL,
    ) {
        let secs = (retry.as_secs_f64() as u64).max(1);
        return (
            StatusCode::TOO_MANY_REQUESTS,
            [(header::RETRY_AFTER, secs.to_string())],
            Json(json!({ "error": "rate_limited" })),
        )
            .into_response();
    }

    // 2b. Idempotency: replay a cached success when the key matches.
    let idem_key = headers
        .get("idempotency-key")
        .and_then(|v| v.to_str().ok())
        .filter(|s| !s.is_empty())
        .map(|k| crate::ratelimit::IdempotencyCache::key(&session.user_id, &action_type, k));
    if let Some(key) = idem_key.as_ref() {
        if let Some((status, body)) = st.idempotency.get(key) {
            return replay_response(status, body);
        }
    }

    // 3. Permission.
    match rustlens_db::admin::has_permission(&st.pool, &session.user_id, spec.permission).await {
        Ok(true) => {}
        Ok(false) => {
            return err(
                StatusCode::FORBIDDEN,
                json!({ "error": "permission_denied", "required": spec.permission }),
            );
        }
        Err(e) => {
            tracing::error!("dispatch: permission check failed: {e:#}");
            return err(
                StatusCode::INTERNAL_SERVER_ERROR,
                json!({ "error": "permission_check_failed" }),
            );
        }
    }

    // 4. Decode body (empty body is allowed → empty params).
    let mut body: Body = if raw.is_empty() {
        Body::default()
    } else {
        match serde_json::from_slice(&raw) {
            Ok(b) => b,
            Err(e) => {
                return err(
                    StatusCode::BAD_REQUEST,
                    json!({ "error": "invalid_json", "message": e.to_string() }),
                );
            }
        }
    };
    if body.params.is_null() {
        body.params = json!({});
    }

    // 5. Reason enforcement.
    if spec.requires_reason && body.reason.is_empty() {
        return err(StatusCode::BAD_REQUEST, json!({ "error": "reason_required" }));
    }

    // 6. Validate params.
    let validated = match validate(&action_type, &body.params) {
        Ok(v) => v,
        Err(msg) => {
            return err(
                StatusCode::BAD_REQUEST,
                json!({ "error": "validation_failed", "message": msg }),
            );
        }
    };

    // 7. Dangerous confirmation.
    if spec.dangerous && body.typed_confirmation != validated.confirmation_expected {
        return err(
            StatusCode::BAD_REQUEST,
            json!({ "error": "confirmation_mismatch", "expected": validated.confirmation_expected }),
        );
    }

    // 8. Execute (+ publish events).
    let exec = execute(&st, &action_type, &session.user_id, &body.params).await;

    // 8a. On-grant hook: fire-and-forget linked-roles push after a pro change.
    if let Ok(ok) = &exec {
        if let Some(uid) = ok.pro_push.clone() {
            if st.config.role_connection_enabled() {
                let st2 = st.clone();
                tokio::spawn(async move {
                    if let Err(e) = crate::role_connection::push(&st2, &uid).await {
                        tracing::warn!("pro-role linked-roles push failed for {uid}: {e:#}");
                    }
                });
            }
        }
    }

    // 9. Audit + respond (cache the success body for idempotency replay).
    let ua = headers
        .get(header::USER_AGENT)
        .and_then(|v| v.to_str().ok());
    let is_success = exec.is_ok();
    let (status, resp_body) =
        finalize(&st, &session, &spec, &action_type, &validated, &body, exec, ua).await;
    if let Some(key) = idem_key {
        if is_success && status == StatusCode::OK {
            st.idempotency
                .put(key, status.as_u16(), serde_json::to_vec(&resp_body).unwrap_or_default());
        }
    }
    (status, Json(resp_body)).into_response()
}

/// Build a replayed idempotent response with the `Idempotent-Replay` marker.
fn replay_response(status: u16, body: Vec<u8>) -> Response {
    axum::http::Response::builder()
        .status(status)
        .header(header::CONTENT_TYPE, "application/json")
        .header("Idempotent-Replay", "true")
        .body(axum::body::Body::from(body))
        .unwrap_or_else(|_| {
            (StatusCode::INTERNAL_SERVER_ERROR, "replay failed").into_response()
        })
}

/// Result of a successful execute: the response payload plus the (possibly
/// overridden) resource id / target user id for the audit row.
struct ExecOk {
    payload: Value,
    resource_id: String,
    target: Option<String>,
    /// When set, fire a fire-and-forget linked-roles metadata push for this user
    /// after responding (a `pro` grant/revoke). Mirrors Go's `OnProRoleChanged`.
    pro_push: Option<String>,
}

#[allow(clippy::too_many_arguments)]
async fn finalize(
    st: &AppState,
    session: &rustlens_session::SessionUser,
    spec: &Spec,
    action_type: &str,
    validated: &Validated,
    body: &Body,
    exec: Result<ExecOk, ActionError>,
    user_agent: Option<&str>,
) -> (StatusCode, Value) {
    let reason = if body.reason.is_empty() { None } else { Some(body.reason.as_str()) };
    match exec {
        Ok(ok) => {
            let entry = AuditEntry {
                action_type,
                actor_user_id: &session.user_id,
                actor_role_snapshot: &session.admin_roles,
                resource_type: spec.resource_type,
                resource_id: &ok.resource_id,
                target_user_id: ok.target.as_deref(),
                params: &body.params,
                reason,
                result: "success",
                error_message: None,
                user_agent,
            };
            let _ = acts::record_audit(&st.pool, &entry).await;
            (StatusCode::OK, json!({ "ok": true, "result": ok.payload }))
        }
        Err(ae) => {
            let (status, code, msg) = ae.parts();
            let entry = AuditEntry {
                action_type,
                actor_user_id: &session.user_id,
                actor_role_snapshot: &session.admin_roles,
                resource_type: spec.resource_type,
                resource_id: &validated.resource_id,
                target_user_id: validated.target.as_deref(),
                params: &body.params,
                reason,
                result: "error",
                error_message: Some(&msg),
                user_agent,
            };
            let _ = acts::record_audit(&st.pool, &entry).await;
            (
                StatusCode::from_u16(status).unwrap_or(StatusCode::INTERNAL_SERVER_ERROR),
                json!({ "error": code, "message": msg }),
            )
        }
    }
}

// ---------------------------------------------------------------------------
// Validation
// ---------------------------------------------------------------------------

fn valid_role_key(k: &str) -> bool {
    let b = k.as_bytes();
    if b.len() < 2 || b.len() > 64 {
        return false;
    }
    if !b[0].is_ascii_lowercase() {
        return false;
    }
    b[1..]
        .iter()
        .all(|c| c.is_ascii_lowercase() || c.is_ascii_digit() || *c == b'_')
}

fn validate_permissions(perms: &[String]) -> Result<(), String> {
    let mut seen = HashSet::new();
    for k in perms {
        if !rbac::is_valid_permission(k) {
            return Err(format!("unknown permission: {k}"));
        }
        if !seen.insert(k) {
            return Err(format!("duplicate permission: {k}"));
        }
    }
    Ok(())
}

fn from_params<T: for<'de> Deserialize<'de>>(params: &Value) -> Result<T, String> {
    serde_json::from_value(params.clone()).map_err(|e| e.to_string())
}

fn validate(action_type: &str, params: &Value) -> Result<Validated, String> {
    match action_type {
        "roles.create" => {
            #[derive(Deserialize)]
            struct P {
                #[serde(default)]
                key: String,
                #[serde(default)]
                name: String,
                #[serde(default)]
                description: String,
                #[serde(default)]
                permissions: Vec<String>,
            }
            let p: P = from_params(params)?;
            if !valid_role_key(&p.key) {
                return Err("key must match ^[a-z][a-z0-9_]{1,63}$".into());
            }
            if p.name.is_empty() || p.name.len() > 80 {
                return Err("name required, max 80 chars".into());
            }
            if p.description.len() > 500 {
                return Err("description too long".into());
            }
            validate_permissions(&p.permissions)?;
            Ok(Validated { resource_id: p.key, target: None, confirmation_expected: String::new() })
        }
        "roles.update" => {
            #[derive(Deserialize)]
            struct P {
                #[serde(default)]
                id: String,
                #[serde(default)]
                name: String,
                #[serde(default)]
                description: String,
            }
            let p: P = from_params(params)?;
            if p.id.is_empty() {
                return Err("id is required".into());
            }
            if p.name.is_empty() || p.name.len() > 80 {
                return Err("name required, max 80 chars".into());
            }
            if p.description.len() > 500 {
                return Err("description too long".into());
            }
            Ok(Validated { resource_id: p.id, target: None, confirmation_expected: String::new() })
        }
        "roles.delete" => {
            let id = require_string(params, "id")?;
            Ok(Validated { resource_id: id, target: None, confirmation_expected: String::new() })
        }
        "roles.permissions.set" => {
            #[derive(Deserialize)]
            struct P {
                #[serde(default)]
                id: String,
                #[serde(default)]
                permissions: Vec<String>,
            }
            let p: P = from_params(params)?;
            if p.id.is_empty() {
                return Err("id is required".into());
            }
            validate_permissions(&p.permissions)?;
            Ok(Validated { resource_id: p.id, target: None, confirmation_expected: String::new() })
        }
        "users.roles.grant" | "users.roles.revoke" => {
            #[derive(Deserialize)]
            struct P {
                #[serde(default, rename = "userId")]
                user_id: String,
                #[serde(default, rename = "roleKey")]
                role_key: String,
            }
            let p: P = from_params(params)?;
            if p.user_id.is_empty() {
                return Err("userId is required".into());
            }
            if p.role_key.is_empty() {
                return Err("roleKey is required".into());
            }
            Ok(Validated {
                resource_id: p.user_id.clone(),
                target: Some(p.user_id),
                confirmation_expected: String::new(),
            })
        }
        "users.suspend" | "users.delete" => {
            #[derive(Deserialize)]
            struct P {
                #[serde(default, rename = "userId")]
                user_id: String,
                #[serde(default)]
                reason: String,
            }
            let p: P = from_params(params)?;
            if p.user_id.is_empty() {
                return Err("userId is required".into());
            }
            if p.reason.len() > 500 {
                return Err("reason too long".into());
            }
            // users.delete is dangerous: confirmation must equal the user id.
            let confirmation_expected = if action_type == "users.delete" {
                p.user_id.clone()
            } else {
                String::new()
            };
            Ok(Validated {
                resource_id: p.user_id.clone(),
                target: Some(p.user_id),
                confirmation_expected,
            })
        }
        "users.unsuspend" | "users.force_logout" => {
            let user_id = require_string(params, "userId")?;
            Ok(Validated {
                resource_id: user_id.clone(),
                target: Some(user_id),
                confirmation_expected: String::new(),
            })
        }
        _ => Err("unknown action".into()),
    }
}

fn require_string(params: &Value, key: &str) -> Result<String, String> {
    match params.get(key).and_then(Value::as_str) {
        Some(s) if !s.is_empty() => Ok(s.to_string()),
        _ => Err(format!("{key} is required")),
    }
}

// ---------------------------------------------------------------------------
// Execute
// ---------------------------------------------------------------------------

async fn execute(
    st: &AppState,
    action_type: &str,
    actor_id: &str,
    params: &Value,
) -> Result<ExecOk, ActionError> {
    let pool = &st.pool;
    match action_type {
        "roles.create" => {
            let key = params.get("key").and_then(Value::as_str).unwrap_or_default();
            let name = params.get("name").and_then(Value::as_str).unwrap_or_default();
            let description = params.get("description").and_then(Value::as_str).unwrap_or_default();
            let perms = string_vec(params, "permissions");
            let new_id = acts::roles_create(pool, key, name, description, &perms).await?;
            Ok(ExecOk {
                payload: json!({ "id": new_id, "key": key }),
                resource_id: new_id,
                target: None,
                pro_push: None,
            })
        }
        "roles.update" => {
            let id = params.get("id").and_then(Value::as_str).unwrap_or_default();
            let name = params.get("name").and_then(Value::as_str).unwrap_or_default();
            let description = params.get("description").and_then(Value::as_str).unwrap_or_default();
            acts::roles_update(pool, id, name, description).await?;
            Ok(ExecOk { payload: json!({ "id": id }), resource_id: id.into(), target: None, pro_push: None })
        }
        "roles.delete" => {
            let id = params.get("id").and_then(Value::as_str).unwrap_or_default();
            acts::roles_delete(pool, id).await?;
            Ok(ExecOk { payload: json!({ "id": id }), resource_id: id.into(), target: None, pro_push: None })
        }
        "roles.permissions.set" => {
            let id = params.get("id").and_then(Value::as_str).unwrap_or_default();
            let perms = string_vec(params, "permissions");
            let affected = acts::roles_set_permissions(pool, id, &perms).await?;
            for uid in &affected {
                st.events
                    .publish_with_fanout(uid, Event::roles_changed(ev::REASON_ROLE_PERMS_UPDATED));
            }
            Ok(ExecOk {
                payload: json!({ "id": id, "count": perms.len() }),
                resource_id: id.into(),
                target: None,
                pro_push: None,
            })
        }
        "users.roles.grant" => {
            let (user_id, role_key) = user_role(params);
            acts::users_grant_role(pool, &user_id, &role_key, actor_id).await?;
            st.events.publish_with_fanout(&user_id, Event::roles_changed(ev::REASON_GRANT));
            let payload = json!({ "userId": &user_id, "roleKey": &role_key });
            Ok(user_payload(user_id, role_key == "pro", payload))
        }
        "users.roles.revoke" => {
            let (user_id, role_key) = user_role(params);
            acts::users_revoke_role(pool, &user_id, &role_key, actor_id).await?;
            st.events.publish_with_fanout(&user_id, Event::roles_changed(ev::REASON_REVOKE));
            let payload = json!({ "userId": &user_id, "roleKey": &role_key });
            Ok(user_payload(user_id, role_key == "pro", payload))
        }
        "users.suspend" => {
            let user_id = params_user(params);
            let reason = params.get("reason").and_then(Value::as_str).unwrap_or_default();
            acts::users_suspend(pool, &user_id, actor_id, reason).await?;
            st.events.publish_with_fanout(&user_id, Event::session_revoked(ev::REASON_SUSPENDED));
            Ok(user_payload(user_id.clone(), false, json!({ "userId": user_id })))
        }
        "users.unsuspend" => {
            let user_id = params_user(params);
            acts::users_unsuspend(pool, &user_id).await?;
            Ok(user_payload(user_id.clone(), false, json!({ "userId": user_id })))
        }
        "users.force_logout" => {
            let user_id = params_user(params);
            acts::users_force_logout(pool, &user_id, actor_id).await?;
            st.events.publish_with_fanout(&user_id, Event::session_revoked(ev::REASON_FORCE_LOGOUT));
            Ok(user_payload(user_id.clone(), false, json!({ "userId": user_id })))
        }
        "users.delete" => {
            let user_id = params_user(params);
            acts::users_delete(pool, &user_id, actor_id).await?;
            st.events.publish_with_fanout(&user_id, Event::session_revoked(ev::REASON_DELETED));
            Ok(user_payload(user_id.clone(), false, json!({ "userId": user_id, "deleted": true })))
        }
        _ => Err(ActionError::Validation("unknown action".into())),
    }
}

fn string_vec(params: &Value, key: &str) -> Vec<String> {
    params
        .get(key)
        .and_then(Value::as_array)
        .map(|a| a.iter().filter_map(|v| v.as_str().map(str::to_string)).collect())
        .unwrap_or_default()
}

fn params_user(params: &Value) -> String {
    params.get("userId").and_then(Value::as_str).unwrap_or_default().to_string()
}

fn user_role(params: &Value) -> (String, String) {
    (
        params_user(params),
        params.get("roleKey").and_then(Value::as_str).unwrap_or_default().to_string(),
    )
}

fn user_payload(user_id: String, pro: bool, payload: Value) -> ExecOk {
    ExecOk {
        payload,
        resource_id: user_id.clone(),
        pro_push: if pro { Some(user_id.clone()) } else { None },
        target: Some(user_id),
    }
}
