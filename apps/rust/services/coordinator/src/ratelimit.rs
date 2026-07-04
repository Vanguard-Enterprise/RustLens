//! Rate-limit consume (`POST /api/internal/bot/setup/rate-limit/consume`,
//! bot-JWT). A Redis fixed-window limiter with escalating cooldowns + the
//! low-guild fraud detector. Mirrors Go's ConsumeRateLimit + ratelimit/fraud.
//! Byte-for-byte Redis keys (`rl:cd:`/`rl:q:`/`rl:lvl:`, `fraud:<cmd>:<guild>:*`).

use std::sync::Arc;

use axum::Json;
use axum::body::Bytes;
use axum::extract::State;
use axum::http::{HeaderMap, StatusCode};
use axum::response::{IntoResponse, Response};
use redis::aio::MultiplexedConnection;
use serde::{Deserialize, Serialize};

use rustlens_db::rbac;

use crate::auth::{err_json, is_snowflake_value, parse_json, require_bot_jwt};
use crate::state::AppState;

type Shared = State<Arc<AppState>>;

struct RlConfig {
    quota: i64,
    window_ms: i64,
    base_cooldown_ms: i64,
    max_cooldown_ms: i64,
    violation_decay_ms: i64,
}

struct FraudConfig {
    window_ms: i64,
    threshold: i64,
    max_member_count: i64,
    alert_cooldown_ms: i64,
}

/// Per-command config lookup (command already trimmed+lowercased).
fn command_config(cmd: &str) -> Option<(RlConfig, Option<FraudConfig>)> {
    match cmd {
        "player" => Some((
            RlConfig {
                quota: 5,
                window_ms: 60_000,
                base_cooldown_ms: 30_000,
                max_cooldown_ms: 3_600_000,
                violation_decay_ms: 3_600_000,
            },
            Some(FraudConfig {
                window_ms: 3_600_000,
                threshold: 30,
                max_member_count: 25,
                alert_cooldown_ms: 6 * 3_600_000,
            }),
        )),
        "view-notadded" => Some((
            RlConfig {
                quota: 3,
                window_ms: 60_000,
                base_cooldown_ms: 60_000,
                max_cooldown_ms: 3_600_000,
                violation_decay_ms: 3_600_000,
            },
            None,
        )),
        _ => None,
    }
}

fn seconds_ceil(ms: i64) -> i64 {
    ((ms + 999) / 1000).max(1)
}

/// `scaleCooldown`: `base * 2^(level-1)` capped at `max` (level ≤ 0 → 1ms floor,
/// level > 30 → max).
fn scale_cooldown(base: i64, max: i64, level: i64) -> i64 {
    if level < 1 {
        return 1;
    }
    if level > 30 {
        return max;
    }
    match base.checked_shl((level - 1) as u32) {
        Some(d) if d > 0 && d <= max => d,
        _ => max,
    }
}

struct ConsumeResult {
    allowed: bool,
    retry_after_seconds: i64,
    violation_level: i64,
}

async fn limiter_consume(
    conn: &mut MultiplexedConnection,
    key: &str,
    cfg: &RlConfig,
) -> redis::RedisResult<ConsumeResult> {
    let cd_key = format!("rl:cd:{key}");
    let q_key = format!("rl:q:{key}");
    let lvl_key = format!("rl:lvl:{key}");

    // 1. Already in cooldown?
    let ttl: i64 = redis::cmd("PTTL").arg(&cd_key).query_async(conn).await?;
    if ttl > 0 {
        return Ok(ConsumeResult {
            allowed: false,
            retry_after_seconds: seconds_ceil(ttl),
            violation_level: 0,
        });
    }
    // 2. Fixed-window counter, anchored at the first hit.
    let n: i64 = redis::cmd("INCR").arg(&q_key).query_async(conn).await?;
    if n == 1 {
        let _: bool = redis::cmd("PEXPIRE").arg(&q_key).arg(cfg.window_ms).query_async(conn).await?;
    }
    // 3. Within quota.
    if n <= cfg.quota {
        return Ok(ConsumeResult { allowed: true, retry_after_seconds: 0, violation_level: 0 });
    }
    // 4. Overflow → escalate + cooldown.
    let level: i64 = redis::cmd("INCR").arg(&lvl_key).query_async(conn).await?;
    let _: bool = redis::cmd("PEXPIRE").arg(&lvl_key).arg(cfg.violation_decay_ms).query_async(conn).await?;
    let cooldown_ms = scale_cooldown(cfg.base_cooldown_ms, cfg.max_cooldown_ms, level);
    let _: () = redis::cmd("SET").arg(&cd_key).arg("1").arg("PX").arg(cooldown_ms).query_async(conn).await?;
    let _: i64 = redis::cmd("DEL").arg(&q_key).query_async(conn).await?;
    Ok(ConsumeResult {
        allowed: false,
        retry_after_seconds: seconds_ceil(cooldown_ms),
        violation_level: level,
    })
}

struct FraudAlert {
    count: i64,
    window_secs: i64,
}

async fn fraud_record(
    conn: &mut MultiplexedConnection,
    guild_id: &str,
    cmd: &str,
    member_count: i64,
    cfg: &FraudConfig,
) -> redis::RedisResult<Option<FraudAlert>> {
    // Low-guild gate: only guilds with a known, small member count.
    if guild_id.is_empty() || member_count <= 0 || member_count >= cfg.max_member_count {
        return Ok(None);
    }
    let count_key = format!("fraud:{cmd}:{guild_id}:count");
    let alerted_key = format!("fraud:{cmd}:{guild_id}:alerted");
    let n: i64 = redis::cmd("INCR").arg(&count_key).query_async(conn).await?;
    if n == 1 {
        let _: bool = redis::cmd("PEXPIRE").arg(&count_key).arg(cfg.window_ms).query_async(conn).await?;
    }
    if n < cfg.threshold {
        return Ok(None);
    }
    // One alert per (command, guild) per cooldown window.
    let set: Option<String> = redis::cmd("SET")
        .arg(&alerted_key)
        .arg("1")
        .arg("NX")
        .arg("PX")
        .arg(cfg.alert_cooldown_ms)
        .query_async(conn)
        .await?;
    if set.is_none() {
        return Ok(None);
    }
    Ok(Some(FraudAlert { count: n, window_secs: cfg.window_ms / 1000 }))
}

#[derive(Deserialize)]
struct ConsumeRequest {
    #[serde(default, rename = "discordId")]
    discord_id: String,
    #[serde(default)]
    command: String,
    #[serde(default, rename = "guildId")]
    guild_id: String,
    #[serde(default, rename = "memberCount")]
    member_count: i64,
}

#[derive(Serialize)]
struct FraudPayload {
    #[serde(rename = "guildId")]
    guild_id: String,
    command: String,
    count: i64,
    #[serde(rename = "memberCount")]
    member_count: i64,
    #[serde(rename = "windowSecs")]
    window_secs: i64,
}

fn is_zero(n: &i64) -> bool {
    *n == 0
}

#[derive(Serialize)]
struct ConsumeResponse {
    allowed: bool,
    #[serde(rename = "retryAfterSeconds")]
    retry_after_seconds: i64,
    #[serde(rename = "violationLevel", skip_serializing_if = "is_zero")]
    violation_level: i64,
    #[serde(rename = "fraudAlert", skip_serializing_if = "Option::is_none")]
    fraud_alert: Option<FraudPayload>,
}

pub async fn consume_rate_limit(State(st): Shared, headers: HeaderMap, body: Bytes) -> Response {
    if let Err(r) = require_bot_jwt(&headers, &st.config) {
        return *r;
    }
    let req: ConsumeRequest = match parse_json(&body) {
        Ok(r) => r,
        Err(r) => return *r,
    };
    if !is_snowflake_value(&req.discord_id) {
        return err_json(StatusCode::BAD_REQUEST, "discordId must be a valid Discord snowflake");
    }
    let cmd = req.command.trim().to_ascii_lowercase();
    if cmd.is_empty() {
        return err_json(StatusCode::BAD_REQUEST, "command is required");
    }
    let Some((rl, fraud)) = command_config(&cmd) else {
        return err_json(StatusCode::BAD_REQUEST, "unknown rate-limit command");
    };
    if !req.guild_id.is_empty() && !is_snowflake_value(&req.guild_id) {
        return err_json(StatusCode::BAD_REQUEST, "guildId must be a valid Discord snowflake");
    }

    // Bypass checks: a DB error is swallowed (logged) → not bypassed (fail-closed).
    let rate_limit_bypass = perm_or_false(&st, &req.discord_id, rbac::COMMANDS_RATE_LIMIT_BYPASS).await;
    let fraud_bypass = perm_or_false(&st, &req.discord_id, rbac::COMMANDS_FRAUD_BYPASS).await;

    let mut allowed = true;
    let mut retry_after_seconds = 0i64;
    let mut violation_level = 0i64;

    if !rate_limit_bypass {
        let key = format!("{}:{}", req.discord_id, cmd);
        let Some(mut conn) = st.redis.clone() else {
            return err_json(StatusCode::INTERNAL_SERVER_ERROR, "rate limit unavailable");
        };
        match limiter_consume(&mut conn, &key, &rl).await {
            Ok(res) => {
                allowed = res.allowed;
                retry_after_seconds = res.retry_after_seconds;
                violation_level = res.violation_level;
            }
            Err(e) => {
                tracing::error!("coordinator/rate-limit consume: {e}");
                return err_json(StatusCode::INTERNAL_SERVER_ERROR, "rate limit unavailable");
            }
        }
    }

    // Fraud detection only runs on an allowed call and only for configured
    // commands; runtime errors are swallowed (logged) → still 200, no alert.
    let mut fraud_alert = None;
    if allowed && !fraud_bypass {
        if let Some(fcfg) = fraud {
            if let Some(mut conn) = st.redis.clone() {
                match fraud_record(&mut conn, &req.guild_id, &cmd, req.member_count, &fcfg).await {
                    Ok(Some(alert)) => {
                        fraud_alert = Some(FraudPayload {
                            guild_id: req.guild_id.clone(),
                            command: cmd.clone(),
                            count: alert.count,
                            member_count: req.member_count,
                            window_secs: alert.window_secs,
                        });
                    }
                    Ok(None) => {}
                    Err(e) => tracing::warn!("coordinator/rate-limit fraud: {e}"),
                }
            }
        }
    }

    // Always 200 (even when rate-limited); the bot inspects `allowed`.
    Json(ConsumeResponse { allowed, retry_after_seconds, violation_level, fraud_alert })
        .into_response()
}

async fn perm_or_false(st: &AppState, discord_id: &str, perm: &str) -> bool {
    match rustlens_db::admin::discord_has_permission(&st.pool, discord_id, perm).await {
        Ok(b) => b,
        Err(e) => {
            tracing::warn!("coordinator/rate-limit bypass check ({perm}): {e}");
            false
        }
    }
}
