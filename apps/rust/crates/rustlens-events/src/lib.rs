//! User-event hub + Redis pub/sub bridge for SSE fan-out.
//!
//! Mirrors the Go `internal/events` package: an in-process [`Hub`] (per-user
//! pub/sub, capped connections) plus a [`RedisBridge`] that fans events across
//! pods over the `rustlens:user-events` channel. The wire shapes — the [`Event`]
//! JSON and the Redis envelope — are a cross-service contract shared with the Go
//! backend and the frontend SSE client.

use std::collections::HashMap;
use std::sync::{Arc, Mutex};

use serde::{Deserialize, Serialize};
use tokio::sync::broadcast;

/// Redis pub/sub channel for cross-pod user-event fan-out (Go `channelName`).
pub const CHANNEL: &str = "rustlens:user-events";

/// Per-user broadcast buffer (Go per-subscriber buffer).
const BUFFER: usize = 16;
/// Maximum concurrent SSE connections per user (Go cap).
const MAX_CONNS_PER_USER: usize = 5;

// Event type + reason constants (mirror Go `events/types.go`).
pub const TYPE_ROLES_CHANGED: &str = "roles.changed";
pub const TYPE_SESSION_REVOKED: &str = "session.revoked";
pub const TYPE_SESSION_EXPIRING: &str = "session.expiring";
pub const TYPE_PING: &str = "ping";

pub const REASON_GRANT: &str = "grant";
pub const REASON_REVOKE: &str = "revoke";
pub const REASON_ROLE_PERMS_UPDATED: &str = "role_perms_updated";
pub const REASON_FORCE_LOGOUT: &str = "force_logout";
pub const REASON_SUSPENDED: &str = "suspended";
pub const REASON_DELETED: &str = "deleted";

/// A user event delivered over SSE. Serializes to `{"type":...,"reason"?:...,
/// "expiresAt"?:...}` — `reason`/`expiresAt` omitted when absent (Go
/// `omitempty`).
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Event {
    #[serde(rename = "type")]
    pub event_type: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub reason: Option<String>,
    /// Milliseconds since the Unix epoch; only set for `session.expiring`.
    #[serde(rename = "expiresAt", default, skip_serializing_if = "Option::is_none")]
    pub expires_at: Option<i64>,
}

impl Event {
    pub fn ping() -> Self {
        Self { event_type: TYPE_PING.into(), reason: None, expires_at: None }
    }
    pub fn roles_changed(reason: &str) -> Self {
        Self { event_type: TYPE_ROLES_CHANGED.into(), reason: Some(reason.into()), expires_at: None }
    }
    pub fn session_revoked(reason: &str) -> Self {
        Self {
            event_type: TYPE_SESSION_REVOKED.into(),
            reason: Some(reason.into()),
            expires_at: None,
        }
    }
    pub fn session_expiring(expires_at_ms: i64) -> Self {
        Self {
            event_type: TYPE_SESSION_EXPIRING.into(),
            reason: None,
            expires_at: Some(expires_at_ms),
        }
    }
}

/// The Redis fan-out envelope (Go `redisEnvelope`).
#[derive(Debug, Clone, Serialize, Deserialize)]
struct Envelope {
    #[serde(rename = "userId")]
    user_id: String,
    event: Event,
    #[serde(rename = "instanceId", default, skip_serializing_if = "Option::is_none")]
    instance_id: Option<String>,
}

/// In-process per-user event hub. Each user maps to a broadcast channel; every
/// live SSE connection for that user subscribes a receiver and gets every event.
/// A full buffer drops the oldest event for a lagging receiver (broadcast
/// semantics), matching the Go drop-oldest behavior.
#[derive(Default)]
pub struct Hub {
    inner: Mutex<HashMap<String, broadcast::Sender<Event>>>,
}

impl Hub {
    pub fn new() -> Self {
        Self::default()
    }

    /// Subscribe a new SSE connection for `user_id`. Returns `None` when the
    /// per-user connection cap is already reached.
    pub fn subscribe(&self, user_id: &str) -> Option<broadcast::Receiver<Event>> {
        let mut map = self.inner.lock().unwrap();
        let tx = map
            .entry(user_id.to_string())
            .or_insert_with(|| broadcast::channel(BUFFER).0);
        if tx.receiver_count() >= MAX_CONNS_PER_USER {
            return None;
        }
        Some(tx.subscribe())
    }

    /// Deliver an event to this pod's live connections for `user_id`. No-op when
    /// the user has no local connections.
    pub fn publish_local(&self, user_id: &str, event: Event) {
        let map = self.inner.lock().unwrap();
        if let Some(tx) = map.get(user_id) {
            // Err only means "no receivers"; that's fine.
            let _ = tx.send(event);
        }
    }
}

/// Publishes a user event to the local hub and (when configured) to other pods.
pub trait Publisher: Send + Sync {
    fn publish_with_fanout(&self, user_id: &str, event: Event);
}

/// Multi-pod bridge: wraps a [`Hub`] and a Redis client. Publishing delivers
/// locally first, then to the Redis channel; a background subscriber relays
/// remote events (those from other instances) into the local hub.
pub struct RedisBridge {
    hub: Arc<Hub>,
    client: Option<redis::Client>,
    instance_id: String,
}

impl RedisBridge {
    /// Build the bridge for `redis_url` and spawn its subscriber task. A bad URL
    /// or unreachable Redis degrades gracefully to local-only delivery (the
    /// subscriber retries in the background).
    pub fn start(hub: Arc<Hub>, redis_url: &str) -> Arc<RedisBridge> {
        let instance_id = uuid::Uuid::new_v4().to_string();
        let client = match redis::Client::open(redis_url) {
            Ok(c) => Some(c),
            Err(e) => {
                tracing::warn!("events: invalid REDIS_URL, fan-out disabled: {e}");
                None
            }
        };
        let bridge = Arc::new(RedisBridge {
            hub: hub.clone(),
            client: client.clone(),
            instance_id: instance_id.clone(),
        });
        if let Some(client) = client {
            tokio::spawn(run_subscriber(hub, client, instance_id));
        }
        bridge
    }

    /// The wrapped hub (for SSE subscription).
    pub fn hub(&self) -> &Arc<Hub> {
        &self.hub
    }
}

impl Publisher for RedisBridge {
    fn publish_with_fanout(&self, user_id: &str, event: Event) {
        // Local delivery first (synchronous, immediate).
        self.hub.publish_local(user_id, event.clone());

        let Some(client) = self.client.clone() else {
            return;
        };
        let envelope = Envelope {
            user_id: user_id.to_string(),
            event,
            instance_id: Some(self.instance_id.clone()),
        };
        let payload = match serde_json::to_string(&envelope) {
            Ok(p) => p,
            Err(e) => {
                tracing::error!("events: marshal envelope: {e}");
                return;
            }
        };
        // Fire-and-forget the cross-pod publish.
        tokio::spawn(async move {
            match client.get_multiplexed_async_connection().await {
                Ok(mut conn) => {
                    let res: redis::RedisResult<()> = redis::cmd("PUBLISH")
                        .arg(CHANNEL)
                        .arg(payload)
                        .query_async(&mut conn)
                        .await;
                    if let Err(e) = res {
                        tracing::warn!("events: redis publish failed: {e}");
                    }
                }
                Err(e) => tracing::warn!("events: redis connect for publish failed: {e}"),
            }
        });
    }
}

/// Subscribe to the Redis channel and relay non-local events into `hub`, forever
/// (reconnecting with a short backoff on error).
async fn run_subscriber(hub: Arc<Hub>, client: redis::Client, instance_id: String) {
    loop {
        if let Err(e) = subscribe_loop(&hub, &client, &instance_id).await {
            tracing::warn!("events: redis subscriber error, retrying: {e}");
        }
        tokio::time::sleep(std::time::Duration::from_secs(2)).await;
    }
}

async fn subscribe_loop(
    hub: &Arc<Hub>,
    client: &redis::Client,
    instance_id: &str,
) -> redis::RedisResult<()> {
    use futures_util::StreamExt;

    let mut pubsub = client.get_async_pubsub().await?;
    pubsub.subscribe(CHANNEL).await?;
    tracing::info!("events: subscribed to {CHANNEL}");
    let mut stream = pubsub.on_message();
    while let Some(msg) = stream.next().await {
        let payload: String = match msg.get_payload() {
            Ok(p) => p,
            Err(e) => {
                tracing::warn!("events: bad redis payload: {e}");
                continue;
            }
        };
        match serde_json::from_str::<Envelope>(&payload) {
            Ok(env) => {
                // Skip our own publishes (already delivered locally).
                if env.instance_id.as_deref() != Some(instance_id) {
                    hub.publish_local(&env.user_id, env.event);
                }
            }
            Err(e) => tracing::warn!("events: bad envelope JSON: {e}"),
        }
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn event_json_shapes() {
        let ping = serde_json::to_value(Event::ping()).unwrap();
        assert_eq!(ping, serde_json::json!({"type": "ping"}));

        let roles = serde_json::to_value(Event::roles_changed(REASON_GRANT)).unwrap();
        assert_eq!(roles, serde_json::json!({"type": "roles.changed", "reason": "grant"}));

        let exp = serde_json::to_value(Event::session_expiring(1_893_456_000_000)).unwrap();
        assert_eq!(
            exp,
            serde_json::json!({"type": "session.expiring", "expiresAt": 1_893_456_000_000_i64})
        );
    }

    #[test]
    fn hub_caps_connections_per_user() {
        let hub = Hub::new();
        // Hold the receivers so receiver_count reflects live connections.
        let mut subs = Vec::new();
        for _ in 0..MAX_CONNS_PER_USER {
            subs.push(hub.subscribe("u1").expect("under cap"));
        }
        assert!(hub.subscribe("u1").is_none(), "connection over the cap must be rejected");
        // A different user is unaffected.
        assert!(hub.subscribe("u2").is_some());
        // Dropping a receiver frees a slot.
        subs.pop();
        assert!(hub.subscribe("u1").is_some(), "slot frees after a connection drops");
    }

    #[tokio::test]
    async fn local_publish_delivers_to_subscriber() {
        let hub = Hub::new();
        let mut rx = hub.subscribe("u1").unwrap();
        hub.publish_local("u1", Event::roles_changed(REASON_REVOKE));
        let got = rx.recv().await.unwrap();
        assert_eq!(got.event_type, "roles.changed");
        assert_eq!(got.reason.as_deref(), Some("revoke"));
    }
}
