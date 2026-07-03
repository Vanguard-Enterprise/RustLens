//! Admin guild list + detail (`/api/app/admin/guilds[/{guildID}]`), gated by
//! app-secret + session + `guilds.read`. Mirrors the Go admin handlers. Reads
//! across ~12 tables; assembles the big detail aggregate.

use std::sync::Arc;

use axum::Json;
use axum::extract::{Path, Query, State};
use axum::http::{HeaderMap, StatusCode};
use axum::response::{IntoResponse, Response};
use chrono::{DateTime, Utc};
use serde::Deserialize;
use serde_json::json;

use rustlens_db::admin_guilds::{self, GuildListFilter};
use rustlens_db::{guild_settings, tracked_servers, wipe_react_schedules, wipe_reacts};

use crate::auth::err_json;
use crate::state::AppState;

type Shared = State<Arc<AppState>>;

const DETAIL_PAGE_SIZE: i64 = 100;

/// app-secret + session + `guilds.read`. Returns the error response on failure.
async fn require_admin(headers: &HeaderMap, st: &AppState) -> Option<Response> {
    match rustlens_httpauth::require_admin(
        headers,
        &st.pool,
        &st.config.app_api_internal_secret,
        &st.config.app_session_secret,
        rustlens_db::rbac::GUILDS_READ,
    )
    .await
    {
        Ok(_) => None,
        Err(b) => Some(*b),
    }
}

/// `parseLimit`: only an all-digit value in `0..=10000` counts; anything else
/// (non-numeric, overflow, negative, absent) leaves the default.
fn parse_limit(raw: Option<&str>, default: i64) -> i64 {
    match raw {
        Some(s) => s.parse::<i64>().ok().filter(|&n| (0..=10000).contains(&n)).unwrap_or(default),
        None => default,
    }
}

/// `parseAdminGuildDetailPage`: empty / non-numeric / `< 1` → 1; no upper cap.
fn parse_detail_page(raw: Option<&str>) -> i64 {
    match raw.map(str::trim).filter(|s| !s.is_empty()) {
        Some(s) => s.parse::<i64>().ok().filter(|&n| n >= 1).unwrap_or(1),
        None => 1,
    }
}

/// `parseDateBound`: RFC3339 as-is, or `YYYY-MM-DD` at midnight UTC (+24h when
/// `end_exclusive`). `None` on any other format.
fn parse_date_bound(v: &str, end_exclusive: bool) -> Option<DateTime<Utc>> {
    if let Ok(dt) = DateTime::parse_from_rfc3339(v) {
        return Some(dt.with_timezone(&Utc));
    }
    if let Ok(d) = chrono::NaiveDate::parse_from_str(v, "%Y-%m-%d") {
        let base = d.and_hms_opt(0, 0, 0)?.and_utc();
        return Some(if end_exclusive { base + chrono::Duration::hours(24) } else { base });
    }
    None
}

fn internal(context: &str, e: impl std::fmt::Display) -> Response {
    tracing::error!("guilds/admin-guilds {context}: {e}");
    err_json(StatusCode::INTERNAL_SERVER_ERROR, "internal server error")
}

// ---- list ----

#[derive(Deserialize)]
pub struct ListParams {
    page: Option<String>,
    #[serde(rename = "pageSize")]
    page_size: Option<String>,
    q: Option<String>,
    sort: Option<String>,
    #[serde(rename = "joinedSince")]
    joined_since: Option<String>,
    #[serde(rename = "joinedBefore")]
    joined_before: Option<String>,
}

pub async fn list_admin_guilds(
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
    let sort = params.sort.clone().unwrap_or_default();
    if !matches!(
        sort.as_str(),
        "" | "name" | "-name" | "members" | "-members" | "joined" | "-joined"
    ) {
        return err_json(StatusCode::BAD_REQUEST, "invalid sort");
    }
    let joined_since = match params.joined_since.as_deref().map(str::trim).filter(|s| !s.is_empty()) {
        Some(v) => match parse_date_bound(v, false) {
            Some(d) => Some(d),
            None => return err_json(StatusCode::BAD_REQUEST, "invalid joinedSince"),
        },
        None => None,
    };
    let joined_before = match params.joined_before.as_deref().map(str::trim).filter(|s| !s.is_empty()) {
        Some(v) => match parse_date_bound(v, true) {
            Some(d) => Some(d),
            None => return err_json(StatusCode::BAD_REQUEST, "invalid joinedBefore"),
        },
        None => None,
    };
    let filter = GuildListFilter { q: params.q, joined_since, joined_before, sort };
    let offset = (page - 1) * page_size;
    match admin_guilds::list_page(&st.pool, &filter, page_size, offset).await {
        Ok((items, total)) => {
            Json(json!({ "items": items, "total": total, "page": page, "pageSize": page_size }))
                .into_response()
        }
        Err(e) => internal("list", e),
    }
}

// ---- detail ----

#[derive(Deserialize)]
pub struct DetailParams {
    #[serde(rename = "memberPage")]
    member_page: Option<String>,
    #[serde(rename = "linkPage")]
    link_page: Option<String>,
    #[serde(rename = "trackGroupPage")]
    track_group_page: Option<String>,
    #[serde(rename = "usagePage")]
    usage_page: Option<String>,
}

macro_rules! q_or_500 {
    ($ctx:expr, $e:expr) => {
        match $e.await {
            Ok(v) => v,
            Err(e) => return internal($ctx, e),
        }
    };
}

pub async fn get_admin_guild(
    State(st): Shared,
    headers: HeaderMap,
    Path(guild_id): Path<String>,
    Query(params): Query<DetailParams>,
) -> Response {
    if let Some(resp) = require_admin(&headers, &st).await {
        return resp;
    }
    if guild_id.trim().is_empty() {
        return err_json(StatusCode::BAD_REQUEST, "missing guildID");
    }

    let guild = match admin_guilds::get_item(&st.pool, &guild_id).await {
        Ok(Some(g)) => g,
        Ok(None) => return err_json(StatusCode::NOT_FOUND, "guild not found"),
        Err(e) => return internal("get_item", e),
    };

    let member_page = parse_detail_page(params.member_page.as_deref());
    let link_page = parse_detail_page(params.link_page.as_deref());
    let track_group_page = parse_detail_page(params.track_group_page.as_deref());
    let usage_page = parse_detail_page(params.usage_page.as_deref());
    let off = |p: i64| (p - 1) * DETAIL_PAGE_SIZE;
    let now = Utc::now().timestamp();

    let stats = q_or_500!("stats", admin_guilds::stats(&st.pool, &guild_id));
    let settings = q_or_500!("settings", guild_settings::get(&st.pool, &guild_id));
    let tracked = q_or_500!("tracked", tracked_servers::list_guild(&st.pool, &guild_id));
    let active_wr = q_or_500!("active_wr", wipe_reacts::list_active(&st.pool, &guild_id, now));
    let recent_wr = q_or_500!("recent_wr", wipe_reacts::list_past(&st.pool, &guild_id, now, 10));
    let schedules = q_or_500!("schedules", wipe_react_schedules::list(&st.pool, &guild_id));
    let members = q_or_500!("members", admin_guilds::members(&st.pool, &guild_id, DETAIL_PAGE_SIZE, off(member_page)));
    let links = q_or_500!("links", admin_guilds::links(&st.pool, &guild_id, DETAIL_PAGE_SIZE, off(link_page)));
    let track_groups = q_or_500!("track_groups", admin_guilds::track_groups(&st.pool, &guild_id, DETAIL_PAGE_SIZE, off(track_group_page)));
    let recent_usage = q_or_500!("recent_usage", admin_guilds::recent_usage(&st.pool, &guild_id, DETAIL_PAGE_SIZE, off(usage_page)));
    let link_total = q_or_500!("count_links", admin_guilds::count_steam_links(&st.pool, &guild_id));
    let usage_total = q_or_500!("count_usage", admin_guilds::count_usage(&st.pool, &guild_id));
    let usage_summary = q_or_500!("usage_summary", admin_guilds::usage_summary(&st.pool, &guild_id));

    let stats_json = json!({
        "memberCount": stats.member_count,
        "activeMemberCount": stats.active_member_count,
        "linkedMemberCount": stats.linked_member_count,
        "trackedServerCount": stats.tracked_server_count,
        "trackGroupCount": stats.track_group_count,
        "activeWipeReactCount": stats.active_wipe_react_count,
        "wipeReactCount": stats.wipe_react_count,
        "scheduleCount": stats.schedule_count,
        "usage30dCount": usage_summary.total_count,
        "usage30dErrorCount": usage_summary.error_count,
    });
    let pagination = json!({
        "members": { "page": member_page, "pageSize": DETAIL_PAGE_SIZE, "total": stats.member_count },
        "links": { "page": link_page, "pageSize": DETAIL_PAGE_SIZE, "total": link_total },
        "trackGroups": { "page": track_group_page, "pageSize": DETAIL_PAGE_SIZE, "total": stats.track_group_count },
        "recentUsage": { "page": usage_page, "pageSize": DETAIL_PAGE_SIZE, "total": usage_total },
    });

    Json(json!({
        "guild": guild,
        "stats": stats_json,
        "settings": settings,
        "trackedServers": tracked,
        "activeWipeReacts": active_wr,
        "recentWipeReacts": recent_wr,
        "schedules": schedules,
        "members": members,
        "links": links,
        "trackGroups": track_groups,
        "recentUsage": recent_usage,
        "usageSummary": usage_summary,
        "pagination": pagination,
    }))
    .into_response()
}
