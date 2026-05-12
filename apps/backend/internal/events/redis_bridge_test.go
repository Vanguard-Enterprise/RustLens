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

func TestRedisBridge_LocalPublishIsNotDuplicatedByRedisEcho(t *testing.T) {
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

	bridge.PublishWithFanout("user-1", events.Event{Type: events.TypeRolesChanged, Reason: events.ReasonGrant})

	// First receive: the synchronous local fanout.
	select {
	case got := <-ch:
		if got.Type != events.TypeRolesChanged {
			t.Fatalf("unexpected event: %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for local event")
	}

	// Second receive must NOT happen — the Redis echo of our own publish
	// should be filtered out by the bridge's instance-ID check.
	select {
	case got := <-ch:
		t.Fatalf("got duplicate event from redis echo: %+v", got)
	case <-time.After(200 * time.Millisecond):
		// expected: no second delivery.
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
