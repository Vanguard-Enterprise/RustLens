//! RustLens players service.
//!
//! Player identity: Steam↔BattleMetrics resolution, profiles/analytics, and all
//! Steam↔Discord linking. Talks to the same Postgres as the Go backend
//! (schema owned by Go's `EnsureSchema` — read/write only). Default PORT 8091.

mod auth;
mod links;
mod state;

use std::sync::Arc;

use anyhow::Context;
use axum::{
    Router,
    routing::{delete, get, post, put},
};
use rustlens_config::PlayersConfig;

use state::AppState;

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    tracing_subscriber::fmt()
        .with_env_filter(
            tracing_subscriber::EnvFilter::try_from_default_env()
                .unwrap_or_else(|_| "players=info,tower_http=info".into()),
        )
        .init();

    let config = PlayersConfig::from_env().context("loading configuration")?;
    let pool = rustlens_db::connect(&config.postgres_url, 10)
        .await
        .context("connecting to Postgres")?;

    let state = Arc::new(AppState { config, pool });

    let app = Router::new()
        .route("/api/healthz", get(healthz))
        // ---- global links ----
        .route("/api/internal/bot/setup/steam/links", post(links::create_soft_link))
        .route(
            "/api/internal/bot/setup/steam/bulk-hard-links",
            post(links::bulk_upsert_hard_links),
        )
        .route(
            "/api/internal/bot/setup/steam/{steamID}/links",
            get(links::list_links_by_steam),
        )
        .route(
            "/api/internal/bot/setup/discord/{discordID}/links",
            get(links::list_links_by_discord),
        )
        .route(
            "/api/internal/bot/setup/steam/{steamID}/links/{discordID}/soft",
            delete(links::delete_soft_link),
        )
        .route(
            "/api/internal/bot/setup/discord/{discordID}/setsteam-candidates",
            get(links::list_setsteam_candidates),
        )
        // ---- link visibility ----
        .route(
            "/api/internal/bot/setup/discord/{discordID}/link-visibility-hidden",
            put(links::hide_discord_link_identity).delete(links::unhide_discord_link_identity),
        )
        // ---- guild-scoped links ----
        .route(
            "/api/internal/bot/setup/guilds/{guildID}/links",
            post(links::create_guild_link).get(links::list_guild_links),
        )
        .route(
            "/api/internal/bot/setup/guilds/{guildID}/discord/{discordID}/links",
            get(links::list_guild_links_by_discord),
        )
        .route(
            "/api/internal/bot/setup/guilds/{guildID}/steam/{steamID}/links",
            get(links::list_guild_links_by_steam),
        )
        .route(
            "/api/internal/bot/setup/guilds/{guildID}/discord/{discordID}/steam/{steamID}",
            delete(links::delete_guild_link),
        )
        .route(
            "/api/internal/bot/setup/guilds/{guildID}/nickname-targets",
            get(links::list_guild_nickname_targets),
        )
        .with_state(state.clone());

    let addr = format!("0.0.0.0:{}", state.config.port);
    let listener = tokio::net::TcpListener::bind(&addr)
        .await
        .with_context(|| format!("binding {addr}"))?;
    tracing::info!("players service listening on {addr}");
    axum::serve(listener, app).await.context("serving")?;
    Ok(())
}

async fn healthz() -> &'static str {
    "ok"
}
