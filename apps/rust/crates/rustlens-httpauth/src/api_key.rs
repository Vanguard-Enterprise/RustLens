//! Admin API-key secret generation, hashing, and authentication, mirroring the
//! Go `internal/appauth/api_key_middleware.go` + `internal/admin/actions/
//! api_keys_create.go`.
//!
//! Wire contract (shared DB, so keys minted by Go validate here and vice versa):
//!   - Secret shape: `"rl_admin_"` + 32 chars from a fixed 62-char alphabet.
//!   - `prefix` = the first 16 chars of the secret (the `rl_admin_` word plus 7
//!     random chars); `last4` = the final 4 chars.
//!   - Stored `secret_hash` = raw `sha256(secret)` bytes.
//!   - Auth header: `Authorization: Bearer rl_admin_...`; the prefix is looked
//!     up, then the full sha256 is constant-time compared.

use axum::http::{HeaderMap, StatusCode, header};
use chrono::Utc;
use rand::Rng;
use sha2::{Digest, Sha256};
use subtle::ConstantTimeEq;

use rustlens_db::PgPool;
use rustlens_db::api_keys;

use crate::{AuthError, err_json};

/// The literal word every admin API key starts with (Go `apiKeyPrefixWord`).
pub const API_KEY_PREFIX_WORD: &str = "rl_admin_";
/// Prefix length stored/looked up: `rl_admin_` (9) + 7 random chars = 16.
pub const API_KEY_PREFIX_LEN: usize = 16;
/// Number of random chars appended after the prefix word (Go `apiKeySecretBytes`).
const API_KEY_SECRET_CHARS: usize = 32;
/// The Authorization-header prefix that routes a request to API-key auth.
const AUTH_HEADER_PREFIX: &str = "Bearer rl_admin_";
/// Alphabet for the random tail (Go `alphabet`, order significant only for RNG).
const ALPHABET: &[u8] = b"0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz";

/// A freshly minted secret plus its stored `prefix` / `last4` (Go
/// `generateAPIKeySecret`).
pub struct GeneratedSecret {
    pub secret: String,
    pub prefix: String,
    pub last4: String,
}

/// Generate a new API-key secret. Uses the OS CSPRNG (`rand::thread_rng`), the
/// same fixed alphabet as Go, so both mints are interchangeable.
pub fn generate_secret() -> GeneratedSecret {
    let mut rng = rand::thread_rng();
    let mut secret = String::with_capacity(API_KEY_PREFIX_WORD.len() + API_KEY_SECRET_CHARS);
    secret.push_str(API_KEY_PREFIX_WORD);
    for _ in 0..API_KEY_SECRET_CHARS {
        let idx = rng.gen_range(0..ALPHABET.len());
        secret.push(ALPHABET[idx] as char);
    }
    let prefix = secret[..API_KEY_PREFIX_LEN].to_string();
    let last4 = secret[secret.len() - 4..].to_string();
    GeneratedSecret { secret, prefix, last4 }
}

/// `sha256(secret)` raw bytes, byte-identical to Go's `sha256.Sum256`.
pub fn hash_secret(secret: &str) -> Vec<u8> {
    let mut hasher = Sha256::new();
    hasher.update(secret.as_bytes());
    hasher.finalize().to_vec()
}

/// The resolved actor when a request authenticates via an admin API key.
#[derive(Debug, Clone)]
pub struct ApiKeyActor {
    pub key_id: String,
    pub key_name: String,
    pub user_id: String,
    /// The key's snapshot permission set; effective admin permissions are the
    /// intersection of this with the creator's *current* role permissions.
    pub permissions: Vec<String>,
}

/// Whether the request carries an admin API-key `Authorization` header (Go
/// `strings.HasPrefix(hdr, apiKeyAuthHeaderPrefix)`). When false, callers fall
/// back to the app-secret + session gate.
pub fn has_api_key_header(headers: &HeaderMap) -> bool {
    headers
        .get(header::AUTHORIZATION)
        .and_then(|v| v.to_str().ok())
        .map(|h| h.starts_with(AUTH_HEADER_PREFIX))
        .unwrap_or(false)
}

/// Authenticate an admin API key from the `Authorization` header. Mirrors the Go
/// middleware's validation order + error codes exactly:
///   - malformed / unknown prefix / hash mismatch → 401 `invalid_api_key`
///   - revoked                                     → 401 `api_key_revoked`
///   - expired                                     → 401 `api_key_expired`
///   - creator suspended/deleted (auto-revokes)    → 401 `api_key_revoked`
///   - status lookup failure                       → 500 `internal`
///
/// On success it records a best-effort, debounced `last_used_at` (inline, only
/// when the per-process debounce window has elapsed).
pub async fn authenticate_api_key(
    headers: &HeaderMap,
    pool: &PgPool,
) -> Result<ApiKeyActor, AuthError> {
    let hdr = headers
        .get(header::AUTHORIZATION)
        .and_then(|v| v.to_str().ok())
        .unwrap_or("");
    // Caller should gate on has_api_key_header; defend anyway.
    let secret = hdr.strip_prefix("Bearer ").unwrap_or("");
    if secret.len() < API_KEY_PREFIX_LEN {
        return Err(err_json(StatusCode::UNAUTHORIZED, "invalid_api_key"));
    }
    let prefix = &secret[..API_KEY_PREFIX_LEN];

    let key = match api_keys::get_by_prefix(pool, prefix).await {
        Ok(k) => k,
        Err(api_keys::ApiKeyError::NotFound) => {
            return Err(err_json(StatusCode::UNAUTHORIZED, "invalid_api_key"));
        }
        Err(api_keys::ApiKeyError::Db(e)) => {
            tracing::error!("api-key auth: lookup failed: {e:#}");
            return Err(err_json(StatusCode::UNAUTHORIZED, "invalid_api_key"));
        }
    };

    let presented = hash_secret(secret);
    if presented.ct_eq(&key.secret_hash).unwrap_u8() != 1 {
        return Err(err_json(StatusCode::UNAUTHORIZED, "invalid_api_key"));
    }
    if key.revoked_at.is_some() {
        return Err(err_json(StatusCode::UNAUTHORIZED, "api_key_revoked"));
    }
    if let Some(exp) = key.expires_at {
        if exp < Utc::now() {
            return Err(err_json(StatusCode::UNAUTHORIZED, "api_key_expired"));
        }
    }

    match api_keys::load_user_status(pool, &key.created_by_user_id).await {
        Ok(status) => {
            if !status.active {
                let reason = if status.deleted {
                    "creator_deleted"
                } else if status.suspended {
                    "creator_suspended"
                } else {
                    "creator_inactive"
                };
                let _ = api_keys::revoke(pool, &key.id, reason).await;
                return Err(err_json(StatusCode::UNAUTHORIZED, "api_key_revoked"));
            }
        }
        Err(e) => {
            tracing::error!("api-key auth: creator status lookup failed: {e:#}");
            return Err(err_json(StatusCode::INTERNAL_SERVER_ERROR, "internal"));
        }
    }

    maybe_update_last_used(pool, &key.id).await;

    Ok(ApiKeyActor {
        key_id: key.id,
        key_name: key.name,
        user_id: key.created_by_user_id,
        permissions: key.permissions,
    })
}

// ---------------------------------------------------------------------------
// Best-effort debounced last-used update (Go's 60s in-memory debounce).
// ---------------------------------------------------------------------------

use std::collections::HashMap;
use std::sync::{Mutex, OnceLock};
use std::time::{Duration, Instant};

const LAST_USED_DEBOUNCE: Duration = Duration::from_secs(60);

fn last_used_map() -> &'static Mutex<HashMap<String, Instant>> {
    static MAP: OnceLock<Mutex<HashMap<String, Instant>>> = OnceLock::new();
    MAP.get_or_init(|| Mutex::new(HashMap::new()))
}

/// Write `last_used_at` at most once per key per debounce window. The write is
/// awaited inline (rare), so no runtime handle is required. IP is left NULL:
/// behind Traefik the Go path records the proxy hop; a faithful client IP would
/// need `ConnectInfo` wiring, which is out of scope for A4.
async fn maybe_update_last_used(pool: &PgPool, key_id: &str) {
    {
        let mut map = match last_used_map().lock() {
            Ok(m) => m,
            Err(_) => return,
        };
        let now = Instant::now();
        if let Some(last) = map.get(key_id) {
            if now.duration_since(*last) < LAST_USED_DEBOUNCE {
                return;
            }
        }
        map.insert(key_id.to_string(), now);
        // Opportunistic prune so the map cannot grow unbounded.
        map.retain(|_, t| now.duration_since(*t) < LAST_USED_DEBOUNCE);
    }
    if let Err(e) = api_keys::update_last_used(pool, key_id, "").await {
        tracing::warn!("api-key auth: last-used update failed: {e:#}");
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn generated_secret_shape_matches_go() {
        let g = generate_secret();
        assert!(g.secret.starts_with(API_KEY_PREFIX_WORD));
        assert_eq!(g.secret.len(), API_KEY_PREFIX_WORD.len() + API_KEY_SECRET_CHARS);
        // prefix is exactly the first 16 chars.
        assert_eq!(g.prefix.len(), API_KEY_PREFIX_LEN);
        assert_eq!(g.prefix, &g.secret[..API_KEY_PREFIX_LEN]);
        assert!(g.prefix.starts_with(API_KEY_PREFIX_WORD));
        // last4 is exactly the final 4 chars.
        assert_eq!(g.last4.len(), 4);
        assert_eq!(g.last4, &g.secret[g.secret.len() - 4..]);
        // random tail is drawn from the fixed alphabet.
        assert!(g.secret[API_KEY_PREFIX_WORD.len()..]
            .bytes()
            .all(|b| ALPHABET.contains(&b)));
    }

    #[test]
    fn generated_secrets_are_unique() {
        let a = generate_secret();
        let b = generate_secret();
        assert_ne!(a.secret, b.secret);
    }

    #[test]
    fn hash_secret_matches_known_sha256() {
        // Precomputed `sha256("rl_admin_test")` (via GNU sha256sum). Guards the
        // wire contract with Go's `sha256.Sum256`.
        let hex: String = hash_secret("rl_admin_test")
            .iter()
            .map(|b| format!("{b:02x}"))
            .collect();
        assert_eq!(
            hex,
            "027f03d7908a67a1be26f51c8b114bd193ebee055a2aaf24ff46f8d1bfbef087"
        );
    }

    #[test]
    fn hash_round_trips_via_constant_time_compare() {
        let g = generate_secret();
        let stored = hash_secret(&g.secret);
        // Hashing the same secret again matches under constant-time compare.
        let presented = hash_secret(&g.secret);
        assert_eq!(presented.ct_eq(&stored).unwrap_u8(), 1);
        // A different secret does not.
        let other = hash_secret("rl_admin_totally_different");
        assert_eq!(other.ct_eq(&stored).unwrap_u8(), 0);
        // Hash is 32 bytes (sha256).
        assert_eq!(stored.len(), 32);
    }
}
