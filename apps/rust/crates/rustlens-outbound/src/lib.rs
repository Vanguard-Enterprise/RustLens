//! Outbound HTTP proxy provider, mirroring the Go `internal/outboundhttp`
//! package. Provides round-robin proxy rotation over a set of credentialed
//! proxy endpoints, a per-proxy 10-minute sliding health window, and automatic
//! disable of a proxy whose error rate reaches ≥50% over ≥10 requests.
//!
//! **Only BattleMetrics traffic is proxied.** Steam / bmsearch / bmmatch /
//! platformsync are always direct and never touch this provider.
//!
//! Rust design note (spec D2a): Go returns an `*http.Client` whose transport
//! records results; `reqwest` has no equally clean transport hook, so this crate
//! exposes a [`Provider::send`] method (and the [`OutboundSender`] trait BM
//! depends on) that selects a proxy, sends, classifies the result, and feeds the
//! health window — all inside the crate.

use std::collections::{HashMap, HashSet};
use std::future::Future;
use std::pin::Pin;
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::RwLock;
use std::sync::Arc;
use std::time::{Duration, Instant};

use reqwest::{Client, Proxy, Request, Response};

/// Sliding health window (Go `proxyHealthWindow`).
pub const PROXY_HEALTH_WINDOW: Duration = Duration::from_secs(10 * 60);
/// Minimum requests in the window before auto-disable can trigger
/// (Go `proxyHealthMinRequests`).
pub const PROXY_HEALTH_MIN_REQUESTS: usize = 10;
/// Default HTTP client timeout when constructed with a zero timeout
/// (Go's nil-provider fallback uses 20s).
pub const DEFAULT_TIMEOUT: Duration = Duration::from_secs(20);

/// Fired (spawned) when a proxy is auto-disabled. The `servers` service wires
/// this to persist the disable + reload the provider from the DB. Async so it
/// can do DB work; the provider `tokio::spawn`s it (matching Go's `go cb(...)`).
pub type AutoDisableCallback =
    Arc<dyn Fn(String) -> Pin<Box<dyn Future<Output = ()> + Send>> + Send + Sync>;

/// Errors from constructing / reconfiguring a provider (not from sending — send
/// errors are the caller's `reqwest::Result`).
#[derive(Debug, thiserror::Error)]
pub enum OutboundError {
    #[error("build http client: {0}")]
    Client(#[from] reqwest::Error),
    #[error("proxy encryption key required to decrypt DB proxies")]
    KeyRequired,
    #[error("decrypt proxy endpoint: {0}")]
    Decrypt(#[from] rustlens_crypto::CryptoError),
    #[error("db: {0}")]
    Db(#[from] sqlx::Error),
}

struct ProxyClient {
    /// Credential-free `scheme://host:port` — the health-map key + log value.
    redacted: String,
    /// A `reqwest::Client` pre-configured with this proxy.
    client: Client,
}

struct Inner {
    enabled: bool,
    proxies: Vec<ProxyClient>,
    /// Per-redacted-endpoint request result history within the window.
    results: HashMap<String, Vec<(Instant, bool)>>,
    auto_disabled: HashSet<String>,
    callback: Option<AutoDisableCallback>,
}

/// The outbound HTTP provider. Cheap to share behind an `Arc`.
pub struct Provider {
    inner: RwLock<Inner>,
    next: AtomicU64,
    timeout: Duration,
    /// Direct (non-proxied) client, used when disabled or no proxies remain.
    direct: Client,
}

impl Provider {
    /// Build a provider. When `enabled` is false or `endpoints` is empty, every
    /// request goes direct. Fails if a proxy URL is invalid.
    pub fn new(enabled: bool, endpoints: &[String], timeout: Duration) -> Result<Self, OutboundError> {
        let timeout = if timeout.is_zero() { DEFAULT_TIMEOUT } else { timeout };
        let direct = Client::builder().timeout(timeout).build()?;
        let proxies = build_proxy_clients(enabled, endpoints, timeout)?;
        Ok(Self {
            inner: RwLock::new(Inner {
                enabled,
                proxies,
                results: HashMap::new(),
                auto_disabled: HashSet::new(),
                callback: None,
            }),
            next: AtomicU64::new(0),
            timeout,
            direct,
        })
    }

    /// Install the auto-disable callback.
    pub fn set_auto_disable_callback(&self, cb: AutoDisableCallback) {
        self.inner.write().unwrap().callback = Some(cb);
    }

    /// Atomically swap the proxy set (Go `Replace`). Validates all endpoints
    /// before mutating, then clears the health window, disabled set, and
    /// round-robin counter.
    pub fn replace(&self, enabled: bool, endpoints: &[String]) -> Result<(), OutboundError> {
        let proxies = build_proxy_clients(enabled, endpoints, self.timeout)?;
        let mut inner = self.inner.write().unwrap();
        inner.enabled = enabled;
        inner.proxies = proxies;
        inner.results.clear();
        inner.auto_disabled.clear();
        self.next.store(0, Ordering::SeqCst);
        Ok(())
    }

    /// Number of active (not-yet-disabled) proxies.
    pub fn proxy_count(&self) -> usize {
        self.inner.read().unwrap().proxies.len()
    }

    /// Send a request through the next proxy (round-robin) or direct. Proxied
    /// results feed the health window; direct results do not (matching Go, whose
    /// stats transport only wraps proxy clients).
    pub async fn send(&self, req: Request) -> reqwest::Result<Response> {
        let (client, redacted) = self.select();
        let result = client.execute(req).await;
        if let Some(redacted) = redacted {
            self.record_result(&redacted, classify_success(&result), Instant::now());
        }
        result
    }

    /// Select the client for the next request. Round-robin over proxies via an
    /// atomic counter; direct when disabled or empty.
    fn select(&self) -> (Client, Option<String>) {
        let inner = self.inner.read().unwrap();
        if !inner.enabled || inner.proxies.is_empty() {
            return (self.direct.clone(), None);
        }
        let idx = self.next.fetch_add(1, Ordering::Relaxed) as usize;
        let p = &inner.proxies[idx % inner.proxies.len()];
        (p.client.clone(), Some(p.redacted.clone()))
    }

    /// Record one request result and auto-disable the proxy if it has ≥10
    /// results in the window with ≥50% failures. `now` is injected for testing.
    fn record_result(&self, redacted: &str, success: bool, now: Instant) {
        let mut fire: Option<(AutoDisableCallback, String)> = None;
        {
            let mut inner = self.inner.write().unwrap();
            let cutoff = now.checked_sub(PROXY_HEALTH_WINDOW);
            let entry = inner.results.entry(redacted.to_string()).or_default();
            entry.push((now, success));
            // Drop results older than the window (Go: skip `at.Before(cutoff)`).
            if let Some(cutoff) = cutoff {
                entry.retain(|(at, _)| *at >= cutoff);
            }
            let kept = entry.len();
            let failures = entry.iter().filter(|(_, ok)| !ok).count();

            if kept >= PROXY_HEALTH_MIN_REQUESTS
                && failures * 2 >= kept
                && !inner.auto_disabled.contains(redacted)
            {
                inner.auto_disabled.insert(redacted.to_string());
                inner.proxies.retain(|p| p.redacted != redacted);
                if let Some(cb) = &inner.callback {
                    fire = Some((cb.clone(), redacted.to_string()));
                }
            }
        }
        if let Some((cb, redacted)) = fire {
            tracing::warn!("outbound: auto-disabled proxy {redacted} after >=50% errors in 10m");
            tokio::spawn(cb(redacted));
        }
    }
}

#[async_trait::async_trait]
impl OutboundSender for Provider {
    async fn send(&self, req: Request) -> reqwest::Result<Response> {
        Provider::send(self, req).await
    }
    fn proxy_count(&self) -> usize {
        Provider::proxy_count(self)
    }
}

/// The send abstraction BattleMetrics requests depend on. Implemented by
/// [`Provider`]; mockable in tests.
#[async_trait::async_trait]
pub trait OutboundSender: Send + Sync {
    /// Send a fully-built request, routing it through a proxy (or direct).
    async fn send(&self, req: Request) -> reqwest::Result<Response>;
    /// Number of active proxies (used to size proxied-request concurrency).
    fn proxy_count(&self) -> usize;
}

/// A request "succeeded" for health purposes when there was no transport error
/// and the response status is `< 500` and not `429` (Go's classification).
fn classify_success(result: &reqwest::Result<Response>) -> bool {
    match result {
        Ok(resp) => status_is_healthy(resp.status().as_u16()),
        Err(_) => false,
    }
}

/// A response status counts as healthy when it is `< 500` and not `429`.
fn status_is_healthy(status: u16) -> bool {
    status < 500 && status != 429
}

fn build_proxy_clients(
    enabled: bool,
    endpoints: &[String],
    timeout: Duration,
) -> Result<Vec<ProxyClient>, OutboundError> {
    if !enabled {
        return Ok(Vec::new());
    }
    let mut out = Vec::new();
    for ep in endpoints {
        let ep = ep.trim();
        if ep.is_empty() {
            continue;
        }
        let proxy = Proxy::all(ep)?;
        let client = Client::builder().proxy(proxy).timeout(timeout).build()?;
        out.push(ProxyClient {
            redacted: redact_proxy_endpoint(ep),
            client,
        });
    }
    Ok(out)
}

/// Strip credentials from a proxy URL, returning `scheme://host[:port]`
/// (Go `RedactProxyEndpoint`). Built from URL components so there is no path /
/// trailing-slash noise. Unparseable input (or a URL with no host) is returned
/// unchanged. Note: the `url` crate normalizes away *default* ports (`:443` for
/// https, `:80` for http); real proxy endpoints always carry an explicit
/// non-default port, so this is inert in practice.
pub fn redact_proxy_endpoint(raw: &str) -> String {
    match url::Url::parse(raw) {
        Ok(u) => match u.host_str() {
            Some(host) => match u.port() {
                Some(port) => format!("{}://{}:{}", u.scheme(), host, port),
                None => format!("{}://{}", u.scheme(), host),
            },
            None => raw.to_string(),
        },
        Err(_) => raw.to_string(),
    }
}

/// Resolve the initial proxy endpoint list (Go `initialProxyEndpoints`):
/// DB-managed `outbound_proxies` take precedence (decrypted with `cipher`);
/// with none, fall back to the legacy CSV **only when `proxy_enabled`**; else
/// empty (direct). Returns the decrypted, credentialed endpoint URLs.
pub async fn load_initial_endpoints(
    pool: &sqlx::PgPool,
    cipher: Option<&rustlens_crypto::Cipher>,
    proxy_enabled: bool,
    csv_endpoints: &[String],
) -> Result<Vec<String>, OutboundError> {
    let db = rustlens_db::outbound_proxies::list_enabled(pool).await?;
    if db.is_empty() {
        if proxy_enabled {
            return Ok(csv_endpoints.to_vec());
        }
        return Ok(Vec::new());
    }
    let cipher = cipher.ok_or(OutboundError::KeyRequired)?;
    let mut endpoints = Vec::with_capacity(db.len());
    for row in db {
        endpoints.push(cipher.decrypt(&row.ciphertext, &row.nonce)?);
    }
    Ok(endpoints)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn provider(endpoints: &[&str]) -> Provider {
        let eps: Vec<String> = endpoints.iter().map(|s| s.to_string()).collect();
        Provider::new(!eps.is_empty(), &eps, Duration::from_secs(5)).unwrap()
    }

    #[test]
    fn round_robin_cycles_in_order() {
        let p = provider(&[
            "http://u:pw@a.example:1000",
            "http://u:pw@b.example:2000",
            "http://u:pw@c.example:3000",
        ]);
        let got: Vec<String> = (0..6).map(|_| p.select().1.unwrap()).collect();
        assert_eq!(
            got,
            vec![
                "http://a.example:1000",
                "http://b.example:2000",
                "http://c.example:3000",
                "http://a.example:1000",
                "http://b.example:2000",
                "http://c.example:3000",
            ]
        );
    }

    #[test]
    fn disabled_or_empty_goes_direct() {
        let p = provider(&[]);
        assert!(p.select().1.is_none());
        assert_eq!(p.proxy_count(), 0);
    }

    #[test]
    fn ten_failures_auto_disables() {
        let p = provider(&["http://u:pw@h.example:8080"]);
        let r = "http://h.example:8080";
        let now = Instant::now();
        for _ in 0..9 {
            p.record_result(r, false, now);
        }
        assert_eq!(p.proxy_count(), 1, "9 failures must not disable yet");
        p.record_result(r, false, now);
        assert_eq!(p.proxy_count(), 0, "10th failure disables");
    }

    #[test]
    fn exactly_half_failures_disables_at_ten() {
        let p = provider(&["http://u:pw@h.example:8080"]);
        let r = "http://h.example:8080";
        let now = Instant::now();
        for _ in 0..5 {
            p.record_result(r, true, now);
        }
        for _ in 0..4 {
            p.record_result(r, false, now);
        }
        assert_eq!(p.proxy_count(), 1, "9 results (4 fail) not enough");
        p.record_result(r, false, now); // 10 results, 5 fail => 50%
        assert_eq!(p.proxy_count(), 0);
    }

    #[test]
    fn all_successes_never_disable() {
        let p = provider(&["http://u:pw@h.example:8080"]);
        let r = "http://h.example:8080";
        let now = Instant::now();
        for _ in 0..50 {
            p.record_result(r, true, now);
        }
        assert_eq!(p.proxy_count(), 1);
    }

    #[test]
    fn old_failures_evicted_from_window() {
        let p = provider(&["http://u:pw@h.example:8080"]);
        let r = "http://h.example:8080";
        let base = Instant::now();
        // 9 failures at t=base.
        for _ in 0..9 {
            p.record_result(r, false, base);
        }
        // A 10th result 11 minutes later: cutoff = (base+660s) - 600s = base+60s,
        // so all 9 old failures (at base) are evicted; only this success remains.
        p.record_result(r, true, base + Duration::from_secs(11 * 60));
        assert_eq!(p.proxy_count(), 1, "stale failures must not trigger disable");
    }

    #[test]
    fn replace_clears_health_state_and_counter() {
        let p = provider(&["http://u:pw@h.example:8080"]);
        let r = "http://h.example:8080";
        let now = Instant::now();
        for _ in 0..10 {
            p.record_result(r, false, now);
        }
        assert_eq!(p.proxy_count(), 0);
        p.replace(
            true,
            &[
                "http://u:pw@x.example:1".to_string(),
                "http://u:pw@y.example:2".to_string(),
            ],
        )
        .unwrap();
        assert_eq!(p.proxy_count(), 2);
        // counter reset => first pick is x
        assert_eq!(p.select().1.as_deref(), Some("http://x.example:1"));
    }

    #[test]
    fn redact_strips_credentials() {
        assert_eq!(
            redact_proxy_endpoint("http://user:pass@host.example:8080"),
            "http://host.example:8080"
        );
        assert_eq!(
            redact_proxy_endpoint("https://user:pass@host.example:8443"),
            "https://host.example:8443"
        );
        // no credentials -> just scheme://host:port, no trailing slash
        assert_eq!(
            redact_proxy_endpoint("http://host.example:3128"),
            "http://host.example:3128"
        );
        // unparseable -> unchanged
        assert_eq!(redact_proxy_endpoint("not a url"), "not a url");
    }

    #[test]
    fn status_classification_matches_go() {
        assert!(status_is_healthy(200));
        assert!(status_is_healthy(404));
        assert!(status_is_healthy(499));
        assert!(!status_is_healthy(429));
        assert!(!status_is_healthy(500));
        assert!(!status_is_healthy(503));
    }

    #[tokio::test]
    async fn auto_disable_fires_callback() {
        use std::sync::atomic::{AtomicBool, Ordering};
        let p = Arc::new(provider(&["http://u:pw@h.example:8080"]));
        let flag = Arc::new(AtomicBool::new(false));
        let f2 = flag.clone();
        p.set_auto_disable_callback(Arc::new(move |_redacted| {
            let f = f2.clone();
            Box::pin(async move {
                f.store(true, Ordering::SeqCst);
            })
        }));
        let r = "http://h.example:8080";
        let now = Instant::now();
        for _ in 0..10 {
            p.record_result(r, false, now);
        }
        // callback is spawned; yield to let it run.
        tokio::task::yield_now().await;
        tokio::time::sleep(Duration::from_millis(20)).await;
        assert!(flag.load(Ordering::SeqCst), "callback should have fired");
        assert_eq!(p.proxy_count(), 0);
    }
}
