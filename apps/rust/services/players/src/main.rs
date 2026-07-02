//! RustLens players service.
//!
//! Player identity: Steam↔BattleMetrics resolution, profiles/analytics, and all
//! Steam↔Discord linking. Talks to the same Postgres as the Go backend
//! (schema owned by Go's `EnsureSchema` — read/write only). Default PORT 8091.

mod auth;
mod gamebans;
mod links;
mod platformsync;
mod resolve;
mod state;
mod steam;

use std::sync::Arc;
use std::time::Duration;

use anyhow::Context;
use axum::{
    Router,
    routing::{delete, get, post, put},
};
use rustlens_config::PlayersConfig;
use rustlens_steam::Resolver;

use state::AppState;

/// bmsearch cold-lookup polling ceiling. Inline calls bound this tighter with an
/// outer timeout (`BM_SEARCH_INLINE_TIMEOUT_MS`); the detached backfill uses the
/// full 90s. See `resolve.rs`.
const BMSEARCH_MAX_WAIT: Duration = Duration::from_secs(90);
/// BattleMetrics HTTP client timeout (Go's outbound fallback).
const BM_HTTP_TIMEOUT: Duration = Duration::from_secs(20);

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

    // Steam Web API client (direct).
    let steam = rustlens_steam::steamapi::Client::new(config.steam_api_key.clone())
        .context("building steam client")?;

    // Steam↔BM resolver chain: bmsearch primary (if enabled) → bmmatch fallback.
    let mut resolvers: Vec<Box<dyn Resolver>> = Vec::new();
    if config.bm_search_enabled {
        resolvers.push(Box::new(
            rustlens_steam::bmsearch::Client::new(&config.bm_search_url, BMSEARCH_MAX_WAIT)
                .context("building bmsearch client")?,
        ));
    }
    resolvers.push(Box::new(
        rustlens_steam::bmmatch::Client::new(&config.bm_match_url, &config.bm_match_token)
            .context("building bmmatch client")?,
    ));
    let resolver = Arc::new(rustlens_steam::Chain::new(resolvers));

    // Outbound proxy provider (BattleMetrics only). DB proxies decrypted with the
    // cipher take precedence over the PROXY_ENDPOINTS fallback.
    let cipher = match config.proxy_encryption_key.as_deref() {
        Some(key) => Some(
            rustlens_crypto::Cipher::from_base64_key(key).context("PROXY_ENCRYPTION_KEY")?,
        ),
        None => None,
    };
    let endpoints = rustlens_outbound::load_initial_endpoints(
        &pool,
        cipher.as_ref(),
        config.proxy_enabled,
        &config.proxy_endpoints,
    )
    .await
    .context("loading outbound proxies")?;
    let provider = rustlens_outbound::Provider::new(!endpoints.is_empty(), &endpoints, BM_HTTP_TIMEOUT)
        .context("building outbound provider")?;
    let bm = rustlens_battlemetrics::Client::new(
        Arc::new(provider) as Arc<dyn rustlens_outbound::OutboundSender>,
    );

    // platformsync.io client (direct).
    let platform_sync = rustlens_steam::platformsync::Client::new(config.platformsync_token.clone())
        .context("building platformsync client")?;

    let state = Arc::new(AppState {
        config,
        pool,
        steam,
        resolver,
        bm,
        platform_sync,
        backfill: state::Backfill::default(),
    });

    let app = Router::new()
        .route("/api/healthz", get(healthz))
        // ---- resolve / profile / analytics ----
        .route(
            "/api/internal/bot/setup/steam/{steamID}",
            get(steam::get_steam_account),
        )
        .route(
            "/api/internal/bot/setup/steam/by-bm/{bmID}",
            get(steam::get_steam_account_by_bm),
        )
        .route(
            "/api/internal/bot/setup/steam/{steamID}/profile",
            get(steam::get_steam_profile),
        )
        .route(
            "/api/internal/bot/setup/steam/{steamID}/analytics",
            get(steam::get_steam_analytics),
        )
        // ---- game-bans + platform-sync ----
        .route(
            "/api/internal/bot/setup/guilds/{guildID}/game-bans",
            get(gamebans::check_guild_game_bans),
        )
        .route(
            "/api/internal/bot/setup/platform-sync/{platform}/{id}",
            post(platformsync::sync_platform),
        )
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
