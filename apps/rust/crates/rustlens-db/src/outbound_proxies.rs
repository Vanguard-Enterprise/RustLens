//! Read access to `outbound_proxies` (owned/written by the future `servers`
//! service; the Go backend still owns the schema). Rows store the proxy URL
//! encrypted (AES-GCM via `rustlens-crypto`, key `PROXY_ENCRYPTION_KEY`); only
//! the credential-free `redacted_endpoint` is ever logged/exposed.
//!
//! Phase 1 needs just the two queries the outbound provider uses: list the
//! enabled rows (decrypted by the caller) and disable a row by its redacted
//! endpoint (the auto-disable callback). Full CRUD lands with the `servers`
//! service.

use chrono::{DateTime, Utc};
use sqlx::{PgPool, Postgres, QueryBuilder, Row};

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

// ---- full CRUD (servers service) ----

#[derive(Debug, thiserror::Error)]
pub enum ProxyError {
    #[error("proxy already exists")]
    AlreadyExists,
    #[error("proxy not found")]
    NotFound,
    #[error(transparent)]
    Db(#[from] sqlx::Error),
}

/// Metadata for a proxy row (no credentials). `status` is derived in the handler.
#[derive(Debug, Clone)]
pub struct OutboundProxyMeta {
    pub id: i64,
    pub redacted_endpoint: String,
    pub enabled: bool,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
    pub auto_disabled_at: Option<DateTime<Utc>>,
}

const META_COLS: &str = "id, redacted_endpoint, enabled, created_at, updated_at, auto_disabled_at";

fn meta_row(row: &sqlx::postgres::PgRow) -> Result<OutboundProxyMeta, sqlx::Error> {
    Ok(OutboundProxyMeta {
        id: row.try_get("id")?,
        redacted_endpoint: row.try_get("redacted_endpoint")?,
        enabled: row.try_get("enabled")?,
        created_at: row.try_get("created_at")?,
        updated_at: row.try_get("updated_at")?,
        auto_disabled_at: row.try_get("auto_disabled_at")?,
    })
}

/// Create a proxy (`ON CONFLICT (redacted_endpoint) DO NOTHING`); `AlreadyExists`
/// when the redacted endpoint is taken.
pub async fn create(
    pool: &PgPool,
    redacted_endpoint: &str,
    ciphertext: &[u8],
    nonce: &[u8],
    key_id: &str,
) -> Result<OutboundProxyMeta, ProxyError> {
    let row = sqlx::query(&format!(
        r#"INSERT INTO outbound_proxies (redacted_endpoint, endpoint_ciphertext, endpoint_nonce, key_id)
           VALUES ($1, $2, $3, $4)
           ON CONFLICT (redacted_endpoint) DO NOTHING
           RETURNING {META_COLS}"#
    ))
    .bind(redacted_endpoint)
    .bind(ciphertext)
    .bind(nonce)
    .bind(key_id)
    .fetch_optional(pool)
    .await?;
    match row {
        Some(row) => Ok(meta_row(&row)?),
        None => Err(ProxyError::AlreadyExists),
    }
}

pub async fn delete(pool: &PgPool, id: i64) -> Result<(), ProxyError> {
    let res = sqlx::query("DELETE FROM outbound_proxies WHERE id = $1")
        .bind(id)
        .execute(pool)
        .await?;
    if res.rows_affected() == 0 {
        return Err(ProxyError::NotFound);
    }
    Ok(())
}

/// Enable/disable a proxy; enabling also clears `auto_disabled_at`.
pub async fn set_enabled(pool: &PgPool, id: i64, enabled: bool) -> Result<OutboundProxyMeta, ProxyError> {
    let row = sqlx::query(&format!(
        r#"UPDATE outbound_proxies
           SET enabled = $2,
               auto_disabled_at = CASE WHEN $2 THEN NULL ELSE auto_disabled_at END,
               updated_at = NOW()
           WHERE id = $1
           RETURNING {META_COLS}"#
    ))
    .bind(id)
    .bind(enabled)
    .fetch_optional(pool)
    .await?;
    row.as_ref().map(meta_row).transpose()?.ok_or(ProxyError::NotFound)
}

/// Re-enable + clear the auto-disabled marker (admin "clear broken").
pub async fn clear_auto_disabled(pool: &PgPool, id: i64) -> Result<OutboundProxyMeta, ProxyError> {
    let row = sqlx::query(&format!(
        r#"UPDATE outbound_proxies
           SET enabled = TRUE, auto_disabled_at = NULL, updated_at = NOW()
           WHERE id = $1
           RETURNING {META_COLS}"#
    ))
    .bind(id)
    .fetch_optional(pool)
    .await?;
    row.as_ref().map(meta_row).transpose()?.ok_or(ProxyError::NotFound)
}

/// List all proxies (enabled + disabled), broken-first then disabled then enabled.
pub async fn list_all(pool: &PgPool) -> Result<Vec<OutboundProxyMeta>, sqlx::Error> {
    let rows = sqlx::query(&format!(
        "SELECT {META_COLS} FROM outbound_proxies ORDER BY (auto_disabled_at IS NOT NULL) DESC, enabled ASC, id ASC"
    ))
    .fetch_all(pool)
    .await?;
    rows.iter().map(meta_row).collect()
}

/// Paginated admin list with optional `q` (redacted substring) + `status`
/// (`active`/`paused`/`broken`) filter, plus total count.
pub async fn list_page(
    pool: &PgPool,
    q: Option<&str>,
    status: Option<&str>,
    limit: i64,
    offset: i64,
) -> Result<(Vec<OutboundProxyMeta>, i64), sqlx::Error> {
    fn apply<'a>(qb: &mut QueryBuilder<'a, Postgres>, q: Option<&'a str>, status: Option<&str>) {
        qb.push(" WHERE 1=1");
        if let Some(q) = q.map(str::trim).filter(|s| !s.is_empty()) {
            qb.push(" AND redacted_endpoint ILIKE ").push_bind(format!("%{q}%"));
        }
        match status {
            Some("active") => { qb.push(" AND enabled = TRUE AND auto_disabled_at IS NULL"); }
            Some("paused") => { qb.push(" AND enabled = FALSE AND auto_disabled_at IS NULL"); }
            Some("broken") => { qb.push(" AND auto_disabled_at IS NOT NULL"); }
            _ => {}
        }
    }

    let mut count_qb: QueryBuilder<Postgres> = QueryBuilder::new("SELECT COUNT(*) FROM outbound_proxies");
    apply(&mut count_qb, q, status);
    let total: i64 = count_qb.build_query_scalar().fetch_one(pool).await?;

    let mut qb: QueryBuilder<Postgres> = QueryBuilder::new(format!("SELECT {META_COLS} FROM outbound_proxies"));
    apply(&mut qb, q, status);
    qb.push(" ORDER BY (auto_disabled_at IS NOT NULL) DESC, enabled ASC, id ASC LIMIT ")
        .push_bind(limit)
        .push(" OFFSET ")
        .push_bind(offset);
    let rows = qb.build().fetch_all(pool).await?;
    let metas = rows.iter().map(meta_row).collect::<Result<Vec<_>, _>>()?;
    Ok((metas, total))
}
