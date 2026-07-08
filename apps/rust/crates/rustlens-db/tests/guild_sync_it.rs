//! Integration test for `sync_user_guilds` against a real Postgres.
//!
//! Skipped (returns early) unless `RUSTLENS_TEST_PG_URL` is set, so it is a
//! no-op in normal `cargo test` / CI runs with no database. The login
//! guild-sync smoke sets that env var to a throwaway Postgres to exercise the
//! real array-binding + transaction path.

use rustlens_db::identity::{sync_user_guilds, UserGuildInput};

const DISCORD_ID: &str = "test_guild_sync_user";

#[tokio::test]
async fn sync_user_guilds_upserts_active_and_deactivates_absent() {
    let Ok(url) = std::env::var("RUSTLENS_TEST_PG_URL") else {
        eprintln!("RUSTLENS_TEST_PG_URL unset — skipping guild sync integration test");
        return;
    };
    let pool = rustlens_db::connect(&url, 5).await.expect("connect");

    // The distilled 0001 baseline is the tested schema contract; apply the
    // embedded migrations rather than hand-writing DDL here.
    rustlens_db::migrate(&pool).await.expect("migrate");
    sqlx::query("DELETE FROM user_guilds WHERE discord_id = $1")
        .bind(DISCORD_ID)
        .execute(&pool)
        .await
        .unwrap();

    // Pre-existing active membership that will NOT be in the new list, with an
    // old first_seen_at we expect to be preserved on re-sync of survivors.
    sqlx::query(
        "INSERT INTO user_guilds (discord_id, guild_id, is_active, first_seen_at, name)
         VALUES ($1, 'g_stale', TRUE, TIMESTAMPTZ '2020-01-01T00:00:00Z', 'old'),
                ($1, 'g_keep',  TRUE, TIMESTAMPTZ '2020-01-01T00:00:00Z', 'old')",
    )
    .bind(DISCORD_ID)
    .execute(&pool)
    .await
    .unwrap();

    // New list: keep g_keep (survivor), add g_new; g_stale is now absent.
    let guilds = vec![
        UserGuildInput {
            guild_id: "g_keep".into(),
            name: "Keep".into(),
            icon_hash: "icon_keep".into(),
            approximate_member_count: Some(42),
            is_owner: true,
        },
        UserGuildInput {
            guild_id: "g_new".into(),
            name: "New".into(),
            icon_hash: String::new(), // no icon
            approximate_member_count: None,
            is_owner: false,
        },
    ];
    sync_user_guilds(&pool, DISCORD_ID, &guilds).await.unwrap();

    // g_new: inserted active.
    let (active, name, icon, members, owner): (bool, String, Option<String>, Option<i32>, bool) =
        sqlx::query_as(
            "SELECT is_active, name, icon_hash, approximate_member_count, is_owner
             FROM user_guilds WHERE discord_id = $1 AND guild_id = 'g_new'",
        )
        .bind(DISCORD_ID)
        .fetch_one(&pool)
        .await
        .unwrap();
    assert!(active);
    assert_eq!(name, "New");
    assert_eq!(icon.as_deref(), Some("")); // empty string, not NULL (matches Go)
    assert_eq!(members, None);
    assert!(!owner);

    // g_keep: upserted active, fields refreshed, first_seen_at preserved.
    let (active, name, members, first_seen): (bool, String, Option<i32>, chrono::DateTime<chrono::Utc>) =
        sqlx::query_as(
            "SELECT is_active, name, approximate_member_count, first_seen_at
             FROM user_guilds WHERE discord_id = $1 AND guild_id = 'g_keep'",
        )
        .bind(DISCORD_ID)
        .fetch_one(&pool)
        .await
        .unwrap();
    assert!(active);
    assert_eq!(name, "Keep");
    assert_eq!(members, Some(42));
    assert_eq!(
        first_seen,
        "2020-01-01T00:00:00Z".parse::<chrono::DateTime<chrono::Utc>>().unwrap(),
        "first_seen_at must be preserved across re-sync"
    );

    // g_stale: deactivated with left_at stamped.
    let (active, left_at): (bool, Option<chrono::DateTime<chrono::Utc>>) = sqlx::query_as(
        "SELECT is_active, left_at FROM user_guilds WHERE discord_id = $1 AND guild_id = 'g_stale'",
    )
    .bind(DISCORD_ID)
    .fetch_one(&pool)
    .await
    .unwrap();
    assert!(!active, "absent guild must be deactivated");
    assert!(left_at.is_some(), "deactivated guild must have left_at stamped");

    // Empty list deactivates everyone remaining.
    sync_user_guilds(&pool, DISCORD_ID, &[]).await.unwrap();
    let remaining_active: i64 =
        sqlx::query_scalar("SELECT COUNT(*) FROM user_guilds WHERE discord_id = $1 AND is_active")
            .bind(DISCORD_ID)
            .fetch_one(&pool)
            .await
            .unwrap();
    assert_eq!(remaining_active, 0, "empty list must deactivate all guilds");

    sqlx::query("DELETE FROM user_guilds WHERE discord_id = $1")
        .bind(DISCORD_ID)
        .execute(&pool)
        .await
        .unwrap();
}
