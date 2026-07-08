//! Admin server list (`/api/app/admin/servers`), gated by app-secret + session
//! plus `bm_servers.read`. Distinct from the `/api/app/admin/bm-servers` CRUD
//! surface (`bmservers.rs`): this is a lightweight list with a per-server
//! guild-selection count, no query params, capped at 500 rows. Mirrors
//! `ListAdminServers` in `apps/backend/internal/http/admin_read_handlers.go`.

use std::sync::Arc;

use axum::Json;
use axum::extract::State;
use axum::http::{HeaderMap, StatusCode};
use axum::response::{IntoResponse, Response};
use serde_json::json;

use rustlens_db::tracked_servers;

use crate::auth::err_json;
use crate::state::AppState;

type Shared = State<Arc<AppState>>;

const PERM_READ: &str = "bm_servers.read";

pub async fn list_admin_servers(State(st): Shared, headers: HeaderMap) -> Response {
    if let Err(resp) = rustlens_httpauth::require_admin(
        &headers,
        &st.pool,
        &st.config.app_api_internal_secret,
        &st.config.app_session_secret,
        PERM_READ,
    )
    .await
    {
        return *resp;
    }

    match tracked_servers::list_admin(&st.pool).await {
        Ok(items) => Json(json!({ "items": items })).into_response(),
        Err(e) => {
            tracing::error!("servers/admin-servers list: {e}");
            err_json(StatusCode::INTERNAL_SERVER_ERROR, "internal server error")
        }
    }
}
