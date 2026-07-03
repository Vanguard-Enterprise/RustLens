//! RustLens guilds service.
//!
//! Guild settings, wipe-reacts + schedules + the schedule-post scheduler,
//! track-groups, guild/user/install sync, and the admin guild list/detail.
//! Talks to the same Postgres as the Go backend (schema owned by Go). PORT 8093.

mod auth;
mod settings;
mod state;
mod sync;

use std::sync::Arc;

use anyhow::Context;
use axum::{
    Router,
    routing::{get, post},
};
use rustlens_config::GuildsConfig;

use state::AppState;

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    tracing_subscriber::fmt()
        .with_env_filter(
            tracing_subscriber::EnvFilter::try_from_default_env()
                .unwrap_or_else(|_| "guilds=info,tower_http=info".into()),
        )
        .init();

    let config = GuildsConfig::from_env().context("loading configuration")?;
    let pool = rustlens_db::connect(&config.postgres_url, 10)
        .await
        .context("connecting to Postgres")?;

    let state = Arc::new(AppState { config, pool });

    let app = Router::new()
        .route("/api/healthz", get(healthz))
        // ---- guild settings ----
        .route(
            "/api/internal/bot/setup/guilds/{guildID}/settings",
            get(settings::get_guild_settings).put(settings::update_guild_settings),
        )
        // ---- user-guilds ----
        .route("/api/internal/bot/setup/user-guilds", post(sync::create_user_guild_link))
        .route("/api/internal/bot/setup/user-guilds/leave", post(sync::mark_user_guild_left))
        .route(
            "/api/internal/bot/setup/discord/{discordID}/guilds",
            get(sync::list_user_guild_links_by_discord),
        )
        // ---- bot-guilds ----
        .route("/api/internal/bot/setup/bot-guilds/upsert", post(sync::upsert_bot_guild))
        .route("/api/internal/bot/setup/bot-guilds/leave", post(sync::mark_bot_guild_left))
        .route("/api/internal/bot/setup/bot-guilds/reconcile", post(sync::reconcile_bot_guilds))
        // ---- bot-user-installs ----
        .route(
            "/api/internal/bot/setup/bot-user-installs/seen",
            post(sync::touch_bot_user_install_seen),
        )
        .with_state(state.clone());

    let addr = format!("0.0.0.0:{}", state.config.port);
    let listener = tokio::net::TcpListener::bind(&addr)
        .await
        .with_context(|| format!("binding {addr}"))?;
    tracing::info!("guilds service listening on {addr}");
    axum::serve(listener, app).await.context("serving")?;
    Ok(())
}

async fn healthz() -> &'static str {
    "ok"
}
