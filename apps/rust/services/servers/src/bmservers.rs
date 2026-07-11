//! Admin BattleMetrics-server management: `/api/app/admin/bm-servers*`, gated by
//! app-secret + session + `bm_servers.read` (reads) / `bm_servers.edit`
//! (mutations). Every mutation writes a `bm_server` audit_events row. Mirrors
//! `apps/backend/internal/http/admin_bm_servers_handler.go`.

use std::sync::Arc;

use axum::Json;
use axum::body::Bytes;
use axum::extract::{Path, Query, State};
use axum::http::{HeaderMap, StatusCode, header};
use axum::response::{IntoResponse, Response};
use serde::Deserialize;
use serde_json::{Value, json};

use rustlens_db::admin_actions::{AuditEntry, record_audit};
use rustlens_db::tracked_servers::{
    self as ts, ServerFlags, TrackedServer, TrackedServerError, TrackedServerFilter,
};

use crate::auth::{err_json, parse_json};
use crate::state::AppState;

type Shared = State<Arc<AppState>>;

const PERM_READ: &str = "bm_servers.read";
const PERM_EDIT: &str = "bm_servers.edit";

fn db_error(context: &str, e: impl std::fmt::Display) -> Response {
    tracing::error!("servers/bm-servers {context}: {e}");
    err_json(StatusCode::INTERNAL_SERVER_ERROR, "internal error")
}

/// Admin JSON shape (`adminBmServerToMap`): `lastPollError` always present,
/// `lastPolledAt` omitted when the server has never been polled.
fn bmserver_map(s: &TrackedServer) -> Value {
    let mut v = json!({
        "bmId": s.bm_id,
        "name": s.name,
        "isVitality": s.is_vitality,
        "isAtlas": s.is_atlas,
        "isVital": s.is_vital,
        "isModded": s.is_modded,
        "isOfficial": s.is_official,
        "isBuild": s.is_build,
        "highFrequency": s.high_frequency,
        "lastPollError": s.last_poll_error,
    });
    if let Some(at) = s.last_polled_at {
        v["lastPolledAt"] = json!(at);
    }
    v
}

fn flags(
    is_vitality: bool,
    is_atlas: bool,
    is_vital: bool,
    is_modded: bool,
    is_official: bool,
    is_build: bool,
    high_frequency: bool,
) -> ServerFlags {
    ServerFlags { is_vitality, is_atlas, is_vital, is_modded, is_official, is_build, high_frequency }
}

/// `validateBmServerInput`: numeric `bmId` (1..=64) + non-empty `name` (<=200).
fn validate_bm_server_input(bm_id: &str, name: &str) -> Result<(), String> {
    let bm_id = bm_id.trim();
    let name = name.trim();
    if bm_id.is_empty() {
        return Err("bmId is required".into());
    }
    if bm_id.len() > 64 {
        return Err("bmId is too long".into());
    }
    if !bm_id.bytes().all(|c| c.is_ascii_digit()) {
        return Err("bmId must be numeric".into());
    }
    if name.is_empty() {
        return Err("name is required".into());
    }
    if name.chars().count() > 200 {
        return Err("name is too long".into());
    }
    Ok(())
}

async fn require_admin(
    headers: &HeaderMap,
    st: &AppState,
    perm: &str,
) -> Result<rustlens_session::SessionUser, Response> {
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

fn client_ip(headers: &HeaderMap) -> Option<String> {
    rustlens_httpauth::clientip::extract(headers)
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
    ip: Option<&str>,
) {
    let entry = AuditEntry {
        action_type,
        actor_user_id: &actor.user_id,
        actor_role_snapshot: &actor.admin_roles,
        resource_type: "bm_server",
        resource_id,
        target_user_id: None,
        params: &json!({}),
        reason,
        result,
        error_message,
        ip,
        user_agent: ua,
        via_api_key_id: None,
        via_api_key_name: None,
    };
    if let Err(e) = record_audit(&st.pool, &entry).await {
        tracing::warn!("servers/bm-servers audit: {e}");
    }
}

// ---- list ----

fn parse_bool_query(v: Option<&str>) -> Option<bool> {
    match v.map(|s| s.trim().to_ascii_lowercase()).as_deref() {
        Some("true" | "1" | "yes") => Some(true),
        Some("false" | "0" | "no") => Some(false),
        _ => None,
    }
}

#[derive(Deserialize)]
pub struct ListParams {
    page: Option<i64>,
    #[serde(rename = "pageSize")]
    page_size: Option<i64>,
    q: Option<String>,
    #[serde(rename = "highFrequency")]
    high_frequency: Option<String>,
    #[serde(rename = "hasError")]
    has_error: Option<String>,
    #[serde(rename = "isVitality")]
    is_vitality: Option<String>,
    #[serde(rename = "isAtlas")]
    is_atlas: Option<String>,
    #[serde(rename = "isVital")]
    is_vital: Option<String>,
    #[serde(rename = "isModded")]
    is_modded: Option<String>,
    #[serde(rename = "isOfficial")]
    is_official: Option<String>,
    #[serde(rename = "isBuild")]
    is_build: Option<String>,
    #[serde(rename = "sortBy")]
    sort_by: Option<String>,
    #[serde(rename = "sortOrder")]
    sort_order: Option<String>,
}

pub async fn list_admin_bm_servers(
    State(st): Shared,
    headers: HeaderMap,
    Query(params): Query<ListParams>,
) -> Response {
    if let Err(resp) = require_admin(&headers, &st, PERM_READ).await {
        return resp;
    }
    let page = params.page.filter(|&p| p > 0).unwrap_or(1);
    let page_size = params.page_size.filter(|&n| n > 0 && n <= 200).unwrap_or(50);
    let offset = (page - 1) * page_size;

    let filter = TrackedServerFilter {
        q: params.q.clone(),
        high_frequency: parse_bool_query(params.high_frequency.as_deref()),
        has_error: parse_bool_query(params.has_error.as_deref()),
        is_vitality: parse_bool_query(params.is_vitality.as_deref()),
        is_atlas: parse_bool_query(params.is_atlas.as_deref()),
        is_vital: parse_bool_query(params.is_vital.as_deref()),
        is_modded: parse_bool_query(params.is_modded.as_deref()),
        is_official: parse_bool_query(params.is_official.as_deref()),
        is_build: parse_bool_query(params.is_build.as_deref()),
    };
    let sort_by = params
        .sort_by
        .as_deref()
        .filter(|s| matches!(*s, "name" | "bmId" | "highFrequency" | "lastPolledAt" | "pollStatus"))
        .unwrap_or("");
    let sort_order = params
        .sort_order
        .as_deref()
        .filter(|s| matches!(*s, "asc" | "desc"))
        .unwrap_or("");

    match ts::list_page(&st.pool, &filter, sort_by, sort_order, page_size, offset).await {
        Ok((items, total)) => {
            let maps: Vec<Value> = items.iter().map(bmserver_map).collect();
            Json(json!({ "items": maps, "total": total, "page": page, "pageSize": page_size }))
                .into_response()
        }
        Err(e) => db_error("list", e),
    }
}

// ---- create ----

#[derive(Deserialize)]
struct CreateReq {
    #[serde(default, rename = "bmId")]
    bm_id: String,
    #[serde(default)]
    name: String,
    #[serde(default, rename = "isVitality")]
    is_vitality: bool,
    #[serde(default, rename = "isAtlas")]
    is_atlas: bool,
    #[serde(default, rename = "isVital")]
    is_vital: bool,
    #[serde(default, rename = "isModded")]
    is_modded: bool,
    #[serde(default, rename = "isOfficial")]
    is_official: bool,
    #[serde(default, rename = "isBuild")]
    is_build: bool,
    #[serde(default, rename = "highFrequency")]
    high_frequency: bool,
    #[serde(default)]
    reason: Option<String>,
}

pub async fn create_admin_bm_server(
    State(st): Shared,
    headers: HeaderMap,
    body: Bytes,
) -> Response {
    let actor = match require_admin(&headers, &st, PERM_EDIT).await {
        Ok(a) => a,
        Err(resp) => return resp,
    };
    let ua = user_agent(&headers);
    let ip = client_ip(&headers);
    let req: CreateReq = match parse_json(&body) {
        Ok(r) => r,
        Err(resp) => return *resp,
    };
    let bm_id = req.bm_id.trim().to_string();
    let name = req.name.trim().to_string();
    let reason = req.reason.as_deref();
    if let Err(msg) = validate_bm_server_input(&bm_id, &name) {
        return err_json(StatusCode::BAD_REQUEST, &msg);
    }
    let f = flags(
        req.is_vitality, req.is_atlas, req.is_vital, req.is_modded, req.is_official, req.is_build,
        req.high_frequency,
    );
    match ts::create(&st.pool, &bm_id, &name, f).await {
        Ok(server) => {
            audit(&st, &actor, "bm_servers.create", &bm_id, reason, "success", None, ua.as_deref(), ip.as_deref()).await;
            (StatusCode::CREATED, Json(bmserver_map(&server))).into_response()
        }
        Err(TrackedServerError::AlreadyExists) => {
            audit(&st, &actor, "bm_servers.create", &bm_id, reason, "error", Some("already_exists"), ua.as_deref(), ip.as_deref()).await;
            err_json(StatusCode::CONFLICT, "tracked server already exists")
        }
        Err(e) => {
            audit(&st, &actor, "bm_servers.create", &bm_id, reason, "error", Some(&e.to_string()), ua.as_deref(), ip.as_deref()).await;
            db_error("create", e)
        }
    }
}

// ---- update (PUT) ----

#[derive(Deserialize)]
struct UpdateReq {
    #[serde(default, rename = "bmId")]
    bm_id: String,
    #[serde(default)]
    name: String,
    #[serde(default, rename = "isVitality")]
    is_vitality: bool,
    #[serde(default, rename = "isAtlas")]
    is_atlas: bool,
    #[serde(default, rename = "isVital")]
    is_vital: bool,
    #[serde(default, rename = "isModded")]
    is_modded: bool,
    #[serde(default, rename = "isOfficial")]
    is_official: bool,
    #[serde(default, rename = "isBuild")]
    is_build: bool,
    #[serde(default, rename = "highFrequency")]
    high_frequency: bool,
    #[serde(default)]
    reason: Option<String>,
}

pub async fn update_admin_bm_server(
    State(st): Shared,
    headers: HeaderMap,
    Path(current_bm_id): Path<String>,
    body: Bytes,
) -> Response {
    let actor = match require_admin(&headers, &st, PERM_EDIT).await {
        Ok(a) => a,
        Err(resp) => return resp,
    };
    let ua = user_agent(&headers);
    let ip = client_ip(&headers);
    let current = current_bm_id.trim().to_string();
    if current.is_empty() {
        return err_json(StatusCode::BAD_REQUEST, "bmID is required");
    }
    let req: UpdateReq = match parse_json(&body) {
        Ok(r) => r,
        Err(resp) => return *resp,
    };
    let mut next = req.bm_id.trim().to_string();
    if next.is_empty() {
        next = current.clone();
    }
    let name = req.name.trim().to_string();
    let reason = req.reason.as_deref();
    if let Err(msg) = validate_bm_server_input(&next, &name) {
        return err_json(StatusCode::BAD_REQUEST, &msg);
    }
    let f = flags(
        req.is_vitality, req.is_atlas, req.is_vital, req.is_modded, req.is_official, req.is_build,
        req.high_frequency,
    );
    match ts::update(&st.pool, &current, &next, &name, f).await {
        Ok(server) => {
            audit(&st, &actor, "bm_servers.update", &server.bm_id, reason, "success", None, ua.as_deref(), ip.as_deref()).await;
            Json(bmserver_map(&server)).into_response()
        }
        Err(TrackedServerError::NotFound) => {
            audit(&st, &actor, "bm_servers.update", &current, reason, "error", Some("not_found"), ua.as_deref(), ip.as_deref()).await;
            err_json(StatusCode::NOT_FOUND, "tracked server not found")
        }
        Err(TrackedServerError::AlreadyExists) => {
            audit(&st, &actor, "bm_servers.update", &current, reason, "error", Some("already_exists"), ua.as_deref(), ip.as_deref()).await;
            err_json(StatusCode::CONFLICT, "tracked server with new bmId already exists")
        }
        Err(e) => {
            audit(&st, &actor, "bm_servers.update", &current, reason, "error", Some(&e.to_string()), ua.as_deref(), ip.as_deref()).await;
            db_error("update", e)
        }
    }
}

// ---- delete ----

#[derive(Deserialize)]
pub struct DeleteParams {
    reason: Option<String>,
}

pub async fn delete_admin_bm_server(
    State(st): Shared,
    headers: HeaderMap,
    Path(bm_id): Path<String>,
    Query(params): Query<DeleteParams>,
) -> Response {
    let actor = match require_admin(&headers, &st, PERM_EDIT).await {
        Ok(a) => a,
        Err(resp) => return resp,
    };
    let ua = user_agent(&headers);
    let ip = client_ip(&headers);
    let bm_id = bm_id.trim().to_string();
    if bm_id.is_empty() {
        return err_json(StatusCode::BAD_REQUEST, "bmID is required");
    }
    let reason = params.reason.as_deref();
    match ts::delete(&st.pool, &bm_id).await {
        Ok(()) => {
            audit(&st, &actor, "bm_servers.delete", &bm_id, reason, "success", None, ua.as_deref(), ip.as_deref()).await;
            Json(json!({ "status": "deleted" })).into_response()
        }
        Err(TrackedServerError::NotFound) => {
            audit(&st, &actor, "bm_servers.delete", &bm_id, reason, "error", Some("not_found"), ua.as_deref(), ip.as_deref()).await;
            err_json(StatusCode::NOT_FOUND, "tracked server not found")
        }
        Err(e) => {
            audit(&st, &actor, "bm_servers.delete", &bm_id, reason, "error", Some(&e.to_string()), ua.as_deref(), ip.as_deref()).await;
            db_error("delete", e)
        }
    }
}

// ---- bulk ----

#[derive(Deserialize)]
struct BulkReq {
    #[serde(default)]
    action: String,
    #[serde(default, rename = "bmIds")]
    bm_ids: Vec<String>,
    #[serde(default)]
    reason: Option<String>,
}

fn result_of(ok: bool) -> &'static str {
    if ok { "success" } else { "error" }
}

pub async fn bulk_admin_bm_servers(
    State(st): Shared,
    headers: HeaderMap,
    body: Bytes,
) -> Response {
    let actor = match require_admin(&headers, &st, PERM_EDIT).await {
        Ok(a) => a,
        Err(resp) => return resp,
    };
    let ua = user_agent(&headers);
    let ip = client_ip(&headers);
    let req: BulkReq = match parse_json(&body) {
        Ok(r) => r,
        Err(resp) => return *resp,
    };
    if req.bm_ids.is_empty() || req.bm_ids.len() > 200 {
        return err_json(StatusCode::BAD_REQUEST, "bmIds must be 1..200");
    }
    if !matches!(req.action.as_str(), "set_high_freq" | "set_standard" | "delete") {
        return err_json(
            StatusCode::BAD_REQUEST,
            "action must be one of: set_high_freq, set_standard, delete",
        );
    }
    let reason = req.reason.as_deref();
    let action_type = format!("bm_servers.bulk.{}", req.action);
    let mut results = Vec::with_capacity(req.bm_ids.len());
    let mut succeeded = 0;
    let mut failed = 0;

    for raw in &req.bm_ids {
        let id = raw.trim();
        if id.is_empty() {
            failed += 1;
            results.push(json!({ "bmId": id, "ok": false, "error": "empty_bmId" }));
            continue;
        }
        let outcome = match req.action.as_str() {
            "set_high_freq" => ts::set_high_frequency(&st.pool, id, true).await,
            "set_standard" => ts::set_high_frequency(&st.pool, id, false).await,
            _ => ts::delete(&st.pool, id).await,
        };
        match outcome {
            Ok(()) => {
                succeeded += 1;
                results.push(json!({ "bmId": id, "ok": true }));
                audit(&st, &actor, &action_type, id, reason, result_of(true), None, ua.as_deref(), ip.as_deref()).await;
            }
            Err(TrackedServerError::NotFound) => {
                failed += 1;
                results.push(json!({ "bmId": id, "ok": false, "error": "not_found" }));
                audit(&st, &actor, &action_type, id, reason, result_of(false), Some("not_found"), ua.as_deref(), ip.as_deref()).await;
            }
            Err(e) => {
                failed += 1;
                results.push(json!({ "bmId": id, "ok": false, "error": "store_error" }));
                tracing::error!("servers/bm-servers bulk {}: {e}", req.action);
                audit(&st, &actor, &action_type, id, reason, result_of(false), Some("store_error"), ua.as_deref(), ip.as_deref()).await;
            }
        }
    }

    Json(json!({ "action": req.action, "succeeded": succeeded, "failed": failed, "results": results }))
        .into_response()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn validate_bm_server() {
        assert!(validate_bm_server_input("12345", "My Server").is_ok());
        assert!(validate_bm_server_input("", "n").unwrap_err().contains("required"));
        assert!(validate_bm_server_input(&"1".repeat(65), "n").unwrap_err().contains("too long"));
        assert!(validate_bm_server_input("12a45", "n").unwrap_err().contains("numeric"));
        assert!(validate_bm_server_input("123", "").unwrap_err().contains("name is required"));
        assert!(validate_bm_server_input("123", &"x".repeat(201)).unwrap_err().contains("name is too long"));
    }

    #[test]
    fn bool_query_parsing() {
        assert_eq!(parse_bool_query(Some("true")), Some(true));
        assert_eq!(parse_bool_query(Some("1")), Some(true));
        assert_eq!(parse_bool_query(Some("no")), Some(false));
        assert_eq!(parse_bool_query(Some("maybe")), None);
        assert_eq!(parse_bool_query(None), None);
    }

    #[test]
    fn map_omits_null_last_polled() {
        let s = TrackedServer {
            bm_id: "1".into(),
            name: "n".into(),
            is_vitality: false,
            is_atlas: false,
            is_vital: false,
            is_modded: false,
            is_official: false,
            is_build: false,
            high_frequency: true,
            last_polled_at: None,
            last_poll_error: String::new(),
        };
        let m = bmserver_map(&s);
        assert!(m.get("lastPolledAt").is_none());
        assert_eq!(m["highFrequency"], json!(true));
        assert_eq!(m["lastPollError"], json!(""));
    }
}
