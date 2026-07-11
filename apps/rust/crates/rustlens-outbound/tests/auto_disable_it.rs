//! DB-gated integration test for the auto-disable callback *pattern* the
//! `servers` and `players` services wire onto their `Provider` in `main.rs`
//! (Go `cmd/server/main.go` ~line 129, `SetAutoDisableCallback`): persist the
//! disable via `disable_by_redacted_endpoint`, then reload the provider from
//! the DB via `load_initial_endpoints` + `Provider::replace`.
//!
//! This drives the real trigger path (proxied requests failing through
//! `Provider::send`, not the private `record_result` the crate's own unit
//! tests call directly) against a bare TCP stub that always answers 502, so
//! the auto-disable threshold in `record_result` actually fires and invokes
//! the callback we install here. Skipped (returns early) unless
//! `RUSTLENS_TEST_PG_URL` is set, so it is a no-op in normal `cargo test` / CI
//! runs with no database.

use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;
use std::time::Duration;

use reqwest::{Method, Request};
use rustlens_crypto::Cipher;
use rustlens_outbound::{load_initial_endpoints, Provider, PROXY_HEALTH_MIN_REQUESTS};
use tokio::io::{AsyncReadExt, AsyncWriteExt};
use tokio::net::TcpListener;

// A fixed base64-encoded 32-byte key (32 zero bytes), matching the pattern in
// `proxies_it.rs`.
const KEY_B64: &str = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=";

/// Spawn a bare TCP listener that answers every connection with a 502 (mirrors
/// the Go provider test's `httptest.Server` acting as a mis-behaving proxy).
/// Returns the `http://127.0.0.1:{port}` endpoint the provider should proxy
/// through.
async fn spawn_failing_proxy() -> String {
    let listener = TcpListener::bind("127.0.0.1:0").await.expect("bind stub proxy");
    let addr = listener.local_addr().expect("stub proxy addr");
    tokio::spawn(async move {
        loop {
            let Ok((mut socket, _)) = listener.accept().await else {
                break;
            };
            tokio::spawn(async move {
                let mut buf = [0u8; 1024];
                // Best-effort read of the request line so the client sees a
                // clean response instead of a connection reset.
                let _ = socket.read(&mut buf).await;
                let resp = b"HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\nConnection: close\r\n\r\n";
                let _ = socket.write_all(resp).await;
                let _ = socket.shutdown().await;
            });
        }
    });
    format!("http://{addr}")
}

#[tokio::test]
async fn callback_fires_persists_disable_and_reloads_provider() {
    let Ok(url) = std::env::var("RUSTLENS_TEST_PG_URL") else {
        eprintln!("RUSTLENS_TEST_PG_URL unset, skipping auto-disable integration test");
        return;
    };
    let pool = rustlens_db::connect(&url, 5).await.expect("connect");

    sqlx::query(
        r#"CREATE TABLE IF NOT EXISTS outbound_proxies (
            id BIGSERIAL PRIMARY KEY,
            redacted_endpoint TEXT NOT NULL UNIQUE,
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

    let proxy_url = spawn_failing_proxy().await;
    // No credentials on the stub endpoint, so the redacted form is identical.
    let redacted = proxy_url.clone();
    sqlx::query("DELETE FROM outbound_proxies WHERE redacted_endpoint = $1")
        .bind(&redacted)
        .execute(&pool)
        .await
        .unwrap();

    let cipher = Cipher::from_base64_key(KEY_B64).expect("cipher");
    let (ct, nonce) = cipher.encrypt(&proxy_url).expect("encrypt");
    sqlx::query(
        "INSERT INTO outbound_proxies (redacted_endpoint, endpoint_ciphertext, endpoint_nonce, enabled)
         VALUES ($1, $2, $3, TRUE)",
    )
    .bind(&redacted)
    .bind(&ct)
    .bind(&nonce)
    .execute(&pool)
    .await
    .unwrap();

    let provider = Arc::new(
        Provider::new(true, std::slice::from_ref(&proxy_url), Duration::from_secs(2)).expect("provider"),
    );
    assert_eq!(provider.proxy_count(), 1, "provider should start with the one seeded proxy");

    // Wire the same persist -> reload -> log pattern the servers/players
    // services install on their provider (mirrors Go's
    // `SetAutoDisableCallback`); signal completion over a channel since the
    // callback runs on a spawned task.
    let (tx, mut rx) = tokio::sync::mpsc::channel::<()>(1);
    let ran_ok = Arc::new(AtomicBool::new(false));
    {
        let cb_pool = pool.clone();
        let cb_cipher = cipher.clone();
        let cb_provider = provider.clone();
        let cb_ran_ok = ran_ok.clone();
        provider.set_auto_disable_callback(Arc::new(move |redacted_endpoint: String| {
            let pool = cb_pool.clone();
            let cipher = cb_cipher.clone();
            let provider = cb_provider.clone();
            let ran_ok = cb_ran_ok.clone();
            let tx = tx.clone();
            Box::pin(async move {
                let disabled = rustlens_db::outbound_proxies::disable_by_redacted_endpoint(
                    &pool,
                    &redacted_endpoint,
                )
                .await
                .expect("persist disable");
                if !disabled {
                    // Already disabled by a concurrent trigger; nothing to do.
                    let _ = tx.send(()).await;
                    return;
                }
                let endpoints = load_initial_endpoints(&pool, Some(&cipher), false, &[])
                    .await
                    .expect("reload endpoints");
                provider
                    .replace(!endpoints.is_empty(), &endpoints)
                    .expect("apply reload");
                ran_ok.store(true, Ordering::SeqCst);
                let _ = tx.send(()).await;
            })
        }));
    }

    // Drive real proxied requests through the stub until the health window
    // crosses the >=50%-of->=10 threshold and the callback fires.
    for _ in 0..PROXY_HEALTH_MIN_REQUESTS {
        let req = Request::new(Method::GET, "http://example.invalid/test".parse().unwrap());
        let resp = provider.send(req).await.expect("proxied request should get a response");
        assert_eq!(resp.status().as_u16(), 502);
    }

    tokio::time::timeout(Duration::from_secs(5), rx.recv())
        .await
        .expect("callback should fire within 5s")
        .expect("callback channel should not close early");

    assert!(ran_ok.load(Ordering::SeqCst), "callback should have completed the persist+reload path");
    assert_eq!(provider.proxy_count(), 0, "the auto-disabled proxy must drop out of the provider");

    let remaining = rustlens_db::outbound_proxies::list_enabled(&pool).await.unwrap();
    assert!(
        !remaining.iter().any(|r| r.redacted_endpoint == redacted),
        "the auto-disabled row must no longer be enabled in the DB"
    );

    sqlx::query("DELETE FROM outbound_proxies WHERE redacted_endpoint = $1")
        .bind(&redacted)
        .execute(&pool)
        .await
        .unwrap();
}
