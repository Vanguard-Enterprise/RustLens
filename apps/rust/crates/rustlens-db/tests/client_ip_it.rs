//! Integration test for the client-IP write paths ported from Go's
//! `internal/clientip`: `api_keys.last_used_ip` (via `api_keys::update_last_used`)
//! and `audit_events.ip_address` (via `admin_actions::record_audit`). Both bind
//! a text value into an `inet` column with an explicit `$N::inet` cast, so this
//! proves the cast round-trips through sqlx at runtime (a pure `cargo build`
//! cannot). Skipped unless `RUSTLENS_TEST_PG_URL` is set (no-op in CI runs with
//! no database configured for this crate).

use rustlens_db::admin_actions::{record_audit, AuditEntry};
use rustlens_db::api_keys::{self, ApiKeyCreate};

#[tokio::test]
async fn client_ip_round_trips_into_inet_columns() {
    let Ok(url) = std::env::var("RUSTLENS_TEST_PG_URL") else {
        eprintln!("RUSTLENS_TEST_PG_URL unset, skipping client-IP integration test");
        return;
    };
    let pool = rustlens_db::connect(&url, 5).await.expect("connect");
    rustlens_db::migrate(&pool).await.expect("migrate");

    // Isolated fixtures so re-runs are clean.
    let user_id = "clientip-it-user";
    let discord_id = "clientip-it-discord";
    sqlx::query("DELETE FROM api_keys WHERE created_by_user_id = $1")
        .bind(user_id)
        .execute(&pool)
        .await
        .unwrap();
    sqlx::query("DELETE FROM audit_events WHERE actor_user_id = $1")
        .bind(user_id)
        .execute(&pool)
        .await
        .unwrap();
    sqlx::query("DELETE FROM app_users WHERE id = $1").bind(user_id).execute(&pool).await.unwrap();
    sqlx::query(
        "INSERT INTO app_users (id, discord_id, discord_username) VALUES ($1, $2, 'clientip-it')",
    )
    .bind(user_id)
    .bind(discord_id)
    .execute(&pool)
    .await
    .unwrap();

    // --- api_keys.last_used_ip ---
    let key = api_keys::create(
        &pool,
        ApiKeyCreate {
            name: "clientip-it",
            prefix: "rl_admin_itkey1",
            last4: "aaaa",
            secret_hash: &[0u8; 32],
            created_by_user_id: user_id,
            permissions: &[],
            expires_at: None,
        },
    )
    .await
    .expect("create api key");

    // A real IPv4 is stored; the `$N::inet` cast accepts the text bind. The read
    // helper's `host(...)` projection renders the bare IP (no `/32` netmask).
    api_keys::update_last_used(&pool, &key.id, "203.0.113.7").await.expect("update ipv4");
    let read = api_keys::get_by_prefix(&pool, &key.prefix).await.expect("read key");
    assert_eq!(read.last_used_ip.as_deref(), Some("203.0.113.7"));

    // Empty string stores NULL (the trust-off / unparseable path).
    api_keys::update_last_used(&pool, &key.id, "").await.expect("update null");
    let read_null = api_keys::get_by_prefix(&pool, &key.prefix).await.expect("read key null");
    assert_eq!(read_null.last_used_ip, None);

    // --- audit_events.ip_address (IPv6, to exercise the inet parser too) ---
    let params = serde_json::json!({});
    let entry = AuditEntry {
        action_type: "roles.update",
        actor_user_id: user_id,
        actor_role_snapshot: &[],
        resource_type: "role",
        resource_id: "r1",
        target_user_id: None,
        params: &params,
        reason: Some("client-ip it"),
        result: "success",
        error_message: None,
        ip: Some("2001:db8::1"),
        user_agent: Some("client-ip-it/1.0"),
        via_api_key_id: None,
        via_api_key_name: None,
    };
    record_audit(&pool, &entry).await.expect("record audit with ip");
    let audit_ip: Option<String> = sqlx::query_scalar(
        "SELECT host(ip_address) FROM audit_events WHERE actor_user_id = $1 ORDER BY created_at DESC LIMIT 1",
    )
    .bind(user_id)
    .fetch_one(&pool)
    .await
    .unwrap();
    assert_eq!(audit_ip.as_deref(), Some("2001:db8::1"));

    // A None ip stores NULL (unchanged from the pre-port behavior).
    let entry_null = AuditEntry { ip: None, ..entry };
    record_audit(&pool, &entry_null).await.expect("record audit without ip");
    let audit_ip_null: Option<String> = sqlx::query_scalar(
        "SELECT host(ip_address) FROM audit_events WHERE actor_user_id = $1 ORDER BY created_at DESC LIMIT 1",
    )
    .bind(user_id)
    .fetch_one(&pool)
    .await
    .unwrap();
    assert_eq!(audit_ip_null, None);

    // Cleanup.
    sqlx::query("DELETE FROM api_keys WHERE created_by_user_id = $1").bind(user_id).execute(&pool).await.unwrap();
    sqlx::query("DELETE FROM audit_events WHERE actor_user_id = $1").bind(user_id).execute(&pool).await.unwrap();
    sqlx::query("DELETE FROM app_users WHERE id = $1").bind(user_id).execute(&pool).await.unwrap();
}
