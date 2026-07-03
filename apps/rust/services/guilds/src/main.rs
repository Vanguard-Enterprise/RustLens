//! RustLens guilds service.
//!
//! Guild settings, wipe-reacts + schedules + the schedule-post scheduler,
//! track-groups, guild/user/install sync, and the admin guild list/detail.
//! Talks to the same Postgres as the Go backend (schema owned by Go). PORT 8093.

mod admin_guilds;
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
    routing::{delete, get, post},
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
        // ---- wipe-reacts ----
        .route(
            "/api/internal/bot/setup/guilds/{guildID}/wipe-reacts/active",
            get(wipe_reacts::list_active_wipe_reacts),
        )
        .route(
            "/api/internal/bot/setup/guilds/{guildID}/wipe-reacts",
            get(wipe_reacts::list_wipe_reacts).post(wipe_reacts::create_wipe_react),
        )
        .route(
            "/api/internal/bot/setup/guilds/{guildID}/wipe-reacts/{messageID}",
            get(wipe_reacts::get_wipe_react)
                .put(wipe_reacts::update_wipe_react)
                .delete(wipe_reacts::delete_wipe_react),
        )
        .route(
            "/api/internal/bot/setup/guilds/{guildID}/wipe-reacts/{messageID}/reactions",
            post(wipe_reacts::set_wipe_react_reaction),
        )
        .route(
            "/api/internal/bot/setup/guilds/{guildID}/wipe-reacts/{messageID}/reactions/{userID}",
            delete(wipe_reacts::remove_wipe_react_reaction),
        )
        // ---- wipe-react schedules ----
        .route(
            "/api/internal/bot/setup/guilds/{guildID}/wipe-react-schedules",
            get(schedules::list_wipe_react_schedules).post(schedules::create_wipe_react_schedule),
        )
        .route(
            "/api/internal/bot/setup/guilds/{guildID}/wipe-react-schedules/{scheduleID}",
            get(schedules::get_wipe_react_schedule)
                .put(schedules::update_wipe_react_schedule)
                .delete(schedules::delete_wipe_react_schedule),
        )
        // ---- wipe-react schedule-post queue ----
        .route(
            "/api/internal/bot/setup/guilds/{guildID}/wipe-react-schedule-posts/claim",
            post(schedules::claim_next_schedule_post),
        )
        .route(
            "/api/internal/bot/setup/guilds/{guildID}/wipe-react-schedule-posts/{scheduleID}/complete",
            post(schedules::complete_schedule_post),
        )
        .route(
            "/api/internal/bot/setup/guilds/{guildID}/wipe-react-schedule-posts/{scheduleID}/release",
            post(schedules::release_schedule_post),
        )
        // ---- track groups ----
        .route(
            "/api/internal/bot/setup/guilds/{guildID}/track-groups",
            get(track_groups::list_track_groups).post(track_groups::create_track_group),
        )
        .route(
            "/api/internal/bot/setup/guilds/{guildID}/track-groups/{groupID}",
            get(track_groups::get_track_group)
                .put(track_groups::update_track_group)
                .delete(track_groups::delete_track_group),
        )
        .route(
            "/api/internal/bot/setup/guilds/{guildID}/track-groups/{groupID}/members",
            get(track_groups::list_track_group_members).post(track_groups::add_track_group_members),
        )
        .route(
            "/api/internal/bot/setup/guilds/{guildID}/track-groups/{groupID}/members/remove",
            post(track_groups::remove_track_group_members),
        )
        .route(
            "/api/internal/bot/setup/guilds/{guildID}/track-groups/{groupID}/online-state",
            get(track_groups::list_track_group_online_state),
        )
        .route(
            "/api/internal/bot/setup/guilds/{guildID}/track-groups/{groupID}/notifications/claim",
            post(track_groups::claim_track_group_notification),
        )
        // ---- admin guild list + detail (app-secret + session + guilds.read) ----
        .route("/api/app/admin/guilds", get(admin_guilds::list_admin_guilds))
        .route("/api/app/admin/guilds/{guildID}", get(admin_guilds::get_admin_guild))
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
