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
    routing::{delete, get, post},
};
use rustlens_config::IdentityConfig;

use state::AppState;

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    let _otel_guard = rustlens_otel::init("rustlens-identity", "identity=info,tower_http=info");

    let config = IdentityConfig::from_env().context("loading configuration")?;
    // Install the client-IP trust config so api-key last-used + admin audit rows
    // record the real client IP from the trusted edge header (Go `clientip`).
    rustlens_httpauth::clientip::init(config.trust_proxy_headers, &config.trust_proxy_ip_header);
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

    // ---- Bot-internal (JWT-authed) identity routes, under the `identity/`
    // domain prefix. ----
    let router = router.route(
        "/api/internal/bot/identity/command-usage",
        post(bot::record_command_usage),
    );
    let router = router.route(
        "/api/internal/bot/identity/bans/{discordID}",
        get(bot::get_user_ban).put(bot::update_user_ban),
    );
    let router = router.route(
        "/api/internal/bot/identity/discord/{discordID}/is-website-admin",
        get(bot::is_website_admin),
    );
    let router = router.route(
        "/api/internal/bot/identity/discord/{discordID}/has-pro-role",
        get(bot::has_pro_role),
    );
    router.route(
        "/api/internal/bot/identity/users/ensure",
        post(bot::ensure_app_user),
    )
}

#[cfg(test)]
mod tests {
    use super::build_router;

    /// Constructing the router adds every route to matchit, so a bare-param /
    /// literal-segment conflict panics here in `cargo test` rather than at
    /// service boot.
    #[test]
    fn router_builds() {
        let _ = build_router();
    }
}
