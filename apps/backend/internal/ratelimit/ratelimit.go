// Package ratelimit implements a Redis-backed escalating-cooldown rate
// limiter for bot slash commands. Each (user, command) key has a normal
// quota window; once exceeded, the user is placed in a cooldown whose
// duration doubles per repeat violation, capped at MaxCooldown. The
// violation counter decays after ViolationDecay of no further overflows.
package ratelimit

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/redis/go-redis/v9"
)

type Config struct {
	Quota          int
	Window         time.Duration
	BaseCooldown   time.Duration
	MaxCooldown    time.Duration
	ViolationDecay time.Duration
}

type Result struct {
	Allowed           bool
	RetryAfterSeconds int
	ViolationLevel    int
}

type Limiter struct {
	rdb *redis.Client
}

func New(rdb *redis.Client) *Limiter {
	return &Limiter{rdb: rdb}
}

// Consume attempts to consume one quota slot for the given key. Returns
// Allowed=true when the call should proceed, or Allowed=false with a
// RetryAfterSeconds hint when the user is in cooldown.
func (l *Limiter) Consume(ctx context.Context, key string, cfg Config) (Result, error) {
	if cfg.Quota <= 0 || cfg.Window <= 0 || cfg.BaseCooldown <= 0 || cfg.MaxCooldown <= 0 {
		return Result{}, errors.New("ratelimit: invalid config")
	}

	cooldownKey := "rl:cd:" + key
	quotaKey := "rl:q:" + key
	levelKey := "rl:lvl:" + key

	ttl, err := l.rdb.PTTL(ctx, cooldownKey).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return Result{}, fmt.Errorf("ratelimit: pttl: %w", err)
	}
	if ttl > 0 {
		rejectionCounter.Add(ctx, 1, metric.WithAttributes(attribute.String("bucket", key)))
		return Result{Allowed: false, RetryAfterSeconds: secondsCeil(ttl)}, nil
	}

	n, err := l.rdb.Incr(ctx, quotaKey).Result()
	if err != nil {
		return Result{}, fmt.Errorf("ratelimit: incr quota: %w", err)
	}
	if n == 1 {
		if err := l.rdb.PExpire(ctx, quotaKey, cfg.Window).Err(); err != nil {
			return Result{}, fmt.Errorf("ratelimit: expire quota: %w", err)
		}
	}
	if int(n) <= cfg.Quota {
		return Result{Allowed: true}, nil
	}

	level, err := l.rdb.Incr(ctx, levelKey).Result()
	if err != nil {
		return Result{}, fmt.Errorf("ratelimit: incr level: %w", err)
	}
	if err := l.rdb.PExpire(ctx, levelKey, cfg.ViolationDecay).Err(); err != nil {
		return Result{}, fmt.Errorf("ratelimit: expire level: %w", err)
	}

	cooldown := scaleCooldown(cfg.BaseCooldown, cfg.MaxCooldown, int(level))
	if err := l.rdb.Set(ctx, cooldownKey, "1", cooldown).Err(); err != nil {
		return Result{}, fmt.Errorf("ratelimit: set cooldown: %w", err)
	}
	// Reset the quota counter so the user gets a fresh window after cooldown.
	if err := l.rdb.Del(ctx, quotaKey).Err(); err != nil {
		return Result{}, fmt.Errorf("ratelimit: del quota: %w", err)
	}
	rejectionCounter.Add(ctx, 1, metric.WithAttributes(attribute.String("bucket", key)))
	return Result{
		Allowed:           false,
		RetryAfterSeconds: secondsCeil(cooldown),
		ViolationLevel:    int(level),
	}, nil
}

func scaleCooldown(base, max time.Duration, level int) time.Duration {
	if level < 1 {
		level = 1
	}
	if level > 30 {
		return max
	}
	d := base << uint(level-1)
	if d <= 0 || d > max {
		return max
	}
	return d
}

func secondsCeil(d time.Duration) int {
	s := int((d + time.Second - 1) / time.Second)
	if s < 1 {
		return 1
	}
	return s
}
