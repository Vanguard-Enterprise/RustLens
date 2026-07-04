//! The Redis-backed shard lease store. Byte-for-byte with the Go
//! `internal/store/redis.go` shard keys/operations — the bot workers share this
//! Redis state, so the key formats and the `SetLeases` Lua swap must match.

use std::collections::HashMap;
use std::time::{SystemTime, UNIX_EPOCH};

use redis::aio::MultiplexedConnection;

pub const WORKERS_KEY: &str = "rustlens:bot:workers";
pub const HEARTBEAT_PREFIX: &str = "rustlens:bot:worker:heartbeat:";
pub const STATS_PREFIX: &str = "rustlens:bot:worker:stats:";
pub const LEASES_KEY: &str = "rustlens:bot:leases";

/// The atomic lease-swap: remove every field owned by `worker`, then write its
/// new shard set. Byte-identical to the Go Lua.
const SET_LEASES_LUA: &str = r#"
local key    = KEYS[1]
local worker = ARGV[1]
local count  = tonumber(ARGV[2])
local all = redis.call("HGETALL", key)
for i = 1, #all, 2 do
    if all[i+1] == worker then
        redis.call("HDEL", key, all[i])
    end
end
for i = 0, count - 1 do
    redis.call("HSET", key, ARGV[3 + i], worker)
end
return 0
"#;

fn heartbeat_key(worker_id: &str) -> String {
    format!("{HEARTBEAT_PREFIX}{worker_id}")
}
fn stats_key(worker_id: &str) -> String {
    format!("{STATS_PREFIX}{worker_id}")
}

fn now_unix_nanos() -> String {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_nanos())
        .unwrap_or(0)
        .to_string()
}

/// `MarkWorker`/`RefreshWorker` — MULTI/EXEC: `SADD workers <id>` + `SETEX
/// heartbeat:<id> ttl <unixNano>`.
pub async fn mark_worker(
    conn: &mut MultiplexedConnection,
    worker_id: &str,
    ttl_secs: u64,
) -> redis::RedisResult<()> {
    redis::pipe()
        .atomic()
        .cmd("SADD")
        .arg(WORKERS_KEY)
        .arg(worker_id)
        .ignore()
        .cmd("SETEX")
        .arg(heartbeat_key(worker_id))
        .arg(ttl_secs)
        .arg(now_unix_nanos())
        .ignore()
        .query_async(conn)
        .await
}

/// `SetWorkerStats`: `SET stats:<id> <json> EX ttl`.
pub async fn set_worker_stats(
    conn: &mut MultiplexedConnection,
    worker_id: &str,
    payload: &str,
    ttl_secs: u64,
) -> redis::RedisResult<()> {
    redis::cmd("SET")
        .arg(stats_key(worker_id))
        .arg(payload)
        .arg("EX")
        .arg(ttl_secs)
        .query_async(conn)
        .await
}

/// `RemoveWorker` — MULTI/EXEC: `SREM workers <id>` + `DEL heartbeat:<id>` +
/// `DEL stats:<id>`.
pub async fn remove_worker(conn: &mut MultiplexedConnection, worker_id: &str) -> redis::RedisResult<()> {
    redis::pipe()
        .atomic()
        .cmd("SREM")
        .arg(WORKERS_KEY)
        .arg(worker_id)
        .ignore()
        .cmd("DEL")
        .arg(heartbeat_key(worker_id))
        .ignore()
        .cmd("DEL")
        .arg(stats_key(worker_id))
        .ignore()
        .query_async(conn)
        .await
}

/// `ActiveWorkers`: members of the workers set whose heartbeat key still exists;
/// expired members are reaped (worker keys + their lease-hash fields). Sorted.
pub async fn active_workers(conn: &mut MultiplexedConnection) -> redis::RedisResult<Vec<String>> {
    let members: Vec<String> = redis::cmd("SMEMBERS").arg(WORKERS_KEY).query_async(conn).await?;
    let mut alive = Vec::new();
    for m in members {
        let exists: bool = redis::cmd("EXISTS").arg(heartbeat_key(&m)).query_async(conn).await?;
        if exists {
            alive.push(m);
        } else {
            remove_worker(conn, &m).await?;
            remove_leases_by_worker(conn, &m).await?;
        }
    }
    alive.sort();
    Ok(alive)
}

/// `CurrentAssignments`: `HGETALL leases` → `worker -> sorted shard ids`
/// (unparseable fields skipped).
pub async fn current_assignments(
    conn: &mut MultiplexedConnection,
) -> redis::RedisResult<HashMap<String, Vec<i64>>> {
    let flat: Vec<String> = redis::cmd("HGETALL").arg(LEASES_KEY).query_async(conn).await?;
    let mut map: HashMap<String, Vec<i64>> = HashMap::new();
    for pair in flat.chunks_exact(2) {
        if let Ok(shard) = pair[0].trim().parse::<i64>() {
            map.entry(pair[1].clone()).or_default().push(shard);
        }
    }
    for v in map.values_mut() {
        v.sort_unstable();
    }
    Ok(map)
}

/// `RemoveLeasesByWorker`: `HGETALL leases` → `HDEL` every field owned by worker.
pub async fn remove_leases_by_worker(
    conn: &mut MultiplexedConnection,
    worker_id: &str,
) -> redis::RedisResult<()> {
    let flat: Vec<String> = redis::cmd("HGETALL").arg(LEASES_KEY).query_async(conn).await?;
    let fields: Vec<String> = flat
        .chunks_exact(2)
        .filter(|p| p[1] == worker_id)
        .map(|p| p[0].clone())
        .collect();
    if !fields.is_empty() {
        let _: () = redis::cmd("HDEL").arg(LEASES_KEY).arg(fields).query_async(conn).await?;
    }
    Ok(())
}

/// `SetLeases`: atomic Lua swap of this worker's shard set (empty → clears).
pub async fn set_leases(
    conn: &mut MultiplexedConnection,
    worker_id: &str,
    shard_ids: &[i64],
) -> redis::RedisResult<()> {
    let mut cmd = redis::cmd("EVAL");
    cmd.arg(SET_LEASES_LUA).arg(1).arg(LEASES_KEY).arg(worker_id).arg(shard_ids.len());
    for s in shard_ids {
        cmd.arg(s.to_string());
    }
    let _: i64 = cmd.query_async(conn).await?;
    Ok(())
}

/// Deterministic round-robin allocation (`AssignShards`): shard `i` → the
/// `i % n`-th worker in sorted order. Empty when no shards / no workers.
pub fn assign_shards(total_shards: i64, workers: &[String]) -> HashMap<String, Vec<i64>> {
    let mut out: HashMap<String, Vec<i64>> = HashMap::new();
    if total_shards <= 0 || workers.is_empty() {
        return out;
    }
    let mut sorted = workers.to_vec();
    sorted.sort();
    for i in 0..total_shards {
        let owner = &sorted[(i as usize) % sorted.len()];
        out.entry(owner.clone()).or_default().push(i);
    }
    out
}

#[cfg(test)]
mod tests {
    use super::assign_shards;

    #[test]
    fn round_robin_4_shards_2_workers() {
        let m = assign_shards(4, &["b".into(), "a".into()]);
        assert_eq!(m["a"], vec![0, 2]);
        assert_eq!(m["b"], vec![1, 3]);
    }

    #[test]
    fn more_workers_than_shards_leaves_some_unassigned() {
        let m = assign_shards(2, &["a".into(), "b".into(), "c".into()]);
        assert_eq!(m["a"], vec![0]);
        assert_eq!(m["b"], vec![1]);
        assert!(!m.contains_key("c"));
    }

    #[test]
    fn empty_inputs() {
        assert!(assign_shards(0, &["a".into()]).is_empty());
        assert!(assign_shards(4, &[]).is_empty());
    }
}
