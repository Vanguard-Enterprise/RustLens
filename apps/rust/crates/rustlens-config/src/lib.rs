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
            app_session_secret: req("APP_SESSION_SECRET")?,
            app_api_internal_secret: req("APP_API_INTERNAL_SECRET")?,
            discord_client_id: req("DISCORD_CLIENT_ID")?,
            discord_client_secret: req("DISCORD_CLIENT_SECRET")?,
            app_origin: opt("APP_ORIGIN").unwrap_or_else(|| "http://localhost".to_string()),
            app_base_url: opt("APP_BASE_URL")
                .unwrap_or_else(|| "http://localhost:3000".to_string()),
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
