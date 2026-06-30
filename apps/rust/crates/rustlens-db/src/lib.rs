//! Shared Postgres access for the RustLens Rust services.
//!
//! The Rust identity service connects to the SAME database as the Go backend.
//! Schema is owned/migrated by the Go backend's `EnsureSchema`; this crate only
//! reads/writes existing tables. No schema migration happens here.
//!
//! Query modules are added per-domain (see `identity`).

pub use sqlx::PgPool;
use sqlx::postgres::PgPoolOptions;

pub mod identity;

/// Connect to Postgres and return a pooled handle.
pub async fn connect(database_url: &str, max_connections: u32) -> Result<PgPool, sqlx::Error> {
    PgPoolOptions::new()
        .max_connections(max_connections)
        .connect(database_url)
        .await
}
