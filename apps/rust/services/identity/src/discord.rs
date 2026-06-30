//! Discord OAuth client. Mirrors `apps/backend/internal/oauth/discord`:
//! PKCE (S256), `identify guilds` scope, form-encoded token exchange,
//! `GET /api/users/@me` profile fetch.

use anyhow::{Context, anyhow};
use base64::Engine;
use base64::engine::general_purpose::URL_SAFE_NO_PAD;
use rand::RngCore;
use serde::Deserialize;
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
