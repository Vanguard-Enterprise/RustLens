package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"rustlens/backend/internal/appauth"
	"rustlens/backend/internal/events"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	sseHeartbeatInterval = 25 * time.Second
	sseExpiringWindow    = 30 * time.Minute
)

// EventsHandler serves GET /api/events/stream. Cookie-authenticated; pushes
// events from the supplied Hub. Pool is optional — when nil, the embedded
// session epoch is not validated against the database (used in tests).
type EventsHandler struct {
	sessionSecret string
	hub           *events.Hub
	pool          *pgxpool.Pool
}

// NewEventsHandler constructs an EventsHandler.
func NewEventsHandler(sessionSecret string, hub *events.Hub, pool *pgxpool.Pool) *EventsHandler {
	return &EventsHandler{sessionSecret: sessionSecret, hub: hub, pool: pool}
}

// ServeHTTP implements http.Handler.
func (h *EventsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// 1. Authenticate via cookie.
	session, err := appauth.ValidateSessionFromCookie(r.Context(), r, h.sessionSecret, h.pool)
	if err != nil {
		status := http.StatusUnauthorized
		writeJSON(w, status, map[string]string{"error": classifySessionError(err)})
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	// 2. Subscribe to hub for this user. Honour the per-user cap. Do this
	//    before setting SSE response headers so error responses don't claim a
	//    streaming Content-Type.
	ctx := r.Context()
	ch, unsub, err := h.hub.Subscribe(ctx, session.UserID)
	if err != nil {
		if errors.Is(err, events.ErrTooManyConnections) {
			writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "too_many_connections"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "subscribe_failed"})
		return
	}
	defer unsub()

	// 3. Headers required for SSE through HTTP/1.1 + Traefik.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	// 4. Send initial ping so the client knows the channel is live.
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	if !writeSSE(w, events.Event{Type: events.TypePing}) {
		return
	}
	flusher.Flush()

	// 5. Loop: forward hub events + heartbeat + one-shot session.expiring.
	heartbeat := time.NewTicker(sseHeartbeatInterval)
	defer heartbeat.Stop()

	expiringTicker := time.NewTicker(time.Minute)
	defer expiringTicker.Stop()
	expiringSent := false

	for {
		select {
		case <-ctx.Done():
			return
		case <-heartbeat.C:
			if !writeSSE(w, events.Event{Type: events.TypePing}) {
				return
			}
			flusher.Flush()
		case <-expiringTicker.C:
			if expiringSent {
				continue
			}
			remaining := time.Until(time.UnixMilli(session.ExpiresAt))
			if remaining > 0 && remaining < sseExpiringWindow {
				if !writeSSE(w, events.Event{
					Type:      events.TypeSessionExpiring,
					ExpiresAt: session.ExpiresAt,
				}) {
					return
				}
				flusher.Flush()
				expiringSent = true
			}
		case ev, ok := <-ch:
			if !ok {
				return
			}
			if !writeSSE(w, ev) {
				return
			}
			flusher.Flush()
		}
	}
}

// writeSSE writes one "event: ...\ndata: ...\n\n" record. Returns false on
// write error.
func writeSSE(w http.ResponseWriter, e events.Event) bool {
	payload, err := json.Marshal(e)
	if err != nil {
		return false
	}
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.Type, payload); err != nil {
		return false
	}
	return true
}

func classifySessionError(err error) string {
	switch {
	case errors.Is(err, appauth.ErrSessionRevoked):
		return "session_revoked"
	case errors.Is(err, appauth.ErrExpired):
		return "session_expired"
	case errors.Is(err, http.ErrNoCookie):
		return "session_required"
	default:
		return "invalid_session"
	}
}

// Compile-time check: handler is an http.Handler.
var _ http.Handler = (*EventsHandler)(nil)
