//! RustLens guilds service.
//!
//! Guild settings, wipe-reacts + schedules + the schedule-post scheduler,
//! track-groups, guild/user/install sync, and the admin guild list/detail.
//! Talks to the same Postgres as the Go backend (schema owned by Go). PORT 8093.

mod admin_guilds;
mod admin_user_installs;
mod auth;
mod redis_lock;
mod scheduler;
mod schedules;
mod settings;
mod state;
mod sync;
mod track_groups;
mod wipe_reacts;

use std::sync::Arc;

use anyhow::Context;
use axum::{
    Router,
    routing::{MethodRouter, delete, get, post},
};
use rustlens_config::GuildsConfig;

use state::AppState;

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    let _otel_guard = rustlens_otel::init("rustlens-guilds", "guilds=info,tower_http=info");

    let config = GuildsConfig::from_env().context("loading configuration")?;
    let pool = rustlens_db::connect(&config.postgres_url, 10)
        .await
        .context("connecting to Postgres")?;

    // Redis for the scheduler worker lock (falls back to single-pod when absent).
    let redis = match redis::Client::open(config.redis_url.as_str()) {
        Ok(client) => match client.get_multiplexed_async_connection().await {
            Ok(conn) => Some(conn),
            Err(e) => {
                tracing::warn!("redis unavailable, scheduler runs single-pod: {e}");
                None
            }
        },
        Err(e) => {
            tracing::warn!("invalid REDIS_URL, scheduler runs single-pod: {e}");
            None
        }
    };

    let state = Arc::new(AppState { config, pool, redis });

    // Spawn the background wipe-react scheduler.
    tokio::spawn(scheduler::run(state.clone()));

    let app = build_router().with_state(state.clone());

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

/// Registers a bot-internal route under BOTH the legacy flat `setup/` path and
/// the new `guilds/` domain-prefix path (D1 domain-prefix reorg). The flat
/// paths are dropped wholesale in Phase E once every domain's bot deploy has
/// cut over; until then Rust serves both so the bot deploy is the only cutover
/// moment and reverting the bot is a full rollback.
///
/// `MethodRouter` is `Clone`, so the same handler(s) back both paths. Callers
/// pass `old` and `new` explicitly rather than deriving one from the other, so
/// the reorg stays auditable route-by-route.
fn dual_route(
    router: Router<Arc<AppState>>,
    old_path: &str,
    new_path: &str,
    method: MethodRouter<Arc<AppState>>,
) -> Router<Arc<AppState>> {
    router.route(old_path, method.clone()).route(new_path, method)
}

/// Builds the full guilds router (state left unresolved so tests can construct
/// it without a live pool). Adding the routes is what triggers any matchit
/// conflict panic, so `router_builds` below constructs this at test time and
/// fails `cargo test` instead of failing at service boot.
///
/// Route-scheme note: per the D1 decision, guild-scoped routes read
/// `/guilds/{guildID}/...` with NO doubled `guilds/guilds/` segment. That puts
/// a bare `{guildID}` param at the same path position as this service's other
/// top-level literals (`user-guilds`, `discord`, `bot-guilds`,
/// `bot-user-installs`) directly under `/api/internal/bot/guilds/`. Unlike
/// players' `bm/{bmID}` case (where axum/matchit panics because the bare param
/// shares its position with sibling literals), matchit 0.8 accepts this shape:
/// static segments take priority over a param at the same position, so
/// `/guilds/user-guilds` and `/guilds/{guildID}/settings` can coexist. Proven
/// by `router_builds` below; if that test ever starts panicking (e.g. on a
/// matchit upgrade), fall back to a doubled `/guilds/guilds/{guildID}/...`
/// segment for the guild-scoped routes instead.
fn build_router() -> Router<Arc<AppState>> {
    let router = Router::new().route("/api/healthz", get(healthz));

    // ---- guild settings ----
    let router = dual_route(
        router,
        "/api/internal/bot/setup/guilds/{guildID}/settings",
        "/api/internal/bot/guilds/{guildID}/settings",
        get(settings::get_guild_settings).put(settings::update_guild_settings),
    );

    // ---- user-guilds ----
    let router = dual_route(
        router,
        "/api/internal/bot/setup/user-guilds",
        "/api/internal/bot/guilds/user-guilds",
        post(sync::create_user_guild_link),
    );
    let router = dual_route(
        router,
        "/api/internal/bot/setup/user-guilds/leave",
        "/api/internal/bot/guilds/user-guilds/leave",
        post(sync::mark_user_guild_left),
    );
    let router = dual_route(
        router,
        "/api/internal/bot/setup/discord/{discordID}/guilds",
        "/api/internal/bot/guilds/discord/{discordID}/guilds",
        get(sync::list_user_guild_links_by_discord),
    );

    // ---- bot-guilds ----
    let router = dual_route(
        router,
        "/api/internal/bot/setup/bot-guilds/upsert",
        "/api/internal/bot/guilds/bot-guilds/upsert",
        post(sync::upsert_bot_guild),
    );
    let router = dual_route(
        router,
        "/api/internal/bot/setup/bot-guilds/leave",
        "/api/internal/bot/guilds/bot-guilds/leave",
        post(sync::mark_bot_guild_left),
    );
    let router = dual_route(
        router,
        "/api/internal/bot/setup/bot-guilds/reconcile",
        "/api/internal/bot/guilds/bot-guilds/reconcile",
        post(sync::reconcile_bot_guilds),
    );

    // ---- bot-user-installs ----
    let router = dual_route(
        router,
        "/api/internal/bot/setup/bot-user-installs/seen",
        "/api/internal/bot/guilds/bot-user-installs/seen",
        post(sync::touch_bot_user_install_seen),
    );

    // ---- wipe-reacts ----
    let router = dual_route(
        router,
        "/api/internal/bot/setup/guilds/{guildID}/wipe-reacts/active",
        "/api/internal/bot/guilds/{guildID}/wipe-reacts/active",
        get(wipe_reacts::list_active_wipe_reacts),
    );
    let router = dual_route(
        router,
        "/api/internal/bot/setup/guilds/{guildID}/wipe-reacts",
        "/api/internal/bot/guilds/{guildID}/wipe-reacts",
        get(wipe_reacts::list_wipe_reacts).post(wipe_reacts::create_wipe_react),
    );
    let router = dual_route(
        router,
        "/api/internal/bot/setup/guilds/{guildID}/wipe-reacts/{messageID}",
        "/api/internal/bot/guilds/{guildID}/wipe-reacts/{messageID}",
        get(wipe_reacts::get_wipe_react)
            .put(wipe_reacts::update_wipe_react)
            .delete(wipe_reacts::delete_wipe_react),
    );
    let router = dual_route(
        router,
        "/api/internal/bot/setup/guilds/{guildID}/wipe-reacts/{messageID}/reactions",
        "/api/internal/bot/guilds/{guildID}/wipe-reacts/{messageID}/reactions",
        post(wipe_reacts::set_wipe_react_reaction),
    );
    let router = dual_route(
        router,
        "/api/internal/bot/setup/guilds/{guildID}/wipe-reacts/{messageID}/reactions/{userID}",
        "/api/internal/bot/guilds/{guildID}/wipe-reacts/{messageID}/reactions/{userID}",
        delete(wipe_reacts::remove_wipe_react_reaction),
    );

    // ---- wipe-react schedules ----
    let router = dual_route(
        router,
        "/api/internal/bot/setup/guilds/{guildID}/wipe-react-schedules",
        "/api/internal/bot/guilds/{guildID}/wipe-react-schedules",
        get(schedules::list_wipe_react_schedules).post(schedules::create_wipe_react_schedule),
    );
    let router = dual_route(
        router,
        "/api/internal/bot/setup/guilds/{guildID}/wipe-react-schedules/{scheduleID}",
        "/api/internal/bot/guilds/{guildID}/wipe-react-schedules/{scheduleID}",
        get(schedules::get_wipe_react_schedule)
            .put(schedules::update_wipe_react_schedule)
            .delete(schedules::delete_wipe_react_schedule),
    );

    // ---- wipe-react schedule-post queue ----
    let router = dual_route(
        router,
        "/api/internal/bot/setup/guilds/{guildID}/wipe-react-schedule-posts/claim",
        "/api/internal/bot/guilds/{guildID}/wipe-react-schedule-posts/claim",
        post(schedules::claim_next_schedule_post),
    );
    let router = dual_route(
        router,
        "/api/internal/bot/setup/guilds/{guildID}/wipe-react-schedule-posts/{scheduleID}/complete",
        "/api/internal/bot/guilds/{guildID}/wipe-react-schedule-posts/{scheduleID}/complete",
        post(schedules::complete_schedule_post),
    );
    let router = dual_route(
        router,
        "/api/internal/bot/setup/guilds/{guildID}/wipe-react-schedule-posts/{scheduleID}/release",
        "/api/internal/bot/guilds/{guildID}/wipe-react-schedule-posts/{scheduleID}/release",
        post(schedules::release_schedule_post),
    );

    // ---- track groups ----
    let router = dual_route(
        router,
        "/api/internal/bot/setup/guilds/{guildID}/track-groups",
        "/api/internal/bot/guilds/{guildID}/track-groups",
        get(track_groups::list_track_groups).post(track_groups::create_track_group),
    );
    let router = dual_route(
        router,
        "/api/internal/bot/setup/guilds/{guildID}/track-groups/{groupID}",
        "/api/internal/bot/guilds/{guildID}/track-groups/{groupID}",
        get(track_groups::get_track_group)
            .put(track_groups::update_track_group)
            .delete(track_groups::delete_track_group),
    );
    let router = dual_route(
        router,
        "/api/internal/bot/setup/guilds/{guildID}/track-groups/{groupID}/members",
        "/api/internal/bot/guilds/{guildID}/track-groups/{groupID}/members",
        get(track_groups::list_track_group_members).post(track_groups::add_track_group_members),
    );
    let router = dual_route(
        router,
        "/api/internal/bot/setup/guilds/{guildID}/track-groups/{groupID}/members/remove",
        "/api/internal/bot/guilds/{guildID}/track-groups/{groupID}/members/remove",
        post(track_groups::remove_track_group_members),
    );
    let router = dual_route(
        router,
        "/api/internal/bot/setup/guilds/{guildID}/track-groups/{groupID}/online-state",
        "/api/internal/bot/guilds/{guildID}/track-groups/{groupID}/online-state",
        get(track_groups::list_track_group_online_state),
    );
    let router = dual_route(
        router,
        "/api/internal/bot/setup/guilds/{guildID}/track-groups/{groupID}/notifications/claim",
        "/api/internal/bot/guilds/{guildID}/track-groups/{groupID}/notifications/claim",
        post(track_groups::claim_track_group_notification),
    );

    router
        // ---- admin guild list + detail (app-secret + session + guilds.read); unchanged ----
        .route("/api/app/admin/guilds", get(admin_guilds::list_admin_guilds))
        .route("/api/app/admin/guilds/{guildID}", get(admin_guilds::get_admin_guild))
        // ---- admin user-installs list (app-secret + session + users.read); unchanged ----
        .route(
            "/api/app/admin/user-installs",
            get(admin_user_installs::list_admin_user_installs),
        )
}

#[cfg(test)]
mod tests {
    use super::build_router;

    /// Constructing the router adds every route to matchit, so a bare-param /
    /// literal-segment conflict panics here in `cargo test` rather than at
    /// service boot. This guards the dual-registration reorg, including the
    /// no-doubled-segment guild-scoped scheme (see `build_router` docs).
    #[test]
    fn router_builds() {
        let _ = build_router();
    }
}
