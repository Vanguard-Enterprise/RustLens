//! Per-user fixed-window rate limiter for the app player routes. Atomic Redis
//! `INCR` + `PEXPIRE` Lua script; **fails open** on any Redis error / missing
//! connection / empty user. Mirrors the Go `allowPlayerSearch`.

use redis::aio::MultiplexedConnection;

const SCRIPT: &str = r#"
local count = redis.call("INCR", KEYS[1])
if count == 1 then
    redis.call("PEXPIRE", KEYS[1], ARGV[1])
end
return count
"#;

/// Returns `true` if the request is allowed. `scope` is `"search"` or `"profile"`.
pub async fn allow(
    conn: &Option<MultiplexedConnection>,
    user_id: &str,
    scope: &str,
    max: i64,
    window_ms: i64,
) -> bool {
    let Some(conn) = conn else {
        return true; // no redis → fail open
    };
    if user_id.is_empty() {
        return true;
    }
    let key = format!("rustlens:players_{scope}:{user_id}");
    let mut conn = conn.clone();
    let res: redis::RedisResult<i64> = redis::cmd("EVAL")
        .arg(SCRIPT)
        .arg(1)
        .arg(&key)
        .arg(window_ms)
        .query_async(&mut conn)
        .await;
    match res {
        Ok(count) => count <= max,
        Err(_) => true, // fail open on redis hiccup
    }
}
