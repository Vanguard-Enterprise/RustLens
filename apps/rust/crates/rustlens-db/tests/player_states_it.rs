//! Integration test for the player-states presence sync + reads. Skipped unless
//! `RUSTLENS_TEST_PG_URL` is set.

use rustlens_db::player_states as ps;
use rustlens_db::player_states::PresenceInput;

fn inputs(v: &[(&str, &str)]) -> Vec<PresenceInput> {
    v.iter().map(|(p, n)| PresenceInput { player_bm_id: p.to_string(), bm_name: n.to_string() }).collect()
}

async fn online_since(pool: &sqlx::PgPool, p: &str) -> Option<chrono::DateTime<chrono::Utc>> {
    sqlx::query_scalar("SELECT online_since FROM player_states WHERE server_bm_id='100' AND player_bm_id=$1")
        .bind(p).fetch_one(pool).await.unwrap()
}
async fn is_online(pool: &sqlx::PgPool, p: &str) -> bool {
    sqlx::query_scalar("SELECT is_online FROM player_states WHERE server_bm_id='100' AND player_bm_id=$1")
        .bind(p).fetch_one(pool).await.unwrap()
}

#[tokio::test]
async fn presence_sync_transitions_and_reads() {
    let Ok(url) = std::env::var("RUSTLENS_TEST_PG_URL") else {
        eprintln!("RUSTLENS_TEST_PG_URL unset — skipping player states integration test");
        return;
    };
    let pool = rustlens_db::connect(&url, 5).await.expect("connect");
    // The distilled 0001 baseline is the tested schema contract.
    rustlens_db::migrate(&pool).await.expect("migrate");
    for t in ["player_states", "tracked_servers", "steam_accounts", "bm_profile_cache", "steam_account_cache", "guild_steam_discord_links", "guild_tracked_servers"] {
        sqlx::query(&format!("DELETE FROM {t}")).execute(&pool).await.unwrap();
    }
    sqlx::query("INSERT INTO tracked_servers (bm_id, name) VALUES ('100','Alpha')").execute(&pool).await.unwrap();

    let t0: chrono::DateTime<chrono::Utc> = "2026-01-01T00:00:00Z".parse().unwrap();
    let t1: chrono::DateTime<chrono::Utc> = "2026-01-01T01:00:00Z".parse().unwrap();
    let t2: chrono::DateTime<chrono::Utc> = "2026-01-01T02:00:00Z".parse().unwrap();

    // t0: both online.
    ps::sync_player_states(&pool, "100", &inputs(&[("p1", "Alice"), ("p2", "Bob")]), t0).await.unwrap();
    assert!(is_online(&pool, "p1").await && is_online(&pool, "p2").await);
    assert_eq!(online_since(&pool, "p1").await, Some(t0));
    // names cached with epoch updated_at.
    let name: String = sqlx::query_scalar("SELECT bm_name FROM bm_profile_cache WHERE bm_id='p1'").fetch_one(&pool).await.unwrap();
    assert_eq!(name, "Alice");

    // t1: only p1 → p1 online_since preserved (t0), p2 offline.
    ps::sync_player_states(&pool, "100", &inputs(&[("p1", "Alice")]), t1).await.unwrap();
    assert!(is_online(&pool, "p1").await);
    assert_eq!(online_since(&pool, "p1").await, Some(t0), "online streak preserved");
    assert!(!is_online(&pool, "p2").await);
    let p2_off: Option<chrono::DateTime<chrono::Utc>> =
        sqlx::query_scalar("SELECT offline_since FROM player_states WHERE server_bm_id='100' AND player_bm_id='p2'").fetch_one(&pool).await.unwrap();
    assert_eq!(p2_off, Some(t1));

    // t2: only p2 → p2 back online with NEW online_since (t2), p1 offline.
    ps::sync_player_states(&pool, "100", &inputs(&[("p2", "Bob")]), t2).await.unwrap();
    assert!(is_online(&pool, "p2").await);
    assert_eq!(online_since(&pool, "p2").await, Some(t2), "offline→online restamps online_since");
    assert!(!is_online(&pool, "p1").await);
    assert_eq!(ps::count_online(&pool, "100").await.unwrap(), 1);

    // reads: list_online → [p2], get p1 (offline), get missing → None.
    let online = ps::list_online_by_server(&pool, "100").await.unwrap();
    assert_eq!(online.len(), 1);
    assert_eq!(online[0].player_bm_id, "p2");
    assert!(ps::get_state(&pool, "100", "p1").await.unwrap().is_some());
    assert!(ps::get_state(&pool, "100", "missing").await.unwrap().is_none());

    // Put both online for the join reads, seed accounts + links.
    ps::sync_player_states(&pool, "100", &inputs(&[("p1", "Alice"), ("p2", "Bob")]), t2).await.unwrap();
    sqlx::query("INSERT INTO steam_accounts (steam_id, bm_id) VALUES ('S1','p1'),('S2','p2')").execute(&pool).await.unwrap();
    sqlx::query("INSERT INTO steam_account_cache (steam_id, avatar_image) VALUES ('S1','http://av')").execute(&pool).await.unwrap();
    sqlx::query("INSERT INTO guild_steam_discord_links (guild_id, discord_id, steam_id) VALUES ('999999999999999999','111111111111111111','S1')").execute(&pool).await.unwrap();
    sqlx::query("INSERT INTO guild_tracked_servers (guild_id, server_bm_id) VALUES ('999999999999999999','100')").execute(&pool).await.unwrap();

    // search: bm_name ILIKE, requires steam_accounts.
    let hits = ps::search_by_name(&pool, "100", "lic", 20, false).await.unwrap();
    assert_eq!(hits.len(), 1);
    assert_eq!(hits[0].steam_id, "S1");
    assert_eq!(hits[0].bm_name, "Alice");
    // no steam link for p2 in search when q matches only p2? Bob has S2 linked -> matches.
    assert_eq!(ps::search_by_name(&pool, "100", "Bob", 20, false).await.unwrap().len(), 1);

    // online summary: one row per discord user.
    let summary = ps::list_online_summary(&pool, "999999999999999999").await.unwrap();
    assert_eq!(summary.len(), 1);
    assert_eq!(summary[0].discord_id, "111111111111111111");
    assert_eq!(summary[0].player_bm_id, "p1");
    assert!(summary[0].online_unix > 0);

    // log events: guild-scoped selection + link, avatar from cache.
    let events = ps::list_log_events(&pool, "999999999999999999").await.unwrap();
    assert_eq!(events.len(), 1);
    assert_eq!(events[0].avatar_image, "http://av");
    assert_eq!(events[0].discord_id, "111111111111111111");
    assert!(events[0].is_online);

    for t in ["player_states", "tracked_servers", "steam_accounts", "bm_profile_cache", "steam_account_cache", "guild_steam_discord_links", "guild_tracked_servers"] {
        sqlx::query(&format!("DELETE FROM {t}")).execute(&pool).await.unwrap();
    }
}
