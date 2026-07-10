//! Real-client-IP extraction from a trusted edge header, ported from the Go
//! backend's `internal/clientip/clientip.go`.
//!
//! The Rust fleet sits behind Traefik and (for `/api/app/*`) the Next.js
//! server, so a request's peer socket address is never the real client and the
//! services don't wire `axum`'s `ConnectInfo`. As in Go, the only trustworthy
//! source is an edge-injected header (e.g. Cloudflare's `CF-Connecting-IP`),
//! and trusting it is safe only when the edge strips inbound copies before
//! re-injecting. So it is opt-in via `TRUST_PROXY_HEADERS`, with a configurable
//! header name (`TRUST_PROXY_IP_HEADER`, default `CF-Connecting-IP`). With
//! trust off (the default) no IP is recorded, exactly like the pre-port
//! behavior where `api_keys.last_used_ip` / `audit_events.ip_address` stayed
//! NULL.
//!
//! The trust config is installed once per process from the service config (see
//! [`init`]) so the auth helpers and the admin dispatcher can resolve the
//! acting IP without threading config through every signature.

use std::net::IpAddr;
use std::sync::OnceLock;

use axum::http::HeaderMap;

struct Trust {
    enabled: bool,
    /// Lowercased header name. `axum`/`http` compares header names
    /// case-insensitively, but we normalize so an empty value is unambiguous.
    header: String,
}

static TRUST: OnceLock<Trust> = OnceLock::new();

/// Install the process-wide client-IP trust config. Idempotent: only the first
/// call wins, which is fine since every binary sets it once in `main()` before
/// serving. `header` is matched case-insensitively.
pub fn init(enabled: bool, header: &str) {
    let _ = TRUST.set(Trust {
        enabled,
        header: header.trim().to_ascii_lowercase(),
    });
}

/// Resolve the real client IP for a request, or `None` when trust is disabled
/// (or never configured), the header is absent, or its value is not a valid IP.
///
/// Mirrors Go `clientip.Extract` + `firstAddr`: it reads the configured header,
/// takes the first comma-separated token (some edges chain values), trims it,
/// and validates it parses as an IP. Go additionally falls back to
/// `RemoteAddr`, but the services don't wire `ConnectInfo`, so an untrusted or
/// unparseable value yields `None` (recorded as NULL) rather than the proxy hop.
pub fn extract(headers: &HeaderMap) -> Option<String> {
    let trust = TRUST.get()?;
    resolve(headers, trust.enabled, &trust.header)
}

fn resolve(headers: &HeaderMap, enabled: bool, header: &str) -> Option<String> {
    if !enabled || header.is_empty() {
        return None;
    }
    let raw = headers.get(header)?.to_str().ok()?;
    first_addr(raw)
}

/// First comma-separated token, trimmed, validated as an IP (Go `firstAddr`).
/// Returns the original trimmed token (not the reserialized parse) so the
/// stored value matches what the edge sent.
fn first_addr(raw: &str) -> Option<String> {
    let first = raw.split(',').next().unwrap_or("").trim();
    if first.is_empty() {
        return None;
    }
    first.parse::<IpAddr>().ok().map(|_| first.to_string())
}

#[cfg(test)]
mod tests {
    use super::*;
    use axum::http::HeaderValue;

    fn headers_with(name: &str, value: &str) -> HeaderMap {
        let mut h = HeaderMap::new();
        h.insert(
            axum::http::HeaderName::from_bytes(name.as_bytes()).unwrap(),
            HeaderValue::from_str(value).unwrap(),
        );
        h
    }

    #[test]
    fn first_addr_takes_first_valid_token() {
        assert_eq!(first_addr("1.2.3.4").as_deref(), Some("1.2.3.4"));
        // Chained edges: first token wins.
        assert_eq!(first_addr("1.2.3.4, 5.6.7.8").as_deref(), Some("1.2.3.4"));
        // IPv6 is valid too.
        assert_eq!(first_addr("::1").as_deref(), Some("::1"));
        // Non-IP tokens fall through to None (Go would use RemoteAddr).
        assert_eq!(first_addr("not-an-ip"), None);
        assert_eq!(first_addr(""), None);
        assert_eq!(first_addr("   "), None);
    }

    #[test]
    fn resolve_requires_trust_and_header() {
        let h = headers_with("cf-connecting-ip", "1.2.3.4");
        // Trust off → None even with a valid header present.
        assert_eq!(resolve(&h, false, "cf-connecting-ip"), None);
        // Empty header name → None.
        assert_eq!(resolve(&h, true, ""), None);
        // Trust on + matching header → the IP (lookup is case-insensitive).
        assert_eq!(
            resolve(&h, true, "cf-connecting-ip").as_deref(),
            Some("1.2.3.4")
        );
        // Configured header missing from the request → None.
        assert_eq!(resolve(&h, true, "x-real-ip"), None);
    }
}
