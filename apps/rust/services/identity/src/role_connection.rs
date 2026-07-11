//! Discord Linked Roles ("role connection") — the `pro` entitlement flow.
//!
//! Mirrors the Go `internal/roleconnection`: a session-gated OAuth flow
//! (`identify role_connections.write`) that stores AES-GCM-encrypted tokens and
//! pushes `is_pro` / `pro_since` metadata to Discord. Phase 4a implements
//! initiate / callback / disconnect + the initial (fresh-token) push; the 6h
//! reconciler and refresh-on-expiry path are deferred (Phase 4b).

use std::collections::BTreeMap;
use std::sync::Arc;

use axum::Json;
use axum::extract::{Query, State};
use axum::http::{HeaderMap, StatusCode, header};
use axum::response::{IntoResponse, Response};
use chrono::{DateTime, Duration, SecondsFormat, Utc};
use serde::Deserialize;
use sha2::{Digest, Sha256};

use rustlens_db::role_connections as rc;

use crate::cookies::{
    self, RC_RETURN_COOKIE, RC_STATE_COOKIE, SHORT_MAX_AGE,
};
use crate::discord::{self, RoleConnectionBody};
use crate::state::AppState;
use crate::appauth;

type Shared = State<Arc<AppState>>;

const PLATFORM_NAME: &str = "RustLens";
const RC_SCOPES: &str = "identify role_connections.write";
/// Skip an unchanged push that happened within this many hours.
const PUSH_SKIP_HOURS: i64 = 24;

#[derive(Debug, Deserialize)]
pub struct InitiateQuery {
    #[serde(rename = "return")]
    return_to: Option<String>,
}

/// `GET /api/auth/discord/role-connection` — start the linked-roles OAuth flow.
pub async fn initiate(
    State(st): Shared,
    headers: HeaderMap,
    Query(q): Query<InitiateQuery>,
) -> Response {
    if appauth::validate_session_cookie(&headers, &st).await.is_err() {
        return redirect(&st.config.frontend_url("/login?notice=session-required"), HeaderMap::new());
    }
    if st.config.discord_client_id.is_empty() {
        return redirect(&st.config.frontend_url("/dashboard?error=discord-config"), HeaderMap::new());
    }

    let state = discord::random_token_32();
    let return_to = q
        .return_to
        .filter(|s| !s.is_empty())
        .unwrap_or_else(|| "/dashboard".to_string());

    let mut out = HeaderMap::new();
    cookies::append_set_cookie(&mut out, &cookies::set_cookie(RC_STATE_COOKIE, &state, SHORT_MAX_AGE));
    cookies::append_set_cookie(&mut out, &cookies::set_cookie(RC_RETURN_COOKIE, &return_to, SHORT_MAX_AGE));
    redirect(&authorize_url(&st, &state), out)
}

#[derive(Debug, Deserialize)]
pub struct CallbackQuery {
    code: Option<String>,
    state: Option<String>,
}

/// `GET /api/auth/discord/role-connection/callback` — finish the flow, store the
/// encrypted tokens, and push initial metadata.
pub async fn callback(
    State(st): Shared,
    headers: HeaderMap,
    Query(q): Query<CallbackQuery>,
) -> Response {
    // Always clear the per-flow cookies on the way out.
    let mut out = HeaderMap::new();
    cookies::append_set_cookie(&mut out, &cookies::clear_cookie(RC_STATE_COOKIE));
    cookies::append_set_cookie(&mut out, &cookies::clear_cookie(RC_RETURN_COOKIE));

    let session = match appauth::validate_session_cookie(&headers, &st).await {
        Ok(s) => s,
        Err(_) => return redirect(&st.config.frontend_url("/login?notice=session-required"), out),
    };

    let dash_err = |slug: &str| -> String { format!("/dashboard?error={slug}") };

    let state_cookie = cookies::read_cookie(&headers, RC_STATE_COOKIE);
    let code = q.code.as_deref().filter(|s| !s.is_empty());
    let qstate = q.state.as_deref().filter(|s| !s.is_empty());

    let Some(state_cookie) = state_cookie else {
        return redirect(&st.config.frontend_url(&dash_err("discord-state-cookie")), out);
    };
    let (Some(code), Some(qstate)) = (code, qstate) else {
        return redirect(&st.config.frontend_url(&dash_err("discord-state-empty")), out);
    };
    if qstate != state_cookie {
        return redirect(&st.config.frontend_url(&dash_err("discord-state-mismatch")), out);
    }

    let redirect_uri = st.config.role_connection_redirect_uri();
    let (access, refresh, expires_in) = match discord::exchange_code_for_tokens(
        &st.http,
        &st.config.discord_client_id,
        &st.config.discord_client_secret,
        code,
        &redirect_uri,
    )
    .await
    {
        Ok(t) => t,
        Err(e) => {
            tracing::warn!("rc callback: token exchange failed: {e:#}");
            return redirect(&st.config.frontend_url(&dash_err("discord-token")), out);
        }
    };

    let profile = match discord::fetch_profile(&st.http, &access).await {
        Ok(p) => p,
        Err(e) => {
            tracing::warn!("rc callback: profile fetch failed: {e:#}");
            return redirect(&st.config.frontend_url(&dash_err("discord-profile")), out);
        }
    };
    if profile.id != session.discord_id {
        return redirect(&st.config.frontend_url(&dash_err("discord-mismatch")), out);
    }

    let Some(cipher) = st.cipher.as_ref() else {
        return redirect(&st.config.frontend_url(&dash_err("discord-config")), out);
    };
    let (Ok((access_ct, access_nonce)), Ok((refresh_ct, refresh_nonce))) =
        (cipher.encrypt(&access), cipher.encrypt(&refresh))
    else {
        return redirect(&st.config.frontend_url(&dash_err("discord-encrypt")), out);
    };

    let expires_at = Utc::now() + Duration::seconds(expires_in);
    if let Err(e) = rc::upsert_role_connection(
        &st.pool,
        &rc::NewRoleConnection {
            user_id: &session.user_id,
            discord_id: &profile.id,
            access_token_ct: &access_ct,
            access_token_nonce: &access_nonce,
            refresh_token_ct: &refresh_ct,
            refresh_token_nonce: &refresh_nonce,
            access_expires_at: expires_at,
            scopes: RC_SCOPES,
        },
    )
    .await
    {
        tracing::error!("rc callback: upsert failed: {e:#}");
        return redirect(&st.config.frontend_url(&dash_err("discord-persist")), out);
    }

    // Initial push so the role appears before the redirect lands. Best-effort.
    if let Err(e) = push(&st, &session.user_id).await {
        tracing::warn!("rc callback: initial push failed: {e:#}");
    }

    let target = return_target(cookies::read_cookie(&headers, RC_RETURN_COOKIE).as_deref());
    redirect(&st.config.frontend_url(&target), out)
}

/// `DELETE /api/app/me/discord-role-connection` — unlink. Best-effort empty push
/// then delete. Header-authenticated (it's an `/api/app/*` route).
pub async fn disconnect(State(st): Shared, headers: HeaderMap) -> Response {
    if let Err(resp) = appauth::require_app_secret(&headers, &st.config.app_api_internal_secret) {
        return *resp;
    }
    let session = match appauth::require_session(&headers, &st).await {
        Ok(s) => s,
        Err(resp) => return *resp,
    };

    // Best-effort: tell Discord the user is no longer PRO before forgetting them.
    if let Ok(Some(row)) = rc::get_role_connection(&st.pool, &session.user_id).await {
        if row.revoked_at.is_none() {
            if let Some(cipher) = st.cipher.as_ref() {
                if let Ok(access) = cipher.decrypt(&row.access_token_ct, &row.access_token_nonce) {
                    let mut metadata = BTreeMap::new();
                    metadata.insert("is_pro".to_string(), "0".to_string());
                    let body = RoleConnectionBody {
                        platform_name: PLATFORM_NAME.to_string(),
                        platform_username: String::new(),
                        metadata,
                    };
                    if let Err(e) = discord::put_role_connection(
                        &st.http,
                        &access,
                        &st.config.discord_client_id,
                        &body,
                    )
                    .await
                    {
                        tracing::warn!("rc disconnect: empty push failed: {e:#}");
                    }
                }
            }
        }
    }

    if let Err(e) = rc::delete_role_connection(&st.pool, &session.user_id).await {
        tracing::error!("rc disconnect: delete failed: {e:#}");
        return (StatusCode::INTERNAL_SERVER_ERROR, Json(serde_json::json!({ "error": "delete failed" })))
            .into_response();
    }
    StatusCode::NO_CONTENT.into_response()
}

/// Seconds of clock skew: refresh the access token when it expires within this
/// window (Go `tokenSkew`).
const TOKEN_SKEW_SECS: i64 = 60;

/// Push the user's current linked-roles metadata to Discord, refreshing the
/// access token first when it is within 60s of expiry. Skips an unchanged push
/// made within the last 24h. A rejected refresh token marks the row revoked.
pub async fn push(st: &AppState, user_id: &str) -> anyhow::Result<()> {
    use anyhow::Context;
    let Some(row) = rc::get_role_connection(&st.pool, user_id).await? else {
        return Ok(());
    };
    if row.revoked_at.is_some() {
        return Ok(());
    }
    let cipher = st.cipher.as_ref().context("role-connection feature disabled")?;

    // Refresh the access token when it is at/near expiry; otherwise decrypt the
    // stored one.
    let access = if Utc::now() + Duration::seconds(TOKEN_SKEW_SECS) > row.access_expires_at {
        let refresh_pt = cipher
            .decrypt(&row.refresh_token_ct, &row.refresh_token_nonce)
            .map_err(|e| anyhow::anyhow!("decrypt refresh token: {e}"))?;
        match discord::refresh_token(
            &st.http,
            &st.config.discord_client_id,
            &st.config.discord_client_secret,
            &refresh_pt,
        )
        .await?
        {
            discord::RefreshOutcome::Revoked => {
                rc::mark_revoked(&st.pool, user_id, "invalid_grant").await?;
                return Err(anyhow::anyhow!("refresh token revoked"));
            }
            discord::RefreshOutcome::Refreshed(new_access, new_refresh, expires_in) => {
                let (access_ct, access_nonce) = cipher
                    .encrypt(&new_access)
                    .map_err(|e| anyhow::anyhow!("encrypt access: {e}"))?;
                let (refresh_ct, refresh_nonce) = cipher
                    .encrypt(&new_refresh)
                    .map_err(|e| anyhow::anyhow!("encrypt refresh: {e}"))?;
                rc::upsert_role_connection(
                    &st.pool,
                    &rc::NewRoleConnection {
                        user_id,
                        discord_id: &row.discord_id,
                        access_token_ct: &access_ct,
                        access_token_nonce: &access_nonce,
                        refresh_token_ct: &refresh_ct,
                        refresh_token_nonce: &refresh_nonce,
                        access_expires_at: Utc::now() + Duration::seconds(expires_in),
                        scopes: &row.scopes,
                    },
                )
                .await?;
                new_access
            }
        }
    } else {
        cipher
            .decrypt(&row.access_token_ct, &row.access_token_nonce)
            .map_err(|e| anyhow::anyhow!("decrypt access token: {e}"))?
    };

    let granted = rc::get_user_pro_granted_at(&st.pool, user_id).await?;
    let display = rc::get_app_user_display_name(&st.pool, user_id).await?;
    let body = build_metadata(display, granted);
    let hash = hash_metadata(&body);

    if let (Some(prev), Some(at)) = (row.last_pushed_hash.as_deref(), row.last_pushed_at) {
        if prev == hash && Utc::now() - at < Duration::hours(PUSH_SKIP_HOURS) {
            return Ok(());
        }
    }

    match discord::put_role_connection(&st.http, &access, &st.config.discord_client_id, &body).await {
        Ok(()) => {
            rc::record_pushed(&st.pool, user_id, &hash).await?;
            Ok(())
        }
        Err(e) => {
            let _ = rc::record_push_error(&st.pool, user_id, &e.to_string()).await;
            Err(e)
        }
    }
}

/// Advisory-lock key shared with the Go reconciler so only one pod reconciles.
const RECONCILE_LOCK_KEY: i64 = 7_427_198_631_011;
/// Reconcile interval (Go default: 6h).
const RECONCILE_INTERVAL_SECS: u64 = 6 * 60 * 60;
/// Politeness delay between per-user pushes (Go default: 200ms).
const RECONCILE_SLEEP_MS: u64 = 200;

/// Background reconciler: re-push metadata for every active connection on
/// startup and every 6h, so PRO grants/revokes and token refreshes converge even
/// if a live push was missed. Spawned only when the feature is configured.
pub async fn reconcile_loop(st: Arc<AppState>) {
    loop {
        reconcile_once(&st).await;
        tokio::time::sleep(std::time::Duration::from_secs(RECONCILE_INTERVAL_SECS)).await;
    }
}

async fn reconcile_once(st: &AppState) {
    // Hold the advisory lock on a dedicated connection for the whole pass so
    // only one pod runs at a time (matches the Go reconciler).
    let mut conn = match st.pool.acquire().await {
        Ok(c) => c,
        Err(e) => {
            tracing::warn!("rc reconciler: acquire connection failed: {e}");
            return;
        }
    };
    let got: bool = match sqlx::query_scalar("SELECT pg_try_advisory_lock($1)")
        .bind(RECONCILE_LOCK_KEY)
        .fetch_one(&mut *conn)
        .await
    {
        Ok(b) => b,
        Err(e) => {
            tracing::warn!("rc reconciler: advisory lock failed: {e}");
            return;
        }
    };
    if !got {
        tracing::info!("rc reconciler: another pod holds the lock, skipping");
        return;
    }

    let users = rc::list_active_role_connections(&st.pool).await.unwrap_or_default();
    let count = users.len();
    for uid in users {
        if let Err(e) = push(st, &uid).await {
            tracing::warn!("rc reconciler: push failed for {uid}: {e:#}");
        }
        tokio::time::sleep(std::time::Duration::from_millis(RECONCILE_SLEEP_MS)).await;
    }

    let _ = sqlx::query("SELECT pg_advisory_unlock($1)")
        .bind(RECONCILE_LOCK_KEY)
        .execute(&mut *conn)
        .await;
    tracing::info!("rc reconciler: pushed {count} connection(s)");
}

/// Register the linked-roles metadata schema with Discord (bot-token auth). Runs
/// once at startup when the feature + bot token are configured.
pub async fn register_schema(st: &AppState) -> anyhow::Result<()> {
    let Some(bot_token) = st.config.discord_bot_token.as_deref() else {
        return Ok(());
    };
    let records = vec![
        discord::ApplicationMetadataRecord {
            field_type: 7,
            key: "is_pro".to_string(),
            name: "PRO subscriber".to_string(),
            description: "Active RustLens PRO status".to_string(),
        },
        discord::ApplicationMetadataRecord {
            field_type: 6,
            key: "pro_since".to_string(),
            name: "PRO since".to_string(),
            description: "Date PRO was granted".to_string(),
        },
    ];
    discord::register_application_metadata(
        &st.http,
        &st.config.discord_client_id,
        bot_token,
        &records,
    )
    .await
}

fn build_metadata(display: String, granted: Option<DateTime<Utc>>) -> RoleConnectionBody {
    let mut metadata = BTreeMap::new();
    match granted {
        Some(t) => {
            metadata.insert("is_pro".to_string(), "1".to_string());
            metadata.insert(
                "pro_since".to_string(),
                t.to_rfc3339_opts(SecondsFormat::Secs, false),
            );
        }
        None => {
            metadata.insert("is_pro".to_string(), "0".to_string());
        }
    }
    RoleConnectionBody {
        platform_name: PLATFORM_NAME.to_string(),
        platform_username: display,
        metadata,
    }
}

fn hash_metadata(body: &RoleConnectionBody) -> String {
    let raw = serde_json::to_vec(body).unwrap_or_default();
    let digest = Sha256::digest(&raw);
    let mut out = String::with_capacity(digest.len() * 2);
    for b in digest {
        out.push_str(&format!("{b:02x}"));
    }
    out
}

fn authorize_url(st: &AppState, state: &str) -> String {
    let mut url = url::Url::parse("https://discord.com/oauth2/authorize")
        .expect("static authorize URL is valid");
    url.query_pairs_mut()
        .append_pair("client_id", &st.config.discord_client_id)
        .append_pair("response_type", "code")
        .append_pair("redirect_uri", &st.config.role_connection_redirect_uri())
        .append_pair("scope", RC_SCOPES)
        .append_pair("prompt", "consent")
        .append_pair("state", state);
    url.to_string()
}

/// Compute the post-link redirect: the `return` destination if it is a safe
/// same-origin relative path, else `/dashboard`; always with the
/// `notice=discord-linked` query param.
fn return_target(return_cookie: Option<&str>) -> String {
    let path = return_cookie
        .filter(|v| v.starts_with('/') && !v.starts_with("//"))
        .unwrap_or("/dashboard");
    let sep = if path.contains('?') { '&' } else { '?' };
    format!("{path}{sep}notice=discord-linked")
}

fn redirect(location: &str, mut headers: HeaderMap) -> Response {
    match header::HeaderValue::from_str(location) {
        Ok(hv) => {
            headers.insert(header::LOCATION, hv);
        }
        Err(_) => return (StatusCode::INTERNAL_SERVER_ERROR, "bad redirect target").into_response(),
    }
    (StatusCode::FOUND, headers).into_response()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn metadata_pro_vs_non_pro() {
        let none = build_metadata("Frank".into(), None);
        assert_eq!(none.metadata.get("is_pro").map(String::as_str), Some("0"));
        assert!(!none.metadata.contains_key("pro_since"));
        assert_eq!(none.platform_name, "RustLens");

        let t: DateTime<Utc> = "2026-06-30T12:34:56Z".parse().unwrap();
        let pro = build_metadata("Frank".into(), Some(t));
        assert_eq!(pro.metadata.get("is_pro").map(String::as_str), Some("1"));
        assert_eq!(
            pro.metadata.get("pro_since").map(String::as_str),
            Some("2026-06-30T12:34:56+00:00")
        );
        // Hash is stable for identical input and differs across pro state.
        assert_eq!(hash_metadata(&pro), hash_metadata(&build_metadata("Frank".into(), Some(t))));
        assert_ne!(hash_metadata(&pro), hash_metadata(&none));
    }

    #[test]
    fn return_target_is_safe() {
        assert_eq!(return_target(None), "/dashboard?notice=discord-linked");
        assert_eq!(
            return_target(Some("/dashboard/guilds/1")),
            "/dashboard/guilds/1?notice=discord-linked"
        );
        // Already-has-query path uses '&'.
        assert_eq!(return_target(Some("/x?a=1")), "/x?a=1&notice=discord-linked");
        // Open-redirect attempts fall back to /dashboard.
        assert_eq!(return_target(Some("//evil.com")), "/dashboard?notice=discord-linked");
        assert_eq!(return_target(Some("https://evil.com")), "/dashboard?notice=discord-linked");
    }
}
