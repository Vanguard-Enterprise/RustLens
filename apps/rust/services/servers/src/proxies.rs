//! Outbound-proxy management: owner routes (`/api/internal/bot/owner/proxies*`,
//! bot-JWT only) + admin routes (`/api/app/admin/proxies*`, app-secret+session+
//! `proxies.*`) + the in-process provider live-reload. Mirrors the Go handlers.

use std::sync::Arc;

use axum::Json;
use axum::body::Bytes;
use axum::extract::{Path, Query, State};
use axum::http::{HeaderMap, StatusCode, header};
use axum::response::{IntoResponse, Response};
use chrono::{DateTime, SecondsFormat, Utc};
use serde::Deserialize;
use serde_json::{json, Value};

use rustlens_db::admin_actions::{record_audit, AuditEntry};
use rustlens_db::outbound_proxies::{self as op, OutboundProxyMeta, ProxyError};

use crate::auth::{err_json, parse_json, require_bot_jwt};
use crate::state::AppState;

type Shared = State<Arc<AppState>>;

const KEY_ID: &str = "default";

fn rfc3339(dt: DateTime<Utc>) -> String {
    dt.to_rfc3339_opts(SecondsFormat::Micros, true)
}

fn proxy_status(m: &OutboundProxyMeta) -> &'static str {
    if m.auto_disabled_at.is_some() {
        "broken"
    } else if m.enabled {
        "active"
    } else {
        "paused"
    }
}

fn owner_map(m: &OutboundProxyMeta) -> Value {
    json!({ "id": m.id, "endpoint": m.redacted_endpoint, "enabled": m.enabled })
}

fn admin_map(m: &OutboundProxyMeta) -> Value {
    let mut v = json!({
        "id": m.id,
        "endpoint": m.redacted_endpoint,
        "enabled": m.enabled,
        "status": proxy_status(m),
        "createdAt": rfc3339(m.created_at),
        "updatedAt": rfc3339(m.updated_at),
    });
    if let Some(at) = m.auto_disabled_at {
        v["autoDisabledAt"] = json!(rfc3339(at));
    }
    v
}

fn db_error(context: &str, e: impl std::fmt::Display) -> Response {
    tracing::error!("servers/proxies {context}: {e}");
    err_json(StatusCode::INTERNAL_SERVER_ERROR, "internal error")
}

/// Validate a proxy endpoint URL → `(canonical, redacted)`. Mirrors
/// `validateProxyEndpoint`.
fn validate_proxy_endpoint(raw: &str) -> Result<(String, String), String> {
    let raw = raw.trim();
    if raw.is_empty() {
        return Err("proxy endpoint is required".into());
    }
    let u = url::Url::parse(raw).map_err(|_| "proxy endpoint must be a valid URL".to_string())?;
    if !matches!(u.scheme(), "http" | "https") {
        return Err("proxy endpoint scheme must be http or https".into());
    }
    if u.host_str().is_none() {
        return Err("proxy endpoint must include host and port".into());
    }
    if u.port().is_none() {
        return Err("proxy endpoint must include host and port".into());
    }
    let has_password = u.password().is_some();
    if u.username().is_empty() && !has_password {
        return Err("proxy endpoint must include username and password".into());
    }
    if u.username().is_empty() {
        return Err("proxy endpoint must include username".into());
    }
    if !has_password {
        return Err("proxy endpoint must include password".into());
    }
    let canonical = u.as_str().to_string();
    let redacted = rustlens_outbound::redact_proxy_endpoint(&canonical);
    Ok((canonical, redacted))
}

/// Rebuild the in-process provider from the enabled DB proxies (decrypted), or
/// fall back to the legacy env config when there are no enabled DB rows.
/// `pub(crate)` so `main.rs` can reuse it when wiring the outbound provider's
/// auto-disable callback (Go `initialProxyEndpoints` + `Provider.Replace`).
pub(crate) async fn reload(st: &AppState) -> Result<(), String> {
    let enabled = op::list_enabled(&st.pool).await.map_err(|e| e.to_string())?;
    if enabled.is_empty() {
        return st
            .outbound
            .replace(st.config.proxy_enabled, &st.config.proxy_endpoints)
            .map_err(|e| e.to_string());
    }
    let Some(cipher) = &st.cipher else {
        return Err("PROXY_ENCRYPTION_KEY required to decrypt proxies".into());
    };
    let mut endpoints = Vec::with_capacity(enabled.len());
    for row in enabled {
        endpoints.push(cipher.decrypt(&row.ciphertext, &row.nonce).map_err(|e| e.to_string())?);
    }
    st.outbound.replace(true, &endpoints).map_err(|e| e.to_string())
}

fn parse_proxy_id(raw: &str) -> Option<i64> {
    raw.trim().parse::<i64>().ok().filter(|&n| n > 0)
}

// ---- owner routes (bot JWT only) ----

#[derive(Deserialize)]
struct EndpointReq {
    #[serde(default)]
    endpoint: String,
}

#[derive(Deserialize)]
struct EnabledReq {
    #[serde(default)]
    enabled: bool,
}

pub async fn list_owner_proxies(State(st): Shared, headers: HeaderMap) -> Response {
    if let Some(resp) = require_bot_jwt(&headers, &st.config) {
        return resp;
    }
    match op::list_all(&st.pool).await {
        Ok(items) => {
            let maps: Vec<Value> = items.iter().map(owner_map).collect();
            Json(json!({ "items": maps })).into_response()
        }
        Err(e) => db_error("list_owner", e),
    }
}

pub async fn create_owner_proxy(State(st): Shared, headers: HeaderMap, body: Bytes) -> Response {
    if let Some(resp) = require_bot_jwt(&headers, &st.config) {
        return resp;
    }
    if st.cipher.is_none() {
        return err_json(
            StatusCode::SERVICE_UNAVAILABLE,
            "PROXY_ENCRYPTION_KEY is required before database proxies can be managed",
        );
    }
    let req: EndpointReq = match parse_json(&body) {
        Ok(r) => r,
        Err(resp) => return *resp,
    };
    match create_proxy(&st, &req.endpoint).await {
        Ok((meta, reload_err)) => reload_or_created(owner_map(&meta), reload_err, "proxy saved but reload failed"),
        Err(CreateError::Validation(msg)) => err_json(StatusCode::BAD_REQUEST, &msg),
        Err(CreateError::AlreadyExists) => err_json(StatusCode::CONFLICT, "proxy already exists"),
        Err(CreateError::Internal(msg)) => { tracing::error!("servers/proxies create_owner: {msg}"); err_json(StatusCode::INTERNAL_SERVER_ERROR, "internal error") }
    }
}

pub async fn update_owner_proxy(
    State(st): Shared,
    headers: HeaderMap,
    Path(proxy_id): Path<String>,
    body: Bytes,
) -> Response {
    if let Some(resp) = require_bot_jwt(&headers, &st.config) {
        return resp;
    }
    let Some(id) = parse_proxy_id(&proxy_id) else {
        return err_json(StatusCode::BAD_REQUEST, "proxyID must be a positive integer");
    };
    let req: EnabledReq = match parse_json(&body) {
        Ok(r) => r,
        Err(resp) => return *resp,
    };
    match op::set_enabled(&st.pool, id, req.enabled).await {
        Ok(meta) => {
            let err = reload(&st).await.err();
            reload_or_ok(owner_map(&meta), err, "proxy updated but reload failed")
        }
        Err(ProxyError::NotFound) => err_json(StatusCode::NOT_FOUND, "proxy not found"),
        Err(ProxyError::Db(e)) => db_error("update_owner", e),
        Err(_) => err_json(StatusCode::INTERNAL_SERVER_ERROR, "internal error"),
    }
}

pub async fn delete_owner_proxy(
    State(st): Shared,
    headers: HeaderMap,
    Path(proxy_id): Path<String>,
) -> Response {
    if let Some(resp) = require_bot_jwt(&headers, &st.config) {
        return resp;
    }
    let Some(id) = parse_proxy_id(&proxy_id) else {
        return err_json(StatusCode::BAD_REQUEST, "proxyID must be a positive integer");
    };
    match op::delete(&st.pool, id).await {
        Ok(()) => {
            let err = reload(&st).await.err();
            reload_or_ok(json!({ "status": "deleted" }), err, "proxy deleted but reload failed")
        }
        Err(ProxyError::NotFound) => err_json(StatusCode::NOT_FOUND, "proxy not found"),
        Err(ProxyError::Db(e)) => db_error("delete_owner", e),
        Err(_) => err_json(StatusCode::INTERNAL_SERVER_ERROR, "internal error"),
    }
}

enum CreateError {
    Validation(String),
    AlreadyExists,
    Internal(String),
}

/// Validate → encrypt → create → reload. Returns the row + any reload error.
async fn create_proxy(st: &AppState, endpoint: &str) -> Result<(OutboundProxyMeta, Option<String>), CreateError> {
    let (canonical, redacted) = validate_proxy_endpoint(endpoint).map_err(CreateError::Validation)?;
    let cipher = st.cipher.as_ref().ok_or(CreateError::Validation(
        "PROXY_ENCRYPTION_KEY is required before database proxies can be managed".into(),
    ))?;
    let (ct, nonce) = cipher.encrypt(&canonical).map_err(|e| CreateError::Internal(e.to_string()))?;
    let meta = match op::create(&st.pool, &redacted, &ct, &nonce, KEY_ID).await {
        Ok(m) => m,
        Err(ProxyError::AlreadyExists) => return Err(CreateError::AlreadyExists),
        Err(e) => return Err(CreateError::Internal(e.to_string())),
    };
    let reload_err = reload(st).await.err();
    Ok((meta, reload_err))
}

fn reload_or_created(body: Value, reload_err: Option<String>, msg: &str) -> Response {
    match reload_err {
        None => (StatusCode::CREATED, Json(body)).into_response(),
        Some(e) => {
            tracing::error!("servers/proxies reload: {e}");
            err_json(StatusCode::INTERNAL_SERVER_ERROR, msg)
        }
    }
}

fn reload_or_ok(body: Value, reload_err: Option<String>, msg: &str) -> Response {
    match reload_err {
        None => Json(body).into_response(),
        Some(e) => {
            tracing::error!("servers/proxies reload: {e}");
            err_json(StatusCode::INTERNAL_SERVER_ERROR, msg)
        }
    }
}

// ---- admin routes (app-secret + session + permission) ----

const PERM_READ: &str = "proxies.read";
const PERM_EDIT: &str = "proxies.edit";

async fn require_admin(headers: &HeaderMap, st: &AppState, perm: &str) -> Result<rustlens_session::SessionUser, Response> {
    rustlens_httpauth::require_admin(
        headers,
        &st.pool,
        &st.config.app_api_internal_secret,
        &st.config.app_session_secret,
        perm,
    )
    .await
    .map_err(|b| *b)
}

fn user_agent(headers: &HeaderMap) -> Option<String> {
    headers.get(header::USER_AGENT).and_then(|v| v.to_str().ok()).map(String::from)
}

#[allow(clippy::too_many_arguments)]
async fn audit(
    st: &AppState,
    actor: &rustlens_session::SessionUser,
    action_type: &str,
    resource_id: &str,
    reason: Option<&str>,
    result: &str,
    error_message: Option<&str>,
    ua: Option<&str>,
) {
    let entry = AuditEntry {
        action_type,
        actor_user_id: &actor.user_id,
        actor_role_snapshot: &actor.admin_roles,
        resource_type: "proxy",
        resource_id,
        target_user_id: None,
        params: &json!({}),
        reason,
        result,
        error_message,
        user_agent: ua,
    };
    if let Err(e) = record_audit(&st.pool, &entry).await {
        tracing::warn!("servers/proxies audit: {e}");
    }
}

#[derive(Deserialize)]
pub struct ListParams {
    page: Option<i64>,
    #[serde(rename = "pageSize")]
    page_size: Option<i64>,
    q: Option<String>,
    status: Option<String>,
}

pub async fn list_admin_proxies(
    State(st): Shared,
    headers: HeaderMap,
    Query(params): Query<ListParams>,
) -> Response {
    if let Err(resp) = require_admin(&headers, &st, PERM_READ).await {
        return resp;
    }
    let page = params.page.filter(|&p| p > 0).unwrap_or(1);
    let page_size = params.page_size.filter(|&n| n > 0 && n <= 200).unwrap_or(50);
    let status = params.status.as_deref().filter(|s| matches!(*s, "active" | "paused" | "broken"));
    let offset = (page - 1) * page_size;
    match op::list_page(&st.pool, params.q.as_deref(), status, page_size, offset).await {
        Ok((items, total)) => {
            let maps: Vec<Value> = items.iter().map(admin_map).collect();
            Json(json!({ "items": maps, "total": total, "page": page, "pageSize": page_size })).into_response()
        }
        Err(e) => db_error("list_admin", e),
    }
}

#[derive(Deserialize)]
struct AdminCreateReq {
    #[serde(default)]
    endpoint: String,
    #[serde(default)]
    reason: Option<String>,
}

pub async fn create_admin_proxy(State(st): Shared, headers: HeaderMap, body: Bytes) -> Response {
    let actor = match require_admin(&headers, &st, PERM_EDIT).await {
        Ok(a) => a,
        Err(resp) => return resp,
    };
    let ua = user_agent(&headers);
    let req: AdminCreateReq = match parse_json(&body) {
        Ok(r) => r,
        Err(resp) => return *resp,
    };
    let reason = req.reason.as_deref();
    match create_proxy(&st, &req.endpoint).await {
        Ok((meta, reload_err)) => {
            if let Some(e) = &reload_err {
                audit(&st, &actor, "proxies.create", &meta.id.to_string(), reason, "error", Some("reload_failed"), ua.as_deref()).await;
                tracing::error!("servers/proxies reload: {e}");
                return err_json(StatusCode::INTERNAL_SERVER_ERROR, "proxy saved but reload failed");
            }
            audit(&st, &actor, "proxies.create", &meta.id.to_string(), reason, "success", None, ua.as_deref()).await;
            (StatusCode::CREATED, Json(admin_map(&meta))).into_response()
        }
        Err(CreateError::Validation(msg)) => err_json(StatusCode::BAD_REQUEST, &msg),
        Err(CreateError::AlreadyExists) => {
            audit(&st, &actor, "proxies.create", "", reason, "error", Some("already_exists"), ua.as_deref()).await;
            err_json(StatusCode::CONFLICT, "proxy already exists")
        }
        Err(CreateError::Internal(msg)) => { tracing::error!("servers/proxies create_admin: {msg}"); err_json(StatusCode::INTERNAL_SERVER_ERROR, "internal error") }
    }
}

#[derive(Deserialize)]
struct AdminUpdateReq {
    #[serde(default)]
    enabled: bool,
    #[serde(default)]
    reason: Option<String>,
}

pub async fn update_admin_proxy(
    State(st): Shared,
    headers: HeaderMap,
    Path(proxy_id): Path<String>,
    body: Bytes,
) -> Response {
    let actor = match require_admin(&headers, &st, PERM_EDIT).await {
        Ok(a) => a,
        Err(resp) => return resp,
    };
    let ua = user_agent(&headers);
    let Some(id) = parse_proxy_id(&proxy_id) else {
        return err_json(StatusCode::BAD_REQUEST, "proxyID must be a positive integer");
    };
    let req: AdminUpdateReq = match parse_json(&body) {
        Ok(r) => r,
        Err(resp) => return *resp,
    };
    let reason = req.reason.as_deref();
    match op::set_enabled(&st.pool, id, req.enabled).await {
        Ok(meta) => {
            if let Some(e) = reload(&st).await.err() {
                audit(&st, &actor, "proxies.update", &id.to_string(), reason, "error", Some("reload_failed"), ua.as_deref()).await;
                tracing::error!("servers/proxies reload: {e}");
                return err_json(StatusCode::INTERNAL_SERVER_ERROR, "proxy updated but reload failed");
            }
            audit(&st, &actor, "proxies.update", &id.to_string(), reason, "success", None, ua.as_deref()).await;
            Json(admin_map(&meta)).into_response()
        }
        Err(ProxyError::NotFound) => {
            audit(&st, &actor, "proxies.update", &id.to_string(), reason, "error", Some("not_found"), ua.as_deref()).await;
            err_json(StatusCode::NOT_FOUND, "proxy not found")
        }
        Err(ProxyError::Db(e)) => db_error("update_admin", e),
        Err(_) => err_json(StatusCode::INTERNAL_SERVER_ERROR, "internal error"),
    }
}

#[derive(Deserialize)]
pub struct DeleteParams {
    reason: Option<String>,
}

pub async fn delete_admin_proxy(
    State(st): Shared,
    headers: HeaderMap,
    Path(proxy_id): Path<String>,
    Query(params): Query<DeleteParams>,
) -> Response {
    let actor = match require_admin(&headers, &st, PERM_EDIT).await {
        Ok(a) => a,
        Err(resp) => return resp,
    };
    let ua = user_agent(&headers);
    let reason = params.reason.as_deref();
    let Some(id) = parse_proxy_id(&proxy_id) else {
        return err_json(StatusCode::BAD_REQUEST, "proxyID must be a positive integer");
    };
    match op::delete(&st.pool, id).await {
        Ok(()) => {
            if let Some(e) = reload(&st).await.err() {
                audit(&st, &actor, "proxies.delete", &id.to_string(), reason, "error", Some("reload_failed"), ua.as_deref()).await;
                tracing::error!("servers/proxies reload: {e}");
                return err_json(StatusCode::INTERNAL_SERVER_ERROR, "proxy deleted but reload failed");
            }
            audit(&st, &actor, "proxies.delete", &id.to_string(), reason, "success", None, ua.as_deref()).await;
            Json(json!({ "status": "deleted" })).into_response()
        }
        Err(ProxyError::NotFound) => {
            audit(&st, &actor, "proxies.delete", &id.to_string(), reason, "error", Some("not_found"), ua.as_deref()).await;
            err_json(StatusCode::NOT_FOUND, "proxy not found")
        }
        Err(ProxyError::Db(e)) => db_error("delete_admin", e),
        Err(_) => err_json(StatusCode::INTERNAL_SERVER_ERROR, "internal error"),
    }
}

#[derive(Deserialize)]
struct BulkReq {
    #[serde(default)]
    action: String,
    #[serde(default)]
    ids: Vec<i64>,
    #[serde(default)]
    enabled: Option<bool>,
    #[serde(default)]
    reason: Option<String>,
}

pub async fn bulk_admin_proxies(State(st): Shared, headers: HeaderMap, body: Bytes) -> Response {
    let actor = match require_admin(&headers, &st, PERM_EDIT).await {
        Ok(a) => a,
        Err(resp) => return resp,
    };
    let ua = user_agent(&headers);
    let req: BulkReq = match parse_json(&body) {
        Ok(r) => r,
        Err(resp) => return *resp,
    };
    if req.ids.is_empty() || req.ids.len() > 200 {
        return err_json(StatusCode::BAD_REQUEST, "ids must be 1..200");
    }
    if !matches!(req.action.as_str(), "enable" | "disable" | "delete" | "clear_broken") {
        return err_json(StatusCode::BAD_REQUEST, "action must be one of: enable, disable, delete, clear_broken");
    }
    let reason = req.reason.as_deref();
    let mut results = Vec::with_capacity(req.ids.len());
    let mut succeeded = 0;
    let mut failed = 0;
    for id in &req.ids {
        let (ok, action_type) = match req.action.as_str() {
            "enable" | "disable" => {
                let enabled = req.enabled.unwrap_or(req.action == "enable");
                let at = if enabled { "proxies.bulk.enable" } else { "proxies.bulk.disable" };
                (op::set_enabled(&st.pool, *id, enabled).await.map(|_| ()), at.to_string())
            }
            "delete" => (op::delete(&st.pool, *id).await, "proxies.bulk.delete".to_string()),
            _ => (op::clear_auto_disabled(&st.pool, *id).await.map(|_| ()), "proxies.bulk.clear_broken".to_string()),
        };
        match ok {
            Ok(()) => {
                succeeded += 1;
                results.push(json!({ "id": id, "ok": true }));
                audit(&st, &actor, &action_type, &id.to_string(), reason, "success", None, ua.as_deref()).await;
            }
            Err(ProxyError::NotFound) => {
                failed += 1;
                results.push(json!({ "id": id, "ok": false, "error": "not_found" }));
                audit(&st, &actor, &action_type, &id.to_string(), reason, "error", Some("not_found"), ua.as_deref()).await;
            }
            Err(_) => {
                failed += 1;
                results.push(json!({ "id": id, "ok": false, "error": "store_error" }));
                audit(&st, &actor, &action_type, &id.to_string(), reason, "error", Some("store_error"), ua.as_deref()).await;
            }
        }
    }
    // Reload once after the batch.
    if let Some(e) = reload(&st).await.err() {
        tracing::error!("servers/proxies bulk reload: {e}");
    }
    Json(json!({ "action": req.action, "succeeded": succeeded, "failed": failed, "results": results })).into_response()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn validate_ok_strips_credentials() {
        let (canonical, redacted) = validate_proxy_endpoint("http://user:pass@proxy.example:8080").unwrap();
        assert!(canonical.contains("user:pass@"));
        assert_eq!(redacted, "http://proxy.example:8080");
    }

    #[test]
    fn validate_rejects() {
        assert!(validate_proxy_endpoint("").unwrap_err().contains("required"));
        assert!(validate_proxy_endpoint("ftp://user:pass@h:1").unwrap_err().contains("scheme"));
        assert!(validate_proxy_endpoint("http://user:pass@host").unwrap_err().contains("host and port"));
        assert!(validate_proxy_endpoint("http://host:8080").unwrap_err().contains("username and password"));
        assert!(validate_proxy_endpoint("http://:pass@host:8080").unwrap_err().contains("username"));
        assert!(validate_proxy_endpoint("http://user@host:8080").unwrap_err().contains("password"));
    }

    #[test]
    fn status_derivation() {
        use chrono::Utc;
        let mut m = OutboundProxyMeta { id: 1, redacted_endpoint: "x".into(), enabled: true, created_at: Utc::now(), updated_at: Utc::now(), auto_disabled_at: None };
        assert_eq!(proxy_status(&m), "active");
        m.enabled = false;
        assert_eq!(proxy_status(&m), "paused");
        m.auto_disabled_at = Some(Utc::now());
        assert_eq!(proxy_status(&m), "broken");
    }
}
