//! Read access to `outbound_proxies` (owned/written by the future `servers`
//! service; the Go backend still owns the schema). Rows store the proxy URL
//! encrypted (AES-GCM via `rustlens-crypto`, key `PROXY_ENCRYPTION_KEY`); only
//! the credential-free `redacted_endpoint` is ever logged/exposed.
//!
//! Phase 1 needs just the two queries the outbound provider uses: list the
//! enabled rows (decrypted by the caller) and disable a row by its redacted
//! endpoint (the auto-disable callback). Full CRUD lands with the `servers`
//! service.

use sqlx::{PgPool, Row};

/// One `outbound_proxies` row. `ciphertext`/`nonce` decrypt to the full
/// `scheme://user:pass@host:port` endpoint; `redacted_endpoint` is the
/// credential-free `scheme://host:port` used as the health-map key + log value.
#[derive(Debug, Clone)]
pub struct OutboundProxyRow {
    pub id: i64,
    pub redacted_endpoint: String,
    pub ciphertext: Vec<u8>,
    pub nonce: Vec<u8>,
}

/// List enabled proxies, oldest first (Go `ListEnabledOutboundProxies`,
/// `WHERE enabled = TRUE ORDER BY id ASC`).
pub async fn list_enabled(pool: &PgPool) -> Result<Vec<OutboundProxyRow>, sqlx::Error> {
    let rows = sqlx::query(
        r#"SELECT id, redacted_endpoint, endpoint_ciphertext, endpoint_nonce
           FROM outbound_proxies
           WHERE enabled = TRUE
           ORDER BY id ASC"#,
    )
    .fetch_all(pool)
    .await?;

    rows.into_iter()
        .map(|row| {
            Ok(OutboundProxyRow {
                id: row.try_get("id")?,
                redacted_endpoint: row.try_get("redacted_endpoint")?,
                ciphertext: row.try_get("endpoint_ciphertext")?,
                nonce: row.try_get("endpoint_nonce")?,
            })
        })
        .collect()
}

/// Disable a proxy by its redacted endpoint, stamping `auto_disabled_at` if not
/// already set (Go `DisableOutboundProxyByRedactedEndpoint`). Returns `true`
/// when a row was affected. Used only by the auto-disable callback.
pub async fn disable_by_redacted_endpoint(
    pool: &PgPool,
    redacted_endpoint: &str,
) -> Result<bool, sqlx::Error> {
    let result = sqlx::query(
        r#"UPDATE outbound_proxies
           SET enabled = FALSE,
               auto_disabled_at = COALESCE(auto_disabled_at, NOW()),
               updated_at = NOW()
           WHERE redacted_endpoint = $1 AND (enabled = TRUE OR auto_disabled_at IS NULL)"#,
    )
    .bind(redacted_endpoint)
    .execute(pool)
    .await?;
    Ok(result.rows_affected() > 0)
}
