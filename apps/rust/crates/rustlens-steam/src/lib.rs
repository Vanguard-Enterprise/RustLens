//! Steam↔BattleMetrics identity stack for RustLens Rust services, mirroring the
//! Go backend's `internal/steamapi`, `internal/steammatch`, `internal/bmsearch`,
//! `internal/bmmatch`, and `internal/platformsync` packages.
//!
//! **Every upstream in this crate is called directly (never proxied)** — only
//! BattleMetrics traffic (a separate crate) goes through the outbound proxy
//! provider.
//!
//! - [`steammatch`] — the [`steammatch::Resolver`] trait + fallthrough
//!   [`steammatch::Chain`] (bmsearch primary → bmmatch fallback).
//! - [`steamapi`] — the Steam Web API client (profile, owned games, bans,
//!   friends, vanity resolve).
//! - [`bmsearch`] — the primary Steam↔BM resolver (bmsearch.io) including the
//!   HYBRID cold-lookup that returns [`steammatch::MatchError::Pending`] when a
//!   lookup is still resolving past the inline budget.
//! - [`bmmatch`] — the optional legacy fallback resolver.
//! - [`platformsync`] — the platformsync.io cross-platform identity client.
//!
//! **Scope note (spec D4):** this crate provides the clients + the resolver
//! Chain. The HYBRID orchestration around it — the inline-timeout wrapper, the
//! singleflight detached backfill, and persisting the resolved `bm_id` to
//! `steam_accounts` — lives in the Go handler layer today and will land in the
//! `players` service (Phase 2), which wires this crate to the DB + the
//! BattleMetrics client. The Chain's pending/fallback-budget logic and
//! bmsearch's `Pending` signal are the crate-level half of that behavior.

pub mod bmmatch;
pub mod bmsearch;
pub mod platformsync;
pub mod steamapi;
pub mod steammatch;

pub use steammatch::{Chain, MatchError, Resolver};
