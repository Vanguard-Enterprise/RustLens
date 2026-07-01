//! Per-actor/action token-bucket rate limiter + idempotency-key replay cache for
//! the admin action dispatcher. Mirrors the Go `internal/admin/actions`
//! `rateLimiter` + `idempotencyCache` (in-process, `Instant`-based).

use std::collections::HashMap;
use std::sync::Mutex;
use std::time::{Duration, Instant};

/// Default bucket for mutating actions (Go `defaultMutating*`). All ported
/// actions use this (none are token-generating or override it).
pub const DEFAULT_MUTATING_CAPACITY: i32 = 30;
pub const DEFAULT_MUTATING_REFILL: f64 = 0.5;

const BUCKET_IDLE_EVICTION: Duration = Duration::from_secs(10 * 60);
const IDEMPOTENCY_TTL: Duration = Duration::from_secs(24 * 60 * 60);
const IDEMPOTENCY_MAX_ENTRIES: usize = 10_000;

struct TokenBucket {
    capacity: f64,
    refill_per_sec: f64,
    tokens: f64,
    last_refill: Instant,
    last_access: Instant,
}

impl TokenBucket {
    /// Consume a token; `Ok(())` if granted, `Err(retry_after)` otherwise.
    fn take(&mut self, now: Instant) -> Result<(), Duration> {
        let elapsed = now.saturating_duration_since(self.last_refill).as_secs_f64();
        if elapsed > 0.0 {
            self.tokens = (self.tokens + elapsed * self.refill_per_sec).min(self.capacity);
            self.last_refill = now;
        }
        self.last_access = now;
        if self.tokens >= 1.0 {
            self.tokens -= 1.0;
            return Ok(());
        }
        let need = 1.0 - self.tokens;
        if self.refill_per_sec <= 0.0 {
            return Err(Duration::from_secs(60));
        }
        Err(Duration::from_secs_f64(need / self.refill_per_sec))
    }
}

#[derive(Default)]
pub struct RateLimiter {
    buckets: Mutex<HashMap<String, TokenBucket>>,
}

impl RateLimiter {
    pub fn new() -> Self {
        Self::default()
    }

    /// Consume a token for `(actor, action)`. `Ok(())` = allowed;
    /// `Err(retry_after)` = denied.
    pub fn allow(
        &self,
        actor: &str,
        action: &str,
        capacity: i32,
        refill_per_sec: f64,
    ) -> Result<(), Duration> {
        let key = format!("{actor}:{action}");
        let now = Instant::now();
        let mut buckets = self.buckets.lock().unwrap();
        buckets.retain(|_, b| now.saturating_duration_since(b.last_access) <= BUCKET_IDLE_EVICTION);
        let bucket = buckets.entry(key).or_insert_with(|| TokenBucket {
            capacity: capacity as f64,
            refill_per_sec,
            tokens: capacity as f64,
            last_refill: now,
            last_access: now,
        });
        bucket.capacity = capacity as f64;
        bucket.refill_per_sec = refill_per_sec;
        bucket.take(now)
    }
}

struct CachedResponse {
    status: u16,
    body: Vec<u8>,
    expires_at: Instant,
}

#[derive(Default)]
pub struct IdempotencyCache {
    entries: Mutex<HashMap<String, CachedResponse>>,
}

impl IdempotencyCache {
    pub fn new() -> Self {
        Self::default()
    }

    /// Cache key `actor:action:idempotency-key` (Go `key`).
    pub fn key(actor: &str, action: &str, idempotency_key: &str) -> String {
        format!("{actor}:{action}:{idempotency_key}")
    }

    /// A fresh cached `(status, body)` for `key`, if any.
    pub fn get(&self, key: &str) -> Option<(u16, Vec<u8>)> {
        let now = Instant::now();
        let mut entries = self.entries.lock().unwrap();
        match entries.get(key) {
            Some(e) if now <= e.expires_at => Some((e.status, e.body.clone())),
            Some(_) => {
                entries.remove(key);
                None
            }
            None => None,
        }
    }

    /// Store a response for later replay (24h TTL, bounded entry count).
    pub fn put(&self, key: String, status: u16, body: Vec<u8>) {
        let now = Instant::now();
        let mut entries = self.entries.lock().unwrap();
        entries.retain(|_, e| now <= e.expires_at);
        if entries.len() >= IDEMPOTENCY_MAX_ENTRIES {
            if let Some(k) = entries.keys().next().cloned() {
                entries.remove(&k);
            }
        }
        entries.insert(
            key,
            CachedResponse { status, body, expires_at: now + IDEMPOTENCY_TTL },
        );
    }
}
