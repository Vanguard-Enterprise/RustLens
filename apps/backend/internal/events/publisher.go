package events

import "sync"

// Publisher is the minimal interface action handlers depend on. Production
// wiring uses *RedisBridge; tests substitute a fake.
type Publisher interface {
	PublishWithFanout(userID string, e Event)
}

// NopPublisher is a Publisher that drops every event. Use it in tests that
// don't care about event emission.
type NopPublisher struct{}

func (NopPublisher) PublishWithFanout(string, Event) {}

// PublishRecord captures one PublishWithFanout invocation.
type PublishRecord struct {
	UserID string
	Event  Event
}

// RecordingPublisher captures every PublishWithFanout call for assertion in
// tests. Safe for concurrent use.
type RecordingPublisher struct {
	mu      sync.Mutex
	Records []PublishRecord
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

// Compile-time checks: both concrete types satisfy Publisher, and *RedisBridge
// satisfies it via its existing PublishWithFanout method.
var (
	_ Publisher = NopPublisher{}
	_ Publisher = (*RecordingPublisher)(nil)
	_ Publisher = (*RedisBridge)(nil)
)
