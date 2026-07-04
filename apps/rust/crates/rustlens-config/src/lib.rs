//! Shared env-based configuration for the RustLens Rust services.
//!
//! Variable names and defaults mirror the Go backend
//! (`apps/backend/internal/config/config.go`) so the services can share the
//! same environment/secret set.

use std::env;

#[derive(Debug, thiserror::Error)]
pub enum ConfigError {
    #[error("missing required environment variable: {0}")]
    Missing(&'static str),
    #[error("invalid value for {0}: {1}")]
    Invalid(&'static str, String),
}

/// Configuration for the identity service.
#[derive(Debug, Clone)]
pub struct IdentityConfig {
    pub port: u16,
    pub postgres_url: String,
    /// Redis connection URL for the SSE cross-pod event bridge. Mirrors the Go
    /// `REDIS_URL` default.
    pub redis_url: String,
    /// HMAC secret for the `rustlens_session` cookie. Shared with the Go
    /// backend and the Next.js frontend.
    pub app_session_secret: String,
    /// Shared internal secret gating the `/api/app/*` surface (the
    /// `X-RustLens-App-Secret` header). Only the Next.js server sends it.
    /// Shared with the Go backend (`APP_API_INTERNAL_SECRET`).
    pub app_api_internal_secret: String,
    pub discord_client_id: String,
    pub discord_client_secret: String,
    /// Canonical external origin used to build OAuth redirect URIs, e.g.
    /// `https://rustlens.app`. The Discord callback is
    /// `{app_origin}/api/auth/discord/callback`.
    pub app_origin: String,
    /// Where the browser lands after a successful login when no explicit
    /// return path is supplied. Defaults to `{app_base_url}/dashboard`.
    pub app_base_url: String,
    /// Base64 AES-256 key for encrypting Discord role-connection OAuth tokens at
    /// rest (`PROXY_ENCRYPTION_KEY`, shared with the Go backend). When unset the
    /// linked-roles feature is disabled.
    pub proxy_encryption_key: Option<String>,
    /// Discord bot token, used to register the linked-roles metadata schema at
    /// startup (`DISCORD_BOT_TOKEN`).
    pub discord_bot_token: Option<String>,
    /// HMAC secret for the bot service JWT gating `/api/internal/bot/*`. Shared
    /// with the Go backend + bot (`BOT_SERVICE_JWT_SECRET`).
    pub bot_service_jwt_secret: String,
    /// Bot JWT issuer claim (`BOT_JWT_ISSUER`, default `rustlens-bot`).
    pub bot_jwt_issuer: String,
    /// Bot JWT audience claim (`BOT_JWT_AUDIENCE`, default `rustlens-backend`).
    pub bot_jwt_audience: String,
}

impl IdentityConfig {
    /// Load from the process environment, applying the same defaults as the Go
    /// backend. Returns an error if a required secret is missing.
    pub fn from_env() -> Result<Self, ConfigError> {
        Ok(Self {
            port: parse_port("PORT", 8090)?,
            postgres_url: opt("POSTGRES_URL").unwrap_or_else(|| {
                "postgres://rustlens:rustlens@localhost:5432/rustlens?sslmode=disable".to_string()
            }),
            redis_url: opt("REDIS_URL").unwrap_or_else(|| "redis://localhost:6379/0".to_string()),
            app_session_secret: req("APP_SESSION_SECRET")?,
            app_api_internal_secret: req("APP_API_INTERNAL_SECRET")?,
            discord_client_id: req("DISCORD_CLIENT_ID")?,
            discord_client_secret: req("DISCORD_CLIENT_SECRET")?,
            app_origin: opt("APP_ORIGIN").unwrap_or_else(|| "http://localhost".to_string()),
            app_base_url: opt("APP_BASE_URL")
                .unwrap_or_else(|| "http://localhost:3000".to_string()),
            proxy_encryption_key: opt("PROXY_ENCRYPTION_KEY"),
            discord_bot_token: opt("DISCORD_BOT_TOKEN"),
            bot_service_jwt_secret: req("BOT_SERVICE_JWT_SECRET")?,
            bot_jwt_issuer: opt("BOT_JWT_ISSUER").unwrap_or_else(|| "rustlens-bot".to_string()),
            bot_jwt_audience: opt("BOT_JWT_AUDIENCE")
                .unwrap_or_else(|| "rustlens-backend".to_string()),
        })
    }

    /// The Discord OAuth callback URL: `{app_origin}/api/auth/discord/callback`.
    pub fn discord_redirect_uri(&self) -> String {
        format!(
            "{}/api/auth/discord/callback",
            self.app_origin.trim_end_matches('/')
        )
    }

    /// Build a redirect URL to a frontend path. Mirrors the Go backend's
    /// `frontendURL(path)`: `trim_end('/', app_origin) + path`. `path` should
    /// start with `/`.
    pub fn frontend_url(&self, path: &str) -> String {
        format!("{}{}", self.app_origin.trim_end_matches('/'), path)
    }

    /// Default post-login destination: `{app_origin}/dashboard`.
    pub fn default_post_login_url(&self) -> String {
        self.frontend_url("/dashboard")
    }

    /// The Discord role-connection OAuth callback URL.
    pub fn role_connection_redirect_uri(&self) -> String {
        format!(
            "{}/api/auth/discord/role-connection/callback",
            self.app_origin.trim_end_matches('/')
        )
    }

    /// Whether the Discord linked-roles feature is configured (encryption key +
    /// Discord client credentials present).
    pub fn role_connection_enabled(&self) -> bool {
        self.proxy_encryption_key.is_some() && !self.discord_client_id.is_empty()
    }
}

/// Configuration for the players service (Phase 2). Shares the identity env set
/// plus the Steam/BattleMetrics/proxy/platformsync variables. Names + defaults
/// mirror the Go backend.
#[derive(Debug, Clone)]
pub struct PlayersConfig {
    pub port: u16,
    pub postgres_url: String,
    pub redis_url: String,
    /// HMAC secret for the `rustlens_session` token (app routes).
    pub app_session_secret: String,
    /// `X-RustLens-App-Secret` gate for `/api/app/*`.
    pub app_api_internal_secret: String,
    /// Bot service JWT secret + issuer/audience for `/api/internal/bot/*`.
    pub bot_service_jwt_secret: String,
    pub bot_jwt_issuer: String,
    pub bot_jwt_audience: String,
    /// Steam Web API key (`STEAM_API_KEY`); empty disables Steam calls.
    pub steam_api_key: String,
    /// bmsearch.io primary resolver toggle + URL + inline cold-lookup budget.
    pub bm_search_enabled: bool,
    pub bm_search_url: String,
    pub bm_search_inline_timeout_ms: u64,
    /// Legacy bmmatch fallback (empty disables).
    pub bm_match_url: String,
    pub bm_match_token: String,
    /// platformsync.io bearer token (empty disables).
    pub platformsync_token: String,
    /// Outbound proxy provider (BattleMetrics only).
    pub proxy_enabled: bool,
    pub proxy_endpoints: Vec<String>,
    pub proxy_encryption_key: Option<String>,
}

impl PlayersConfig {
    /// Load from the process environment with the Go backend's defaults.
    pub fn from_env() -> Result<Self, ConfigError> {
        Ok(Self {
            port: parse_port("PORT", 8091)?,
            postgres_url: opt("POSTGRES_URL").unwrap_or_else(|| {
                "postgres://rustlens:rustlens@localhost:5432/rustlens?sslmode=disable".to_string()
            }),
            redis_url: opt("REDIS_URL").unwrap_or_else(|| "redis://localhost:6379/0".to_string()),
            app_session_secret: req("APP_SESSION_SECRET")?,
            app_api_internal_secret: req("APP_API_INTERNAL_SECRET")?,
            bot_service_jwt_secret: req("BOT_SERVICE_JWT_SECRET")?,
            bot_jwt_issuer: opt("BOT_JWT_ISSUER").unwrap_or_else(|| "rustlens-bot".to_string()),
            bot_jwt_audience: opt("BOT_JWT_AUDIENCE")
                .unwrap_or_else(|| "rustlens-backend".to_string()),
            steam_api_key: opt("STEAM_API_KEY").unwrap_or_default(),
            bm_search_enabled: parse_bool("BM_SEARCH_ENABLED", true)?,
            bm_search_url: opt("BM_SEARCH_URL")
                .unwrap_or_else(|| "https://bmsearch.io/api".to_string()),
            bm_search_inline_timeout_ms: parse_u64("BM_SEARCH_INLINE_TIMEOUT_MS", 4000)?,
            bm_match_url: opt("BM_MATCH_URL").unwrap_or_default(),
            bm_match_token: opt("BM_MATCH_TOKEN").unwrap_or_default(),
            platformsync_token: opt("PLATFORMSYNC_TOKEN").unwrap_or_default(),
            proxy_enabled: parse_bool("PROXY_ENABLED", false)?,
            proxy_endpoints: opt("PROXY_ENDPOINTS")
                .map(|v| {
                    v.split(',')
                        .map(str::trim)
                        .filter(|s| !s.is_empty())
                        .map(str::to_string)
                        .collect()
                })
                .unwrap_or_default(),
            proxy_encryption_key: opt("PROXY_ENCRYPTION_KEY"),
        })
    }
}

/// Configuration for the servers service (Phase 3): tracked servers, presence,
/// analytics, and the outbound-proxy infra + the poller/refresher workers.
#[derive(Debug, Clone)]
pub struct ServersConfig {
    pub port: u16,
    pub postgres_url: String,
    pub redis_url: String,
    pub app_session_secret: String,
    pub app_api_internal_secret: String,
    pub bot_service_jwt_secret: String,
    pub bot_jwt_issuer: String,
    pub bot_jwt_audience: String,
    /// BattleMetrics poll interval (seconds; `0` disables the poller).
    pub battlemetrics_poll_interval_seconds: u64,
    pub proxy_enabled: bool,
    pub proxy_endpoints: Vec<String>,
    pub proxy_encryption_key: Option<String>,
}

impl ServersConfig {
    /// Load from the process environment with the Go backend's defaults.
    pub fn from_env() -> Result<Self, ConfigError> {
        Ok(Self {
            port: parse_port("PORT", 8092)?,
            postgres_url: opt("POSTGRES_URL").unwrap_or_else(|| {
                "postgres://rustlens:rustlens@localhost:5432/rustlens?sslmode=disable".to_string()
            }),
            redis_url: opt("REDIS_URL").unwrap_or_else(|| "redis://localhost:6379/0".to_string()),
            app_session_secret: req("APP_SESSION_SECRET")?,
            app_api_internal_secret: req("APP_API_INTERNAL_SECRET")?,
            bot_service_jwt_secret: req("BOT_SERVICE_JWT_SECRET")?,
            bot_jwt_issuer: opt("BOT_JWT_ISSUER").unwrap_or_else(|| "rustlens-bot".to_string()),
            bot_jwt_audience: opt("BOT_JWT_AUDIENCE")
                .unwrap_or_else(|| "rustlens-backend".to_string()),
            battlemetrics_poll_interval_seconds: parse_u64(
                "BATTLEMETRICS_SERVER_POLL_INTERVAL_SECONDS",
                30,
            )?,
            proxy_enabled: parse_bool("PROXY_ENABLED", false)?,
            proxy_endpoints: opt("PROXY_ENDPOINTS")
                .map(|v| {
                    v.split(',')
                        .map(str::trim)
                        .filter(|s| !s.is_empty())
                        .map(str::to_string)
                        .collect()
                })
                .unwrap_or_default(),
            proxy_encryption_key: opt("PROXY_ENCRYPTION_KEY"),
        })
    }
}

/// Configuration for the guilds service (Phase 4): guild settings, wipe-reacts +
/// schedules + the schedule-post scheduler, track-groups, guild/user/install
/// sync, and the admin guild list/detail. Shares the identity env set; needs
/// Redis for the scheduler worker lock and the admin auth secrets.
#[derive(Debug, Clone)]
pub struct GuildsConfig {
    pub port: u16,
    pub postgres_url: String,
    pub redis_url: String,
    pub app_session_secret: String,
    pub app_api_internal_secret: String,
    pub bot_service_jwt_secret: String,
    pub bot_jwt_issuer: String,
    pub bot_jwt_audience: String,
    /// Wipe-react scheduler tick interval (seconds; `0` disables the poller).
    /// Mirrors the Go `schedulerInterval` of 60s.
    pub wipe_scheduler_interval_seconds: u64,
}

impl GuildsConfig {
    /// Load from the process environment with the Go backend's defaults.
    pub fn from_env() -> Result<Self, ConfigError> {
        Ok(Self {
            port: parse_port("PORT", 8093)?,
            postgres_url: opt("POSTGRES_URL").unwrap_or_else(|| {
                "postgres://rustlens:rustlens@localhost:5432/rustlens?sslmode=disable".to_string()
            }),
            redis_url: opt("REDIS_URL").unwrap_or_else(|| "redis://localhost:6379/0".to_string()),
            app_session_secret: req("APP_SESSION_SECRET")?,
            app_api_internal_secret: req("APP_API_INTERNAL_SECRET")?,
            bot_service_jwt_secret: req("BOT_SERVICE_JWT_SECRET")?,
            bot_jwt_issuer: opt("BOT_JWT_ISSUER").unwrap_or_else(|| "rustlens-bot".to_string()),
            bot_jwt_audience: opt("BOT_JWT_AUDIENCE")
                .unwrap_or_else(|| "rustlens-backend".to_string()),
            wipe_scheduler_interval_seconds: parse_u64("WIPE_SCHEDULER_INTERVAL_SECONDS", 60)?,
        })
    }
}

/// Configuration for the coordinator service (Phase 5): shard coordination,
/// owner stats, rate-limit consume, Discord webhook events, and the cleanup
/// retention sweep. Shard/rate-limit state lives in Redis (shared with the bot).
#[derive(Debug, Clone)]
pub struct CoordinatorConfig {
    pub port: u16,
    pub postgres_url: String,
    pub redis_url: String,
    pub bot_service_jwt_secret: String,
    pub bot_jwt_issuer: String,
    pub bot_jwt_audience: String,
    /// Target shard count (`BOT_TARGET_SHARDS`, default 4).
    pub bot_target_shards: i64,
    /// Worker heartbeat / lease TTL in seconds (`BOT_HEARTBEAT_TTL_SECONDS`, 45).
    pub bot_heartbeat_ttl_seconds: u64,
    /// Discord application public key (hex Ed25519) for webhook-event signature
    /// verification (`DISCORD_APP_PUBLIC_KEY`); empty disables the webhook route.
    pub discord_app_public_key: String,
    /// Cleanup sweep cadence + retention windows (seconds). Defaults mirror Go's
    /// 1h / 7d / 48h / 90d.
    pub cleanup_interval_seconds: u64,
    pub cleanup_wipe_react_retention_seconds: i64,
    pub cleanup_analytics_cache_retention_seconds: i64,
    pub cleanup_command_usage_retention_seconds: i64,
}

impl CoordinatorConfig {
    /// Load from the process environment with the Go backend's defaults.
    pub fn from_env() -> Result<Self, ConfigError> {
        Ok(Self {
            port: parse_port("PORT", 8094)?,
            postgres_url: opt("POSTGRES_URL").unwrap_or_else(|| {
                "postgres://rustlens:rustlens@localhost:5432/rustlens?sslmode=disable".to_string()
            }),
            redis_url: opt("REDIS_URL").unwrap_or_else(|| "redis://localhost:6379/0".to_string()),
            bot_service_jwt_secret: req("BOT_SERVICE_JWT_SECRET")?,
            bot_jwt_issuer: opt("BOT_JWT_ISSUER").unwrap_or_else(|| "rustlens-bot".to_string()),
            bot_jwt_audience: opt("BOT_JWT_AUDIENCE")
                .unwrap_or_else(|| "rustlens-backend".to_string()),
            bot_target_shards: parse_i64("BOT_TARGET_SHARDS", 4)?,
            bot_heartbeat_ttl_seconds: parse_u64("BOT_HEARTBEAT_TTL_SECONDS", 45)?,
            discord_app_public_key: opt("DISCORD_APP_PUBLIC_KEY").unwrap_or_default(),
            cleanup_interval_seconds: parse_u64("CLEANUP_INTERVAL_SECONDS", 3600)?,
            cleanup_wipe_react_retention_seconds: parse_i64(
                "CLEANUP_WIPE_REACT_RETENTION_SECONDS",
                7 * 24 * 3600,
            )?,
            cleanup_analytics_cache_retention_seconds: parse_i64(
                "CLEANUP_ANALYTICS_CACHE_RETENTION_SECONDS",
                48 * 3600,
            )?,
            cleanup_command_usage_retention_seconds: parse_i64(
                "CLEANUP_COMMAND_USAGE_RETENTION_SECONDS",
                90 * 24 * 3600,
            )?,
        })
    }
}

fn req(key: &'static str) -> Result<String, ConfigError> {
    match env::var(key) {
        Ok(v) if !v.is_empty() => Ok(v),
        _ => Err(ConfigError::Missing(key)),
    }
}

fn opt(key: &str) -> Option<String> {
    env::var(key).ok().filter(|v| !v.is_empty())
}

fn parse_port(key: &'static str, default: u16) -> Result<u16, ConfigError> {
    match opt(key) {
        None => Ok(default),
        Some(v) => v
            .parse::<u16>()
            .map_err(|e| ConfigError::Invalid(key, e.to_string())),
    }
}

fn parse_u64(key: &'static str, default: u64) -> Result<u64, ConfigError> {
    match opt(key) {
        None => Ok(default),
        Some(v) => v
            .parse::<u64>()
            .map_err(|e| ConfigError::Invalid(key, e.to_string())),
    }
}

fn parse_i64(key: &'static str, default: i64) -> Result<i64, ConfigError> {
    match opt(key) {
        None => Ok(default),
        Some(v) => v
            .parse::<i64>()
            .map_err(|e| ConfigError::Invalid(key, e.to_string())),
    }
}

fn parse_bool(key: &'static str, default: bool) -> Result<bool, ConfigError> {
    match opt(key) {
        None => Ok(default),
        Some(v) => match v.to_ascii_lowercase().as_str() {
            "1" | "true" | "t" | "yes" | "y" => Ok(true),
            "0" | "false" | "f" | "no" | "n" => Ok(false),
            _ => Err(ConfigError::Invalid(key, v)),
        },
    }
}
