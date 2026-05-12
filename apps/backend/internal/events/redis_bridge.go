package events

import (
	"context"
	"encoding/json"
	"log"
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
	ctx, cancel := context.WithTimeout(context.Background(), redisPublishTimeout)
	defer cancel()
	if err := b.rdb.Publish(ctx, RedisChannel, payload).Err(); err != nil {
		log.Printf("[events] bridge: redis publish: %v", err)
	}
}
