//! Redis SetNX lock primitives for the cleanup worker (same scheme as the
//! servers/guilds pollers): `SET key owner NX PX ttl` to acquire, Lua
//! compare-and-del to release.

use redis::aio::MultiplexedConnection;

/// The owner id for the worker locks (`{host}:{pid}`).
pub fn worker_owner() -> String {
    let host = std::env::var("HOSTNAME")
        .ok()
        .filter(|h| !h.is_empty())
        .unwrap_or_else(|| "unknown-host".to_string());
    format!("{host}:{}", std::process::id())
}

const RELEASE: &str = r#"
if redis.call("get", KEYS[1]) == ARGV[1] then
    return redis.call("del", KEYS[1])
end
return 0
"#;

/// `SET key owner NX PX ttl_ms` → `true` if acquired. `false` on any Redis error
/// (fail-closed: a worker that can't reach Redis does not run the cycle).
pub async fn try_acquire(
    conn: &mut MultiplexedConnection,
    key: &str,
    owner: &str,
    ttl_ms: u64,
) -> bool {
    let res: redis::RedisResult<Option<String>> = redis::cmd("SET")
        .arg(key)
        .arg(owner)
        .arg("NX")
        .arg("PX")
        .arg(ttl_ms)
        .query_async(conn)
        .await;
    matches!(res, Ok(Some(_)))
}

/// Release a lock we own (best-effort).
pub async fn release(conn: &mut MultiplexedConnection, key: &str, owner: &str) {
    let res: redis::RedisResult<i64> = redis::cmd("EVAL")
        .arg(RELEASE)
        .arg(1)
        .arg(key)
        .arg(owner)
        .query_async(conn)
        .await;
    if let Err(e) = res {
        tracing::warn!("coordinator/redis_lock release {key}: {e}");
    }
}
