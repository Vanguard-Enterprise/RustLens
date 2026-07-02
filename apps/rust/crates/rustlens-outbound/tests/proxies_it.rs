//! Integration test for the `outbound_proxies` read path + AES-GCM decrypt,
//! against a real Postgres. Skipped (returns early) unless `RUSTLENS_TEST_PG_URL`
//! is set, so it is a no-op in normal `cargo test` / CI runs with no database.

use rustlens_crypto::Cipher;
use rustlens_outbound::load_initial_endpoints;

// A fixed base64-encoded 32-byte key (32 zero bytes) for the round-trip.
const KEY_B64: &str = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=";
const ENDPOINT: &str = "http://user:pass@proxy.example:8080";
const REDACTED: &str = "http://proxy.example:8080";

#[tokio::test]
async fn list_enabled_and_load_initial_endpoints_decrypt() {
    let Ok(url) = std::env::var("RUSTLENS_TEST_PG_URL") else {
        eprintln!("RUSTLENS_TEST_PG_URL unset — skipping outbound proxies integration test");
        return;
    };
    let pool = rustlens_db::connect(&url, 5).await.expect("connect");

    sqlx::query(
        r#"CREATE TABLE IF NOT EXISTS outbound_proxies (
            id BIGSERIAL PRIMARY KEY,
            redacted_endpoint TEXT NOT NULL,
            endpoint_ciphertext BYTEA NOT NULL,
            endpoint_nonce BYTEA NOT NULL,
            key_id TEXT NOT NULL DEFAULT 'default',
            enabled BOOLEAN NOT NULL DEFAULT TRUE,
            created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
            updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
            auto_disabled_at TIMESTAMPTZ
        )"#,
    )
    .execute(&pool)
    .await
    .unwrap();
    sqlx::query("DELETE FROM outbound_proxies WHERE redacted_endpoint LIKE '%proxy.example%'")
        .execute(&pool)
        .await
        .unwrap();

    let cipher = Cipher::from_base64_key(KEY_B64).expect("cipher");
    let (ct, nonce) = cipher.encrypt(ENDPOINT).expect("encrypt");

    // One enabled row (our endpoint) + one disabled row that must be ignored.
    sqlx::query(
        "INSERT INTO outbound_proxies (redacted_endpoint, endpoint_ciphertext, endpoint_nonce, enabled)
         VALUES ($1, $2, $3, TRUE)",
    )
    .bind(REDACTED)
    .bind(&ct)
    .bind(&nonce)
    .execute(&pool)
    .await
    .unwrap();
    sqlx::query(
        "INSERT INTO outbound_proxies (redacted_endpoint, endpoint_ciphertext, endpoint_nonce, enabled)
         VALUES ('http://proxy.example:9999', $1, $2, FALSE)",
    )
    .bind(&ct)
    .bind(&nonce)
    .execute(&pool)
    .await
    .unwrap();

    // list_enabled returns only the enabled row, with the redacted key intact.
    let rows = rustlens_db::outbound_proxies::list_enabled(&pool).await.unwrap();
    let ours: Vec<_> = rows.iter().filter(|r| r.redacted_endpoint == REDACTED).collect();
    assert_eq!(ours.len(), 1, "exactly one enabled row for our endpoint");
    assert!(
        !rows.iter().any(|r| r.redacted_endpoint == "http://proxy.example:9999"),
        "disabled rows must be excluded"
    );

    // load_initial_endpoints decrypts DB rows back to the credentialed URL and
    // prefers them over the (ignored) CSV fallback.
    let endpoints = load_initial_endpoints(&pool, Some(&cipher), true, &["http://ignored:1".into()])
        .await
        .unwrap();
    assert!(
        endpoints.contains(&ENDPOINT.to_string()),
        "decrypted endpoint must round-trip; got {endpoints:?}"
    );
    assert!(
        !endpoints.iter().any(|e| e.contains("ignored")),
        "DB proxies must take precedence over the CSV fallback"
    );

    // disable_by_redacted_endpoint flips our row off and stamps auto_disabled_at.
    let disabled = rustlens_db::outbound_proxies::disable_by_redacted_endpoint(&pool, REDACTED)
        .await
        .unwrap();
    assert!(disabled, "row should have been disabled");
    let after = rustlens_db::outbound_proxies::list_enabled(&pool).await.unwrap();
    assert!(
        !after.iter().any(|r| r.redacted_endpoint == REDACTED),
        "disabled endpoint must drop out of list_enabled"
    );

    sqlx::query("DELETE FROM outbound_proxies WHERE redacted_endpoint LIKE '%proxy.example%'")
        .execute(&pool)
        .await
        .unwrap();
}
