//! Shared Postgres access for the RustLens Rust services.
//!
//! The Rust fleet connects to a single Postgres database whose schema is owned
//! by this crate's embedded migrations in `migrations/`. The one-shot
//! `rustlens-migrate` binary (`apps/rust/tools/migrate`) runs [`migrate`] to
//! apply them; the long-running services never migrate, they only read/write
//! existing tables. `0001_baseline.sql` is the distilled end-state schema
//! (previously produced by the Go backend's `EnsureSchema`); subsequent changes
//! land as new `000N` migrations.
//!
//! Query modules are added per-domain (see `identity`).

pub use sqlx::PgPool;
use sqlx::postgres::PgPoolOptions;

pub mod admin;
pub mod admin_actions;
pub mod admin_guilds;
pub mod api_keys;
pub mod bot;
pub mod bot_guilds;
pub mod cleanup;
pub mod bot_user_installs;
pub mod command_usage;
pub mod game_bans;
pub mod guild_settings;
pub mod identity;
pub mod outbound_proxies;
pub mod owner_stats;
pub mod platform_sync;
pub mod player_states;
pub mod players_links;
pub mod players_search;
pub mod players_steam;
pub mod preferences;
pub mod rbac;
pub mod server_analytics;
pub mod track_analytics;
pub mod track_groups;
pub mod tracked_servers;
pub mod user_guilds;
pub mod wipe_react_schedule_posts;
pub mod wipe_react_schedules;
pub mod wipe_reacts;
pub mod role_connections;

/// Connect to Postgres and return a pooled handle.
pub async fn connect(database_url: &str, max_connections: u32) -> Result<PgPool, sqlx::Error> {
    PgPoolOptions::new()
        .max_connections(max_connections)
        .connect(database_url)
        .await
}

/// Apply all embedded schema migrations from `crates/rustlens-db/migrations/`.
///
/// Wraps `sqlx::migrate!`, which validates each already-applied migration's
/// checksum against `_sqlx_migrations` and runs any that are pending. Applying
/// against a database already migrated by the Go backend's `EnsureSchema` is a
/// safe no-op-plus-journal: every statement in `0001_baseline.sql` is
/// idempotent (`CREATE ... IF NOT EXISTS`, `INSERT ... ON CONFLICT DO NOTHING`),
/// so it records the baseline in `_sqlx_migrations` without altering the schema.
pub async fn migrate(pool: &PgPool) -> Result<(), sqlx::migrate::MigrateError> {
    sqlx::migrate!("./migrations").run(pool).await
}
