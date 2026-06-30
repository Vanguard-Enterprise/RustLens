//! Discord OAuth client. Mirrors `apps/backend/internal/oauth/discord`:
//! PKCE (S256), `identify guilds` scope, form-encoded token exchange,
//! `GET /api/users/@me` profile fetch.

use anyhow::{Context, anyhow};
use base64::Engine;
use base64::engine::general_purpose::URL_SAFE_NO_PAD;
use rand::RngCore;
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};

use rustlens_db::identity::DiscordProfile;

const DISCORD_BASE: &str = "https://discord.com";

/// 32 random bytes, base64url(no-pad) — used for both the PKCE verifier and the
/// CSRF `state` (matches Go's `NewPkceVerifier` / state generation).
pub fn random_token_32() -> String {
    let mut buf = [0u8; 32];
    rand::thread_rng().fill_bytes(&mut buf);
    URL_SAFE_NO_PAD.encode(buf)
}

/// PKCE S256 challenge: base64url(no-pad) of SHA256(verifier). Matches
/// `PkceChallenge`.
pub fn pkce_challenge(verifier: &str) -> String {
    let digest = Sha256::digest(verifier.as_bytes());
    URL_SAFE_NO_PAD.encode(digest)
}

/// New app-user id: `usr_` + 24 lowercase hex chars (12 random bytes). Matches
/// `appauth.newUserID`.
pub fn new_user_id() -> String {
    let mut buf = [0u8; 12];
    rand::thread_rng().fill_bytes(&mut buf);
    let mut hex = String::with_capacity(24);
    for b in buf {
        hex.push_str(&format!("{b:02x}"));
    }
    format!("usr_{hex}")
}

/// Build the Discord authorize URL with the exact query params the Go backend
/// uses.
pub fn authorize_url(client_id: &str, redirect_uri: &str, state: &str, challenge: &str) -> String {
    let mut url = url::Url::parse("https://discord.com/oauth2/authorize")
        .expect("static authorize URL is valid");
    url.query_pairs_mut()
        .append_pair("client_id", client_id)
        .append_pair("response_type", "code")
        .append_pair("redirect_uri", redirect_uri)
        .append_pair("scope", "identify guilds")
        .append_pair("prompt", "consent")
        .append_pair("state", state)
        .append_pair("code_challenge", challenge)
        .append_pair("code_challenge_method", "S256");
    url.to_string()
}

/// Exchange an authorization code for an access token (form-encoded POST).
pub async fn exchange_code(
    http: &reqwest::Client,
    client_id: &str,
    client_secret: &str,
    code: &str,
    code_verifier: &str,
    redirect_uri: &str,
) -> anyhow::Result<String> {
    let params = [
        ("client_id", client_id),
        ("client_secret", client_secret),
        ("grant_type", "authorization_code"),
        ("code", code),
        ("code_verifier", code_verifier),
        ("redirect_uri", redirect_uri),
    ];
    let resp = http
        .post(format!("{DISCORD_BASE}/api/oauth2/token"))
        .header("Cache-Control", "no-store")
        .form(&params)
        .send()
        .await
        .context("discord token request")?;
    let status = resp.status();
    let body = resp.text().await.unwrap_or_default();
    if !status.is_success() {
        return Err(anyhow!("discord token exchange: {status} {body}"));
    }

    #[derive(Deserialize)]
    struct TokenResp {
        access_token: Option<String>,
    }
    let parsed: TokenResp = serde_json::from_str(&body).context("parse token response")?;
    parsed
        .access_token
        .filter(|t| !t.is_empty())
        .ok_or_else(|| anyhow!("discord token exchange: empty access_token"))
}

/// Fetch the authenticated user's profile.
pub async fn fetch_profile(
    http: &reqwest::Client,
    access_token: &str,
) -> anyhow::Result<DiscordProfile> {
    let resp = http
        .get(format!("{DISCORD_BASE}/api/users/@me"))
        .header("Authorization", format!("Bearer {access_token}"))
        .header("Cache-Control", "no-store")
        .send()
        .await
        .context("discord profile request")?;
    let status = resp.status();
    let body = resp.text().await.unwrap_or_default();
    if !status.is_success() {
        return Err(anyhow!("discord profile fetch: {status} {body}"));
    }

    #[derive(Deserialize)]
    struct Raw {
        id: String,
        username: String,
        global_name: Option<String>,
        avatar: Option<String>,
    }
    let raw: Raw = serde_json::from_str(&body).context("parse profile response")?;
    Ok(DiscordProfile {
        id: raw.id,
        username: raw.username,
        global_name: raw.global_name.unwrap_or_default(),
        avatar_hash: raw.avatar.unwrap_or_default(),
    })
}

/// A guild the authenticated user is a member of, from
/// `GET /users/@me/guilds`. Mirrors the Go `discord.UserGuild` subset.
#[derive(Debug, Clone)]
pub struct UserGuild {
    pub id: String,
    pub name: String,
    /// Icon hash, empty when the guild has no icon (Go maps `*string` → `""`).
    pub icon_hash: String,
    pub approximate_member_count: Option<i32>,
    pub owner: bool,
}

/// Exchange an authorization code for access **and** refresh tokens (the
/// role-connection flow, no PKCE). Returns `(access, refresh, expires_in_secs)`.
/// Mirrors the Go `ExchangeCodeForTokens`.
pub async fn exchange_code_for_tokens(
    http: &reqwest::Client,
    client_id: &str,
    client_secret: &str,
    code: &str,
    redirect_uri: &str,
) -> anyhow::Result<(String, String, i64)> {
    let params = [
        ("client_id", client_id),
        ("client_secret", client_secret),
        ("grant_type", "authorization_code"),
        ("code", code),
        ("redirect_uri", redirect_uri),
    ];
    let resp = http
        .post(format!("{DISCORD_BASE}/api/oauth2/token"))
        .header("Cache-Control", "no-store")
        .form(&params)
        .send()
        .await
        .context("discord rc token request")?;
    let status = resp.status();
    let body = resp.text().await.unwrap_or_default();
    if !status.is_success() {
        return Err(anyhow!("discord token exchange: {status} {body}"));
    }
    #[derive(Deserialize)]
    struct TokenResp {
        access_token: Option<String>,
        refresh_token: Option<String>,
        expires_in: Option<i64>,
    }
    let parsed: TokenResp = serde_json::from_str(&body).context("parse rc token response")?;
    let access = parsed
        .access_token
        .filter(|t| !t.is_empty())
        .ok_or_else(|| anyhow!("discord token exchange: empty access_token"))?;
    Ok((
        access,
        parsed.refresh_token.unwrap_or_default(),
        parsed.expires_in.unwrap_or(0),
    ))
}

/// The PUT body for `/users/@me/applications/{id}/role-connection`.
#[derive(Debug, Clone, Serialize)]
pub struct RoleConnectionBody {
    pub platform_name: String,
    #[serde(skip_serializing_if = "String::is_empty")]
    pub platform_username: String,
    pub metadata: std::collections::BTreeMap<String, String>,
}

/// Push linked-roles metadata for the user. Mirrors the Go `PutRoleConnection`.
pub async fn put_role_connection(
    http: &reqwest::Client,
    access_token: &str,
    application_id: &str,
    body: &RoleConnectionBody,
) -> anyhow::Result<()> {
    let resp = http
        .put(format!(
            "{DISCORD_BASE}/api/users/@me/applications/{application_id}/role-connection"
        ))
        .header("Authorization", format!("Bearer {access_token}"))
        .header("Cache-Control", "no-store")
        .json(body)
        .send()
        .await
        .context("discord put role-connection")?;
    let status = resp.status();
    if status.is_success() {
        return Ok(());
    }
    let text = resp.text().await.unwrap_or_default();
    Err(anyhow!("discord put role-connection: {status} {text}"))
}

/// One linked-roles metadata field definition.
#[derive(Debug, Clone, Serialize)]
pub struct ApplicationMetadataRecord {
    #[serde(rename = "type")]
    pub field_type: i32,
    pub key: String,
    pub name: String,
    pub description: String,
}

/// Register the linked-roles metadata schema (bot-token auth). Mirrors the Go
/// `RegisterApplicationMetadata`.
pub async fn register_application_metadata(
    http: &reqwest::Client,
    application_id: &str,
    bot_token: &str,
    records: &[ApplicationMetadataRecord],
) -> anyhow::Result<()> {
    let resp = http
        .put(format!(
            "{DISCORD_BASE}/api/applications/{application_id}/role-connections/metadata"
        ))
        .header("Authorization", format!("Bot {bot_token}"))
        .json(records)
        .send()
        .await
        .context("discord register app metadata")?;
    let status = resp.status();
    if status.is_success() {
        return Ok(());
    }
    let text = resp.text().await.unwrap_or_default();
    Err(anyhow!("discord register app metadata: {status} {text}"))
}

/// Fetch the authenticated user's guild list
/// (`GET /api/users/@me/guilds?with_counts=true`). Requires the `guilds` scope,
/// already requested at authorize time. No pagination (mirrors the Go client).
pub async fn fetch_user_guilds(
    http: &reqwest::Client,
    access_token: &str,
) -> anyhow::Result<Vec<UserGuild>> {
    let resp = http
        .get(format!("{DISCORD_BASE}/api/users/@me/guilds?with_counts=true"))
        .header("Authorization", format!("Bearer {access_token}"))
        .header("Cache-Control", "no-store")
        .send()
        .await
        .context("discord guilds request")?;
    let status = resp.status();
    let body = resp.text().await.unwrap_or_default();
    if !status.is_success() {
        return Err(anyhow!("discord guilds fetch: {status} {body}"));
    }

    #[derive(Deserialize)]
    struct RawGuild {
        id: String,
        name: String,
        icon: Option<String>,
        #[serde(default)]
        owner: bool,
        approximate_member_count: Option<i32>,
    }
    let raw: Vec<RawGuild> = serde_json::from_str(&body).context("parse guilds response")?;
    Ok(raw
        .into_iter()
        .map(|g| UserGuild {
            id: g.id,
            name: g.name,
            icon_hash: g.icon.unwrap_or_default(),
            approximate_member_count: g.approximate_member_count,
            owner: g.owner,
        })
        .collect())
}
