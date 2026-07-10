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
use rustlens_db::{api_keys, rbac};
use rustlens_events::{self as ev, Event, Publisher};
use rustlens_httpauth::{AdminActor, api_key};

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
        "api_keys.create" | "api_keys.rotate" | "api_keys.revoke" => Spec {
            permission: rbac::API_KEYS_MANAGE,
            requires_reason: true,
            dangerous: false,
            resource_type: "api_key",
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

    // 2. Auth: admin API key OR app-secret + session header. An API-key header
    //    short-circuits the session gate (matching the Go global middleware).
    let actor = match appauth::authenticate_admin(&headers, &st).await {
        Ok(a) => a,
        Err(resp) => return *resp,
    };

    // 2a. Rate limit (per actor+action token bucket).
    if let Err(retry) = st.rate_limiter.allow(
        &actor.user_id,
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
        .map(|k| crate::ratelimit::IdempotencyCache::key(&actor.user_id, &action_type, k));
    if let Some(key) = idem_key.as_ref() {
        if let Some((status, body)) = st.idempotency.get(key) {
            return replay_response(status, body);
        }
    }

    // 3. Permission. For an API-key actor the effective set is the intersection
    //    of the key snapshot and the creator's current role permissions, so a
    //    key missing the permission denies here just like Go's resolver.
    if let Some(k) = &actor.api_key {
        if !k.permissions.iter().any(|p| p == spec.permission) {
            return err(
                StatusCode::FORBIDDEN,
                json!({ "error": "permission_denied", "required": spec.permission }),
            );
        }
    }
    match rustlens_db::admin::has_permission(&st.pool, &actor.user_id, spec.permission).await {
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
    let exec = execute(&st, &action_type, &actor, &body.params).await;

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
    let client_ip = crate::appauth::client_ip(&headers);
    let is_success = exec.is_ok();
    let (status, resp_body) = finalize(
        &st, &actor, &spec, &action_type, &validated, &body, exec, ua, client_ip.as_deref(),
    )
    .await;
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
    actor: &AdminActor,
    spec: &Spec,
    action_type: &str,
    validated: &Validated,
    body: &Body,
    exec: Result<ExecOk, ActionError>,
    user_agent: Option<&str>,
    ip: Option<&str>,
) -> (StatusCode, Value) {
    let reason = if body.reason.is_empty() { None } else { Some(body.reason.as_str()) };
    let (via_key_id, via_key_name) = match &actor.api_key {
        Some(k) => (Some(k.key_id.as_str()), Some(k.key_name.as_str())),
        None => (None, None),
    };
    match exec {
        Ok(ok) => {
            let entry = AuditEntry {
                action_type,
                actor_user_id: &actor.user_id,
                actor_role_snapshot: &actor.admin_roles,
                resource_type: spec.resource_type,
                resource_id: &ok.resource_id,
                target_user_id: ok.target.as_deref(),
                params: &body.params,
                reason,
                result: "success",
                error_message: None,
                ip,
                user_agent,
                via_api_key_id: via_key_id,
                via_api_key_name: via_key_name,
            };
            let _ = acts::record_audit(&st.pool, &entry).await;
            (StatusCode::OK, json!({ "ok": true, "result": ok.payload }))
        }
        Err(ae) => {
            let (status, code, msg) = ae.parts();
            let entry = AuditEntry {
                action_type,
                actor_user_id: &actor.user_id,
                actor_role_snapshot: &actor.admin_roles,
                resource_type: spec.resource_type,
                resource_id: &validated.resource_id,
                target_user_id: validated.target.as_deref(),
                params: &body.params,
                reason,
                result: "error",
                error_message: Some(&msg),
                ip,
                user_agent,
                via_api_key_id: via_key_id,
                via_api_key_name: via_key_name,
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
        "api_keys.create" => {
            #[derive(Deserialize)]
            struct P {
                #[serde(default)]
                name: String,
                #[serde(default)]
                permissions: Vec<String>,
                #[serde(default, rename = "expiresAt")]
                expires_at: Option<String>,
            }
            let p: P = from_params(params)?;
            if p.name.is_empty() {
                return Err("name is required".into());
            }
            if p.name.len() > 64 {
                return Err("name must be <= 64 chars".into());
            }
            for perm in &p.permissions {
                if !rbac::is_valid_permission(perm) {
                    return Err(format!("unknown permission {perm:?}"));
                }
            }
            if let Some(exp) = p.expires_at.as_deref().filter(|s| !s.is_empty()) {
                if chrono::DateTime::parse_from_rfc3339(exp).is_err() {
                    return Err(format!("expiresAt must be RFC3339: {exp}"));
                }
            }
            // ResourceID is filled from the created key id at execute time (Go
            // returns an empty ValidatedParams here).
            Ok(Validated {
                resource_id: String::new(),
                target: None,
                confirmation_expected: String::new(),
            })
        }
        "api_keys.rotate" | "api_keys.revoke" => {
            let key_id = require_string(params, "keyId")?;
            Ok(Validated {
                resource_id: key_id,
                target: None,
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
    actor: &AdminActor,
    params: &Value,
) -> Result<ExecOk, ActionError> {
    let pool = &st.pool;
    let actor_id = actor.user_id.as_str();
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
        "api_keys.create" => {
            let name = params.get("name").and_then(Value::as_str).unwrap_or_default();
            let perms = string_vec(params, "permissions");
            // Subset guard: an actor cannot mint a key with a permission it does
            // not currently hold. For an API-key actor the effective set is the
            // key snapshot intersected with the creator's live role permissions.
            let mut actor_perms = api_keys::permissions_for_user(pool, actor_id).await?;
            if let Some(k) = &actor.api_key {
                let snap: HashSet<&str> = k.permissions.iter().map(String::as_str).collect();
                actor_perms.retain(|p| snap.contains(p.as_str()));
            }
            assert_permissions_subset(&perms, &actor_perms)?;

            let secret = api_key::generate_secret();
            let hash = api_key::hash_secret(&secret.secret);
            let created = api_keys::create(
                pool,
                api_keys::ApiKeyCreate {
                    name,
                    prefix: &secret.prefix,
                    last4: &secret.last4,
                    secret_hash: &hash,
                    created_by_user_id: actor_id,
                    permissions: &perms,
                    expires_at: parse_expires(params),
                },
            )
            .await?;

            let payload = json!({
                "apiKey": {
                    "id": created.id,
                    "name": created.name,
                    "prefix": created.prefix,
                    "last4": created.last4,
                    "permissions": created.permissions,
                    "expiresAt": created.expires_at.map(go_rfc3339nano),
                    "createdAt": go_rfc3339nano(created.created_at),
                },
                "secret": secret.secret,
            });
            Ok(ExecOk { payload, resource_id: created.id, target: None, pro_push: None })
        }
        "api_keys.rotate" => {
            let key_id = params.get("keyId").and_then(Value::as_str).unwrap_or_default();
            let existing = match api_keys::get_by_id(pool, key_id).await {
                Ok(k) => k,
                Err(api_keys::ApiKeyError::NotFound) => {
                    return Err(ActionError::NotFound(format!("api key {key_id}")));
                }
                Err(api_keys::ApiKeyError::Db(e)) => return Err(e.into()),
            };
            if existing.revoked_at.is_some() {
                return Err(ActionError::Conflict("cannot rotate a revoked key".into()));
            }
            let secret = api_key::generate_secret();
            let hash = api_key::hash_secret(&secret.secret);
            match api_keys::rotate(pool, key_id, &secret.prefix, &secret.last4, &hash).await {
                Ok(()) => {}
                Err(api_keys::ApiKeyError::NotFound) => {
                    return Err(ActionError::NotFound(format!("api key {key_id}")));
                }
                Err(api_keys::ApiKeyError::Db(e)) => return Err(e.into()),
            }
            let payload = json!({
                "apiKey": { "id": key_id, "prefix": secret.prefix, "last4": secret.last4 },
                "secret": secret.secret,
            });
            Ok(ExecOk { payload, resource_id: key_id.to_string(), target: None, pro_push: None })
        }
        "api_keys.revoke" => {
            let key_id = params.get("keyId").and_then(Value::as_str).unwrap_or_default();
            api_keys::revoke(pool, key_id, "manual").await?;
            Ok(ExecOk {
                payload: json!({ "keyId": key_id }),
                resource_id: key_id.to_string(),
                target: None,
                pro_push: None,
            })
        }
        _ => Err(ActionError::Validation("unknown action".into())),
    }
}

/// Reject minting/granting any permission the actor does not currently hold.
/// Mirrors Go `AssertPermissionsSubsetOfActor` (empty request is always fine;
/// `%q`-quoted permission in the error message).
fn assert_permissions_subset(requested: &[String], actor: &[String]) -> Result<(), ActionError> {
    if requested.is_empty() {
        return Ok(());
    }
    let have: HashSet<&str> = actor.iter().map(String::as_str).collect();
    for p in requested {
        if !have.contains(p.as_str()) {
            return Err(ActionError::PermissionDenied(format!(
                "cannot grant permission {p:?} the actor does not hold"
            )));
        }
    }
    Ok(())
}

/// Parse the optional RFC3339 `expiresAt` param into UTC. Validation already
/// rejected a malformed value, so a parse failure here silently drops it.
fn parse_expires(params: &Value) -> Option<chrono::DateTime<chrono::Utc>> {
    params
        .get("expiresAt")
        .and_then(Value::as_str)
        .filter(|s| !s.is_empty())
        .and_then(|s| chrono::DateTime::parse_from_rfc3339(s).ok())
        .map(|dt| dt.with_timezone(&chrono::Utc))
}

/// Format a timestamp exactly like Go's `time.Time` JSON (RFC3339Nano): trailing
/// zeros in the fractional part are trimmed, and the dot is dropped when the
/// fraction is zero. UTC always renders a literal `Z`.
fn go_rfc3339nano(dt: chrono::DateTime<chrono::Utc>) -> String {
    use chrono::Timelike;
    let base = dt.format("%Y-%m-%dT%H:%M:%S").to_string();
    let nanos = dt.nanosecond();
    if nanos == 0 {
        return format!("{base}Z");
    }
    let mut frac = format!("{nanos:09}");
    while frac.ends_with('0') {
        frac.pop();
    }
    format!("{base}.{frac}Z")
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
