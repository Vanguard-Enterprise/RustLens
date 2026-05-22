package events

import (
	"context"
	"errors"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
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
// events until either unsub is called or ctx is done. Receivers should detect
// teardown via their own ctx; the Hub stops delivering once ctx is done or
// unsub is called, but does not close the channel.
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

	// Tie subscription lifetime to ctx. Skip when ctx is never-cancel
	// (Done() == nil) — receiving from a nil channel would leak the goroutine.
	if done := ctx.Done(); done != nil {
		go func() {
			<-done
			unsub()
		}()
	}

	return ch, unsub, nil
}

// Publish delivers e to all subscribers of userID. Never blocks; if a
// subscriber's buffer is full the oldest event is dropped.
func (h *Hub) Publish(userID string, e Event) {
	publishedCounter.Add(context.Background(), 1, metric.WithAttributes(attribute.String("channel", e.Type)))

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
			break
		}
	}
	if len(h.subs[userID]) == 0 {
		delete(h.subs, userID)
	}
}
