//! Redis SetNX lock primitives for the background workers, mirroring the Go
//! `RedisStore` poller/refresher locks: `SET key owner NX PX ttl` to acquire,
//! and a Lua compare-and-del to release (only deletes if we still own it).

use redis::aio::MultiplexedConnection;

/// The Lua compare-and-del release script (only deletes when `owner` matches).
const RELEASE: &str = r#"
if redis.call("get", KEYS[1]) == ARGV[1] then
    return redis.call("del", KEYS[1])
end
return 0
"#;

/// `SET key owner NX PX ttl_ms` → `true` if we acquired it. `false` on any Redis
/// error (fail-closed: a worker that can't reach Redis does not run the cycle).
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

/// Release a lock we own (best-effort; errors are logged by the caller).
pub async fn release(conn: &mut MultiplexedConnection, key: &str, owner: &str) {
    let res: redis::RedisResult<i64> = redis::cmd("EVAL")
        .arg(RELEASE)
        .arg(1)
        .arg(key)
        .arg(owner)
        .query_async(conn)
        .await;
    if let Err(e) = res {
        tracing::warn!("servers/redis_lock release {key}: {e}");
    }
}
