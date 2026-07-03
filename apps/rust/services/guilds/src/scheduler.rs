//! The wipe-react schedule poller (background worker). Mirrors the Go
//! `internal/wipereactscheduler/poller.go`: each tick it computes the next due
//! occurrence per schedule and enqueues a `wipe_react_schedule_posts` row when
//! `post_unix <= now < wipe_unix` and it hasn't already been posted. Coordinated
//! across pods by a Redis cadence + lock (per the servers-phase decision).
//!
//! The `compute_next_*` timezone math mirrors Go's `time.Date`/`AddDate`/
//! `LoadLocation` semantics (chrono-tz), including biweekly's 13-day gap,
//! monthly's first-target-weekday rule, and the first-Thursday-19:00-Europe/London
//! force wipe.

use std::sync::Arc;
use std::time::Duration;

use chrono::{Datelike, NaiveDate, TimeZone, Utc, Weekday};
use chrono_tz::Tz;

use rustlens_db::wipe_react_schedule_posts as posts;
use rustlens_db::wipe_react_schedules::{self as sched, WipeReactSchedule};

use crate::redis_lock;
use crate::state::AppState;

const CADENCE_KEY: &str = "rustlens:wipe-scheduler:cadence";
const LOCK_KEY: &str = "rustlens:wipe-scheduler:lock";

/// Run the scheduler loop until the process exits. Disabled when interval is 0.
pub async fn run(state: Arc<AppState>) {
    let secs = state.config.wipe_scheduler_interval_seconds;
    if secs == 0 {
        tracing::info!("wipe scheduler disabled (interval 0)");
        return;
    }
    let owner = redis_lock::worker_owner();
    let mut ticker = tokio::time::interval(Duration::from_secs(secs));
    ticker.set_missed_tick_behavior(tokio::time::MissedTickBehavior::Delay);
    tracing::info!("wipe scheduler started (interval {secs}s)");
    loop {
        ticker.tick().await; // first tick fires immediately
        run_cycle(&state, &owner).await;
    }
}

async fn run_cycle(state: &Arc<AppState>, owner: &str) {
    let interval = Duration::from_secs(state.config.wipe_scheduler_interval_seconds);
    let Some(conn) = &state.redis else {
        // No Redis: single-pod mode, no distributed locking.
        do_tick(state).await;
        return;
    };
    let mut conn = conn.clone();
    let margin = (interval / 10).max(Duration::from_millis(250));
    let cadence_ttl = interval
        .checked_sub(margin)
        .filter(|d| !d.is_zero())
        .unwrap_or(interval / 2);
    if !redis_lock::try_acquire(&mut conn, CADENCE_KEY, owner, cadence_ttl.as_millis() as u64).await {
        return;
    }
    let lock_ttl = if interval > Duration::from_secs(75) {
        interval + Duration::from_secs(30)
    } else {
        Duration::from_secs(75)
    };
    if !redis_lock::try_acquire(&mut conn, LOCK_KEY, owner, lock_ttl.as_millis() as u64).await {
        return;
    }
    do_tick(state).await;
    redis_lock::release(&mut conn, LOCK_KEY, owner).await;
}

async fn do_tick(state: &Arc<AppState>) {
    let schedules = match sched::list_all(&state.pool).await {
        Ok(s) => s,
        Err(e) => {
            tracing::error!("wipe scheduler list_all: {e}");
            return;
        }
    };
    let now_unix = Utc::now().timestamp();
    for s in &schedules {
        let Some((wipe_unix, post_unix)) = compute_next_post_time(s, now_unix) else {
            continue;
        };
        if now_unix < post_unix {
            continue;
        }
        if s.last_posted_for_unix >= wipe_unix {
            continue;
        }
        if let Err(e) =
            posts::enqueue(&state.pool, s.id, &s.guild_id, &s.channel_id, wipe_unix, post_unix).await
        {
            tracing::error!("wipe scheduler enqueue guild={} schedule={}: {e}", s.guild_id, s.id);
        }
    }
}

fn post_before_seconds(pb: &str) -> Option<i64> {
    Some(match pb {
        "1d" => 86_400,
        "2d" => 172_800,
        "3d" => 259_200,
        "4d" => 345_600,
        "1w" => 604_800,
        "2w" => 1_209_600,
        _ => return None,
    })
}

fn weekday_from_name(name: &str) -> Option<Weekday> {
    Some(match name {
        "sunday" => Weekday::Sun,
        "monday" => Weekday::Mon,
        "tuesday" => Weekday::Tue,
        "wednesday" => Weekday::Wed,
        "thursday" => Weekday::Thu,
        "friday" => Weekday::Fri,
        "saturday" => Weekday::Sat,
        _ => return None,
    })
}

/// Parse `HH:MM` (Go `time.ParseInLocation("15:04", ...)`): 0-23 / 0-59.
fn parse_hhmm(s: &str) -> Option<(u32, u32)> {
    let (h, m) = s.split_once(':')?;
    let hh: u32 = h.parse().ok()?;
    let mm: u32 = m.parse().ok()?;
    if hh > 23 || mm > 59 {
        return None;
    }
    Some((hh, mm))
}

/// Resolve a local wall-clock time, mirroring Go's `time.Date`: `Single` → it,
/// `Ambiguous` (fall-back) → earliest, `None` (spring-forward gap) → normalize
/// forward by one hour.
fn resolve_local(loc: &Tz, date: NaiveDate, hh: u32, mm: u32) -> Option<chrono::DateTime<Tz>> {
    let naive = date.and_hms_opt(hh, mm, 0)?;
    if let Some(dt) = loc.from_local_datetime(&naive).earliest() {
        return Some(dt);
    }
    let bumped = naive.checked_add_signed(chrono::Duration::hours(1))?;
    loc.from_local_datetime(&bumped).earliest()
}

/// `computeNextPostTime` → `(wipe_unix, post_unix)`.
fn compute_next_post_time(s: &WipeReactSchedule, now_unix: i64) -> Option<(i64, i64)> {
    let post_before = post_before_seconds(&s.post_before)?;
    // Fresh schedule (never posted): backdate the search floor for immediate lead time.
    let after_unix = if s.last_posted_for_unix <= 0 {
        now_unix - post_before - 1
    } else {
        s.last_posted_for_unix
    };
    let mut earliest: i64 = 0;
    for day in &s.days {
        let occ = compute_next_weekday_occurrence(
            day,
            &s.time,
            &s.time_zone,
            &s.repeat,
            after_unix,
            now_unix,
        );
        if occ != 0 && (earliest == 0 || occ < earliest) {
            earliest = occ;
        }
    }
    if s.includes_force_wipe {
        let fw = compute_next_force_wipe_occurrence(after_unix, now_unix);
        if fw != 0 && (earliest == 0 || fw < earliest) {
            earliest = fw;
        }
    }
    if earliest == 0 {
        return None;
    }
    Some((earliest, earliest - post_before))
}

/// `computeNextWeekdayOccurrence` (0 = none). `weekly` = next future; `biweekly`
/// = next future ≥13 days after the floor; `monthly` = first target-weekday of a
/// month (day-of-month ≤ 7).
fn compute_next_weekday_occurrence(
    day_name: &str,
    time_text: &str,
    tz: &str,
    repeat: &str,
    after_unix: i64,
    now_unix: i64,
) -> i64 {
    let Some(target) = weekday_from_name(day_name) else {
        return 0;
    };
    let Ok(loc) = tz.parse::<Tz>() else {
        return 0;
    };
    let Some((hh, mm)) = parse_hhmm(time_text) else {
        return 0;
    };
    let Some(after) = loc.timestamp_opt(after_unix, 0).single() else {
        return 0;
    };
    let start_date = after.date_naive();
    let min_gap: i64 = if repeat == "biweekly" { 13 * 86_400 } else { 0 };
    for offset in 0..=62 {
        let Some(candidate_date) = start_date.checked_add_signed(chrono::Duration::days(offset))
        else {
            continue;
        };
        if candidate_date.weekday() != target {
            continue;
        }
        if repeat == "monthly" && candidate_date.day() > 7 {
            continue;
        }
        let Some(candidate) = resolve_local(&loc, candidate_date, hh, mm) else {
            continue;
        };
        let candidate_unix = candidate.timestamp();
        if candidate_unix <= after_unix {
            continue;
        }
        if candidate_unix <= now_unix {
            continue;
        }
        if min_gap > 0 && candidate_unix - after_unix < min_gap {
            continue;
        }
        return candidate_unix;
    }
    0
}

/// `computeNextForceWipeOccurrence`: first Thursday of the month at 19:00
/// Europe/London, searched over the current + next 3 months.
fn compute_next_force_wipe_occurrence(after_unix: i64, now_unix: i64) -> i64 {
    let Ok(loc) = "Europe/London".parse::<Tz>() else {
        return 0;
    };
    let Some(after) = loc.timestamp_opt(after_unix, 0).single() else {
        return 0;
    };
    let (year, month) = (after.year(), after.month() as i32);
    for month_offset in 0..4 {
        let mut cy = year;
        let mut cm = month + month_offset;
        while cm > 12 {
            cm -= 12;
            cy += 1;
        }
        for day in 1..=7u32 {
            let Some(date) = NaiveDate::from_ymd_opt(cy, cm as u32, day) else {
                continue;
            };
            let Some(candidate) = resolve_local(&loc, date, 19, 0) else {
                continue;
            };
            if candidate.weekday() != Weekday::Thu {
                continue;
            }
            // First Thursday of this month found.
            let ts = candidate.timestamp();
            if ts > after_unix && ts > now_unix {
                return ts;
            }
            break; // move to the next month; ignore later Thursdays this month
        }
    }
    0
}

#[cfg(test)]
mod tests {
    use super::*;
    use chrono::Timelike;
    use chrono_tz::Europe::London;

    /// Convert a unix timestamp to Europe/London wall-clock parts.
    fn london(ts: i64) -> (Weekday, u32, u32, u32) {
        let dt = London.timestamp_opt(ts, 0).single().unwrap();
        (dt.weekday(), dt.hour(), dt.minute(), dt.day())
    }

    const NOW: i64 = 1_700_000_000; // 2023-11-14T22:13:20Z

    #[test]
    fn weekly_next_thursday_1900_london() {
        let ts = compute_next_weekday_occurrence("thursday", "19:00", "Europe/London", "weekly", NOW - 90_000, NOW);
        assert!(ts > NOW);
        let (wd, h, m, _) = london(ts);
        assert_eq!(wd, Weekday::Thu);
        assert_eq!((h, m), (19, 0));
    }

    #[test]
    fn monthly_is_first_week_of_month() {
        let ts = compute_next_weekday_occurrence("thursday", "19:00", "Europe/London", "monthly", NOW - 90_000, NOW);
        assert!(ts > NOW);
        let (wd, h, _, day) = london(ts);
        assert_eq!(wd, Weekday::Thu);
        assert_eq!(h, 19);
        assert!(day <= 7, "monthly occurrence must be in the first week, got day {day}");
    }

    #[test]
    fn biweekly_gap_at_least_13_days() {
        let after = NOW - 90_000;
        let ts = compute_next_weekday_occurrence("thursday", "19:00", "Europe/London", "biweekly", after, NOW);
        assert!(ts - after >= 13 * 86_400, "biweekly must be >= 13 days after the floor");
    }

    #[test]
    fn force_wipe_first_thursday_1900_london() {
        let ts = compute_next_force_wipe_occurrence(NOW - 90_000, NOW);
        assert!(ts > NOW);
        let (wd, h, m, day) = london(ts);
        assert_eq!(wd, Weekday::Thu);
        assert_eq!((h, m), (19, 0));
        assert!(day <= 7, "force wipe must be the first Thursday, got day {day}");
    }

    #[test]
    fn invalid_timezone_and_postbefore_yield_none() {
        assert_eq!(compute_next_weekday_occurrence("thursday", "19:00", "Not/AZone", "weekly", NOW, NOW), 0);
        assert_eq!(compute_next_weekday_occurrence("notaday", "19:00", "Europe/London", "weekly", NOW, NOW), 0);
        assert_eq!(post_before_seconds("9d"), None);
        assert_eq!(post_before_seconds("2w"), Some(1_209_600));
    }

    #[test]
    fn post_time_lead_equals_post_before() {
        let s = WipeReactSchedule {
            id: 1,
            guild_id: "1".into(),
            days: vec!["thursday".into()],
            includes_force_wipe: false,
            time: "19:00".into(),
            time_zone: "Europe/London".into(),
            repeat: "weekly".into(),
            post_before: "2d".into(),
            name: String::new(),
            description: String::new(),
            ask_late_reason: false,
            ask_declined_reason: false,
            channel_id: String::new(),
            last_posted_for_unix: 0,
            created_by: String::new(),
        };
        let (wipe, post) = compute_next_post_time(&s, NOW).unwrap();
        assert_eq!(wipe - post, 172_800); // 2d
        assert!(wipe > NOW);
    }
}
