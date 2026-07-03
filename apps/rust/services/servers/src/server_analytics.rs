//! Server player-count-history route + the daily analytics refresher worker.
//! Mirrors `internal/http/server_analytics_handler.go` + `internal/serveranalytics/`.

use std::sync::Arc;
use std::time::Duration;

use axum::Json;
use axum::extract::{Path, Query, State};
use axum::http::{HeaderMap, StatusCode};
use axum::response::{IntoResponse, Response};
use chrono::{DateTime, Datelike, Duration as ChronoDuration, SecondsFormat, Utc};
use serde::{Deserialize, Serialize};

use rustlens_battlemetrics::BmError;
use rustlens_db::server_analytics::{self as sa, CacheEntry, ServerPlayerCountPoint};
use rustlens_db::tracked_servers as ts;

use crate::auth::{err_json, require_bot_jwt};
use crate::redis_lock;
use crate::state::AppState;

type Shared = State<Arc<AppState>>;

const CACHE_DAYS: i32 = 90;
const REFRESH_LOCK_KEY: &str = "rustlens:server-analytics:refresh:lock";
const REFRESH_LOCK_TTL_MS: u64 = 23 * 3600 * 1000;
const REFRESH_GAP: Duration = Duration::from_secs(30);
const WEEKDAY_NAMES: [&str; 7] = [
    "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday",
];

fn is_bm_id(s: &str) -> bool {
    (1..=16).contains(&s.len()) && s.bytes().all(|b| b.is_ascii_digit())
}

fn round_to_2(value: f64) -> f64 {
    ((value * 100.0 + 0.5) as i64) as f64 / 100.0
}

fn rfc3339(dt: DateTime<Utc>) -> String {
    dt.to_rfc3339_opts(SecondsFormat::Secs, true)
}

fn timeframe_days(tf: &str) -> Option<i32> {
    match tf {
        "7d" => Some(7),
        "14d" => Some(14),
        "30d" => Some(30),
        "60d" => Some(60),
        "90d" => Some(90),
        _ => None,
    }
}

/// Inclusive UTC range for the last `days` completed days (`start` at 00:00:00,
/// `stop` = yesterday 23:59:59). Mirrors `CompletedDayRange`.
fn completed_day_range(now: DateTime<Utc>, days: i32) -> (DateTime<Utc>, DateTime<Utc>) {
    let days = days.max(1);
    let today_midnight = now.date_naive().and_hms_opt(0, 0, 0).unwrap().and_utc();
    let yesterday = today_midnight - ChronoDuration::days(1);
    let start = yesterday - ChronoDuration::days((days - 1) as i64);
    let stop = yesterday.date_naive().and_hms_opt(23, 59, 59).unwrap().and_utc();
    (start, stop)
}

/// Next 00:01 UTC after `now`.
fn next_daily_refresh(now: DateTime<Utc>) -> DateTime<Utc> {
    let today = now.date_naive().and_hms_opt(0, 1, 0).unwrap().and_utc();
    if today > now {
        today
    } else {
        today + ChronoDuration::days(1)
    }
}

fn convert_bm_points(points: &[rustlens_battlemetrics::ServerPlayerCountPoint]) -> Vec<ServerPlayerCountPoint> {
    points
        .iter()
        .map(|p| ServerPlayerCountPoint {
            timestamp_unix: p.timestamp.timestamp(),
            min: p.min,
            value: p.value,
            max: p.max,
        })
        .collect()
}

// ---- response shapes ----

#[derive(Serialize)]
struct Point {
    #[serde(rename = "timestampUnix")]
    timestamp_unix: i64,
    timestamp: String,
    min: i32,
    value: f64,
    max: i32,
}

#[derive(Serialize)]
struct Weekday {
    weekday: String,
    min: f64,
    value: f64,
    max: f64,
}

#[derive(Serialize)]
struct AnalyticsResponse {
    #[serde(rename = "serverBmId")]
    server_bm_id: String,
    #[serde(rename = "serverName")]
    server_name: String,
    timeframe: String,
    start: String,
    stop: String,
    points: Vec<Point>,
    weekdays: Vec<Weekday>,
    #[serde(rename = "cachedAt")]
    cached_at: String,
    #[serde(rename = "cacheWindow")]
    cache_window: String,
}

fn to_response_points(stored: &[ServerPlayerCountPoint]) -> Vec<Point> {
    let mut pts: Vec<Point> = stored
        .iter()
        .filter(|p| p.timestamp_unix != 0)
        .map(|p| Point {
            timestamp_unix: p.timestamp_unix,
            timestamp: DateTime::from_timestamp(p.timestamp_unix, 0)
                .map(rfc3339)
                .unwrap_or_default(),
            min: p.min,
            value: round_to_2(p.value),
            max: p.max,
        })
        .collect();
    pts.sort_by_key(|p| p.timestamp_unix);
    pts
}

fn filter_points(points: &[Point], start_unix: i64, stop_unix: i64) -> Vec<Point> {
    points
        .iter()
        .filter(|p| p.timestamp_unix >= start_unix && p.timestamp_unix <= stop_unix)
        .map(|p| Point {
            timestamp_unix: p.timestamp_unix,
            timestamp: p.timestamp.clone(),
            min: p.min,
            value: p.value,
            max: p.max,
        })
        .collect()
}

/// Per-weekday averages of daily min/value/max over ALL points (Monday=0), 7 entries.
fn compute_weekdays(stored: &[ServerPlayerCountPoint]) -> Vec<Weekday> {
    let mut count = [0i64; 7];
    let mut min_sum = [0.0f64; 7];
    let mut val_sum = [0.0f64; 7];
    let mut max_sum = [0.0f64; 7];
    for p in stored {
        let Some(dt) = DateTime::from_timestamp(p.timestamp_unix, 0) else {
            continue;
        };
        let wd = dt.weekday().num_days_from_monday() as usize;
        count[wd] += 1;
        min_sum[wd] += p.min as f64;
        val_sum[wd] += p.value;
        max_sum[wd] += p.max as f64;
    }
    (0..7)
        .map(|wd| {
            if count[wd] > 0 {
                let c = count[wd] as f64;
                Weekday {
                    weekday: WEEKDAY_NAMES[wd].to_string(),
                    min: round_to_2(min_sum[wd] / c),
                    value: round_to_2(val_sum[wd] / c),
                    max: round_to_2(max_sum[wd] / c),
                }
            } else {
                Weekday { weekday: WEEKDAY_NAMES[wd].to_string(), min: 0.0, value: 0.0, max: 0.0 }
            }
        })
        .collect()
}

fn cache_window(stored: &[ServerPlayerCountPoint]) -> String {
    let times: Vec<i64> = stored.iter().map(|p| p.timestamp_unix).filter(|&t| t != 0).collect();
    match (times.iter().min(), times.iter().max()) {
        (Some(&lo), Some(&hi)) => {
            let days = ((hi - lo) as f64 / 86400.0) as i64 + 1;
            format!("{}d", days.max(1))
        }
        _ => "0d".to_string(),
    }
}

enum CacheError {
    NotFound,
    Upstream,
}

/// Load the cached history (fresh iff fetched today), else fetch the 90d window,
/// upsert, re-read. On BM error, serve stale cache if present.
async fn get_cache(
    st: &AppState,
    bm_id: &str,
    cache_start: DateTime<Utc>,
    cache_stop: DateTime<Utc>,
    now: DateTime<Utc>,
) -> Result<CacheEntry, CacheError> {
    let day_start = now.date_naive().and_hms_opt(0, 0, 0).unwrap().and_utc();
    let entry = match sa::get_history(&st.pool, bm_id).await {
        Ok(v) => v,
        Err(e) => {
            tracing::warn!("servers/analytics cache read {bm_id}: {e}");
            None
        }
    };
    if let Some(e) = &entry {
        if e.fetched_at >= day_start {
            return Ok(e.clone());
        }
    }
    match st.bm.fetch_server_player_count_history(bm_id, cache_start, cache_stop).await {
        Ok(bm_points) => {
            let converted = convert_bm_points(&bm_points);
            if let Err(e) = sa::upsert_history(&st.pool, bm_id, &converted).await {
                tracing::warn!("servers/analytics cache write {bm_id}: {e}");
            }
            let reread = sa::get_history(&st.pool, bm_id).await.ok().flatten();
            Ok(reread.unwrap_or(CacheEntry { points: converted, fetched_at: now }))
        }
        Err(e) => {
            if let Some(stale) = entry {
                Ok(stale)
            } else if matches!(e, BmError::PlayerNotFound) {
                Err(CacheError::NotFound)
            } else {
                tracing::warn!("servers/analytics fetch {bm_id}: {e}");
                Err(CacheError::Upstream)
            }
        }
    }
}

#[derive(Deserialize)]
pub struct AnalyticsParams {
    timeframe: Option<String>,
}

pub async fn get_player_count_analytics(
    State(st): Shared,
    headers: HeaderMap,
    Path(bm_id): Path<String>,
    Query(params): Query<AnalyticsParams>,
) -> Response {
    if let Some(resp) = require_bot_jwt(&headers, &st.config) {
        return resp;
    }
    let bm_id = bm_id.trim().to_string();
    if !is_bm_id(&bm_id) {
        return err_json(StatusCode::BAD_REQUEST, "invalid BattleMetrics server ID");
    }
    let timeframe = params.timeframe.unwrap_or_default();
    let Some(days) = timeframe_days(&timeframe) else {
        return err_json(StatusCode::BAD_REQUEST, "timeframe must be one of 7d, 14d, 30d, 60d, 90d");
    };
    let server = match ts::get(&st.pool, &bm_id).await {
        Ok(Some(s)) => s,
        Ok(None) => return err_json(StatusCode::NOT_FOUND, "tracked server not found"),
        Err(e) => {
            tracing::error!("servers/analytics get server {bm_id}: {e}");
            return err_json(StatusCode::INTERNAL_SERVER_ERROR, "internal error");
        }
    };

    let now = Utc::now();
    let (cache_start, cache_stop) = completed_day_range(now, CACHE_DAYS);
    let (sel_start, sel_stop) = completed_day_range(now, days);

    let entry = match get_cache(&st, &bm_id, cache_start, cache_stop, now).await {
        Ok(e) => e,
        Err(CacheError::NotFound) => {
            return err_json(StatusCode::NOT_FOUND, "BattleMetrics server not found");
        }
        Err(CacheError::Upstream) => {
            return err_json(StatusCode::BAD_GATEWAY, "failed to fetch server analytics");
        }
    };

    let all_points = to_response_points(&entry.points);
    let selected = filter_points(&all_points, sel_start.timestamp(), sel_stop.timestamp());

    Json(AnalyticsResponse {
        server_bm_id: server.bm_id,
        server_name: server.name,
        timeframe,
        start: rfc3339(sel_start),
        stop: rfc3339(sel_stop),
        points: selected,
        weekdays: compute_weekdays(&entry.points),
        cached_at: rfc3339(entry.fetched_at),
        cache_window: cache_window(&entry.points),
    })
    .into_response()
}

// ---- daily refresher worker ----

/// Run the daily analytics refresher until the process exits (00:01 UTC, per
/// server 30s apart, 90d window, Redis-locked 23h).
pub async fn run_refresher(state: Arc<AppState>) {
    let owner = redis_lock::worker_owner();
    tracing::info!("server-analytics refresher started");
    loop {
        let next = next_daily_refresh(Utc::now());
        let wait = (next - Utc::now()).to_std().unwrap_or(Duration::ZERO);
        tokio::time::sleep(wait).await;
        refresh_run(&state, &owner).await;
    }
}

async fn refresh_run(state: &Arc<AppState>, owner: &str) {
    if let Some(conn) = &state.redis {
        let mut conn = conn.clone();
        if !redis_lock::try_acquire(&mut conn, REFRESH_LOCK_KEY, owner, REFRESH_LOCK_TTL_MS).await {
            return;
        }
        do_refresh(state).await;
        redis_lock::release(&mut conn, REFRESH_LOCK_KEY, owner).await;
    } else {
        do_refresh(state).await;
    }
}

async fn do_refresh(state: &Arc<AppState>) {
    let servers = ts::list(&state.pool).await.unwrap_or_default();
    if servers.is_empty() {
        return;
    }
    let (start, stop) = completed_day_range(Utc::now(), CACHE_DAYS);
    let n = servers.len();
    for (i, server) in servers.iter().enumerate() {
        match state.bm.fetch_server_player_count_history(&server.bm_id, start, stop).await {
            Ok(points) => {
                let converted = convert_bm_points(&points);
                if let Err(e) = sa::upsert_history(&state.pool, &server.bm_id, &converted).await {
                    tracing::warn!("servers/refresher upsert {}: {e}", server.bm_id);
                }
            }
            Err(e) => tracing::warn!("servers/refresher fetch {}: {e}", server.bm_id),
        }
        if i + 1 < n {
            tokio::time::sleep(REFRESH_GAP).await;
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn ts(s: &str) -> DateTime<Utc> {
        s.parse().unwrap()
    }

    #[test]
    fn completed_day_range_14d() {
        let (start, stop) = completed_day_range(ts("2026-05-05T12:34:00Z"), 14);
        assert_eq!(start, ts("2026-04-21T00:00:00Z"));
        assert_eq!(stop, ts("2026-05-04T23:59:59Z"));
    }

    #[test]
    fn completed_day_range_90d_time_independent() {
        let (start, stop) = completed_day_range(ts("2026-05-05T00:31:00Z"), 90);
        assert_eq!(start, ts("2026-02-04T00:00:00Z"));
        assert_eq!(stop, ts("2026-05-04T23:59:59Z"));
    }

    #[test]
    fn next_refresh_is_0001_utc() {
        assert_eq!(next_daily_refresh(ts("2026-05-05T12:00:00Z")), ts("2026-05-06T00:01:00Z"));
        assert_eq!(next_daily_refresh(ts("2026-05-05T00:00:30Z")), ts("2026-05-05T00:01:00Z"));
    }

    #[test]
    fn round_to_2_matches_go() {
        assert_eq!(round_to_2(1.235), 1.24);
        assert_eq!(round_to_2(0.0), 0.0);
    }

    #[test]
    fn weekdays_average_over_all_points() {
        // 2026-01-05 is a Monday. Two Monday points averaged.
        let mon = ts("2026-01-05T12:00:00Z").timestamp();
        let stored = vec![
            ServerPlayerCountPoint { timestamp_unix: mon, min: 2, value: 10.0, max: 20 },
            ServerPlayerCountPoint { timestamp_unix: mon + 3600, min: 4, value: 30.0, max: 40 },
        ];
        let wds = compute_weekdays(&stored);
        assert_eq!(wds.len(), 7);
        assert_eq!(wds[0].weekday, "Monday");
        assert_eq!(wds[0].value, 20.0); // (10+30)/2
        assert_eq!(wds[0].min, 3.0); // (2+4)/2
        assert_eq!(wds[1].value, 0.0); // Tuesday empty
    }

    #[test]
    fn cache_window_days() {
        let day = 86400;
        let stored = vec![
            ServerPlayerCountPoint { timestamp_unix: 1000, min: 0, value: 0.0, max: 0 },
            ServerPlayerCountPoint { timestamp_unix: 1000 + 6 * day, min: 0, value: 0.0, max: 0 },
        ];
        assert_eq!(cache_window(&stored), "7d");
        assert_eq!(cache_window(&[]), "0d");
    }
}
