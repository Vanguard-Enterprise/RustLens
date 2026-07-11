//! Integration test for tracked-server CRUD incl. the rename-remap transaction,
//! guild ops, and poll helpers. Skipped unless `RUSTLENS_TEST_PG_URL` is set.

use rustlens_db::tracked_servers::{self as ts, ServerFlags, TrackedServerError};

async fn clean(pool: &sqlx::PgPool) {
    for t in [
        "guild_tracked_servers", "player_states", "track_groups", "server_player_count_history",
        "server_player_count_history_fetches", "track_analytics_sessions_cache", "tracked_servers",
    ] {
        sqlx::query(&format!("DELETE FROM {t}")).execute(pool).await.unwrap();
    }
}

fn vit() -> ServerFlags {
    ServerFlags { is_vitality: true, high_frequency: true, ..Default::default() }
}

#[tokio::test]
async fn tracked_server_crud_rename_and_guild() {
    let Ok(url) = std::env::var("RUSTLENS_TEST_PG_URL") else {
        eprintln!("RUSTLENS_TEST_PG_URL unset — skipping tracked servers integration test");
        return;
    };
    let pool = rustlens_db::connect(&url, 5).await.expect("connect");
    // The distilled 0001 baseline is the tested schema contract.
    rustlens_db::migrate(&pool).await.expect("migrate");
    clean(&pool).await;

    // create + dup + get + list.
    let s = ts::create(&pool, "100", "Alpha", vit()).await.unwrap();
    assert_eq!(s.bm_id, "100");
    assert!(s.is_vitality && s.high_frequency);
    assert!(matches!(ts::create(&pool, "100", "Alpha", vit()).await, Err(TrackedServerError::AlreadyExists)));
    assert!(ts::get(&pool, "100").await.unwrap().is_some());
    ts::create(&pool, "200", "Bravo", ServerFlags::default()).await.unwrap();
    assert_eq!(ts::list(&pool).await.unwrap().len(), 2);

    // same-id update.
    let up = ts::update(&pool, "100", "100", "Alpha2", ServerFlags { is_modded: true, ..Default::default() }).await.unwrap();
    assert_eq!(up.name, "Alpha2");
    assert!(up.is_modded && !up.is_vitality);

    // record poll metadata on 100, seed children, then RENAME 100 -> 999.
    ts::record_poll_result(&pool, "100", "2026-01-01T00:00:00Z".parse().unwrap(), "boom").await.unwrap();
    sqlx::query("INSERT INTO player_states (server_bm_id, player_bm_id, is_online) VALUES ('100','p1',TRUE)").execute(&pool).await.unwrap();
    sqlx::query("INSERT INTO guild_tracked_servers (guild_id, server_bm_id) VALUES ('999999999999999999','100')").execute(&pool).await.unwrap();
    sqlx::query("INSERT INTO server_player_count_history (server_bm_id, timestamp_unix, min, value, max) VALUES ('100',1,0,1.0,2)").execute(&pool).await.unwrap();
    sqlx::query("INSERT INTO track_analytics_sessions_cache (player_bm_id, server_bm_id) VALUES ('p1','100')").execute(&pool).await.unwrap();

    let renamed = ts::update(&pool, "100", "999", "Renamed", ServerFlags::default()).await.unwrap();
    assert_eq!(renamed.bm_id, "999");
    assert_eq!(renamed.last_poll_error, "boom", "poll metadata carried over");
    assert!(renamed.last_polled_at.is_some());
    // old row gone, new present.
    assert!(ts::get(&pool, "100").await.unwrap().is_none());
    assert!(ts::get(&pool, "999").await.unwrap().is_some());
    // children repointed.
    for (t, col) in [("player_states", "server_bm_id"), ("guild_tracked_servers", "server_bm_id"),
                     ("server_player_count_history", "server_bm_id"), ("track_analytics_sessions_cache", "server_bm_id")] {
        let n: i64 = sqlx::query_scalar(&format!("SELECT COUNT(*) FROM {t} WHERE {col} = '999'")).fetch_one(&pool).await.unwrap();
        assert_eq!(n, 1, "{t} repointed to 999");
        let old: i64 = sqlx::query_scalar(&format!("SELECT COUNT(*) FROM {t} WHERE {col} = '100'")).fetch_one(&pool).await.unwrap();
        assert_eq!(old, 0, "{t} no longer references 100");
    }
    // rename collision.
    assert!(matches!(ts::update(&pool, "999", "200", "x", ServerFlags::default()).await, Err(TrackedServerError::AlreadyExists)));
    // update missing.
    assert!(matches!(ts::update(&pool, "nope", "nope2", "x", ServerFlags::default()).await, Err(TrackedServerError::NotFound)));

    // guild ops on 999.
    let g = "999999999999999999";
    // (a guild link already exists from the rename); remove it first for a clean add test.
    ts::remove_guild(&pool, g, "999").await.unwrap();
    ts::add_guild(&pool, g, "999").await.unwrap();
    assert!(matches!(ts::add_guild(&pool, g, "999").await, Err(TrackedServerError::GuildAlreadyEnabled)));
    assert!(matches!(ts::add_guild(&pool, g, "missing").await, Err(TrackedServerError::NotFound)));
    assert_eq!(ts::list_guild(&pool, g).await.unwrap().len(), 1);
    ts::replace_guild(&pool, g, &["200".into(), "999".into()]).await.unwrap();
    assert_eq!(ts::list_guild(&pool, g).await.unwrap().len(), 2);
    ts::remove_guild(&pool, g, "999").await.unwrap();
    assert!(matches!(ts::remove_guild(&pool, g, "999").await, Err(TrackedServerError::GuildNotEnabled)));

    // poll helpers: 999 is high-freq? no (renamed to default). 200 low-freq. list_to_poll.
    let high = ts::list_to_poll(&pool, true, 3).await.unwrap();
    assert!(high.is_empty(), "no high-frequency servers remain");
    let low = ts::list_to_poll(&pool, false, 3).await.unwrap();
    assert_eq!(low.len(), 2);
    // never-polled (999 has last_polled from carry; 200 never) -> NULLS FIRST puts 200 before 999.
    assert_eq!(low[0].bm_id, "200", "never-polled first (NULLS FIRST)");

    // empty-poll counter.
    assert_eq!(ts::increment_empty_polls(&pool, "200").await.unwrap(), 1);
    assert_eq!(ts::increment_empty_polls(&pool, "200").await.unwrap(), 2);
    ts::reset_empty_polls(&pool, "200").await.unwrap();
    assert_eq!(ts::increment_empty_polls(&pool, "200").await.unwrap(), 1);

    // list_page filter.
    let (page, total) = ts::list_page(&pool, &Default::default(), "bmId", "asc", 10, 0).await.unwrap();
    assert_eq!(total, 2);
    assert_eq!(page[0].bm_id, "200");

    // delete + not-found.
    ts::delete(&pool, "200").await.unwrap();
    assert!(matches!(ts::delete(&pool, "200").await, Err(TrackedServerError::NotFound)));

    clean(&pool).await;
}
