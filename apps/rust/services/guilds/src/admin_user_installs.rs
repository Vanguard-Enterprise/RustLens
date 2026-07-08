//! Admin bot-user-installs list (`/api/app/admin/user-installs`), gated by
//! app-secret + session + `users.read`. Mirrors `ListAdminUserInstalls` in
//! `apps/backend/internal/http/admin_read_handlers.go`.

use std::sync::Arc;

use axum::Json;
use axum::extract::{Query, State};
use axum::http::{HeaderMap, StatusCode};
use axum::response::{IntoResponse, Response};
use serde::Deserialize;
use serde_json::json;

use rustlens_db::bot_user_installs::{self, AdminListFilter};

use crate::auth::err_json;
use crate::state::AppState;

type Shared = State<Arc<AppState>>;

/// app-secret + session + `users.read`. Returns the error response on failure.
async fn require_admin(headers: &HeaderMap, st: &AppState) -> Option<Response> {
    match rustlens_httpauth::require_admin(
        headers,
        &st.pool,
        &st.config.app_api_internal_secret,
        &st.config.app_session_secret,
        rustlens_db::rbac::USERS_READ,
    )
    .await
    {
        Ok(_) => None,
        Err(b) => Some(*b),
    }
}

fn internal(context: &str, e: impl std::fmt::Display) -> Response {
    tracing::error!("guilds/admin-user-installs {context}: {e}");
    err_json(StatusCode::INTERNAL_SERVER_ERROR, "internal server error")
}

/// `parseLimit`: only an all-digit value in `0..=10000` counts; anything else
/// (non-numeric, overflow, negative, absent) leaves the default.
fn parse_limit(raw: Option<&str>, default: i64) -> i64 {
    match raw {
        Some(s) => s.parse::<i64>().ok().filter(|&n| (0..=10000).contains(&n)).unwrap_or(default),
        None => default,
    }
}

#[derive(Deserialize)]
pub struct ListParams {
    page: Option<String>,
    #[serde(rename = "pageSize")]
    page_size: Option<String>,
    q: Option<String>,
}

pub async fn list_admin_user_installs(
    State(st): Shared,
    headers: HeaderMap,
    Query(params): Query<ListParams>,
) -> Response {
    if let Some(resp) = require_admin(&headers, &st).await {
        return resp;
    }
    let page = parse_limit(params.page.as_deref(), 1).max(1);
    let page_size = {
        let ps = parse_limit(params.page_size.as_deref(), 20);
        if (1..=100).contains(&ps) { ps } else { 20 }
    };
    let offset = (page - 1) * page_size;
    let filter = AdminListFilter { q: params.q.unwrap_or_default() };

    match bot_user_installs::list_admin(&st.pool, &filter, page_size, offset).await {
        Ok((items, total)) => {
            Json(json!({ "items": items, "total": total, "page": page, "pageSize": page_size }))
                .into_response()
        }
        Err(e) => internal("list", e),
    }
}
