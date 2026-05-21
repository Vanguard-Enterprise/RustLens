package events

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

// redisPublishTimeout caps a single PublishWithFanout's Redis call so a stalled
// connection cannot block the caller for the underlying TCP timeout.
const redisPublishTimeout = 5 * time.Second

// RedisChannel is the pub/sub channel name used to fan events across backend
// pods. Both publishers and subscribers in this binary use it; remote pods
// running the same code can publish JSON-encoded redisEnvelope payloads to
// reach this pod's local Hub subscribers.
const RedisChannel = "rustlens:user-events"

type redisEnvelope struct {
	UserID     string `json:"userId"`
	Event      Event  `json:"event"`
	InstanceID string `json:"instanceId,omitempty"`
}

// RedisBridge mirrors local hub publishes to Redis and reads remote publishes
// back into the local hub. Construct with NewRedisBridge, start with Start.
// PublishWithFanout is the only entry point external code should use when it
// wants an event delivered to all pods.
type RedisBridge struct {
	rdb        *redis.Client
	hub        *Hub
	instanceID string
}

// NewRedisBridge constructs a bridge but does not start any goroutines. A
// random per-instance ID is generated so the bridge can recognise and drop
// its own messages echoed back from Redis (avoiding duplicate local delivery).
func NewRedisBridge(rdb *redis.Client, hub *Hub) *RedisBridge {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		// Fall back to a time-based id; collisions across pods are vanishingly
		// unlikely and the worst case is a brief window of duplicate delivery.
		buf = []byte(time.Now().UTC().Format(time.RFC3339Nano))
	}
	return &RedisBridge{rdb: rdb, hub: hub, instanceID: hex.EncodeToString(buf)}
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
					slog.Error("events bridge: decode payload", slog.Any("error", err))
					continue
				}
				// Drop our own echoes: PublishWithFanout already delivered
				// these to the local hub before publishing to Redis.
				if env.InstanceID == b.instanceID {
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

	payload, err := json.Marshal(redisEnvelope{UserID: userID, Event: e, InstanceID: b.instanceID})
	if err != nil {
		slog.Error("events bridge: encode payload", slog.Any("error", err))
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), redisPublishTimeout)
	defer cancel()
	if err := b.rdb.Publish(ctx, RedisChannel, payload).Err(); err != nil {
		slog.Error("events bridge: redis publish", slog.Any("error", err))
	}
}
