//! RustLens identity service.
//!
//! Discord-OAuth login that mints the shared `rustlens_session` cookie, plus
//! `/api/app/me`. Talks to the same Postgres as the Go backend.

mod admin;
mod appauth;
mod bot;
mod cookies;
mod discord;
mod dispatcher;
mod events_handler;
mod handlers;
mod me;
mod ratelimit;
mod role_connection;
mod state;

use std::sync::Arc;

use anyhow::Context;
use axum::{
    Router,
    routing::{MethodRouter, delete, get, post},
};
use rustlens_config::IdentityConfig;

use state::AppState;

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    tracing_subscriber::fmt()
        .with_env_filter(
            tracing_subscriber::EnvFilter::try_from_default_env()
                .unwrap_or_else(|_| "identity=info,tower_http=info".into()),
        )
        .init();

    let config = IdentityConfig::from_env().context("loading configuration")?;
    let pool = rustlens_db::connect(&config.postgres_url, 10)
        .await
        .context("connecting to Postgres")?;

    // Seed the RBAC catalog (admin_permissions + system admin_roles +
    // admin_role_permissions) before serving. Idempotent; mirrors the Go
    // backend's rbac.Store.SeedPermissionsAndRoles, which the Go server also
    // runs unconditionally at construction time.
    rustlens_db::rbac::seed_permissions_and_roles(&pool)
        .await
        .context("seeding RBAC catalog")?;

    let hub = Arc::new(rustlens_events::Hub::new());
    let events = rustlens_events::RedisBridge::start(hub.clone(), &config.redis_url);

    // Build the role-connection cipher (linked-roles feature) when configured.
    let cipher = match config.proxy_encryption_key.as_deref() {
        Some(key) => match rustlens_crypto::Cipher::from_base64_key(key) {
            Ok(c) => Some(c),
            Err(e) => {
                tracing::error!("invalid PROXY_ENCRYPTION_KEY, linked-roles disabled: {e}");
                None
            }
        },
        None => None,
    };

    let state = Arc::new(AppState {
        config,
        pool,
        http: reqwest::Client::new(),
        hub,
        events,
        cipher,
        rate_limiter: ratelimit::RateLimiter::new(),
        idempotency: ratelimit::IdempotencyCache::new(),
    });

    // Register the linked-roles metadata schema once at startup (best-effort).
    if state.config.role_connection_enabled() && state.config.discord_bot_token.is_some() {
        match role_connection::register_schema(&state).await {
            Ok(()) => tracing::info!("linked-roles metadata schema registered"),
            Err(e) => tracing::warn!("linked-roles metadata schema registration failed: {e:#}"),
        }
    }

    // Spawn the linked-roles reconciler (Phase 4b): re-push metadata on startup
    // and every 6h, advisory-locked so only one pod runs at a time.
    if state.config.role_connection_enabled() {
        tokio::spawn(role_connection::reconcile_loop(state.clone()));
    }

    let app = build_router().with_state(state.clone());

    let addr = format!("0.0.0.0:{}", state.config.port);
    let listener = tokio::net::TcpListener::bind(&addr)
        .await
        .with_context(|| format!("binding {addr}"))?;
    tracing::info!("identity service listening on {addr}");
    axum::serve(listener, app).await.context("serving")?;
    Ok(())
}

async fn healthz() -> &'static str {
    "ok"
}

/// Registers a bot-internal route under BOTH the legacy flat `setup/` path and
/// the new `identity/` domain-prefix path (D1 domain-prefix reorg). The flat
/// paths are dropped wholesale in Phase E once every domain's bot deploy has
/// cut over; until then Rust serves both so the bot deploy is the only cutover
/// moment and reverting the bot is a full rollback.
///
/// `MethodRouter` is `Clone`, so the same handler(s) back both paths. Callers
/// pass `old` and `new` explicitly rather than deriving one from the other, so
/// the reorg stays auditable (mirrors the players service's C1 pattern).
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

/// Builds the full identity router (state left unresolved so tests can
/// construct it without a live pool). Adding the routes is what triggers any
/// matchit conflict panic, so `router_builds` below constructs this at test
/// time and fails `cargo test` instead of failing at service boot.
fn build_router() -> Router<Arc<AppState>> {
    let router = Router::new()
        .route("/api/healthz", get(healthz))
        .route("/api/auth/discord", get(handlers::initiate))
        .route("/api/auth/discord/callback", get(handlers::callback))
        .route("/api/app/me", get(handlers::me))
        .route(
            "/api/app/me/preferences",
            get(handlers::get_preferences).patch(handlers::patch_preferences),
        )
        .route("/api/app/me/connections", get(me::connections))
        .route("/api/app/me/guilds", get(me::guilds))
        .route("/api/app/me/guilds/installed", get(me::guilds_installed))
        .route("/api/app/me/guilds/{guildID}/links", get(me::guild_links))
        .route("/api/app/auth/logout", post(handlers::logout))
        .route("/api/app/admin/permissions", get(admin::list_permissions))
        .route("/api/app/admin/roles", get(admin::list_roles))
        .route("/api/app/admin/users", get(admin::list_users))
        .route("/api/app/admin/administrators", get(admin::list_administrators))
        .route("/api/app/admin/audit", get(admin::list_audit))
        .route("/api/app/admin/audit/event-types", get(admin::list_audit_event_types))
        .route("/api/app/admin/api-keys", get(admin::list_api_keys))
        .route("/api/app/admin/audit/{id}", get(admin::get_audit_event))
        .route("/api/app/admin/users/{id}", get(admin::get_user))
        .route("/api/app/admin/users/{id}/audit", get(admin::get_user_audit))
        .route(
            "/api/app/admin/users/{id}/command-usage",
            get(admin::get_user_command_usage),
        )
        .route(
            "/api/app/admin/users/{id}/command-usage/filter-options",
            get(admin::get_user_command_usage_filter_options),
        )
        .route(
            "/api/app/admin/users/{id}/command-usage/{usageId}/targets",
            get(admin::get_command_usage_targets),
        )
        .route(
            "/api/app/admin/users/{id}/command-usage/{usageId}/children",
            get(admin::get_command_usage_children),
        )
        .route("/api/app/admin/actions/{type}", post(dispatcher::dispatch))
        .route("/api/auth/discord/role-connection", get(role_connection::initiate))
        .route(
            "/api/auth/discord/role-connection/callback",
            get(role_connection::callback),
        )
        .route(
            "/api/app/me/discord-role-connection",
            delete(role_connection::disconnect),
        )
        .route("/api/events/stream", get(events_handler::stream));

    // ---- Bot-internal (JWT-authed) identity routes, dual-registered under
    // the legacy flat `setup/` path and the new `identity/` domain prefix. ----
    let router = dual_route(
        router,
        "/api/internal/bot/setup/command-usage",
        "/api/internal/bot/identity/command-usage",
        post(bot::record_command_usage),
    );
    let router = dual_route(
        router,
        "/api/internal/bot/setup/bans/{discordID}",
        "/api/internal/bot/identity/bans/{discordID}",
        get(bot::get_user_ban).put(bot::update_user_ban),
    );
    let router = dual_route(
        router,
        "/api/internal/bot/setup/discord/{discordID}/is-website-admin",
        "/api/internal/bot/identity/discord/{discordID}/is-website-admin",
        get(bot::is_website_admin),
    );
    let router = dual_route(
        router,
        "/api/internal/bot/setup/discord/{discordID}/has-pro-role",
        "/api/internal/bot/identity/discord/{discordID}/has-pro-role",
        get(bot::has_pro_role),
    );
    dual_route(
        router,
        "/api/internal/bot/setup/users/ensure",
        "/api/internal/bot/identity/users/ensure",
        post(bot::ensure_app_user),
    )
}

#[cfg(test)]
mod tests {
    use super::build_router;

    /// Constructing the router adds every route to matchit, so a bare-param /
    /// literal-segment conflict panics here in `cargo test` rather than at
    /// service boot. This guards the dual-registration reorg.
    #[test]
    fn router_builds() {
        let _ = build_router();
    }
}
