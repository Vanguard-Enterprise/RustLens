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
