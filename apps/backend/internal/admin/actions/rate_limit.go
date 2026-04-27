package actions

import (
	"sync"
	"time"
)

// RateLimitOverride is an optional interface an AdminAction may implement to
// customise its rate-limit capacity and refill rate. When absent, the
// dispatcher falls back to the token-generating vs mutating defaults.
type RateLimitOverride interface {
	RateLimit() (capacity int, refillPerSec float64)
}

// Default bucket parameters.
const (
	defaultMutatingCapacity  = 30
	defaultMutatingRefill    = 0.5
	defaultTokenGenCapacity  = 10
	defaultTokenGenRefill    = 0.1
	bucketIdleEvictionWindow = 10 * time.Minute
)

// tokenGeneratingActions is the hardcoded set of action types that issue
// one-time tokens. These are rate-limited more aggressively than ordinary
// mutating actions. The Discord-only migration removed every entry; the map
// remains so future token-generating actions can opt in without plumbing
// changes.
var tokenGeneratingActions = map[string]struct{}{}

type tokenBucket struct {
	capacity     float64
	refillPerSec float64
	tokens       float64
	lastRefill   time.Time
	lastAccess   time.Time
}

// take attempts to consume a token; returns true if granted, false otherwise.
// Also returns the time the caller should wait before retrying if denied.
func (b *tokenBucket) take(now time.Time) (bool, time.Duration) {
	elapsed := now.Sub(b.lastRefill).Seconds()
	if elapsed > 0 {
		b.tokens += elapsed * b.refillPerSec
		if b.tokens > b.capacity {
			b.tokens = b.capacity
		}
		b.lastRefill = now
	}
	b.lastAccess = now
	if b.tokens >= 1 {
		b.tokens -= 1
		return true, 0
	}
	// Seconds until we have 1 token.
	need := 1 - b.tokens
	if b.refillPerSec <= 0 {
		return false, time.Minute
	}
	secs := need / b.refillPerSec
	return false, time.Duration(secs * float64(time.Second))
}

type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*tokenBucket
	now     func() time.Time
}

func newRateLimiter() *rateLimiter {
	return &rateLimiter{
		buckets: map[string]*tokenBucket{},
		now:     time.Now,
	}
}

// Allow consumes a token for the (actor, action) key. Returns (allowed, retryAfter).
func (r *rateLimiter) Allow(actorID, actionType string, capacity int, refillPerSec float64) (bool, time.Duration) {
	key := actorID + ":" + actionType
	r.mu.Lock()
	defer r.mu.Unlock()

	now := r.now()
	// Lazy eviction of idle buckets.
	for k, b := range r.buckets {
		if now.Sub(b.lastAccess) > bucketIdleEvictionWindow {
			delete(r.buckets, k)
		}
	}

	b, ok := r.buckets[key]
	if !ok {
		b = &tokenBucket{
			capacity:     float64(capacity),
			refillPerSec: refillPerSec,
			tokens:       float64(capacity),
			lastRefill:   now,
			lastAccess:   now,
		}
		r.buckets[key] = b
	}
	// Update params in case of override changes across restarts — keep same bucket.
	if b.capacity != float64(capacity) {
		b.capacity = float64(capacity)
	}
	if b.refillPerSec != refillPerSec {
		b.refillPerSec = refillPerSec
	}
	return b.take(now)
}

// bucketParams returns the effective (capacity, refill) for the given action.
func bucketParams(a AdminAction) (int, float64) {
	if ov, ok := a.(RateLimitOverride); ok {
		return ov.RateLimit()
	}
	if _, tg := tokenGeneratingActions[a.Type()]; tg {
		return defaultTokenGenCapacity, defaultTokenGenRefill
	}
	return defaultMutatingCapacity, defaultMutatingRefill
}

// --- idempotency cache ---

const (
	idempotencyTTL        = 24 * time.Hour
	idempotencyMaxEntries = 10_000
)

type cachedResponse struct {
	status    int
	body      []byte
	expiresAt time.Time
}

type idempotencyCache struct {
	mu      sync.Mutex
	entries map[string]cachedResponse
	now     func() time.Time
}

func newIdempotencyCache() *idempotencyCache {
	return &idempotencyCache{
		entries: map[string]cachedResponse{},
		now:     time.Now,
	}
}

func (c *idempotencyCache) key(actorID, actionType, idempotencyKey string) string {
	return actorID + ":" + actionType + ":" + idempotencyKey
}

func (c *idempotencyCache) get(k string) (cachedResponse, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	e, ok := c.entries[k]
	if !ok {
		return cachedResponse{}, false
	}
	if now.After(e.expiresAt) {
		delete(c.entries, k)
		return cachedResponse{}, false
	}
	return e, true
}

func (c *idempotencyCache) put(k string, status int, body []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	// Lazy eviction of expired entries.
	for key, e := range c.entries {
		if now.After(e.expiresAt) {
			delete(c.entries, key)
		}
	}
	// Bound total entries.
	if len(c.entries) >= idempotencyMaxEntries {
		// Evict an arbitrary entry.
		for key := range c.entries {
			delete(c.entries, key)
			break
		}
	}
	c.entries[k] = cachedResponse{
		status:    status,
		body:      body,
		expiresAt: now.Add(idempotencyTTL),
	}
}
