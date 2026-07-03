//! The BattleMetrics rolling poller (background worker). Mirrors the Go
//! `internal/battlemetrics/poller.go`: per-cycle it polls 2 high-frequency + 1
//! low-frequency due servers concurrently, syncing presence into `player_states`,
//! coordinated across pods by a Redis cadence + lock (see `redis_lock`).

use std::sync::Arc;
use std::time::Duration;

use chrono::{DateTime, Utc};

use rustlens_db::player_states::{self as ps, PresenceInput};
use rustlens_db::tracked_servers::{self as ts, TrackedServer};

use crate::redis_lock;
use crate::state::AppState;

const CADENCE_KEY: &str = "rustlens:battlemetrics:poller:cadence";
const LOCK_KEY: &str = "rustlens:battlemetrics:poller:lock";
const POLL_TIMEOUT: Duration = Duration::from_secs(15);
const EMPTY_POLLS_BEFORE_OFFLINE: i32 = 3;
const HIGH_BATCH: usize = 2;
const LOW_BATCH: usize = 1;
const TOTAL_BATCH: usize = HIGH_BATCH + LOW_BATCH; // 3

/// Run the poller loop until the process exits. Disabled when the interval is 0.
pub async fn run(state: Arc<AppState>) {
    let secs = state.config.battlemetrics_poll_interval_seconds;
    if secs == 0 {
        tracing::info!("battlemetrics poller disabled (interval 0)");
        return;
    }
    let owner = redis_lock::worker_owner();
    let mut ticker = tokio::time::interval(Duration::from_secs(secs));
    ticker.set_missed_tick_behavior(tokio::time::MissedTickBehavior::Delay);
    tracing::info!("battlemetrics poller started (interval {secs}s)");
    loop {
        ticker.tick().await; // first tick fires immediately (runs a cycle now)
        run_cycle(&state, &owner).await;
    }
}

async fn run_cycle(state: &Arc<AppState>, owner: &str) {
    let interval = Duration::from_secs(state.config.battlemetrics_poll_interval_seconds);
    let Some(conn) = &state.redis else {
        // No Redis: single-pod mode, run without distributed locking.
        do_cycle(state).await;
        return;
    };
    let mut conn = conn.clone();

    // Cadence key: throttles to one cycle per interval across pods (self-expires).
    let margin = (interval / 10).max(Duration::from_millis(250));
    let cadence_ttl = interval
        .checked_sub(margin)
        .filter(|d| !d.is_zero())
        .unwrap_or(interval / 2);
    if !redis_lock::try_acquire(&mut conn, CADENCE_KEY, owner, cadence_ttl.as_millis() as u64).await {
        return;
    }

    // Lock key: guards against concurrent cycles; released at the end.
    let lock_ttl = if interval > Duration::from_secs(75) {
        interval + Duration::from_secs(30)
    } else {
        Duration::from_secs(75)
    };
    if !redis_lock::try_acquire(&mut conn, LOCK_KEY, owner, lock_ttl.as_millis() as u64).await {
        return;
    }

    do_cycle(state).await;
    redis_lock::release(&mut conn, LOCK_KEY, owner).await;
}

async fn do_cycle(state: &Arc<AppState>) {
    let high = ts::list_to_poll(&state.pool, true, TOTAL_BATCH as i64)
        .await
        .unwrap_or_default();
    let low = ts::list_to_poll(&state.pool, false, TOTAL_BATCH as i64)
        .await
        .unwrap_or_default();
    let servers = select_servers(&high, &low);
    if servers.is_empty() {
        return;
    }
    let observed_at = Utc::now();
    let mut handles = Vec::with_capacity(servers.len());
    for server in servers {
        let st = state.clone();
        handles.push(tokio::spawn(async move {
            poll_server(&st, &server, observed_at).await;
        }));
    }
    for h in handles {
        let _ = h.await;
    }
}

fn append_n(dst: &mut Vec<TrackedServer>, src: &[TrackedServer], start: usize, limit: usize) -> usize {
    if limit == 0 || start >= src.len() {
        return 0;
    }
    let take = limit.min(src.len() - start);
    dst.extend_from_slice(&src[start..start + take]);
    take
}

/// Select the ≤3 servers for a cycle: 2 high + 1 low, backfilling high-then-low.
fn select_servers(high: &[TrackedServer], low: &[TrackedServer]) -> Vec<TrackedServer> {
    let mut out: Vec<TrackedServer> = Vec::with_capacity(TOTAL_BATCH);
    let high_used = append_n(&mut out, high, 0, HIGH_BATCH);
    let low_used = append_n(&mut out, low, 0, LOW_BATCH);
    if out.len() < TOTAL_BATCH {
        let need = TOTAL_BATCH - out.len();
        append_n(&mut out, high, high_used, need);
    }
    if out.len() < TOTAL_BATCH {
        let need = TOTAL_BATCH - out.len();
        append_n(&mut out, low, low_used, need);
    }
    out
}

async fn poll_server(state: &Arc<AppState>, server: &TrackedServer, observed_at: DateTime<Utc>) {
    let bm_id = &server.bm_id;
    let fetch = tokio::time::timeout(POLL_TIMEOUT, state.bm.fetch_server_players(bm_id)).await;
    let mut poll_error = String::new();

    match fetch {
        Ok(Ok(players)) => {
            let inputs: Vec<PresenceInput> = players
                .into_iter()
                .map(|p| PresenceInput { player_bm_id: p.bm_id, bm_name: p.name })
                .collect();

            let mut should_sync = true;
            if inputs.is_empty() {
                match ps::count_online(&state.pool, bm_id).await {
                    Ok(n) if n > 0 => match ts::increment_empty_polls(&state.pool, bm_id).await {
                        Ok(empty) if empty < EMPTY_POLLS_BEFORE_OFFLINE => should_sync = false,
                        Ok(_) => {} // reached threshold → sync (mark offline)
                        Err(e) => poll_error = format!("record empty poll: {e}"),
                    },
                    Ok(_) => {} // nobody online → sync is a no-op
                    Err(e) => poll_error = format!("count online: {e}"),
                }
            }

            if poll_error.is_empty() && should_sync {
                if let Err(e) = ps::sync_player_states(&state.pool, bm_id, &inputs, observed_at).await {
                    poll_error = format!("sync player states: {e}");
                }
            }
            if poll_error.is_empty() && (!inputs.is_empty() || should_sync) {
                if let Err(e) = ts::reset_empty_polls(&state.pool, bm_id).await {
                    poll_error = format!("reset empty polls: {e}");
                }
            }
        }
        Ok(Err(e)) => poll_error = e.to_string(),
        Err(_elapsed) => poll_error = "battlemetrics poll timed out".to_string(),
    }

    // Always advance last_polled_at (even on error → round-robin coverage).
    if let Err(e) = ts::record_poll_result(&state.pool, bm_id, observed_at, &poll_error).await {
        tracing::warn!("servers/poller record poll result {bm_id}: {e}");
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn srv(id: &str, high: bool) -> TrackedServer {
        TrackedServer {
            bm_id: id.into(),
            name: id.into(),
            is_vitality: false,
            is_atlas: false,
            is_vital: false,
            is_modded: false,
            is_official: false,
            is_build: false,
            high_frequency: high,
            last_polled_at: None,
            last_poll_error: String::new(),
        }
    }
    fn ids(v: &[TrackedServer]) -> Vec<&str> {
        v.iter().map(|s| s.bm_id.as_str()).collect()
    }

    #[test]
    fn both_groups_full_picks_2_high_1_low() {
        let high = [srv("h1", true), srv("h2", true), srv("h3", true)];
        let low = [srv("l1", false), srv("l2", false), srv("l3", false)];
        assert_eq!(ids(&select_servers(&high, &low)), ["h1", "h2", "l1"]);
    }

    #[test]
    fn high_only_backfills_to_3_high() {
        let high = [srv("h1", true), srv("h2", true), srv("h3", true)];
        assert_eq!(ids(&select_servers(&high, &[])), ["h1", "h2", "h3"]);
    }

    #[test]
    fn low_only_backfills_to_3_low() {
        let low = [srv("l1", false), srv("l2", false), srv("l3", false)];
        assert_eq!(ids(&select_servers(&[], &low)), ["l1", "l2", "l3"]);
    }

    #[test]
    fn one_each_takes_both() {
        assert_eq!(
            ids(&select_servers(&[srv("h1", true)], &[srv("l1", false)])),
            ["h1", "l1"]
        );
    }

    #[test]
    fn empty_is_empty() {
        assert!(select_servers(&[], &[]).is_empty());
    }

    // ---- DB-gated worker smoke: one cycle against a mock BM server ----

    use rustlens_config::ServersConfig;
    use wiremock::matchers::{method, path};
    use wiremock::{Mock, MockServer, ResponseTemplate};

    const WORKER_SCHEMA: &str = r#"
        CREATE TABLE IF NOT EXISTS tracked_servers (bm_id TEXT PRIMARY KEY, name TEXT NOT NULL,
            is_vitality BOOL NOT NULL DEFAULT FALSE, is_atlas BOOL NOT NULL DEFAULT FALSE, is_vital BOOL NOT NULL DEFAULT FALSE,
            is_modded BOOL NOT NULL DEFAULT FALSE, is_official BOOL NOT NULL DEFAULT FALSE, is_build BOOL NOT NULL DEFAULT FALSE,
            high_frequency BOOL NOT NULL DEFAULT FALSE, last_polled_at TIMESTAMPTZ, last_poll_error TEXT NOT NULL DEFAULT '',
            empty_player_poll_count INTEGER NOT NULL DEFAULT 0);
        CREATE TABLE IF NOT EXISTS player_states (server_bm_id TEXT NOT NULL, player_bm_id TEXT NOT NULL,
            is_online BOOL NOT NULL DEFAULT FALSE, online_since TIMESTAMPTZ, offline_since TIMESTAMPTZ,
            updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), PRIMARY KEY (server_bm_id, player_bm_id));
        CREATE TABLE IF NOT EXISTS bm_profile_cache (bm_id TEXT PRIMARY KEY, bm_name TEXT NOT NULL DEFAULT '', updated_at TIMESTAMPTZ);
    "#;

    fn dummy_config() -> ServersConfig {
        ServersConfig {
            port: 0,
            postgres_url: String::new(),
            redis_url: String::new(),
            app_session_secret: String::new(),
            app_api_internal_secret: String::new(),
            bot_service_jwt_secret: String::new(),
            bot_jwt_issuer: String::new(),
            bot_jwt_audience: String::new(),
            battlemetrics_poll_interval_seconds: 30,
            proxy_enabled: false,
            proxy_endpoints: Vec::new(),
            proxy_encryption_key: None,
        }
    }

    #[tokio::test]
    async fn poller_cycle_syncs_presence_against_mock_bm() {
        let Ok(url) = std::env::var("RUSTLENS_TEST_PG_URL") else {
            eprintln!("RUSTLENS_TEST_PG_URL unset — skipping poller worker smoke");
            return;
        };
        // Mock BattleMetrics: server 100 has two players.
        let bm_server = MockServer::start().await;
        Mock::given(method("GET"))
            .and(path("/servers/100"))
            .respond_with(ResponseTemplate::new(200).set_body_json(serde_json::json!({
                "included": [
                    {"type":"player","id":"p1","attributes":{"name":"Alice"}},
                    {"type":"player","id":"p2","attributes":{"name":"Bob"}}
                ]
            })))
            .mount(&bm_server)
            .await;

        let pool = rustlens_db::connect(&url, 5).await.unwrap();
        for stmt in WORKER_SCHEMA.split(';').map(str::trim).filter(|s| !s.is_empty()) {
            sqlx::query(stmt).execute(&pool).await.unwrap();
        }
        sqlx::query("DELETE FROM player_states").execute(&pool).await.unwrap();
        sqlx::query("DELETE FROM tracked_servers").execute(&pool).await.unwrap();
        sqlx::query("INSERT INTO tracked_servers (bm_id, name, high_frequency) VALUES ('100','Alpha',TRUE)")
            .execute(&pool)
            .await
            .unwrap();

        // Direct (non-proxied) BM client pointed at the mock.
        let provider = rustlens_outbound::Provider::new(false, &[], Duration::from_secs(5)).unwrap();
        let bm = rustlens_battlemetrics::Client::new(
            std::sync::Arc::new(provider) as std::sync::Arc<dyn rustlens_outbound::OutboundSender>,
        )
        .with_base_url(bm_server.uri());

        let state = Arc::new(AppState { config: dummy_config(), pool: pool.clone(), bm, redis: None });
        do_cycle(&state).await;

        // Both players synced online; last_polled_at advanced, no error.
        let online: i64 = sqlx::query_scalar("SELECT COUNT(*) FROM player_states WHERE server_bm_id='100' AND is_online")
            .fetch_one(&pool).await.unwrap();
        assert_eq!(online, 2, "both mock players marked online");
        let (polled, err): (Option<chrono::DateTime<chrono::Utc>>, String) =
            sqlx::query_as("SELECT last_polled_at, last_poll_error FROM tracked_servers WHERE bm_id='100'")
                .fetch_one(&pool).await.unwrap();
        assert!(polled.is_some(), "last_polled_at advanced");
        assert_eq!(err, "", "no poll error");
        // name cached from the presence sync.
        let name: String = sqlx::query_scalar("SELECT bm_name FROM bm_profile_cache WHERE bm_id='p1'").fetch_one(&pool).await.unwrap();
        assert_eq!(name, "Alice");

        sqlx::query("DELETE FROM player_states").execute(&pool).await.unwrap();
        sqlx::query("DELETE FROM tracked_servers").execute(&pool).await.unwrap();
    }
}
