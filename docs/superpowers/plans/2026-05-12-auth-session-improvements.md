# Auth Session Improvements Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Sonner notifications (session expired, forced logout, role updated), instant role propagation via SSE, and sliding-TTL cookie refresh — without forcing re-auth on permission changes.

**Architecture:** Go backend gains an `internal/events` package (in-process hub + Redis pub/sub bridge) and a cookie-authenticated `GET /api/events/stream` SSE endpoint. Admin role/suspension/delete actions publish events keyed by `userID`. Frontend mounts a `SessionProvider` + `useSessionEvents` hook on the authenticated shell; UI gating reads live perms from context (cookie payload becomes a hint, not source of truth). A `refreshSession` server action re-mints the cookie when remaining TTL < 7 days.

**Tech Stack:** Go 1.24 (chi, pgx, go-redis), Next.js 16 / React 19 / Tailwind v4 / shadcn / sonner, Postgres 16, Redis 7.

**Spec:** [`docs/superpowers/specs/2026-05-12-auth-session-improvements-design.md`](../specs/2026-05-12-auth-session-improvements-design.md)

**Pre-implementation note (Next.js 16):** Per `apps/frontend/AGENTS.md`, this Next.js is not the version in your training data. Before touching any frontend file in Phase 8 onward, read the relevant guide under `apps/frontend/node_modules/next/dist/docs/` (server actions, cookies, `router.refresh`, `EventSource` in client components). The code samples below use the conventions present in the existing repo (`app-shell.tsx`, `lib/session.ts`, `lib/rustlens-api.ts`); when in doubt, follow those.

---

## Phase 1 — Backend events package

### Task 1: Event types

**Files:**
- Create: `apps/backend/internal/events/types.go`

- [ ] **Step 1: Create the types file**

```go
package events

// Event is the JSON payload that travels over SSE and Redis pub/sub.
// Keep this struct small and stable — the frontend mirrors it in
// apps/frontend/lib/events-client.ts. Reason values are documented
// alongside each event type in
// docs/superpowers/specs/2026-05-12-auth-session-improvements-design.md.
type Event struct {
	Type      string `json:"type"`
	Reason    string `json:"reason,omitempty"`
	ExpiresAt int64  `json:"expiresAt,omitempty"` // ms since unix epoch, set only for session.expiring
}

// Event type constants. Use these instead of string literals at call sites
// so a typo is a compile error.
const (
	TypeRolesChanged    = "roles.changed"
	TypeSessionRevoked  = "session.revoked"
	TypeSessionExpiring = "session.expiring"
	TypePing            = "ping"
)

// Reason constants for session.revoked.
const (
	ReasonForceLogout = "force_logout"
	ReasonSuspended   = "suspended"
	ReasonDeleted     = "deleted"
)

// Reason constants for roles.changed.
const (
	ReasonGrant             = "grant"
	ReasonRevoke            = "revoke"
	ReasonRolePermsUpdated  = "role_perms_updated"
)
```

- [ ] **Step 2: Verify build**

Run: `cd apps/backend && go build ./internal/events/...`
Expected: exit 0 (no test target yet — empty package would otherwise warn).

- [ ] **Step 3: Commit**

```bash
git add apps/backend/internal/events/types.go
git commit -m "feat(events): add event types for SSE channel"
```

### Task 2: In-process Hub — failing tests

**Files:**
- Create: `apps/backend/internal/events/hub_test.go`

- [ ] **Step 1: Write the test file**

```go
package events_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"rustlens/backend/internal/events"
)

func TestHub_PublishToSingleSubscriber(t *testing.T) {
	h := events.NewHub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, unsub, err := h.Subscribe(ctx, "user-1")
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer unsub()

	h.Publish("user-1", events.Event{Type: events.TypeRolesChanged, Reason: events.ReasonGrant})

	select {
	case got := <-ch:
		if got.Type != events.TypeRolesChanged || got.Reason != events.ReasonGrant {
			t.Fatalf("unexpected event: %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestHub_PublishFansOutToMultipleSubscribersForSameUser(t *testing.T) {
	h := events.NewHub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	chA, unsubA, err := h.Subscribe(ctx, "user-1")
	if err != nil {
		t.Fatalf("subscribe A: %v", err)
	}
	defer unsubA()
	chB, unsubB, err := h.Subscribe(ctx, "user-1")
	if err != nil {
		t.Fatalf("subscribe B: %v", err)
	}
	defer unsubB()

	h.Publish("user-1", events.Event{Type: events.TypeSessionRevoked, Reason: events.ReasonForceLogout})

	for i, ch := range []<-chan events.Event{chA, chB} {
		select {
		case got := <-ch:
			if got.Type != events.TypeSessionRevoked {
				t.Fatalf("subscriber %d got %+v", i, got)
			}
		case <-time.After(time.Second):
			t.Fatalf("subscriber %d timed out", i)
		}
	}
}

func TestHub_PublishDoesNotReachOtherUsers(t *testing.T) {
	h := events.NewHub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, unsub, err := h.Subscribe(ctx, "user-1")
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer unsub()

	h.Publish("user-2", events.Event{Type: events.TypeRolesChanged})

	select {
	case got := <-ch:
		t.Fatalf("user-1 should not receive events for user-2, got %+v", got)
	case <-time.After(50 * time.Millisecond):
		// expected
	}
}

func TestHub_UnsubscribeStopsDelivery(t *testing.T) {
	h := events.NewHub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, unsub, err := h.Subscribe(ctx, "user-1")
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	unsub()

	h.Publish("user-1", events.Event{Type: events.TypeRolesChanged})

	select {
	case got, ok := <-ch:
		if ok {
			t.Fatalf("expected channel closed; got %+v", got)
		}
	case <-time.After(100 * time.Millisecond):
		// channel may also simply be empty — either is acceptable
	}
}

func TestHub_ContextCancelUnsubscribes(t *testing.T) {
	h := events.NewHub()
	ctx, cancel := context.WithCancel(context.Background())

	ch, _, err := h.Subscribe(ctx, "user-1")
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	cancel()
	// Give the goroutine a moment to react to ctx.Done().
	time.Sleep(50 * time.Millisecond)
	h.Publish("user-1", events.Event{Type: events.TypeRolesChanged})

	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("expected no event after context cancel")
		}
	case <-time.After(100 * time.Millisecond):
		// acceptable
	}
}

func TestHub_PerUserConnectionCap(t *testing.T) {
	h := events.NewHub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var unsubs []func()
	for i := 0; i < 5; i++ {
		_, u, err := h.Subscribe(ctx, "user-1")
		if err != nil {
			t.Fatalf("subscribe %d: %v", i, err)
		}
		unsubs = append(unsubs, u)
	}
	defer func() {
		for _, u := range unsubs {
			u()
		}
	}()

	_, _, err := h.Subscribe(ctx, "user-1")
	if !errors.Is(err, events.ErrTooManyConnections) {
		t.Fatalf("expected ErrTooManyConnections, got %v", err)
	}
}

func TestHub_FullBufferDropsOldest(t *testing.T) {
	h := events.NewHub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Subscribe but don't read — buffer is 16.
	ch, unsub, err := h.Subscribe(ctx, "user-1")
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer unsub()

	// Publish 20 events; publisher must not block.
	done := make(chan struct{})
	go func() {
		for i := 0; i < 20; i++ {
			h.Publish("user-1", events.Event{Type: events.TypeRolesChanged, Reason: "n"})
		}
		close(done)
	}()

	select {
	case <-done:
		// expected
	case <-time.After(time.Second):
		t.Fatal("publisher blocked")
	}

	// Drain whatever the channel holds; we don't assert on count, only that the
	// publisher returned.
	go func() {
		for range ch {
		}
	}()
	_ = sync.Mutex{} // keep import
}
```

- [ ] **Step 2: Run tests, expect failure**

Run: `cd apps/backend && go test ./internal/events/...`
Expected: build error referring to `events.NewHub`, `events.ErrTooManyConnections`, `events.Hub.Subscribe`, `events.Hub.Publish` being undefined.

### Task 3: Implement Hub

**Files:**
- Create: `apps/backend/internal/events/hub.go`

- [ ] **Step 1: Write the implementation**

```go
package events

import (
	"context"
	"errors"
	"sync"
)

// MaxConnectionsPerUser is the cap a single userID may have open at once.
// The SSE handler returns 429 to additional connections past this limit.
const MaxConnectionsPerUser = 5

// subscriberBuffer is the per-channel buffer. When full, the oldest event is
// dropped to keep Publish non-blocking — a redundant roles.changed is cheaper
// than a stuck publisher.
const subscriberBuffer = 16

// ErrTooManyConnections is returned by Subscribe when a user is already at
// MaxConnectionsPerUser open subscribers.
var ErrTooManyConnections = errors.New("events: too many connections for user")

// Hub is an in-process pub/sub keyed by userID. It is safe for concurrent use.
type Hub struct {
	mu   sync.Mutex
	subs map[string][]chan Event // userID -> channels
}

// NewHub returns an empty Hub.
func NewHub() *Hub {
	return &Hub{subs: map[string][]chan Event{}}
}

// Subscribe registers a new subscriber for userID. The returned channel emits
// events until either unsub is called or ctx is done. The returned channel is
// closed exactly once when the subscription ends.
func (h *Hub) Subscribe(ctx context.Context, userID string) (<-chan Event, func(), error) {
	h.mu.Lock()
	if len(h.subs[userID]) >= MaxConnectionsPerUser {
		h.mu.Unlock()
		return nil, func() {}, ErrTooManyConnections
	}
	ch := make(chan Event, subscriberBuffer)
	h.subs[userID] = append(h.subs[userID], ch)
	h.mu.Unlock()

	var once sync.Once
	unsub := func() {
		once.Do(func() {
			h.remove(userID, ch)
		})
	}

	// Tie subscription lifetime to ctx.
	go func() {
		<-ctx.Done()
		unsub()
	}()

	return ch, unsub, nil
}

// Publish delivers e to all subscribers of userID. Never blocks; if a
// subscriber's buffer is full the oldest event is dropped.
func (h *Hub) Publish(userID string, e Event) {
	h.mu.Lock()
	chans := make([]chan Event, len(h.subs[userID]))
	copy(chans, h.subs[userID])
	h.mu.Unlock()

	for _, ch := range chans {
		select {
		case ch <- e:
		default:
			// Buffer full: drop oldest and retry once.
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- e:
			default:
				// Still full — give up; better to drop than block.
			}
		}
	}
}

// Snapshot returns the current connection count for a user. Test-only helper.
func (h *Hub) Snapshot(userID string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs[userID])
}

func (h *Hub) remove(userID string, target chan Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	chans := h.subs[userID]
	for i, ch := range chans {
		if ch == target {
			h.subs[userID] = append(chans[:i], chans[i+1:]...)
			close(ch)
			break
		}
	}
	if len(h.subs[userID]) == 0 {
		delete(h.subs, userID)
	}
}
```

- [ ] **Step 2: Run tests, expect pass**

Run: `cd apps/backend && go test ./internal/events/...`
Expected: `ok rustlens/backend/internal/events`

- [ ] **Step 3: Commit**

```bash
git add apps/backend/internal/events/hub.go apps/backend/internal/events/hub_test.go
git commit -m "feat(events): in-process pub/sub hub with per-user cap"
```

---

## Phase 2 — Redis bridge

### Task 4: Redis bridge — failing test

**Files:**
- Create: `apps/backend/internal/events/redis_bridge_test.go`

- [ ] **Step 1: Write the test**

```go
package events_test

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"rustlens/backend/internal/events"
)

func newTestRedis(t *testing.T) *redis.Client {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)
	return redis.NewClient(&redis.Options{Addr: mr.Addr()})
}

func TestRedisBridge_LocalPublishFansToLocalSubscribers(t *testing.T) {
	rdb := newTestRedis(t)
	hub := events.NewHub()
	bridge := events.NewRedisBridge(rdb, hub)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := bridge.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}

	ch, unsub, err := hub.Subscribe(ctx, "user-1")
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer unsub()

	// PublishWithFanout writes to local hub AND publishes on Redis.
	bridge.PublishWithFanout("user-1", events.Event{Type: events.TypeRolesChanged, Reason: events.ReasonGrant})

	select {
	case got := <-ch:
		if got.Type != events.TypeRolesChanged {
			t.Fatalf("unexpected event: %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestRedisBridge_RemotePublishDeliversToLocalSubscribers(t *testing.T) {
	rdb := newTestRedis(t)
	hub := events.NewHub()
	bridge := events.NewRedisBridge(rdb, hub)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := bridge.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}

	ch, unsub, err := hub.Subscribe(ctx, "user-1")
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer unsub()

	// Simulate a publish from another pod by writing directly to Redis using
	// the same channel name and wire format the bridge uses.
	other := redis.NewClient(rdb.Options())
	defer other.Close()
	payload := `{"userId":"user-1","event":{"type":"roles.changed","reason":"grant"}}`
	if err := other.Publish(ctx, events.RedisChannel, payload).Err(); err != nil {
		t.Fatalf("publish: %v", err)
	}

	select {
	case got := <-ch:
		if got.Type != events.TypeRolesChanged {
			t.Fatalf("unexpected event: %+v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event from remote publish")
	}
}
```

- [ ] **Step 2: Add miniredis dep if missing, then run tests**

Run: `cd apps/backend && go get github.com/alicebob/miniredis/v2 && go mod tidy && go test ./internal/events/...`
Expected: build errors for `events.NewRedisBridge`, `events.RedisChannel`, `events.RedisBridge.Start`, `events.RedisBridge.PublishWithFanout`.

### Task 5: Implement Redis bridge

**Files:**
- Create: `apps/backend/internal/events/redis_bridge.go`

- [ ] **Step 1: Write the implementation**

```go
package events

import (
	"context"
	"encoding/json"
	"log"

	"github.com/redis/go-redis/v9"
)

// RedisChannel is the pub/sub channel name used to fan events across backend
// pods. Both publishers and subscribers in this binary use it; remote pods
// running the same code can publish JSON-encoded redisEnvelope payloads to
// reach this pod's local Hub subscribers.
const RedisChannel = "rustlens:user-events"

type redisEnvelope struct {
	UserID string `json:"userId"`
	Event  Event  `json:"event"`
}

// RedisBridge mirrors local hub publishes to Redis and reads remote publishes
// back into the local hub. Construct with NewRedisBridge, start with Start.
// PublishWithFanout is the only entry point external code should use when it
// wants an event delivered to all pods.
type RedisBridge struct {
	rdb *redis.Client
	hub *Hub
}

// NewRedisBridge constructs a bridge but does not start any goroutines.
func NewRedisBridge(rdb *redis.Client, hub *Hub) *RedisBridge {
	return &RedisBridge{rdb: rdb, hub: hub}
}

// Start subscribes to RedisChannel and routes incoming messages into the
// local Hub. Returns once the SUBSCRIBE handshake has completed so that
// callers can begin publishing immediately. The bridge runs until ctx is done.
func (b *RedisBridge) Start(ctx context.Context) error {
	sub := b.rdb.Subscribe(ctx, RedisChannel)
	// Wait for the SUBSCRIBE to be acknowledged before returning.
	if _, err := sub.Receive(ctx); err != nil {
		_ = sub.Close()
		return err
	}
	ch := sub.Channel()

	go func() {
		defer sub.Close()
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}
				var env redisEnvelope
				if err := json.Unmarshal([]byte(msg.Payload), &env); err != nil {
					log.Printf("[events] bridge: decode payload: %v", err)
					continue
				}
				b.hub.Publish(env.UserID, env.Event)
			}
		}
	}()
	return nil
}

// PublishWithFanout delivers e to the local hub immediately AND publishes it
// over Redis so peer pods can deliver to their subscribers. If Redis is
// unavailable, the local fanout still succeeds.
func (b *RedisBridge) PublishWithFanout(userID string, e Event) {
	// Local first — synchronous, in-process.
	b.hub.Publish(userID, e)

	payload, err := json.Marshal(redisEnvelope{UserID: userID, Event: e})
	if err != nil {
		log.Printf("[events] bridge: encode payload: %v", err)
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := b.rdb.Publish(ctx, RedisChannel, payload).Err(); err != nil {
		log.Printf("[events] bridge: redis publish: %v", err)
	}
}
```

- [ ] **Step 2: Run tests, expect pass**

Run: `cd apps/backend && go test ./internal/events/...`
Expected: `ok rustlens/backend/internal/events`

- [ ] **Step 3: Commit**

```bash
git add apps/backend/internal/events/redis_bridge.go apps/backend/internal/events/redis_bridge_test.go apps/backend/go.mod apps/backend/go.sum
git commit -m "feat(events): redis pub/sub bridge for multi-pod fanout"
```

### Task 6: Publisher interface (decouple actions from concrete bridge)

**Files:**
- Create: `apps/backend/internal/events/publisher.go`

- [ ] **Step 1: Write the file**

```go
package events

// Publisher is the minimal interface action handlers depend on. Production
// wiring uses *RedisBridge; tests substitute a fake.
type Publisher interface {
	PublishWithFanout(userID string, e Event)
}

// NopPublisher is a Publisher that drops every event. Use it in tests that
// don't care about event emission.
type NopPublisher struct{}

func (NopPublisher) PublishWithFanout(string, Event) {}

// RecordingPublisher captures every PublishWithFanout call for assertion in
// tests. Safe for concurrent use.
type RecordingPublisher struct {
	mu      sync.Mutex
	Records []PublishRecord
}

// PublishRecord captures one PublishWithFanout invocation.
type PublishRecord struct {
	UserID string
	Event  Event
}

// PublishWithFanout records the call.
func (r *RecordingPublisher) PublishWithFanout(userID string, e Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Records = append(r.Records, PublishRecord{UserID: userID, Event: e})
}

// Snapshot returns a copy of all records seen so far.
func (r *RecordingPublisher) Snapshot() []PublishRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]PublishRecord, len(r.Records))
	copy(out, r.Records)
	return out
}
```

Add the missing import:

```go
import "sync"
```

- [ ] **Step 2: Build**

Run: `cd apps/backend && go build ./internal/events/...`
Expected: exit 0.

- [ ] **Step 3: Commit**

```bash
git add apps/backend/internal/events/publisher.go
git commit -m "feat(events): Publisher interface plus Nop and Recording test doubles"
```

---

## Phase 3 — SSE endpoint

### Task 7: Cookie-auth middleware test

**Files:**
- Create: `apps/backend/internal/http/events_handlers_test.go`

- [ ] **Step 1: Write the test**

```go
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
```

- [ ] **Step 2: Run tests, expect failure**

Run: `cd apps/backend && go test ./internal/http/... -run TestEventsStream`
Expected: build errors for `httpapi.NewEventsHandler`.

### Task 8: Implement SSE handler

**Files:**
- Create: `apps/backend/internal/http/events_handlers.go`

- [ ] **Step 1: Write the handler**

```go
package httpapi

import (
	"context"
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

	// 2. Headers required for SSE through HTTP/1.1 + Traefik.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	// 3. Subscribe to hub for this user. Honour the per-user cap.
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

	// 4. Send initial ping so the client knows the channel is live.
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	writeSSE(w, events.Event{Type: events.TypePing})
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
```

- [ ] **Step 2: Run tests, expect pass**

Run: `cd apps/backend && go test ./internal/http/... -run TestEventsStream`
Expected: pass.

- [ ] **Step 3: Commit**

```bash
git add apps/backend/internal/http/events_handlers.go apps/backend/internal/http/events_handlers_test.go
git commit -m "feat(events): SSE stream handler at /api/events/stream"
```

### Task 9: Mount /api/events route

**Files:**
- Modify: `apps/backend/internal/http/router.go` (add new route group; pass hub through NewRouter)

- [ ] **Step 1: Modify router signature and add the route group**

Change `NewRouter` to accept the hub. Edit `apps/backend/internal/http/router.go`:

Find the function signature:

```go
func NewRouter(
	cfg config.Config,
	postgres *store.PostgresStore,
	redis *store.RedisStore,
	shardService *shards.Service,
	steamClient *steamapi.Client,
	steamSummaryClient *steamapi.Client,
	battleMetricsClient *battlemetrics.Client,
	bmMatch *bmmatch.Client,
	platformSync *platformsync.Client,
	outboundProvider *outboundhttp.Provider,
	proxyCrypto *proxycrypto.Crypto,
) http.Handler {
```

Replace with:

```go
func NewRouter(
	cfg config.Config,
	postgres *store.PostgresStore,
	redis *store.RedisStore,
	shardService *shards.Service,
	steamClient *steamapi.Client,
	steamSummaryClient *steamapi.Client,
	battleMetricsClient *battlemetrics.Client,
	bmMatch *bmmatch.Client,
	platformSync *platformsync.Client,
	outboundProvider *outboundhttp.Provider,
	proxyCrypto *proxycrypto.Crypto,
	eventsHub *events.Hub,
) http.Handler {
```

Add the import (top of file):

```go
"rustlens/backend/internal/events"
```

After the existing `r.Route("/api/auth/discord", ...)` block and before `r.Route("/api/app", ...)`, insert:

```go
r.Route("/api/events", func(r chi.Router) {
	r.Get("/stream", NewEventsHandler(cfg.AppSessionSecret, eventsHub, postgres.Pool()).ServeHTTP)
})
```

- [ ] **Step 2: Fix the existing NewRouter call site so the binary still builds**

`apps/backend/cmd/server/main.go` calls NewRouter. Open it and add `nil` as the last argument **temporarily** so we can build now — Task 13 will wire the real hub:

Find:

```go
router := httpapi.NewRouter(
	cfg,
	postgresStore,
	redisStore,
	shardService,
	steamapi.NewClient(cfg.SteamAPIKey, directProvider),
	steamapi.NewClient(cfg.SteamAPIKey, directProvider),
	battlemetrics.NewClient(outboundProvider),
	bmMatchClient,
	platformSyncClient,
	outboundProvider,
	proxyCrypto,
)
```

Replace with:

```go
router := httpapi.NewRouter(
	cfg,
	postgresStore,
	redisStore,
	shardService,
	steamapi.NewClient(cfg.SteamAPIKey, directProvider),
	steamapi.NewClient(cfg.SteamAPIKey, directProvider),
	battlemetrics.NewClient(outboundProvider),
	bmMatchClient,
	platformSyncClient,
	outboundProvider,
	proxyCrypto,
	nil, // events hub — wired in main.go in a follow-up task
)
```

- [ ] **Step 3: Build and run all backend tests**

Run: `cd apps/backend && go build ./... && go test ./internal/http/... ./internal/events/...`
Expected: ok.

- [ ] **Step 4: Commit**

```bash
git add apps/backend/internal/http/router.go apps/backend/cmd/server/main.go
git commit -m "feat(events): mount /api/events/stream route"
```

---

## Phase 4 — Session validation tightening

### Task 10: ValidateSession rejects suspended/deleted users — failing test

**Files:**
- Modify: `apps/backend/internal/appauth/session_test.go` (add cases)

- [ ] **Step 1: Read the existing test file**

Open `apps/backend/internal/appauth/session_test.go` (you'll add to it, not rewrite). If you need example setup, look at `helpers_for_test.go` and `repo_discord_test.go`. Find the existing `ValidateSession` test and add the two cases below in the same file:

```go
func TestValidateSession_RejectsSuspendedUser(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	defer pool.Close()

	const userID = "usr_test01suspended00000000"
	mustExec(t, pool, ctx, `
		INSERT INTO app_users (id, discord_id, discord_username, display_name, roles, session_epoch, suspended_at)
		VALUES ($1, '100', 'test', 'test', '["user"]'::jsonb, 0, now())
	`, userID)

	tok, err := appauth.SerializeSession(appauth.SessionUser{
		UserID:    userID,
		Epoch:     0,
		ExpiresAt: time.Now().Add(time.Hour).UnixMilli(),
	}, "secret")
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}

	if _, err := appauth.ValidateSession(ctx, tok, "secret", pool); !errors.Is(err, appauth.ErrSessionRevoked) {
		t.Fatalf("expected ErrSessionRevoked, got %v", err)
	}
}

func TestValidateSession_RejectsDeletedUser(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	defer pool.Close()

	const userID = "usr_test01deleted0000000000"
	mustExec(t, pool, ctx, `
		INSERT INTO app_users (id, discord_id, discord_username, display_name, roles, session_epoch, deleted_at)
		VALUES ($1, '101', 'test', 'test', '["user"]'::jsonb, 0, now())
	`, userID)

	tok, err := appauth.SerializeSession(appauth.SessionUser{
		UserID:    userID,
		Epoch:     0,
		ExpiresAt: time.Now().Add(time.Hour).UnixMilli(),
	}, "secret")
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}

	if _, err := appauth.ValidateSession(ctx, tok, "secret", pool); !errors.Is(err, appauth.ErrSessionRevoked) {
		t.Fatalf("expected ErrSessionRevoked, got %v", err)
	}
}
```

(If `newTestPool` / `mustExec` don't already exist, check `helpers_for_test.go` for the canonical setup; use whichever helper the existing tests use. The file `apps/backend/internal/appauth/repo_discord_test.go` is a working template.)

- [ ] **Step 2: Run, expect failure**

Run: `cd apps/backend && go test ./internal/appauth/... -run "TestValidateSession_Rejects"`
Expected: tests fail because suspended/deleted users are currently accepted.

### Task 11: Implement suspended/deleted rejection

**Files:**
- Modify: `apps/backend/internal/appauth/session.go`

- [ ] **Step 1: Update ValidateSession to read suspended_at and deleted_at**

In `apps/backend/internal/appauth/session.go`, replace the body of `ValidateSession` (lines 74-91) with:

```go
func ValidateSession(ctx context.Context, token, secret string, pool *pgxpool.Pool) (*SessionUser, error) {
	s, err := ParseSession(token, secret)
	if err != nil {
		return nil, err
	}
	if pool == nil {
		return s, nil
	}
	var (
		currentEpoch int
		suspendedAt  *time.Time
		deletedAt    *time.Time
	)
	if err := pool.QueryRow(ctx,
		`SELECT session_epoch, suspended_at, deleted_at FROM app_users WHERE id = $1`,
		s.UserID,
	).Scan(&currentEpoch, &suspendedAt, &deletedAt); err != nil {
		// If the user no longer exists, treat the session as revoked.
		return nil, ErrSessionRevoked
	}
	if s.Epoch < currentEpoch {
		return nil, ErrSessionRevoked
	}
	if suspendedAt != nil || deletedAt != nil {
		return nil, ErrSessionRevoked
	}
	return s, nil
}
```

- [ ] **Step 2: Run, expect pass**

Run: `cd apps/backend && go test ./internal/appauth/...`
Expected: ok.

- [ ] **Step 3: Commit**

```bash
git add apps/backend/internal/appauth/session.go apps/backend/internal/appauth/session_test.go
git commit -m "fix(appauth): reject sessions for suspended or deleted users"
```

---

## Phase 5 — Wire events into admin actions

### Task 12: Add event emission to grant/revoke/suspend/delete/perms-set

**Files:**
- Modify: `apps/backend/internal/admin/actions/users_roles_grant.go`
- Modify: `apps/backend/internal/admin/actions/users_roles_revoke.go`
- Modify: `apps/backend/internal/admin/actions/users_suspend.go`
- Modify: `apps/backend/internal/admin/actions/users_delete.go`
- Modify: `apps/backend/internal/admin/actions/roles_permissions_set.go`
- Modify: `apps/backend/internal/http/admin_actions_register.go`
- Modify: `apps/backend/internal/http/handlers.go` (pass publisher)
- Test: `apps/backend/internal/admin/actions/users_roles_test.go` (extend)
- Test: `apps/backend/internal/admin/actions/users_suspend_test.go` (extend)
- Test: `apps/backend/internal/admin/actions/users_delete_test.go` (extend)
- Test: `apps/backend/internal/admin/actions/roles_permissions_set_test.go` (extend)

- [ ] **Step 1: Write the failing test for grant**

Open `apps/backend/internal/admin/actions/users_roles_test.go` and add (use the same setup pattern as the existing tests in that file):

```go
func TestUsersRolesGrant_PublishesRolesChanged(t *testing.T) {
	ctx, pool := testpool(t)
	defer pool.Close()
	seedTestUser(t, pool, "usr_target001grant000000000")
	resolver := rbac.NewResolver(rbac.NewStore(pool))

	pub := &events.RecordingPublisher{}
	a := actions.NewUsersRolesGrantAction(pool, resolver, pub)

	in := actions.ActionInput{
		ActorUserID: "usr_actor00grant00000000000",
		Params:      json.RawMessage(`{"userId":"usr_target001grant000000000","roleKey":"moderator"}`),
		Reason:      "test",
	}
	if _, err := a.Execute(ctx, in); err != nil {
		t.Fatalf("execute: %v", err)
	}

	recs := pub.Snapshot()
	if len(recs) != 1 {
		t.Fatalf("expected 1 publish, got %d", len(recs))
	}
	if recs[0].UserID != "usr_target001grant000000000" {
		t.Fatalf("publish userID = %q", recs[0].UserID)
	}
	if recs[0].Event.Type != events.TypeRolesChanged || recs[0].Event.Reason != events.ReasonGrant {
		t.Fatalf("publish event = %+v", recs[0].Event)
	}
}
```

(`testpool` and `seedTestUser` reference helpers used elsewhere in the actions test suite — if a helper with this signature doesn't exist, mirror what the existing test in this file does.)

Run: `cd apps/backend && go test ./internal/admin/actions/...`
Expected: fail (constructor doesn't accept a publisher).

- [ ] **Step 2: Update users_roles_grant.go to take Publisher and emit on success**

In `apps/backend/internal/admin/actions/users_roles_grant.go`:

Replace the struct and constructor:

```go
type usersRolesGrantAction struct {
	pool     *pgxpool.Pool
	resolver *rbac.Resolver
	pub      events.Publisher
}

// NewUsersRolesGrantAction grants an admin role to a user. Granting the
// "superadmin" role requires the actor to hold the admins.grant permission
// in addition to the base users.roles.assign permission. On success, publishes
// a roles.changed event for the target user via pub.
func NewUsersRolesGrantAction(pool *pgxpool.Pool, resolver *rbac.Resolver, pub events.Publisher) AdminAction {
	return &usersRolesGrantAction{pool: pool, resolver: resolver, pub: pub}
}
```

At the bottom of `Execute`, immediately before the final `return ActionResult{...}`, add:

```go
if a.pub != nil {
	a.pub.PublishWithFanout(p.UserID, events.Event{Type: events.TypeRolesChanged, Reason: events.ReasonGrant})
}
```

Add the import at the top:

```go
"rustlens/backend/internal/events"
```

- [ ] **Step 3: Update users_roles_revoke.go the same way**

Same pattern. Struct gets `pub events.Publisher`; constructor accepts and stores it; emit `events.ReasonRevoke` after the DELETE succeeds. Import `rustlens/backend/internal/events`. Constructor signature becomes:

```go
func NewUsersRolesRevokeAction(pool *pgxpool.Pool, pub events.Publisher) AdminAction
```

- [ ] **Step 4: Update users_suspend.go and users_delete.go**

Same pattern but emit `session.revoked` with the matching reason:

```go
// In users_suspend.go Execute, after tx.Commit succeeds:
if a.pub != nil {
	a.pub.PublishWithFanout(p.UserID, events.Event{Type: events.TypeSessionRevoked, Reason: events.ReasonSuspended})
}

// In users_delete.go Execute, after tx.Commit succeeds:
if a.pub != nil {
	a.pub.PublishWithFanout(p.UserID, events.Event{Type: events.TypeSessionRevoked, Reason: events.ReasonDeleted})
}
```

Constructor signatures become:

```go
func NewUsersSuspendAction(pool *pgxpool.Pool, pub events.Publisher) AdminAction
func NewUsersDeleteAction(pool *pgxpool.Pool, pub events.Publisher) AdminAction
```

- [ ] **Step 5: Update roles_permissions_set.go with fanout to all role holders**

Same struct/constructor pattern. After `tx.Commit` succeeds in `Execute`, add a fanout query:

```go
if a.pub != nil {
	rows, err := a.pool.Query(ctx,
		`SELECT user_id FROM admin_user_roles WHERE role_id::text = $1`,
		p.ID,
	)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var uid string
			if scanErr := rows.Scan(&uid); scanErr != nil {
				continue
			}
			a.pub.PublishWithFanout(uid, events.Event{Type: events.TypeRolesChanged, Reason: events.ReasonRolePermsUpdated})
		}
	}
}
```

Constructor signature becomes:

```go
func NewRolesPermissionsSetAction(pool *pgxpool.Pool, pub events.Publisher) AdminAction
```

- [ ] **Step 6: Update registerAdminActions to pass the publisher**

In `apps/backend/internal/http/admin_actions_register.go`, change the signature to accept a publisher and thread it through:

```go
func registerAdminActions(reg *actions.Registry, pool *pgxpool.Pool, resolver *rbac.Resolver, pub events.Publisher, _ string) error {
	if err := reg.Register(actions.NewRolesCreateAction(pool)); err != nil {
		return err
	}
	if err := reg.Register(actions.NewRolesDeleteAction(pool)); err != nil {
		return err
	}
	if err := reg.Register(actions.NewRolesPermissionsSetAction(pool, pub)); err != nil {
		return err
	}
	if err := reg.Register(actions.NewRolesUpdateAction(pool)); err != nil {
		return err
	}
	if err := reg.Register(actions.NewUsersEditAction(pool)); err != nil {
		return err
	}
	if err := reg.Register(actions.NewUsersRolesGrantAction(pool, resolver, pub)); err != nil {
		return err
	}
	if err := reg.Register(actions.NewUsersRolesRevokeAction(pool, pub)); err != nil {
		return err
	}
	if err := reg.Register(actions.NewUsersSuspendAction(pool, pub)); err != nil {
		return err
	}
	if err := reg.Register(actions.NewUsersUnsuspendAction(pool)); err != nil {
		return err
	}
	if err := reg.Register(actions.NewUsersDeleteAction(pool, pub)); err != nil {
		return err
	}
	if err := reg.Register(actions.NewUsersRestoreAction(pool)); err != nil {
		return err
	}
	if err := reg.Register(actions.NewUsersPurgeAction(pool)); err != nil {
		return err
	}
	return nil
}
```

Add the import: `"rustlens/backend/internal/events"`.

- [ ] **Step 7: Update Handler to accept and forward the publisher**

In `apps/backend/internal/http/handlers.go`:

1. Add a `publisher events.Publisher` field to the `Handler` struct.
2. Add `publisher events.Publisher` as a new last parameter to `NewHandler`.
3. Pass it through to `registerAdminActions(registry, postgres.Pool(), resolver, publisher, cfg.AppBaseURL)`.
4. Update `NewRouter`'s call to `NewHandler` accordingly — `NewRouter` now also takes `eventsHub` (added in Task 9). Build a `nopPublisher` adapter here too: when `eventsHub` is nil (tests), pass `events.NopPublisher{}`; otherwise, the real bridge gets wired in main.go and the publisher used in tests can be `events.NopPublisher{}`.

Update the relevant parts. In `router.go`, modify the `NewHandler` call:

```go
var publisher events.Publisher = events.NopPublisher{}
if h, ok := w.(events.Publisher); ok { _ = h }
// Publisher is supplied via main.go; if NewRouter received nil, default to nop.
// (We re-use the hub-aware publisher set in main; see Task 13.)
```

Actually simpler: add `publisher events.Publisher` as a parameter to `NewRouter` itself. Replace the signature with:

```go
func NewRouter(
	cfg config.Config,
	postgres *store.PostgresStore,
	redis *store.RedisStore,
	shardService *shards.Service,
	steamClient *steamapi.Client,
	steamSummaryClient *steamapi.Client,
	battleMetricsClient *battlemetrics.Client,
	bmMatch *bmmatch.Client,
	platformSync *platformsync.Client,
	outboundProvider *outboundhttp.Provider,
	proxyCrypto *proxycrypto.Crypto,
	eventsHub *events.Hub,
	publisher events.Publisher,
) http.Handler {
```

Inside `NewRouter`, default `publisher` to `events.NopPublisher{}` when nil so existing test callers don't crash:

```go
if publisher == nil {
	publisher = events.NopPublisher{}
}
```

Then update the `NewHandler` call to pass `publisher` as the last argument.

In `main.go`, add a temporary `nil, nil` for the two new args; Task 13 replaces them.

- [ ] **Step 8: Run all action tests, expect pass**

Run: `cd apps/backend && go build ./... && go test ./internal/admin/... ./internal/http/...`
Expected: ok. (You'll likely need to update existing test setups in `users_roles_test.go`, `users_suspend_test.go`, `users_delete_test.go`, `roles_permissions_set_test.go` to pass `events.NopPublisher{}` to the constructors. Use `events.NopPublisher{}` everywhere the publisher isn't being asserted; only the new "PublishesXxx" tests use `RecordingPublisher`.)

- [ ] **Step 9: Commit**

```bash
git add apps/backend/internal/admin/actions/users_roles_grant.go apps/backend/internal/admin/actions/users_roles_revoke.go apps/backend/internal/admin/actions/users_suspend.go apps/backend/internal/admin/actions/users_delete.go apps/backend/internal/admin/actions/roles_permissions_set.go apps/backend/internal/admin/actions/users_roles_test.go apps/backend/internal/admin/actions/users_suspend_test.go apps/backend/internal/admin/actions/users_delete_test.go apps/backend/internal/admin/actions/roles_permissions_set_test.go apps/backend/internal/http/admin_actions_register.go apps/backend/internal/http/handlers.go apps/backend/internal/http/router.go
git commit -m "feat(events): publish roles.changed and session.revoked from admin actions"
```

---

## Phase 6 — Force-logout action

### Task 13: `users.force_logout` admin action

**Files:**
- Create: `apps/backend/internal/admin/actions/users_force_logout.go`
- Create: `apps/backend/internal/admin/actions/users_force_logout_test.go`
- Modify: `apps/backend/internal/http/admin_actions_register.go`

- [ ] **Step 1: Failing test**

Create `apps/backend/internal/admin/actions/users_force_logout_test.go`:

```go
package actions_test

import (
	"context"
	"encoding/json"
	"testing"

	"rustlens/backend/internal/admin/actions"
	"rustlens/backend/internal/events"
)

func TestUsersForceLogout_BumpsEpochAndPublishesRevoked(t *testing.T) {
	ctx, pool := testpool(t)
	defer pool.Close()
	const target = "usr_target0force000000000000"
	seedTestUser(t, pool, target)
	// Seed initial epoch.
	mustExec(t, pool, ctx, `UPDATE app_users SET session_epoch = 3 WHERE id = $1`, target)

	pub := &events.RecordingPublisher{}
	a := actions.NewUsersForceLogoutAction(pool, pub)

	if _, err := a.Execute(ctx, actions.ActionInput{
		ActorUserID: "usr_actor00force00000000000",
		Params:      json.RawMessage(`{"userId":"` + target + `"}`),
		Reason:      "abuse",
	}); err != nil {
		t.Fatalf("execute: %v", err)
	}

	var epoch int
	mustQueryRow(t, pool, ctx, `SELECT session_epoch FROM app_users WHERE id = $1`, target).Scan(&epoch)
	if epoch != 4 {
		t.Fatalf("expected epoch 4, got %d", epoch)
	}

	recs := pub.Snapshot()
	if len(recs) != 1 {
		t.Fatalf("expected 1 publish, got %d", len(recs))
	}
	if recs[0].Event.Type != events.TypeSessionRevoked || recs[0].Event.Reason != events.ReasonForceLogout {
		t.Fatalf("event = %+v", recs[0].Event)
	}
}

func TestUsersForceLogout_RejectsSelf(t *testing.T) {
	ctx, pool := testpool(t)
	defer pool.Close()
	const self = "usr_actor00selfforce00000000"
	seedTestUser(t, pool, self)

	a := actions.NewUsersForceLogoutAction(pool, events.NopPublisher{})
	_, err := a.Execute(ctx, actions.ActionInput{
		ActorUserID: self,
		Params:      json.RawMessage(`{"userId":"` + self + `"}`),
		Reason:      "x",
	})
	if err == nil {
		t.Fatal("expected GuardActingOnSelf error")
	}
}

func _ = context.TODO // keep import
```

Replace any test helper names that diverge from your codebase's conventions (`mustQueryRow`, `testpool`, `seedTestUser`) with the existing helpers used in `users_suspend_test.go`.

Run: `cd apps/backend && go test ./internal/admin/actions/... -run ForceLogout`
Expected: build error for `actions.NewUsersForceLogoutAction`.

- [ ] **Step 2: Create the action**

Write `apps/backend/internal/admin/actions/users_force_logout.go`:

```go
package actions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"rustlens/backend/internal/admin/rbac"
	"rustlens/backend/internal/events"
)

// UsersForceLogoutParams identifies the user whose sessions should be invalidated.
type UsersForceLogoutParams struct {
	UserID string `json:"userId"`
}

type usersForceLogoutAction struct {
	pool *pgxpool.Pool
	pub  events.Publisher
}

// NewUsersForceLogoutAction bumps a user's session_epoch (invalidating every
// signed cookie they hold) and publishes a session.revoked event so any open
// SSE connections close on the receiving end. Required permission:
// users.security.reset.
func NewUsersForceLogoutAction(pool *pgxpool.Pool, pub events.Publisher) AdminAction {
	return &usersForceLogoutAction{pool: pool, pub: pub}
}

func (a *usersForceLogoutAction) Type() string                 { return "users.force_logout" }
func (a *usersForceLogoutAction) Permission() string           { return rbac.PermUsersSecurityReset }
func (a *usersForceLogoutAction) RequiresReason() bool         { return true }
func (a *usersForceLogoutAction) Dangerous() bool              { return false }
func (a *usersForceLogoutAction) SensitiveParamKeys() []string { return nil }
func (a *usersForceLogoutAction) ResourceType() string         { return "user" }

func (a *usersForceLogoutAction) Validate(raw json.RawMessage) (ValidatedParams, error) {
	var p UsersForceLogoutParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return ValidatedParams{}, fmt.Errorf("validation: %w", err)
	}
	if p.UserID == "" {
		return ValidatedParams{}, errors.New("userId is required")
	}
	target := p.UserID
	return ValidatedParams{ResourceID: p.UserID, TargetUserID: &target}, nil
}

func (a *usersForceLogoutAction) Execute(ctx context.Context, in ActionInput) (ActionResult, error) {
	var p UsersForceLogoutParams
	if err := json.Unmarshal(in.Params, &p); err != nil {
		return ActionResult{}, fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}
	if err := GuardActingOnSelf(in.ActorUserID, p.UserID); err != nil {
		return ActionResult{}, err
	}

	var updatedID string
	err := a.pool.QueryRow(ctx,
		`UPDATE app_users SET session_epoch = session_epoch + 1, updated_at = now() WHERE id = $1 RETURNING id`,
		p.UserID,
	).Scan(&updatedID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ActionResult{}, fmt.Errorf("%w: user not found", ErrNotFound)
	}
	if err != nil {
		return ActionResult{}, err
	}

	if a.pub != nil {
		a.pub.PublishWithFanout(p.UserID, events.Event{
			Type:   events.TypeSessionRevoked,
			Reason: events.ReasonForceLogout,
		})
	}

	target := p.UserID
	return ActionResult{
		ResourceID:   p.UserID,
		TargetUserID: &target,
		Payload:      map[string]any{"userId": p.UserID},
	}, nil
}
```

- [ ] **Step 3: Register in admin_actions_register.go**

Add to `registerAdminActions` (keep alphabetised — place before `NewUsersPurgeAction`):

```go
if err := reg.Register(actions.NewUsersForceLogoutAction(pool, pub)); err != nil {
	return err
}
```

- [ ] **Step 4: Run, expect pass**

Run: `cd apps/backend && go test ./internal/admin/actions/...`
Expected: ok.

- [ ] **Step 5: Commit**

```bash
git add apps/backend/internal/admin/actions/users_force_logout.go apps/backend/internal/admin/actions/users_force_logout_test.go apps/backend/internal/http/admin_actions_register.go
git commit -m "feat(admin): users.force_logout action bumps epoch + publishes session.revoked"
```

---

## Phase 7 — main.go wiring

### Task 14: Wire Hub + RedisBridge in main.go

**Files:**
- Modify: `apps/backend/cmd/server/main.go`

- [ ] **Step 1: Construct Hub + Bridge and pass them in**

Find the section in `main.go`:

```go
shardService := shards.NewService(redisStore, postgresStore, cfg.BotTargetShards, cfg.BotHeartbeatTTL)
bmMatchClient := bmmatch.NewClient(cfg.BMMatchURL, cfg.BMMatchToken, 10*time.Second)
platformSyncClient := platformsync.NewClient(cfg.PlatformSyncToken, 10*time.Second)
router := httpapi.NewRouter(
	cfg,
	postgresStore,
	...
	proxyCrypto,
	nil, // events hub — wired in main.go in a follow-up task
)
```

Replace with:

```go
shardService := shards.NewService(redisStore, postgresStore, cfg.BotTargetShards, cfg.BotHeartbeatTTL)
bmMatchClient := bmmatch.NewClient(cfg.BMMatchURL, cfg.BMMatchToken, 10*time.Second)
platformSyncClient := platformsync.NewClient(cfg.PlatformSyncToken, 10*time.Second)

eventsHub := events.NewHub()
eventsBridge := events.NewRedisBridge(redisStore.Client(), eventsHub)
if err := eventsBridge.Start(ctx); err != nil {
	log.Fatalf("events bridge start failed: %v", err)
}

router := httpapi.NewRouter(
	cfg,
	postgresStore,
	redisStore,
	shardService,
	steamapi.NewClient(cfg.SteamAPIKey, directProvider),
	steamapi.NewClient(cfg.SteamAPIKey, directProvider),
	battlemetrics.NewClient(outboundProvider),
	bmMatchClient,
	platformSyncClient,
	outboundProvider,
	proxyCrypto,
	eventsHub,
	eventsBridge,
)
```

Add the import: `"rustlens/backend/internal/events"`.

- [ ] **Step 2: Add a `Client()` accessor on RedisStore so the bridge can reach the underlying client**

In `apps/backend/internal/store/redis.go`, add at the bottom:

```go
// Client returns the underlying go-redis client. Used by callers that need
// direct pub/sub access; the typed wrapper methods on RedisStore are still
// preferred for storage-style operations.
func (r *RedisStore) Client() *redis.Client {
	return r.client
}
```

- [ ] **Step 3: Build and smoke-test**

Run: `cd apps/backend && go build ./... && go vet ./...`
Expected: exit 0.

Then start infra and the backend, hit the route through Traefik:

```bash
make infra-up
make dev-backend &
sleep 5
# In a separate terminal (or the same after backgrounding):
curl -i -N http://localhost/api/events/stream
```

Expected: HTTP 401 with `{"error":"session_required"}` (no cookie).

- [ ] **Step 4: Commit**

```bash
git add apps/backend/cmd/server/main.go apps/backend/internal/store/redis.go
git commit -m "feat(events): wire Hub + RedisBridge into backend boot"
```

---

## Phase 8 — Frontend session-revoked error type

> **Reminder:** Before editing any frontend file, read the relevant Next.js 16 docs under `apps/frontend/node_modules/next/dist/docs/`. The samples below match conventions already in the repo (`lib/session.ts`, `lib/rustlens-api.ts`).

### Task 15: Typed `SessionRevokedError` and detection helper

**Files:**
- Modify: `apps/frontend/lib/rustlens-api.ts`
- Create: `apps/frontend/lib/session-errors.ts`

- [ ] **Step 1: Create the error class**

Write `apps/frontend/lib/session-errors.ts`:

```ts
import "server-only";

/**
 * Thrown by lib/rustlens-api.ts when the backend rejects a session for a
 * recoverable reason (revoked by admin / suspended / deleted / expired /
 * missing). Server components that catch this should clear the cookie and
 * redirect to /login with the matching ?notice= value so the user sees a
 * Sonner toast on the next render.
 */
export class SessionRevokedError extends Error {
	readonly notice: SessionRevokedNotice;

	constructor(notice: SessionRevokedNotice) {
		super(`session_revoked:${notice}`);
		this.name = "SessionRevokedError";
		this.notice = notice;
	}
}

export type SessionRevokedNotice =
	| "force_logout"
	| "suspended"
	| "deleted"
	| "expired"
	| "connection_lost"
	| "unknown";

// Maps backend error codes to user-facing notice slugs. Keep in sync with the
// classifySessionError() function in apps/backend/internal/http/events_handlers.go
// and with SESSION_INVALID_ERRORS in apps/frontend/lib/rustlens-api.ts.
export function noticeFromBackendError(code: string): SessionRevokedNotice {
	switch (code) {
		case "session_revoked":
			// We don't know the precise reason here — the SSE channel delivers
			// reason; pre-SSE 401 paths see only the generic code.
			return "unknown";
		case "session_expired":
			return "expired";
		case "session_required":
		case "invalid_session":
			return "expired";
		default:
			return "unknown";
	}
}
```

- [ ] **Step 2: Update `backendFetch` in `rustlens-api.ts` to throw the typed error**

In `apps/frontend/lib/rustlens-api.ts`, around line 251-273, replace the 401 handling block. Find:

```ts
if (
	response.status === 401 &&
	options.requireSession &&
	errorCode &&
	SESSION_INVALID_ERRORS.has(errorCode)
) {
	await clearSession();
	redirect("/login?error=session");
}
throw new Error(message);
```

Replace with:

```ts
if (
	response.status === 401 &&
	options.requireSession &&
	errorCode &&
	SESSION_INVALID_ERRORS.has(errorCode)
) {
	await clearSession();
	const notice = noticeFromBackendError(errorCode);
	redirect(`/login?notice=${notice}`);
}
throw new Error(message);
```

Add the import at the top of the file:

```ts
import { noticeFromBackendError } from "@/lib/session-errors";
```

(`redirect()` throws so the surrounding callers don't need a `throw` after it. Existing behaviour preserved.)

- [ ] **Step 3: Type-check**

Run: `pnpm --filter @rustlens/frontend exec tsc --noEmit`
Expected: exit 0.

- [ ] **Step 4: Commit**

```bash
git add apps/frontend/lib/session-errors.ts apps/frontend/lib/rustlens-api.ts
git commit -m "feat(frontend): typed session-revoked notices, redirect to /login?notice="
```

---

## Phase 9 — Sliding-TTL refresh server action

### Task 16: `refreshSession` server action

**Files:**
- Create: `apps/frontend/lib/session-refresh.ts`

- [ ] **Step 1: Read Next.js 16 server-action conventions**

Read: `apps/frontend/node_modules/next/dist/docs/01-app/02-guides/server-actions-and-mutations.mdx` (file name may differ — use the table of contents in that directory). Confirm the file-level `"use server"` directive is still the entry pattern.

- [ ] **Step 2: Write the server action**

Write `apps/frontend/lib/session-refresh.ts`:

```ts
"use server";

import { getCurrentUser } from "@/lib/rustlens-api";
import { getSession, setSession, toSessionUser } from "@/lib/session";

const REFRESH_THRESHOLD_MS = 7 * 24 * 60 * 60 * 1000; // 7 days

export type RefreshResult =
	| { ok: true; refreshed: boolean }
	| { ok: false; reason: "no_session" | "fetch_failed" };

/**
 * Re-mints the rustlens_session cookie when the remaining TTL drops below 7
 * days. No-ops (returns ok:true, refreshed:false) when the cookie has more
 * than 7 days remaining. Returns ok:false when the cookie is missing or the
 * backend rejects /api/app/me (the caller is responsible for clearing state
 * and redirecting to /login — usually the SSE event handler does this).
 */
export async function refreshSession(): Promise<RefreshResult> {
	const session = await getSession();
	if (!session) {
		return { ok: false, reason: "no_session" };
	}
	const remaining = session.expiresAt - Date.now();
	if (remaining > REFRESH_THRESHOLD_MS) {
		return { ok: true, refreshed: false };
	}
	try {
		const current = await getCurrentUser();
		await setSession(toSessionUser(current));
		return { ok: true, refreshed: true };
	} catch {
		return { ok: false, reason: "fetch_failed" };
	}
}
```

- [ ] **Step 3: Type-check**

Run: `pnpm --filter @rustlens/frontend exec tsc --noEmit`
Expected: exit 0.

- [ ] **Step 4: Commit**

```bash
git add apps/frontend/lib/session-refresh.ts
git commit -m "feat(frontend): refreshSession server action for sliding 14d TTL"
```

---

## Phase 10 — Frontend SSE client + SessionProvider

### Task 17: SSE client helper

**Files:**
- Create: `apps/frontend/lib/events-client.ts`

- [ ] **Step 1: Read Next 16 client-component conventions**

Confirm `"use client"` placement on hooks that touch `window.EventSource`. Browse `apps/frontend/node_modules/next/dist/docs/01-app/01-getting-started/`.

- [ ] **Step 2: Write the helper (typed event shapes, no React yet)**

Write `apps/frontend/lib/events-client.ts`:

```ts
// Pure browser helpers — keep React out so this file can be unit-tested with
// no DOM-React harness. The hook is in components/session-events.tsx.

export type SessionEvent =
	| { type: "roles.changed"; reason: "grant" | "revoke" | "role_perms_updated" }
	| { type: "session.revoked"; reason: "force_logout" | "suspended" | "deleted" }
	| { type: "session.expiring"; expiresAt: number }
	| { type: "ping" };

export type SessionEventHandlers = {
	onRolesChanged: (e: Extract<SessionEvent, { type: "roles.changed" }>) => void;
	onSessionRevoked: (e: Extract<SessionEvent, { type: "session.revoked" }>) => void;
	onSessionExpiring: (e: Extract<SessionEvent, { type: "session.expiring" }>) => void;
	onConnectionLost: () => void;
};

const FAILURE_WINDOW_MS = 30_000;
const FAILURE_THRESHOLD = 3;

/**
 * Opens an EventSource against /api/events/stream and routes parsed events
 * to the supplied handlers. The returned function closes the connection.
 *
 * Treats 3 consecutive failures in a 30s window as a "connection lost" event
 * — handlers should treat it like a revoked session and redirect to /login.
 */
export function openSessionEventStream(handlers: SessionEventHandlers): () => void {
	const source = new EventSource("/api/events/stream", { withCredentials: true });
	let failures: number[] = [];
	let closed = false;

	const safeClose = () => {
		if (closed) return;
		closed = true;
		source.close();
	};

	source.addEventListener("roles.changed", (ev) => {
		const data = parseData(ev as MessageEvent);
		if (data?.type === "roles.changed") handlers.onRolesChanged(data);
	});
	source.addEventListener("session.revoked", (ev) => {
		const data = parseData(ev as MessageEvent);
		if (data?.type === "session.revoked") {
			handlers.onSessionRevoked(data);
			safeClose();
		}
	});
	source.addEventListener("session.expiring", (ev) => {
		const data = parseData(ev as MessageEvent);
		if (data?.type === "session.expiring") handlers.onSessionExpiring(data);
	});
	source.addEventListener("ping", () => {
		// no-op; presence of the event proves the channel is alive.
	});
	source.addEventListener("error", () => {
		const now = Date.now();
		failures = failures.filter((t) => now - t < FAILURE_WINDOW_MS);
		failures.push(now);
		if (failures.length >= FAILURE_THRESHOLD) {
			handlers.onConnectionLost();
			safeClose();
		}
	});

	return safeClose;
}

function parseData(ev: MessageEvent): SessionEvent | null {
	try {
		return JSON.parse(ev.data) as SessionEvent;
	} catch {
		return null;
	}
}
```

- [ ] **Step 3: Type-check**

Run: `pnpm --filter @rustlens/frontend exec tsc --noEmit`
Expected: exit 0.

- [ ] **Step 4: Commit**

```bash
git add apps/frontend/lib/events-client.ts
git commit -m "feat(frontend): EventSource client helper for /api/events/stream"
```

### Task 18: SessionProvider + useSessionEvents hook

**Files:**
- Create: `apps/frontend/components/session-provider.tsx`
- Create: `apps/frontend/components/session-events.tsx`

- [ ] **Step 1: Write SessionProvider**

Write `apps/frontend/components/session-provider.tsx`:

```tsx
"use client";

import { createContext, useCallback, useContext, useState } from "react";

import type { CurrentUser } from "@/lib/rustlens-api";
import { refreshCurrentUser } from "@/lib/session-actions";

export type SessionContextValue = {
	user: CurrentUser["user"] | null;
	roles: string[];
	adminRoles: string[];
	permissions: string[];
	refresh: () => Promise<void>;
};

const SessionContext = createContext<SessionContextValue | null>(null);

export function SessionProvider({
	initial,
	children,
}: {
	initial: CurrentUser | null;
	children: React.ReactNode;
}) {
	const [snapshot, setSnapshot] = useState<CurrentUser | null>(initial);

	const refresh = useCallback(async () => {
		const next = await refreshCurrentUser();
		setSnapshot(next);
	}, []);

	const value: SessionContextValue = {
		user: snapshot?.user ?? null,
		roles: snapshot?.user.roles ?? [],
		adminRoles: snapshot?.adminRoles ?? [],
		permissions: snapshot?.permissions ?? [],
		refresh,
	};

	return <SessionContext.Provider value={value}>{children}</SessionContext.Provider>;
}

export function useSession() {
	const ctx = useContext(SessionContext);
	if (!ctx) {
		throw new Error("useSession must be used inside <SessionProvider>");
	}
	return ctx;
}
```

- [ ] **Step 2: Write the `refreshCurrentUser` server action**

Create `apps/frontend/lib/session-actions.ts`:

```ts
"use server";

import { getCurrentUserSafe } from "@/lib/rustlens-api";

/**
 * Re-fetches /api/app/me from the backend so the client SessionProvider can
 * update without a navigation. Returns null when the backend rejects the
 * cookie — callers should treat null as "session ended" and redirect.
 */
export async function refreshCurrentUser() {
	return getCurrentUserSafe();
}
```

- [ ] **Step 3: Write SessionEvents (mounts the hook)**

Write `apps/frontend/components/session-events.tsx`:

```tsx
"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { toast } from "sonner";

import { openSessionEventStream } from "@/lib/events-client";
import { clearSessionAction } from "@/lib/session-actions";
import { refreshSession } from "@/lib/session-refresh";
import { useSession } from "@/components/session-provider";

const REVOKED_NOTICE = {
	force_logout: "force_logout",
	suspended: "suspended",
	deleted: "deleted",
} as const;

/**
 * Mounts inside <SessionProvider>. Opens the SSE stream on mount, dispatches
 * server-pushed events to Sonner + session refresh + redirect. No DOM output.
 */
export function SessionEvents() {
	const session = useSession();
	const router = useRouter();

	useEffect(() => {
		const close = openSessionEventStream({
			async onRolesChanged() {
				await session.refresh();
				router.refresh();
				toast.success("Your permissions were updated");
			},
			async onSessionRevoked(ev) {
				await clearSessionAction();
				const notice = REVOKED_NOTICE[ev.reason] ?? "unknown";
				router.push(`/login?notice=${notice}`);
			},
			async onSessionExpiring() {
				const result = await refreshSession();
				if (!result.ok) {
					await clearSessionAction();
					router.push("/login?notice=expired");
				}
			},
			async onConnectionLost() {
				await clearSessionAction();
				router.push("/login?notice=connection_lost");
			},
		});
		return close;
	}, [router, session]);

	return null;
}
```

- [ ] **Step 4: Add `clearSessionAction` to session-actions.ts**

Append to `apps/frontend/lib/session-actions.ts`:

```ts
import { clearSession } from "@/lib/session";

export async function clearSessionAction() {
	await clearSession();
}
```

- [ ] **Step 5: Type-check**

Run: `pnpm --filter @rustlens/frontend exec tsc --noEmit`
Expected: exit 0.

- [ ] **Step 6: Commit**

```bash
git add apps/frontend/components/session-provider.tsx apps/frontend/components/session-events.tsx apps/frontend/lib/session-actions.ts
git commit -m "feat(frontend): SessionProvider + SessionEvents hook"
```

---

## Phase 11 — Mount provider on the authenticated shell

### Task 19: Wrap app-shell with SessionProvider

**Files:**
- Modify: `apps/frontend/components/app-shell.tsx`

- [ ] **Step 1: Add `currentUser` prop and wrap children**

Open `apps/frontend/components/app-shell.tsx`. Replace the file with:

```tsx
"use client";

import { SidebarInset, SidebarProvider } from "@/components/ui/sidebar";
import { AppHeader } from "@/components/app-header";
import { AppSidebar } from "@/components/app-sidebar";
import type { AppShellVariant } from "@/components/app-shared";
import { SessionProvider } from "@/components/session-provider";
import { SessionEvents } from "@/components/session-events";
import type { CurrentUser } from "@/lib/rustlens-api";
import { usePathname } from "next/navigation";

type AppShellProps = {
	children: React.ReactNode;
	variant: AppShellVariant;
	user: {
		displayName: string;
		discordGlobalName?: string;
		discordUsername?: string;
		avatarUrl?: string;
		isInternalAdmin: boolean;
		permissions?: string[];
	};
	csrfToken: string;
	currentUser: CurrentUser | null;
};

export function AppShell({
	children,
	csrfToken,
	currentUser,
	variant,
	user,
}: AppShellProps) {
	const pathname = usePathname();

	return (
		<SessionProvider initial={currentUser}>
			<SessionEvents />
			<SidebarProvider>
				<AppSidebar
					pathname={pathname}
					permissions={variant === "admin" ? (user.permissions ?? []) : undefined}
					variant={variant}
				/>
				<SidebarInset className="p-4 md:p-6">
					<AppHeader csrfToken={csrfToken} pathname={pathname} user={user} variant={variant} />
					<div className="flex flex-1 flex-col gap-4 overflow-y-auto">
						{children}
					</div>
				</SidebarInset>
			</SidebarProvider>
		</SessionProvider>
	);
}
```

- [ ] **Step 2: Pass `currentUser` from every AppShell call site**

Find all callers:

Run: `pnpm --filter @rustlens/frontend exec rg "<AppShell" -l apps/frontend/app/`

For each call site, ensure the surrounding server component has already called `getCurrentUserSafe()` (most already do) and pass it through as `currentUser={currentUser}`. Example diff:

```tsx
// before
<AppShell csrfToken={csrfToken} user={user} variant="dashboard">

// after
<AppShell csrfToken={csrfToken} user={user} currentUser={currentUser} variant="dashboard">
```

If a caller doesn't already fetch `currentUser`, add:

```tsx
import { getCurrentUserSafe } from "@/lib/rustlens-api";
const currentUser = await getCurrentUserSafe();
```

The dashboard root (`apps/frontend/app/dashboard/page.tsx`) does **not** currently use `AppShell` — it uses its own `SidebarProvider` directly. Skip that file for now; the SessionProvider only needs to wrap pages that go through `<AppShell>`.

- [ ] **Step 3: Type-check + build**

Run: `pnpm --filter @rustlens/frontend exec tsc --noEmit && pnpm --filter @rustlens/frontend build`
Expected: exit 0.

- [ ] **Step 4: Commit**

```bash
git add apps/frontend/components/app-shell.tsx <list of modified callers>
git commit -m "feat(frontend): mount SessionProvider + SessionEvents on AppShell"
```

---

## Phase 12 — /login Sonner notice toast

### Task 20: Notice-toast client component

**Files:**
- Create: `apps/frontend/app/login/notice-toast.tsx`
- Modify: `apps/frontend/app/login/page.tsx`

- [ ] **Step 1: Write the notice-toast component**

Write `apps/frontend/app/login/notice-toast.tsx`:

```tsx
"use client";

import { useEffect, useRef } from "react";
import { toast } from "sonner";

const MESSAGES: Record<string, { variant: "error" | "info" | "warning"; text: string }> = {
	force_logout: { variant: "error", text: "You were signed out by an administrator." },
	suspended: { variant: "error", text: "Your account has been suspended." },
	deleted: { variant: "error", text: "Your account has been deleted." },
	expired: { variant: "info", text: "Your session expired, please sign in again." },
	connection_lost: { variant: "warning", text: "You were signed out. Please sign in again." },
};

/**
 * Reads ?notice= from the URL and fires a one-shot Sonner toast. Server-render
 * safe — only fires on first client mount, with a ref guard against React 19
 * strict-mode double-invocation.
 */
export function NoticeToast({ notice }: { notice: string | undefined }) {
	const fired = useRef(false);
	useEffect(() => {
		if (!notice || fired.current) return;
		const entry = MESSAGES[notice];
		if (!entry) return;
		fired.current = true;
		toast[entry.variant](entry.text);
	}, [notice]);
	return null;
}
```

- [ ] **Step 2: Render it inside LoginPage**

In `apps/frontend/app/login/page.tsx`, add the import:

```tsx
import { NoticeToast } from "./notice-toast";
```

Modify the returned JSX to include `<NoticeToast notice={notice} />` alongside `<AuthPage ... />`:

```tsx
return (
	<>
		<NoticeToast notice={notice} />
		<AuthPage error={error} notice={notice} csrfToken={csrfToken} />
	</>
);
```

Also update the early-return path that auto-redirects logged-in visitors so it still allows ?notice= values to render the toast — current logic already does this (it only redirects when both error and notice are absent), so no change needed there.

- [ ] **Step 3: Type-check + manual smoke**

Run: `pnpm --filter @rustlens/frontend exec tsc --noEmit`
Then: manual — open `http://localhost/login?notice=force_logout` and confirm the toast renders. (Requires `make dev`.)

- [ ] **Step 4: Commit**

```bash
git add apps/frontend/app/login/notice-toast.tsx apps/frontend/app/login/page.tsx
git commit -m "feat(frontend): Sonner notice toast on /login?notice="
```

---

## Phase 13 — Sliding-TTL client trigger

### Task 21: Periodic + focus-based refresh trigger

**Files:**
- Create: `apps/frontend/components/session-refresh-trigger.tsx`
- Modify: `apps/frontend/components/app-shell.tsx` (mount the trigger)

- [ ] **Step 1: Write the trigger**

Write `apps/frontend/components/session-refresh-trigger.tsx`:

```tsx
"use client";

import { useEffect, useRef } from "react";

import { refreshSession } from "@/lib/session-refresh";

const MIN_INTERVAL_MS = 60 * 1000; // throttle
const POLL_INTERVAL_MS = 60 * 60 * 1000; // 1 hour

/**
 * Triggers refreshSession() on mount, on window focus, on online events, and
 * every hour while the tab is alive. Throttled so a rapid focus loop doesn't
 * hammer the backend. Renders nothing.
 */
export function SessionRefreshTrigger() {
	const lastAt = useRef(0);

	useEffect(() => {
		const tryRefresh = () => {
			const now = Date.now();
			if (now - lastAt.current < MIN_INTERVAL_MS) return;
			lastAt.current = now;
			void refreshSession();
		};

		tryRefresh();
		const onFocus = () => tryRefresh();
		const onOnline = () => tryRefresh();
		window.addEventListener("focus", onFocus);
		window.addEventListener("online", onOnline);
		const id = window.setInterval(tryRefresh, POLL_INTERVAL_MS);

		return () => {
			window.removeEventListener("focus", onFocus);
			window.removeEventListener("online", onOnline);
			window.clearInterval(id);
		};
	}, []);

	return null;
}
```

- [ ] **Step 2: Mount inside AppShell**

In `apps/frontend/components/app-shell.tsx`, add the import:

```tsx
import { SessionRefreshTrigger } from "@/components/session-refresh-trigger";
```

And render alongside `<SessionEvents />`:

```tsx
<SessionProvider initial={currentUser}>
	<SessionEvents />
	<SessionRefreshTrigger />
	...
```

- [ ] **Step 3: Type-check**

Run: `pnpm --filter @rustlens/frontend exec tsc --noEmit`
Expected: exit 0.

- [ ] **Step 4: Commit**

```bash
git add apps/frontend/components/session-refresh-trigger.tsx apps/frontend/components/app-shell.tsx
git commit -m "feat(frontend): sliding-TTL refresh trigger on focus + hourly poll"
```

---

## Phase 14 — End-to-end smoke + docs

### Task 22: Manual smoke test through Traefik

This is a manual checklist, not code. Run each step against a local `make dev` stack:

- [ ] **Step 1: Verify SSE handshake**

```bash
make dev   # in one terminal
```

Sign in via Discord OAuth. Open browser DevTools → Network → filter "stream". Confirm `/api/events/stream` is open, status 200, content-type `text/event-stream`, and a `ping` line arrives every ~25 seconds.

- [ ] **Step 2: Verify roles.changed propagation**

While signed in as user A in one window, sign in as a superadmin user B in an Incognito window. From user B's `/admin/users/<A>` page, grant a role to A.

Expected: within ~1 second user A's tab shows the Sonner success toast "Your permissions were updated", and admin-only nav items appear (if the new role grants admin perms).

- [ ] **Step 3: Verify force_logout**

From user B's admin page, run `users.force_logout` against user A (via whatever UI you wire up — or directly via curl):

```bash
# Replace tokens with real values from your session cookie + app secret
curl -X POST http://localhost/api/app/admin/actions/users.force_logout \
	-H 'X-RustLens-App-Secret: <APP_API_INTERNAL_SECRET>' \
	-H 'X-RustLens-Session: <admin session token>' \
	-H 'Content-Type: application/json' \
	-d '{"params":{"userId":"<user A id>"},"reason":"smoke"}'
```

Expected: user A's tab redirects to `/login?notice=force_logout` and shows the "You were signed out by an administrator." Sonner toast.

- [ ] **Step 4: Verify sliding TTL no-op**

In DevTools → Application → Cookies, inspect `rustlens_session`. With a fresh login (14d remaining), focus the tab and confirm no `/api/auth/refresh` server-action POST fires (or fires but the cookie's `Expires` stays roughly the same).

- [ ] **Step 5: Verify sliding TTL refresh path**

Manually decode the session cookie (base64url payload) and confirm `expiresAt` is set to `Date.now() + 14d`. To force the refresh path, edit the cookie's value in DevTools to one with an `expiresAt` < 7 days from now (or wait 7 days — not recommended), reload, and confirm `expiresAt` is re-stamped to ~14 days out.

- [ ] **Step 6: Commit anything you discovered**

If you found a bug during smoke testing, fix it inline and commit. Do **not** mark this task complete until all five smoke checks pass.

```bash
git commit -m "fix(events): <whatever you fixed>"   # only if needed
```

### Task 23: Update AGENTS.md

**Files:**
- Modify: `AGENTS.md` (root)

- [ ] **Step 1: Add bullet points for the new surface**

Open `AGENTS.md` and:

1. In the Frontend section's "Key patterns" list, add a bullet after the existing auth-related one:

> - **Real-time session events**: `<SessionProvider>` + `<SessionEvents>` mounted inside `AppShell` open a persistent `EventSource` to `/api/events/stream`. The backend pushes `roles.changed`, `session.revoked`, `session.expiring`, and 25-second `ping` events. Role/permission changes refresh `/api/app/me` and `router.refresh()` without a re-login; admin force-logout / suspension / deletion redirects to `/login?notice=...` and shows a Sonner toast. `<SessionRefreshTrigger>` re-mints the cookie when remaining TTL drops below 7 days.

2. In the Backend "Key patterns" list, add a bullet for the new surface:

> - **`/api/events/stream`** is cookie-authenticated (not `/api/app/*`-secret) and serves SSE. Backed by `internal/events/Hub` (in-process pub/sub) + `internal/events/RedisBridge` (multi-pod fanout via the `rustlens:user-events` channel). Admin actions in `internal/admin/actions/` (`users.roles.grant`, `users.roles.revoke`, `roles.permissions.set`, `users.suspend`, `users.delete`, `users.force_logout`) publish events through this Hub.

3. In the Backend file-tree, add `internal/events/` between `internal/cleanup/` and `internal/config/`:

```
internal/events/         # in-process pub/sub Hub + Redis bridge for SSE
```

- [ ] **Step 2: Commit**

```bash
git add AGENTS.md
git commit -m "docs: document /api/events/stream + SessionProvider session updates"
```

---

## Self-review checklist

Before declaring done, run the full backend and frontend test suites:

- [ ] `cd apps/backend && go test ./...` — all green
- [ ] `pnpm --filter @rustlens/frontend exec tsc --noEmit` — all green
- [ ] `pnpm --filter @rustlens/frontend build` — exit 0
- [ ] `go vet ./...` from `apps/backend` — exit 0
- [ ] Manual smoke (Task 22) — all five sub-steps confirmed

If any check fails, fix inline and re-commit.

## Spec coverage check

| Spec section | Implemented in |
|---|---|
| Goals: Sonner toasts for session expired / forced logout / role updated | Tasks 18, 20 |
| Goals: Instant role updates within ~1s, no re-login | Tasks 7–12, 17, 18 |
| Goals: Sliding TTL | Tasks 16, 21 |
| Goals: Epoch wiring audit | Tasks 10–13 |
| Architecture: `internal/events/` package | Tasks 1–6 |
| Architecture: `/api/events/stream` cookie-authenticated | Tasks 7–9 |
| Architecture: `SessionProvider` reads from `/api/app/me` not cookie | Tasks 18, 19 |
| Architecture: Sliding TTL via server action | Tasks 16, 21 |
| Architecture: Global 401 surfacing | Task 15 |
| Event catalog: `roles.changed` / `session.revoked` / `session.expiring` / `ping` | Tasks 1, 8 |
| Emission: grant/revoke/perms-set | Task 12 |
| Emission: suspend/delete | Task 12 |
| Emission: new `users.force_logout` | Task 13 |
| Emission: heartbeat + per-conn `session.expiring` | Task 8 |
| Backend rejects suspended/deleted sessions | Tasks 10, 11 |
| Per-user 5-conn cap | Tasks 2, 3, 8 |
| Redis multi-pod fanout | Tasks 4, 5 |
| Frontend reaction: refresh /me + router.refresh + toast | Task 18 |
| Frontend reaction: redirect on revoked with reason → notice | Tasks 18, 20 |
| Frontend reaction: silent refresh on session.expiring | Task 18 |
| File layout summary | All tasks |

No unmet spec requirements.
