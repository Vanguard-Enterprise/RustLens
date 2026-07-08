//! RustLens players service.
//!
//! Player identity: Steam↔BattleMetrics resolution, profiles/analytics, and all
//! Steam↔Discord linking. Talks to the same Postgres as the Go backend
//! (schema owned by Go's `EnsureSchema` — read/write only). Default PORT 8091.

mod app_players;
mod auth;
mod classifier;
mod gamebans;
mod links;
mod platformsync;
mod ratelimit;
mod resolve;
mod state;
mod steam;
mod track_analytics;
mod track_group_analytics;

use std::sync::Arc;
use std::time::Duration;

use anyhow::Context;
use axum::{
    Router,
    routing::{MethodRouter, delete, get, post, put},
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
    let provider = Arc::new(
        rustlens_outbound::Provider::new(!endpoints.is_empty(), &endpoints, BM_HTTP_TIMEOUT)
            .context("building outbound provider")?,
    );
    // Trait-object handle onto the SAME provider instance, kept on AppState so
    // handlers (track-group analytics) can size fetch concurrency from
    // `proxy_count()` without the concrete type.
    let outbound: Arc<dyn rustlens_outbound::OutboundSender> = provider.clone();
    let bm = rustlens_battlemetrics::Client::new(
        provider.clone() as Arc<dyn rustlens_outbound::OutboundSender>,
    );

    // Wire the outbound provider's auto-disable callback (Go
    // `cmd/server/main.go` ~line 129, `SetAutoDisableCallback`): persist the
    // disable, then reload the provider from the DB (falling back to the
    // legacy env proxies when no DB rows are enabled). The players service has
    // no proxy-admin routes of its own (unlike `servers`, which reuses its
    // `proxies::reload` for this), so this mirrors
    // `rustlens_outbound::load_initial_endpoints` + `Provider::replace`
    // directly. A failed persist or reload is logged and left for the next
    // auto-disable to retry, same as Go.
    {
        let cb_provider = provider.clone();
        let cb_pool = pool.clone();
        let cb_cipher = cipher.clone();
        let cb_proxy_enabled = config.proxy_enabled;
        let cb_proxy_endpoints = config.proxy_endpoints.clone();
        provider.set_auto_disable_callback(Arc::new(move |redacted: String| {
            let provider = cb_provider.clone();
            let pool = cb_pool.clone();
            let cipher = cb_cipher.clone();
            let proxy_endpoints = cb_proxy_endpoints.clone();
            Box::pin(async move {
                let disabled = match rustlens_db::outbound_proxies::disable_by_redacted_endpoint(
                    &pool, &redacted,
                )
                .await
                {
                    Ok(disabled) => disabled,
                    Err(e) => {
                        tracing::error!("players: auto-disable proxy {redacted:?} persist failed: {e}");
                        return;
                    }
                };
                if !disabled {
                    return;
                }
                let endpoints = match rustlens_outbound::load_initial_endpoints(
                    &pool,
                    cipher.as_ref(),
                    cb_proxy_enabled,
                    &proxy_endpoints,
                )
                .await
                {
                    Ok(endpoints) => endpoints,
                    Err(e) => {
                        tracing::error!(
                            "players: reload proxies after auto-disable {redacted:?} failed: {e}"
                        );
                        return;
                    }
                };
                if let Err(e) = provider.replace(!endpoints.is_empty(), &endpoints) {
                    tracing::error!(
                        "players: apply proxies after auto-disable {redacted:?} failed: {e}"
                    );
                    return;
                }
                tracing::warn!(
                    "players: auto-disabled outbound proxy {redacted:?} after >=50% errors in 10m"
                );
            })
        }));
    }

    // platformsync.io client (direct).
    let platform_sync = rustlens_steam::platformsync::Client::new(config.platformsync_token.clone())
        .context("building platformsync client")?;

    // Redis for app-route rate limiting (fails open when unavailable).
    let redis = match redis::Client::open(config.redis_url.as_str()) {
        Ok(client) => match client.get_multiplexed_async_connection().await {
            Ok(conn) => Some(conn),
            Err(e) => {
                tracing::warn!("redis unavailable, rate limiting fails open: {e}");
                None
            }
        },
        Err(e) => {
            tracing::warn!("invalid REDIS_URL, rate limiting fails open: {e}");
            None
        }
    };

    let state = Arc::new(AppState {
        config,
        pool,
        steam,
        resolver,
        bm,
        platform_sync,
        redis,
        backfill: state::Backfill::default(),
        outbound,
    });

    let app = build_router().with_state(state.clone());

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

/// Registers a bot-internal route under BOTH the legacy flat `setup/` path and
/// the new `players/` domain-prefix path (D1 domain-prefix reorg). The flat
/// paths are dropped wholesale in Phase E once every domain's bot deploy has
/// cut over; until then Rust serves both so the bot deploy is the only cutover
/// moment and reverting the bot is a full rollback.
///
/// `MethodRouter` is `Clone`, so the same handler(s) back both paths. Callers
/// pass `old` and `new` explicitly rather than deriving one from the other:
/// most tails are identical (`/steam/{steamID}/profile`), but the bm-scoped
/// routes differ (`/players/{bmID}/...` becomes `/bm/{bmID}/...`) and being
/// explicit keeps the reorg auditable.
///
/// matchit `bm/` rule (the reason the bm routes are not a plain prefix swap):
/// axum/matchit panics when a bare `{param}` shares a path position with any
/// literal segment. Under the new prefix, position 6 already holds the literals
/// `steam` / `discord` / `guilds` / `platform-sync` / `steam-accounts`, so the
/// bm-scoped routes MUST use a literal `bm/{bmID}` segment there. This is the
/// same care every sibling reorg (C2-C5) must take for its own bare-param
/// routes.
fn dual_route(
    router: Router<Arc<AppState>>,
    old_path: &str,
    new_path: &str,
    method: MethodRouter<Arc<AppState>>,
) -> Router<Arc<AppState>> {
    router
        .route(old_path, method.clone())
        .route(new_path, method)
}

/// Builds the full players router (state left unresolved so tests can construct
/// it without a live pool). Adding the routes is what triggers any matchit
/// conflict panic, so `router_builds` below constructs this at test time and
/// fails `cargo test` instead of failing at service boot.
fn build_router() -> Router<Arc<AppState>> {
    let router = Router::new()
        .route("/api/healthz", get(healthz))
        // ---- app-facing (session-authed); not part of the bot-internal reorg ----
        .route("/api/app/players/search", get(app_players::search_players))
        .route(
            "/api/app/players/{identifier}/profile",
            get(app_players::get_player_profile_app),
        );

    // ---- resolve / profile / analytics ----
    let router = dual_route(
        router,
        "/api/internal/bot/setup/steam/{steamID}",
        "/api/internal/bot/players/steam/{steamID}",
        get(steam::get_steam_account),
    );
    let router = dual_route(
        router,
        "/api/internal/bot/setup/steam/by-bm/{bmID}",
        "/api/internal/bot/players/steam/by-bm/{bmID}",
        get(steam::get_steam_account_by_bm),
    );
    let router = dual_route(
        router,
        "/api/internal/bot/setup/steam/{steamID}/profile",
        "/api/internal/bot/players/steam/{steamID}/profile",
        get(steam::get_steam_profile),
    );
    let router = dual_route(
        router,
        "/api/internal/bot/setup/steam/{steamID}/analytics",
        "/api/internal/bot/players/steam/{steamID}/analytics",
        get(steam::get_steam_analytics),
    );
    // bm-scoped: new path uses the literal `bm/{bmID}` segment (see dual_route docs).
    let router = dual_route(
        router,
        "/api/internal/bot/setup/players/{bmID}/offline-analytics",
        "/api/internal/bot/players/bm/{bmID}/offline-analytics",
        get(track_analytics::get_player_offline_analytics),
    );
    let router = dual_route(
        router,
        "/api/internal/bot/setup/players/{bmID}/bm-profile",
        "/api/internal/bot/players/bm/{bmID}/bm-profile",
        get(steam::get_bm_player_profile),
    );
    let router = dual_route(
        router,
        "/api/internal/bot/setup/players/{bmID}/bm-analytics",
        "/api/internal/bot/players/bm/{bmID}/bm-analytics",
        get(steam::get_bm_player_analytics),
    );
    let router = dual_route(
        router,
        "/api/internal/bot/setup/steam/{steamID}/friends",
        "/api/internal/bot/players/steam/{steamID}/friends",
        get(steam::get_steam_friends),
    );
    let router = dual_route(
        router,
        "/api/internal/bot/setup/steam/resolve",
        "/api/internal/bot/players/steam/resolve",
        post(steam::resolve_steam_id),
    );
    let router = dual_route(
        router,
        "/api/internal/bot/setup/steam-accounts/count",
        "/api/internal/bot/players/steam-accounts/count",
        get(steam::get_steam_account_count),
    );

    // ---- game-bans + track-group analytics + platform-sync ----
    let router = dual_route(
        router,
        "/api/internal/bot/setup/guilds/{guildID}/game-bans",
        "/api/internal/bot/players/guilds/{guildID}/game-bans",
        get(gamebans::check_guild_game_bans),
    );
    let router = dual_route(
        router,
        "/api/internal/bot/setup/guilds/{guildID}/track-groups/{groupID}/analytics",
        "/api/internal/bot/players/guilds/{guildID}/track-groups/{groupID}/analytics",
        get(track_group_analytics::get_track_group_analytics),
    );
    let router = dual_route(
        router,
        "/api/internal/bot/setup/platform-sync/{platform}/{id}",
        "/api/internal/bot/players/platform-sync/{platform}/{id}",
        post(platformsync::sync_platform),
    );

    // ---- global links ----
    let router = dual_route(
        router,
        "/api/internal/bot/setup/steam/links",
        "/api/internal/bot/players/steam/links",
        post(links::create_soft_link),
    );
    let router = dual_route(
        router,
        "/api/internal/bot/setup/steam/bulk-hard-links",
        "/api/internal/bot/players/steam/bulk-hard-links",
        post(links::bulk_upsert_hard_links),
    );
    let router = dual_route(
        router,
        "/api/internal/bot/setup/steam/{steamID}/links",
        "/api/internal/bot/players/steam/{steamID}/links",
        get(links::list_links_by_steam),
    );
    let router = dual_route(
        router,
        "/api/internal/bot/setup/discord/{discordID}/links",
        "/api/internal/bot/players/discord/{discordID}/links",
        get(links::list_links_by_discord),
    );
    let router = dual_route(
        router,
        "/api/internal/bot/setup/steam/{steamID}/links/{discordID}/soft",
        "/api/internal/bot/players/steam/{steamID}/links/{discordID}/soft",
        delete(links::delete_soft_link),
    );
    let router = dual_route(
        router,
        "/api/internal/bot/setup/discord/{discordID}/setsteam-candidates",
        "/api/internal/bot/players/discord/{discordID}/setsteam-candidates",
        get(links::list_setsteam_candidates),
    );

    // ---- link visibility ----
    let router = dual_route(
        router,
        "/api/internal/bot/setup/discord/{discordID}/link-visibility-hidden",
        "/api/internal/bot/players/discord/{discordID}/link-visibility-hidden",
        put(links::hide_discord_link_identity).delete(links::unhide_discord_link_identity),
    );

    // ---- guild-scoped links ----
    let router = dual_route(
        router,
        "/api/internal/bot/setup/guilds/{guildID}/links",
        "/api/internal/bot/players/guilds/{guildID}/links",
        post(links::create_guild_link).get(links::list_guild_links),
    );
    let router = dual_route(
        router,
        "/api/internal/bot/setup/guilds/{guildID}/discord/{discordID}/links",
        "/api/internal/bot/players/guilds/{guildID}/discord/{discordID}/links",
        get(links::list_guild_links_by_discord),
    );
    let router = dual_route(
        router,
        "/api/internal/bot/setup/guilds/{guildID}/steam/{steamID}/links",
        "/api/internal/bot/players/guilds/{guildID}/steam/{steamID}/links",
        get(links::list_guild_links_by_steam),
    );
    let router = dual_route(
        router,
        "/api/internal/bot/setup/guilds/{guildID}/discord/{discordID}/steam/{steamID}",
        "/api/internal/bot/players/guilds/{guildID}/discord/{discordID}/steam/{steamID}",
        delete(links::delete_guild_link),
    );
    dual_route(
        router,
        "/api/internal/bot/setup/guilds/{guildID}/nickname-targets",
        "/api/internal/bot/players/guilds/{guildID}/nickname-targets",
        get(links::list_guild_nickname_targets),
    )
}

#[cfg(test)]
mod tests {
    use super::build_router;

    /// Constructing the router adds every route to matchit, so a bare-param /
    /// literal-segment conflict (the `bm/` rule) panics here in `cargo test`
    /// rather than at service boot. This guards the dual-registration reorg.
    #[test]
    fn router_builds() {
        let _ = build_router();
    }
}
