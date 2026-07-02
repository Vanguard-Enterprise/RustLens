//! Offline-analytics route (`/players/{bmID}/offline-analytics`): per-weekday
//! hourly minute averages in Europe/London, with a best 2-hour offline window.
//! Mirrors the Go `GetPlayerOfflineAnalytics` + the track-analytics math.

use std::collections::HashMap;
use std::sync::Arc;

use axum::Json;
use axum::extract::{Path, State};
use axum::http::{HeaderMap, StatusCode};
use axum::response::{IntoResponse, Response};
use chrono::{DateTime, Datelike, Duration, Offset, Timelike, Utc};
use chrono_tz::Europe::London;
use serde::Serialize;

use rustlens_db::players_steam as store;
use rustlens_db::track_analytics as cache;
use rustlens_db::track_analytics::TrackAnalyticsSession as Session;

use crate::auth::{err_json, require_bot_jwt};
use crate::state::AppState;

type Shared = State<Arc<AppState>>;

const CACHE_TTL_HOURS: i64 = 24;
const PAGE_SIZE: i32 = 100;
const MAX_PAGES: i32 = 2;
const MIN_DAY_SECONDS: i64 = 60 * 60; // trackAnalyticsMinDayMins * 60
/// 2026-01-05 00:00:00 UTC = Monday (Go `trackAnalyticsRefDayUnix`).
const REF_DAY_UNIX: i64 = 1_767_571_200;
const ALL_SERVERS: &str = "__all__";
const WEEKDAY_NAMES: [&str; 7] = [
    "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday",
];

#[derive(Serialize)]
struct HourValue {
    #[serde(rename = "hourUnix")]
    hour_unix: i64,
    hour: i32,
    avg: f64,
    minutes: f64,
}

#[derive(Serialize)]
struct DayValue {
    weekday: String,
    #[serde(rename = "bestStart")]
    best_start: i64,
    #[serde(rename = "bestEnd")]
    best_end: i64,
    hours: Vec<HourValue>,
}

#[derive(Serialize)]
struct AnalyticsResponse {
    #[serde(rename = "groupName")]
    group_name: String,
    #[serde(rename = "serverName")]
    server_name: String,
    #[serde(rename = "timeZone")]
    time_zone: String,
    members: i32,
    analyzed: i32,
    days: Vec<DayValue>,
}

fn is_bm_id(s: &str) -> bool {
    (1..=16).contains(&s.len()) && s.bytes().all(|b| b.is_ascii_digit())
}

fn round_to_2(value: f64) -> f64 {
    ((value * 100.0 + 0.5) as i64) as f64 / 100.0
}

/// Distribute a session across Europe/London local hours, invoking `f(day_key,
/// weekday(0=Mon), hour, seconds)` per hour segment. Splits at local-hour
/// boundaries (DST-aware via the current offset).
fn distribute_local<F: FnMut(String, usize, usize, i64)>(start: i64, stop: i64, mut f: F) {
    if stop <= start {
        return;
    }
    let mut cursor = start;
    while cursor < stop {
        let Some(utc) = DateTime::<Utc>::from_timestamp(cursor, 0) else {
            break;
        };
        let dt = utc.with_timezone(&London);
        let offset = dt.offset().fix().local_minus_utc() as i64;
        let into_hour = (cursor + offset).rem_euclid(3600);
        let seg_end = (cursor + (3600 - into_hour)).min(stop);
        let seconds = seg_end - cursor;
        if seconds > 0 {
            f(
                dt.format("%Y-%m-%d").to_string(),
                dt.weekday().num_days_from_monday() as usize,
                dt.hour() as usize,
                seconds,
            );
        }
        cursor = seg_end;
    }
}

struct DayBucket {
    total: i64,
    hours: [i64; 24],
    weekday: usize,
}

/// Per-weekday hourly minutes + counts for one player, dropping days with under
/// 60 minutes of play and capping each hour at 3600s.
fn player_weekday_hour_minutes(sessions: &[Session]) -> ([[f64; 24]; 7], [usize; 7]) {
    let mut buckets: HashMap<String, DayBucket> = HashMap::new();
    for s in sessions {
        distribute_local(s.start_unix, s.stop_unix, |day_key, weekday, hour, seconds| {
            let b = buckets.entry(day_key).or_insert(DayBucket {
                total: 0,
                hours: [0; 24],
                weekday,
            });
            b.total += seconds;
            b.hours[hour] += seconds;
        });
    }
    let mut out = [[0.0f64; 24]; 7];
    let mut counts = [0usize; 7];
    for b in buckets.values() {
        if b.total < MIN_DAY_SECONDS {
            continue;
        }
        counts[b.weekday] += 1;
        for (slot, &secs) in out[b.weekday].iter_mut().zip(&b.hours) {
            *slot += secs.min(3600) as f64 / 60.0;
        }
    }
    (out, counts)
}

fn best_offline_window(hours: &[f64; 24], day_base: i64) -> (i64, i64) {
    let mut best_sum = -1.0;
    let mut best_hour = 0;
    for h in 0..24 {
        let sum = hours[h] + hours[(h + 1) % 24];
        if best_sum < 0.0 || sum < best_sum {
            best_sum = sum;
            best_hour = h;
        }
    }
    let start = day_base + best_hour as i64 * 3600;
    (start, start + 2 * 3600)
}

/// Chart-ready per-weekday averages across all players' sessions.
fn compute_days(players: &[Vec<Session>]) -> Vec<DayValue> {
    let mut totals = [[0.0f64; 24]; 7];
    let mut counts = [0usize; 7];
    for sessions in players {
        let (per, per_counts) = player_weekday_hour_minutes(sessions);
        for wd in 0..7 {
            counts[wd] += per_counts[wd];
            for (t, p) in totals[wd].iter_mut().zip(&per[wd]) {
                *t += p;
            }
        }
    }
    for wd in 0..7 {
        if counts[wd] == 0 {
            continue;
        }
        let c = counts[wd] as f64;
        for t in totals[wd].iter_mut() {
            *t /= c;
        }
    }
    (0..7)
        .map(|wd| {
            let day_base = REF_DAY_UNIX + wd as i64 * 86400;
            let hours = (0..24)
                .map(|h| {
                    let m = round_to_2(totals[wd][h]);
                    HourValue {
                        hour_unix: day_base + h as i64 * 3600,
                        hour: h as i32,
                        avg: m,
                        minutes: m,
                    }
                })
                .collect();
            let (best_start, best_end) = best_offline_window(&totals[wd], day_base);
            DayValue {
                weekday: WEEKDAY_NAMES[wd].to_string(),
                best_start,
                best_end,
                hours,
            }
        })
        .collect()
}

fn convert_sessions(input: &[rustlens_battlemetrics::PlayerSession]) -> Vec<Session> {
    input
        .iter()
        .filter_map(|s| {
            let (start, stop) = (s.start?, s.stop?);
            let (su, pu) = (start.timestamp(), stop.timestamp());
            (pu > su).then_some(Session { start_unix: su, stop_unix: pu })
        })
        .collect()
}

/// Load cached sessions (24h TTL) or fetch the first 2 pages across all servers.
async fn fetch_offline_sessions(st: &AppState, bm_id: &str) -> Vec<Session> {
    let entry = match cache::get_track_analytics_cache(&st.pool, bm_id, ALL_SERVERS).await {
        Ok(v) => v,
        Err(e) => {
            tracing::warn!("players/offline cache lookup {bm_id}: {e}");
            None
        }
    };
    if let Some(entry) = &entry {
        if Utc::now() - entry.fetched_at < Duration::hours(CACHE_TTL_HOURS) {
            return entry.sessions.clone();
        }
    }
    match st.bm.fetch_player_sessions_all_servers(bm_id, PAGE_SIZE, MAX_PAGES).await {
        Ok(raw) => {
            let converted = convert_sessions(&raw);
            if let Err(e) = cache::upsert_track_analytics_cache(&st.pool, bm_id, ALL_SERVERS, &converted).await {
                tracing::warn!("players/offline cache write {bm_id}: {e}");
            }
            converted
        }
        Err(e) => {
            tracing::warn!("players/offline fetch {bm_id}: {e}");
            entry.map(|e| e.sessions).unwrap_or_default()
        }
    }
}

pub async fn get_player_offline_analytics(
    State(st): Shared,
    headers: HeaderMap,
    Path(bm_id): Path<String>,
) -> Response {
    if let Some(resp) = require_bot_jwt(&headers, &st.config) {
        return resp;
    }
    let bm_id = bm_id.trim().to_string();
    if !is_bm_id(&bm_id) {
        return err_json(StatusCode::BAD_REQUEST, "invalid BattleMetrics player ID");
    }

    let mut group_name = format!("Player {bm_id}");
    if let Ok(Some(account)) = store::get_steam_account_by_bm_id(&st.pool, &bm_id).await {
        if !account.bm_name.is_empty() {
            group_name = account.bm_name;
        }
    }

    let sessions = fetch_offline_sessions(&st, &bm_id).await;
    let analyzed = if sessions.is_empty() { 0 } else { 1 };
    let days = compute_days(&[sessions]);

    Json(AnalyticsResponse {
        group_name,
        server_name: "All BattleMetrics servers".to_string(),
        time_zone: "Europe/London".to_string(),
        members: 1,
        analyzed,
        days,
    })
    .into_response()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn round_to_2_matches_go() {
        assert_eq!(round_to_2(1.234), 1.23);
        assert_eq!(round_to_2(1.235), 1.24);
        assert_eq!(round_to_2(0.0), 0.0);
        assert_eq!(round_to_2(59.999), 60.0);
    }

    #[test]
    fn best_window_is_lowest_two_hour_sum() {
        let mut hours = [5.0f64; 24];
        hours[3] = 0.0;
        hours[4] = 0.0; // hours 3-4 are the quietest pair
        let (start, end) = best_offline_window(&hours, REF_DAY_UNIX);
        assert_eq!(start, REF_DAY_UNIX + 3 * 3600);
        assert_eq!(end, start + 7200);
    }

    #[test]
    fn compute_days_has_seven_days_each_24_hours() {
        let days = compute_days(&[vec![]]);
        assert_eq!(days.len(), 7);
        assert_eq!(days[0].weekday, "Monday");
        assert_eq!(days[6].weekday, "Sunday");
        assert!(days.iter().all(|d| d.hours.len() == 24));
        // ref day unix is a Monday at 00:00.
        assert_eq!(days[0].hours[0].hour_unix, REF_DAY_UNIX);
    }

    #[test]
    fn one_full_day_session_averages_60_min_per_hour() {
        // A 4-hour session well over the 60-min/day threshold, wholly within one
        // London day, contributes ~60 min to each of its 4 hours.
        // 2026-01-05 is a Monday; 12:00–16:00 UTC (London = UTC in January).
        let start = REF_DAY_UNIX + 12 * 3600;
        let stop = start + 4 * 3600;
        let days = compute_days(&[vec![Session { start_unix: start, stop_unix: stop }]]);
        // Monday, hour 12 should be a full 60 minutes (single day → avg = that day).
        let monday = &days[0];
        assert_eq!(monday.hours[12].minutes, 60.0);
        assert_eq!(monday.hours[15].minutes, 60.0);
        assert_eq!(monday.hours[16].minutes, 0.0);
    }

    #[test]
    fn short_day_dropped_below_threshold() {
        // A 30-minute session is under the 60-min/day threshold → no counted days.
        let start = REF_DAY_UNIX + 12 * 3600;
        let days = compute_days(&[vec![Session { start_unix: start, stop_unix: start + 1800 }]]);
        assert!(days.iter().all(|d| d.hours.iter().all(|h| h.minutes == 0.0)));
    }
}
