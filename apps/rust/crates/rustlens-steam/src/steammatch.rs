//! Steam↔BattleMetrics resolver interface + fallthrough chain, mirroring the Go
//! `internal/steammatch` package. A [`Chain`] tries each [`Resolver`] in order
//! (bmsearch primary, bmmatch fallback) with the exact Go fallthrough semantics.
//!
//! Rust note: Go needed a `Translating` adapter to map each source's own error
//! type onto the shared sentinels; here each client implements [`Resolver`]
//! returning [`MatchError`] directly, so no adapter is required.

use std::time::Duration;

use async_trait::async_trait;

/// A fallback resolver, invoked after a prior resolver returned
/// [`MatchError::Pending`], gets a fresh bounded budget so it isn't skipped with
/// an instantly-exhausted deadline (Go `fallbackResolverTimeout`).
pub const FALLBACK_RESOLVER_TIMEOUT: Duration = Duration::from_secs(4);

/// Shared resolver error sentinels (Go `ErrNotFound` / `ErrNotConfigured` /
/// `ErrPending`).
#[derive(Debug, thiserror::Error)]
pub enum MatchError {
    #[error("steammatch: identity not found")]
    NotFound,
    #[error("steammatch: no resolver configured")]
    NotConfigured,
    #[error("steammatch: lookup pending")]
    Pending,
    #[error("steammatch: {0}")]
    Other(String),
}

/// Resolves a Steam ID to a BattleMetrics player ID and vice versa.
#[async_trait]
pub trait Resolver: Send + Sync {
    async fn steam_to_bm(&self, steam_id: &str) -> Result<String, MatchError>;
    async fn bm_to_steam(&self, bm_id: &str) -> Result<String, MatchError>;
}

/// Which direction a chain run resolves.
#[derive(Clone, Copy)]
enum Op {
    SteamToBm,
    BmToSteam,
}

impl Op {
    fn label(self) -> &'static str {
        match self {
            Op::SteamToBm => "steam->bm",
            Op::BmToSteam => "bm->steam",
        }
    }
}

/// Tries multiple resolvers in order with Go's fallthrough semantics.
pub struct Chain {
    resolvers: Vec<Box<dyn Resolver>>,
}

impl Chain {
    /// Build a chain from resolvers in priority order.
    pub fn new(resolvers: Vec<Box<dyn Resolver>>) -> Self {
        Self { resolvers }
    }

    async fn run(&self, id: &str, op: Op) -> Result<String, MatchError> {
        let mut pending = false;
        let mut configured = false;

        for resolver in &self.resolvers {
            let call = async {
                match op {
                    Op::SteamToBm => resolver.steam_to_bm(id).await,
                    Op::BmToSteam => resolver.bm_to_steam(id).await,
                }
            };
            // After a pending, give the fallback a fresh bounded budget so an
            // exhausted parent deadline can't skip it instantly.
            let result = if pending {
                match tokio::time::timeout(FALLBACK_RESOLVER_TIMEOUT, call).await {
                    Ok(r) => r,
                    Err(_) => Err(MatchError::Pending),
                }
            } else {
                call.await
            };

            match result {
                // A fallback resolving after an ErrPending wins over pending.
                Ok(val) if !val.is_empty() => return Ok(val),
                // Empty value is a miss.
                Ok(_) => configured = true,
                // Source off; try the next without marking configured.
                Err(MatchError::NotConfigured) => continue,
                Err(MatchError::Pending) => {
                    pending = true;
                    configured = true;
                }
                Err(MatchError::NotFound) => configured = true,
                // Hard error: fall through to the next source rather than failing
                // the whole lookup.
                Err(MatchError::Other(msg)) => {
                    configured = true;
                    tracing::warn!("steammatch {} resolver error: {msg}", op.label());
                }
            }
        }

        if pending {
            return Err(MatchError::Pending);
        }
        if !configured {
            return Err(MatchError::NotConfigured);
        }
        Err(MatchError::NotFound)
    }
}

#[async_trait]
impl Resolver for Chain {
    async fn steam_to_bm(&self, steam_id: &str) -> Result<String, MatchError> {
        self.run(steam_id, Op::SteamToBm).await
    }
    async fn bm_to_steam(&self, bm_id: &str) -> Result<String, MatchError> {
        self.run(bm_id, Op::BmToSteam).await
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::atomic::{AtomicUsize, Ordering};
    use std::sync::Arc;

    /// A resolver returning a scripted result, counting calls.
    struct Stub {
        result: fn() -> Result<String, MatchError>,
        calls: Arc<AtomicUsize>,
    }
    impl Stub {
        fn pair(result: fn() -> Result<String, MatchError>) -> (Box<dyn Resolver>, Arc<AtomicUsize>) {
            let calls = Arc::new(AtomicUsize::new(0));
            (
                Box::new(Stub {
                    result,
                    calls: calls.clone(),
                }),
                calls,
            )
        }
    }
    #[async_trait]
    impl Resolver for Stub {
        async fn steam_to_bm(&self, _: &str) -> Result<String, MatchError> {
            self.calls.fetch_add(1, Ordering::SeqCst);
            (self.result)()
        }
        async fn bm_to_steam(&self, _: &str) -> Result<String, MatchError> {
            self.calls.fetch_add(1, Ordering::SeqCst);
            (self.result)()
        }
    }

    #[tokio::test]
    async fn not_configured_is_skipped_and_next_tried() {
        let (a, ca) = Stub::pair(|| Err(MatchError::NotConfigured));
        let (b, cb) = Stub::pair(|| Ok("999".into()));
        let chain = Chain::new(vec![a, b]);
        assert_eq!(chain.steam_to_bm("1").await.unwrap(), "999");
        assert_eq!(ca.load(Ordering::SeqCst), 1);
        assert_eq!(cb.load(Ordering::SeqCst), 1);
    }

    #[tokio::test]
    async fn all_not_configured_yields_not_configured() {
        let (a, _) = Stub::pair(|| Err(MatchError::NotConfigured));
        let (b, _) = Stub::pair(|| Err(MatchError::NotConfigured));
        let chain = Chain::new(vec![a, b]);
        assert!(matches!(
            chain.steam_to_bm("1").await,
            Err(MatchError::NotConfigured)
        ));
    }

    #[tokio::test]
    async fn all_not_found_yields_not_found() {
        let (a, _) = Stub::pair(|| Err(MatchError::NotFound));
        let (b, _) = Stub::pair(|| Err(MatchError::NotFound));
        let chain = Chain::new(vec![a, b]);
        assert!(matches!(chain.steam_to_bm("1").await, Err(MatchError::NotFound)));
    }

    #[tokio::test]
    async fn empty_value_is_treated_as_miss() {
        let (a, _) = Stub::pair(|| Ok(String::new()));
        let chain = Chain::new(vec![a]);
        // configured (returned Ok) but empty => NotFound, not NotConfigured.
        assert!(matches!(chain.steam_to_bm("1").await, Err(MatchError::NotFound)));
    }

    #[tokio::test]
    async fn pending_then_fallback_success_wins() {
        let (a, ca) = Stub::pair(|| Err(MatchError::Pending));
        let (b, cb) = Stub::pair(|| Ok("777".into()));
        let chain = Chain::new(vec![a, b]);
        assert_eq!(chain.steam_to_bm("1").await.unwrap(), "777");
        assert_eq!(ca.load(Ordering::SeqCst), 1);
        assert_eq!(cb.load(Ordering::SeqCst), 1);
    }

    #[tokio::test]
    async fn pending_only_yields_pending() {
        let (a, _) = Stub::pair(|| Err(MatchError::Pending));
        let (b, _) = Stub::pair(|| Err(MatchError::NotFound));
        let chain = Chain::new(vec![a, b]);
        assert!(matches!(chain.steam_to_bm("1").await, Err(MatchError::Pending)));
    }

    #[tokio::test]
    async fn hard_error_falls_through_to_next() {
        let (a, _) = Stub::pair(|| Err(MatchError::Other("boom".into())));
        let (b, cb) = Stub::pair(|| Ok("555".into()));
        let chain = Chain::new(vec![a, b]);
        assert_eq!(chain.steam_to_bm("1").await.unwrap(), "555");
        assert_eq!(cb.load(Ordering::SeqCst), 1);
    }

    #[tokio::test]
    async fn empty_chain_is_not_configured() {
        let chain = Chain::new(vec![]);
        assert!(matches!(
            chain.steam_to_bm("1").await,
            Err(MatchError::NotConfigured)
        ));
    }
}
