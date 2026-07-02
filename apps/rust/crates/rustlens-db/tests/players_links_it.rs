//! Integration test for the players link CRUD + suppression, against a real
//! Postgres. Skipped unless `RUSTLENS_TEST_PG_URL` is set (no-op in normal CI).

use rustlens_db::players_links as pl;

const SCHEMA: &str = r#"
CREATE TABLE IF NOT EXISTS steam_accounts (
    steam_id TEXT PRIMARY KEY, bm_id TEXT UNIQUE, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW());
CREATE TABLE IF NOT EXISTS steam_account_cache (
    steam_id TEXT PRIMARY KEY, persona_name TEXT NOT NULL DEFAULT '');
CREATE TABLE IF NOT EXISTS bm_profile_cache (bm_id TEXT PRIMARY KEY, bm_name TEXT NOT NULL DEFAULT '');
CREATE TABLE IF NOT EXISTS steam_discord_links (
    steam_id TEXT NOT NULL, discord_id TEXT NOT NULL,
    type TEXT NOT NULL CHECK (type IN ('soft','hard')),
    source TEXT NOT NULL DEFAULT '', created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (steam_id, discord_id, type));
CREATE TABLE IF NOT EXISTS hidden_discord_link_identities (
    discord_id TEXT PRIMARY KEY, reason TEXT NOT NULL DEFAULT '',
    created_by TEXT NOT NULL DEFAULT '', created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW());
CREATE TABLE IF NOT EXISTS guild_steam_discord_links (
    guild_id TEXT NOT NULL, discord_id TEXT NOT NULL, steam_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (guild_id, discord_id, steam_id));
"#;

const S1: &str = "76561190000000001";
const S2: &str = "76561190000000002";
const D1: &str = "111111111111111111";
const D2: &str = "222222222222222222";
const G1: &str = "999999999999999999";

async fn clean(pool: &sqlx::PgPool) {
    for t in [
        "steam_discord_links",
        "guild_steam_discord_links",
        "hidden_discord_link_identities",
        "steam_account_cache",
        "bm_profile_cache",
        "steam_accounts",
    ] {
        sqlx::query(&format!("DELETE FROM {t}")).execute(pool).await.unwrap();
    }
}

#[tokio::test]
async fn link_crud_and_suppression() {
    let Ok(url) = std::env::var("RUSTLENS_TEST_PG_URL") else {
        eprintln!("RUSTLENS_TEST_PG_URL unset — skipping players links integration test");
        return;
    };
    let pool = rustlens_db::connect(&url, 5).await.expect("connect");
    for stmt in SCHEMA.split(';').map(str::trim).filter(|s| !s.is_empty()) {
        sqlx::query(stmt).execute(&pool).await.unwrap();
    }
    clean(&pool).await;

    // create soft link (ensures the steam_accounts parent), duplicate -> AlreadyExists.
    let link = pl::create_soft_link(&pool, S1, D1, "bot_command").await.unwrap();
    assert_eq!(link.link_type, "soft");
    assert_eq!(link.source, "bot_command");
    assert!(matches!(
        pl::create_soft_link(&pool, S1, D1, "bot_command").await,
        Err(pl::LinkError::AlreadyExists)
    ));

    // bulk hard: two steam ids claiming the same bm_id -> second's bm_id coerced to NULL.
    let n = pl::bulk_upsert_hard_links(
        &pool,
        &[
            pl::BulkHardLink { steam_id: S1.into(), bm_id: Some("500".into()), discord_id: D1.into(), source: "platformsync".into() },
            pl::BulkHardLink { steam_id: S2.into(), bm_id: Some("500".into()), discord_id: D2.into(), source: "platformsync".into() },
        ],
    )
    .await
    .unwrap();
    assert_eq!(n, 2);
    let (bm1, bm2): (Option<String>, Option<String>) = (
        sqlx::query_scalar("SELECT bm_id FROM steam_accounts WHERE steam_id=$1").bind(S1).fetch_one(&pool).await.unwrap(),
        sqlx::query_scalar("SELECT bm_id FROM steam_accounts WHERE steam_id=$1").bind(S2).fetch_one(&pool).await.unwrap(),
    );
    assert_eq!(bm1.as_deref(), Some("500"));
    assert_eq!(bm2, None, "duplicate bm_id must be coerced to NULL");

    // S1/D1 now has both soft and hard rows (type is in the PK).
    let by_steam = pl::list_links_by_steam(&pool, S1).await.unwrap();
    assert_eq!(by_steam.len(), 2, "one soft + one hard for S1/D1");
    let by_discord = pl::list_links_by_discord(&pool, D1).await.unwrap();
    assert_eq!(by_discord.len(), 2);

    // hidden suppression: hiding D1 removes it from BOTH directions.
    pl::hide_discord_link_identity(&pool, D1, "test", "admin").await.unwrap();
    assert!(pl::list_links_by_discord(&pool, D1).await.unwrap().is_empty(), "discord->steam suppressed");
    assert!(pl::list_links_by_steam(&pool, S1).await.unwrap().is_empty(), "steam->discord suppressed");
    // visible-hard expansion also excludes hidden.
    assert!(pl::list_visible_hard_links_by_discord_ids(&pool, &[D1.into()]).await.unwrap().is_empty());
    // unhide restores.
    pl::unhide_discord_link_identity(&pool, D1).await.unwrap();
    assert_eq!(pl::list_links_by_discord(&pool, D1).await.unwrap().len(), 2);

    // delete removes only the SOFT row; hard remains.
    pl::delete_soft_link(&pool, S1, D1).await.unwrap();
    let after = pl::list_links_by_steam(&pool, S1).await.unwrap();
    assert_eq!(after.len(), 1);
    assert_eq!(after[0].link_type, "hard");
    assert!(matches!(pl::delete_soft_link(&pool, S1, D1).await, Err(pl::LinkError::NotFound)));

    // guild links.
    let gl = pl::create_guild_link(&pool, G1, D1, S1).await.unwrap();
    assert_eq!(gl.guild_id, G1);
    assert!(matches!(pl::create_guild_link(&pool, G1, D1, S1).await, Err(pl::LinkError::AlreadyExists)));
    assert_eq!(pl::list_guild_links(&pool, G1).await.unwrap().len(), 1);
    assert_eq!(pl::list_guild_links_by_discord(&pool, G1, D1).await.unwrap().len(), 1);
    assert_eq!(pl::list_guild_links_by_steam(&pool, G1, S1).await.unwrap().len(), 1);
    // hidden suppresses guild links too.
    pl::hide_discord_link_identity(&pool, D1, "t", "a").await.unwrap();
    assert!(pl::list_guild_links(&pool, G1).await.unwrap().is_empty());
    pl::unhide_discord_link_identity(&pool, D1).await.unwrap();
    // nickname targets present (one row per discord).
    assert_eq!(pl::list_guild_nickname_targets(&pool, G1).await.unwrap().len(), 1);
    // delete guild link + not-found.
    pl::delete_guild_link(&pool, G1, D1, S1).await.unwrap();
    assert!(matches!(pl::delete_guild_link(&pool, G1, D1, S1).await, Err(pl::LinkError::GuildNotFound)));

    // setsteam candidates: S1 has a hard link -> appears.
    let cands = pl::list_setsteam_candidates(&pool, D1).await.unwrap();
    assert!(cands.iter().any(|c| c.steam_id == S1 && c.link_type == "hard"));

    clean(&pool).await;
}
