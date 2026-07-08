//! One-shot schema migrator for the RustLens Rust fleet.
//!
//! Connects to `POSTGRES_URL` and runs the embedded ordered SQL migrations from
//! `crates/rustlens-db/migrations/` via `rustlens_db::migrate`. Services never
//! migrate; this binary is the single owner of schema changes. Runs to
//! completion and exits: 0 on success, non-zero on any failure.

use std::process::ExitCode;

#[tokio::main]
async fn main() -> ExitCode {
    tracing_subscriber::fmt()
        .with_env_filter(
            tracing_subscriber::EnvFilter::try_from_default_env()
                .unwrap_or_else(|_| "migrate=info,sqlx=warn".into()),
        )
        .init();

    let database_url = std::env::var("POSTGRES_URL").unwrap_or_else(|_| {
        "postgres://rustlens:rustlens@localhost:5432/rustlens?sslmode=disable".to_string()
    });

    let pool = match rustlens_db::connect(&database_url, 5).await {
        Ok(pool) => pool,
        Err(err) => {
            tracing::error!("failed to connect to Postgres: {err}");
            return ExitCode::FAILURE;
        }
    };

    match rustlens_db::migrate(&pool).await {
        Ok(()) => {
            tracing::info!("migrations applied successfully");
            ExitCode::SUCCESS
        }
        Err(err) => {
            tracing::error!("migration failed: {err}");
            ExitCode::FAILURE
        }
    }
}
