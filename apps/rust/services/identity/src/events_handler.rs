//! `GET /api/events/stream` — Server-Sent Events for live session/role changes.
//!
//! Cookie-authenticated (browser connects directly). Emits `roles.changed`,
//! `session.revoked`, `session.expiring`, and 25-second `ping` events in the Go
//! wire format (`event: <type>\ndata: <json>\n\n`). Mirrors the Go
//! `internal/http/events_handlers.go`.

use std::convert::Infallible;
use std::sync::Arc;
use std::time::{Duration, SystemTime, UNIX_EPOCH};

use axum::Json;
use axum::extract::State;
use axum::http::{HeaderMap, StatusCode, header};
use axum::response::sse::{Event as SseEvent, Sse};
use axum::response::{IntoResponse, Response};
use tokio::sync::broadcast;
use tokio_stream::wrappers::ReceiverStream;

use rustlens_events::Event;

use crate::appauth;
use crate::state::AppState;

type Shared = State<Arc<AppState>>;

/// 25-second heartbeat (Go `sseHeartbeatInterval`).
const HEARTBEAT: Duration = Duration::from_secs(25);
/// Fire `session.expiring` once when the remaining TTL drops below 30 minutes
/// (Go `sseExpiringWindow`).
const EXPIRING_WINDOW_MS: i64 = 30 * 60 * 1000;

pub async fn stream(State(st): Shared, headers: HeaderMap) -> Response {
    let session = match appauth::validate_session_cookie(&headers, &st).await {
        Ok(s) => s,
        Err((code, msg)) => {
            return (code, Json(serde_json::json!({ "error": msg }))).into_response();
        }
    };

    let Some(rx) = st.hub.subscribe(&session.user_id) else {
        return (
            StatusCode::TOO_MANY_REQUESTS,
            Json(serde_json::json!({ "error": "too_many_connections" })),
        )
            .into_response();
    };

    let stream = build_stream(rx, session.expires_at);
    let mut resp = Sse::new(stream).into_response();
    // Match the Go SSE headers. Sse already sets Content-Type: text/event-stream.
    let h = resp.headers_mut();
    h.insert(header::CACHE_CONTROL, "no-cache, no-transform".parse().unwrap());
    h.insert(header::CONNECTION, "keep-alive".parse().unwrap());
    h.insert("X-Accel-Buffering", "no".parse().unwrap());
    resp
}

fn now_ms() -> i64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_millis() as i64)
        .unwrap_or(0)
}

fn to_sse(ev: &Event) -> SseEvent {
    SseEvent::default()
        .event(&ev.event_type)
        .data(serde_json::to_string(ev).unwrap_or_default())
}

/// Build the per-connection SSE stream: pushed hub events + a 25s ping + a
/// one-shot `session.expiring` when the token enters its final 30 minutes.
fn build_stream(
    mut hub_rx: broadcast::Receiver<Event>,
    expires_at_ms: i64,
) -> ReceiverStream<Result<SseEvent, Infallible>> {
    let (tx, rx) = tokio::sync::mpsc::channel::<Result<SseEvent, Infallible>>(32);

    tokio::spawn(async move {
        let mut ping = tokio::time::interval(HEARTBEAT);
        ping.tick().await; // discard the immediate first tick

        // One-shot expiring timer: fire when remaining TTL < 30 min (clamped to
        // now if already within the window).
        let fire_in_ms = (expires_at_ms - EXPIRING_WINDOW_MS - now_ms()).max(0) as u64;
        let expiring = tokio::time::sleep(Duration::from_millis(fire_in_ms));
        tokio::pin!(expiring);
        let mut expiring_pending = true;
        let mut hub_open = true;

        loop {
            tokio::select! {
                r = hub_rx.recv(), if hub_open => match r {
                    Ok(ev) => {
                        if tx.send(Ok(to_sse(&ev))).await.is_err() {
                            break; // client disconnected
                        }
                    }
                    Err(broadcast::error::RecvError::Lagged(_)) => {}
                    Err(broadcast::error::RecvError::Closed) => { hub_open = false; }
                },
                _ = ping.tick() => {
                    if tx.send(Ok(to_sse(&Event::ping()))).await.is_err() {
                        break;
                    }
                }
                _ = &mut expiring, if expiring_pending => {
                    expiring_pending = false;
                    if tx.send(Ok(to_sse(&Event::session_expiring(expires_at_ms)))).await.is_err() {
                        break;
                    }
                }
            }
        }
    });

    ReceiverStream::new(rx)
}
