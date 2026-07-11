//! Integration test for the players steam-account store round-trips (account
//! join, JSONB caches, name history) against a real Postgres. Skipped unless
//! `RUSTLENS_TEST_PG_URL` is set.

use rustlens_db::players_steam as ps;

const S: &str = "76561190000000055";
const B: &str = "556677";

async fn clean(pool: &sqlx::PgPool) {
    for t in ["steam_account_cache", "bm_profile_cache", "bm_name_history", "steam_accounts"] {
        sqlx::query(&format!("DELETE FROM {t}")).execute(pool).await.unwrap();
    }
}

#[tokio::test]
async fn steam_account_and_cache_round_trips() {
    let Ok(url) = std::env::var("RUSTLENS_TEST_PG_URL") else {
        eprintln!("RUSTLENS_TEST_PG_URL unset — skipping players steam integration test");
        return;
    };
    let pool = rustlens_db::connect(&url, 5).await.expect("connect");
    // The distilled 0001 baseline is the tested schema contract.
    rustlens_db::migrate(&pool).await.expect("migrate");
    clean(&pool).await;

    // upsert account + link, then read back the joined shape.
    let acct = ps::upsert_steam_account(&pool, S, Some(B)).await.unwrap();
    assert_eq!(acct.steam_id, S);
    assert_eq!(acct.bm_id.as_deref(), Some(B));

    // steam cache with JSONB top_games round-trips (incl. game-ban cols default).
    let sc = ps::SteamAccountCache {
        steam_id: S.into(),
        persona_name: "Alice".into(),
        avatar_image: "http://avatar".into(),
        rust_hours: Some(1234),
        total_games: Some(42),
        top_games: vec![ps::AnalyticsGame { app_id: 252490, name: "Rust".into(), hours: 1234 }],
        ..Default::default()
    };
    let out = ps::upsert_steam_account_cache(&pool, &sc).await.unwrap();
    assert_eq!(out.persona_name, "Alice");
    assert_eq!(out.rust_hours, Some(1234));
    assert_eq!(out.top_games.len(), 1);
    assert_eq!(out.top_games[0].app_id, 252490);
    assert!(!out.game_banned);

    // bm profile cache with JSONB top_servers + hours.
    let bc = ps::BmProfileCache {
        bm_id: B.into(),
        bm_name: "AliceBM".into(),
        first_seen_at: Some("2021-01-01T00:00:00Z".parse().unwrap()),
        bm_hours: 500,
        modded_hours: 300,
        bm_servers_played: 3,
        top_servers: vec![ps::AnalyticsServer { bm_id: "900".into(), name: "Srv".into(), hours: 300 }],
        ..Default::default()
    };
    let outb = ps::upsert_bm_profile_cache(&pool, &bc).await.unwrap();
    assert_eq!(outb.bm_name, "AliceBM");
    assert_eq!(outb.bm_hours, 500);
    assert_eq!(outb.bm_servers_played, 3);
    assert_eq!(outb.top_servers[0].bm_id, "900");

    // joined account now surfaces bm_name (bpc) + avatar (sac).
    let joined = ps::get_steam_account_by_steam_id(&pool, S).await.unwrap().unwrap();
    assert_eq!(joined.bm_name, "AliceBM");
    assert_eq!(joined.avatar_image, "http://avatar");
    assert!(joined.first_seen_at.is_some());
    let by_bm = ps::get_steam_account_by_bm_id(&pool, B).await.unwrap().unwrap();
    assert_eq!(by_bm.steam_id, S);

    // name history upsert + ordered read.
    ps::upsert_bm_name_history(
        &pool,
        B,
        &[
            ps::NameHistory { name: "z".into(), last_seen_at: "2024-02-01T00:00:00Z".parse().unwrap() },
            ps::NameHistory { name: "a".into(), last_seen_at: "2024-01-01T00:00:00Z".parse().unwrap() },
        ],
    )
    .await
    .unwrap();
    let hist = ps::list_bm_name_history(&pool, B).await.unwrap();
    assert_eq!(hist.len(), 2);
    assert_eq!(hist[0].name, "a", "ordered last_seen ASC");

    // missing account -> None (not error).
    assert!(ps::get_steam_account_by_steam_id(&pool, "76561190000000099").await.unwrap().is_none());

    clean(&pool).await;
}
