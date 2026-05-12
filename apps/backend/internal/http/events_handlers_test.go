package httpapi_test

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"rustlens/backend/internal/appauth"
	"rustlens/backend/internal/events"
	httpapi "rustlens/backend/internal/http"
)

const testSessionSecret = "test-secret"

func newSignedSessionCookie(t *testing.T, userID string, epoch int) *http.Cookie {
	t.Helper()
	token, err := appauth.SerializeSession(appauth.SessionUser{
		UserID:    userID,
		Epoch:     epoch,
		ExpiresAt: time.Now().Add(time.Hour).UnixMilli(),
	}, testSessionSecret)
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}
	return &http.Cookie{Name: appauth.SessionCookieName, Value: token}
}

func TestEventsStream_RejectsWithoutCookie(t *testing.T) {
	hub := events.NewHub()
	h := httpapi.NewEventsHandler(testSessionSecret, hub, nil)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/events/stream", nil)
	h.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestEventsStream_EmitsInitialPingThenPublishedEvent(t *testing.T) {
	hub := events.NewHub()
	h := httpapi.NewEventsHandler(testSessionSecret, hub, nil)

	srv := httptest.NewServer(h)
	defer srv.Close()

	req, err := http.NewRequest("GET", srv.URL, nil)
	if err != nil {
		t.Fatalf("new req: %v", err)
	}
	req.AddCookie(newSignedSessionCookie(t, "user-1", 0))

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req = req.WithContext(ctx)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/event-stream") {
		t.Fatalf("Content-Type = %q", got)
	}

	reader := bufio.NewReader(resp.Body)
	// First event should be a ping.
	if got, err := readSSEEvent(reader); err != nil || got["event"] != "ping" {
		t.Fatalf("first event = %v, err = %v", got, err)
	}

	// Now publish — a second goroutine waiting on the hub should pick this up.
	go func() {
		time.Sleep(50 * time.Millisecond)
		hub.Publish("user-1", events.Event{Type: events.TypeRolesChanged, Reason: events.ReasonGrant})
	}()

	got, err := readSSEEvent(reader)
	if err != nil {
		t.Fatalf("read event: %v", err)
	}
	if got["event"] != "roles.changed" {
		t.Fatalf("event type = %q", got["event"])
	}
	if !strings.Contains(got["data"], `"reason":"grant"`) {
		t.Fatalf("data = %q", got["data"])
	}
}

// readSSEEvent reads one "event: X\ndata: Y\n\n" record.
func readSSEEvent(r *bufio.Reader) (map[string]string, error) {
	out := map[string]string{}
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			if len(out) == 0 {
				continue
			}
			return out, nil
		}
		if idx := strings.IndexByte(line, ':'); idx > 0 {
			key := line[:idx]
			val := strings.TrimSpace(line[idx+1:])
			out[key] = val
		}
	}
}
