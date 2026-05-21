// Package fraud detects suspicious slash-command usage in small guilds.
//
// MVP: a guild whose approximate member count is below MaxMemberCount and
// whose rolling Window count of a tracked command exceeds Threshold trips
// an alert. AlertCooldown suppresses duplicate alerts for the same
// (guild, command) tuple.
package fraud

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
	Window         time.Duration
	Threshold      int
	MaxMemberCount int
	AlertCooldown  time.Duration
}

type Alert struct {
	Triggered   bool
	GuildID     string
	Command     string
	Count       int
	MemberCount int
	WindowSecs  int
}

type Detector struct {
	rdb *redis.Client
}

func New(rdb *redis.Client) *Detector {
	return &Detector{rdb: rdb}
}

// Record increments the counter for (command, guild) and returns an
// Alert{Triggered:true,...} the first time the threshold is crossed
// within AlertCooldown. guildID="" or memberCount<=0 disables detection
// (used for DMs / user-app installs).
func (d *Detector) Record(ctx context.Context, guildID, command string, memberCount int, cfg Config) (Alert, error) {
	if cfg.Window <= 0 || cfg.Threshold <= 0 || cfg.MaxMemberCount <= 0 || cfg.AlertCooldown <= 0 {
		return Alert{}, errors.New("fraud: invalid config")
	}
	if guildID == "" || memberCount <= 0 || memberCount >= cfg.MaxMemberCount {
		return Alert{}, nil
	}

	signalCounter.Add(ctx, 1, metric.WithAttributes(attribute.String("signal_type", command)))

	counterKey := fmt.Sprintf("fraud:%s:%s:count", command, guildID)
	alertedKey := fmt.Sprintf("fraud:%s:%s:alerted", command, guildID)

	n, err := d.rdb.Incr(ctx, counterKey).Result()
	if err != nil {
		return Alert{}, fmt.Errorf("fraud: incr counter: %w", err)
	}
	if n == 1 {
		if err := d.rdb.PExpire(ctx, counterKey, cfg.Window).Err(); err != nil {
			return Alert{}, fmt.Errorf("fraud: expire counter: %w", err)
		}
	}
	if int(n) < cfg.Threshold {
		return Alert{}, nil
	}

	ok, err := d.rdb.SetNX(ctx, alertedKey, "1", cfg.AlertCooldown).Result()
	if err != nil {
		return Alert{}, fmt.Errorf("fraud: setnx alerted: %w", err)
	}
	if !ok {
		return Alert{}, nil
	}

	return Alert{
		Triggered:   true,
		GuildID:     guildID,
		Command:     command,
		Count:       int(n),
		MemberCount: memberCount,
		WindowSecs:  int(cfg.Window / time.Second),
	}, nil
}
