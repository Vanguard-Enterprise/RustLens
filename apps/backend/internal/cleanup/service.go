// Package cleanup runs periodic background sweeps that drop stale rows from
// volatile tables (cache, long-passed wipe reacts, command usage history).
//
// Retention windows are intentionally conservative: we err on the side of
// keeping recent history available to commands like /compare and
// /wipereact missing. Long-lived product data (user accounts, links,
// audit events, tracked servers, schedules) is never touched here.
package cleanup

import (
	"context"
	"log"
	"time"
)

// Store is the subset of *store.PostgresStore the cleanup service needs.
// Defined as an interface so tests can inject a fake without spinning up
// Postgres.
type Store interface {
	PurgeWipeReactsBefore(ctx context.Context, cutoff time.Time) (int64, error)
	PurgeTrackAnalyticsSessionsCacheBefore(ctx context.Context, cutoff time.Time) (int64, error)
	PurgeCommandUsageBefore(ctx context.Context, cutoff time.Time) (int64, error)
}

// Config holds tuneable cleanup parameters. The defaults applied by
// NewService are conservative: hourly cadence, 7-day wipe-react retention,
// 48-hour analytics cache retention, 90-day command usage retention.
type Config struct {
	Interval                time.Duration
	WipeReactRetention      time.Duration
	AnalyticsCacheRetention time.Duration
	CommandUsageRetention   time.Duration
}

func (c Config) withDefaults() Config {
	if c.Interval <= 0 {
		c.Interval = time.Hour
	}
	if c.WipeReactRetention <= 0 {
		c.WipeReactRetention = 7 * 24 * time.Hour
	}
	if c.AnalyticsCacheRetention <= 0 {
		c.AnalyticsCacheRetention = 48 * time.Hour
	}
	if c.CommandUsageRetention <= 0 {
		c.CommandUsageRetention = 90 * 24 * time.Hour
	}
	return c
}

type Service struct {
	store Store
	cfg   Config
}

func NewService(store Store, cfg Config) *Service {
	return &Service{store: store, cfg: cfg.withDefaults()}
}

// Start launches the cleanup goroutine. It returns immediately; the goroutine
// runs an initial sweep, then sweeps on the configured interval until ctx is
// cancelled.
func (s *Service) Start(ctx context.Context) {
	go s.Run(ctx)
}

// Run is exported for tests that want to drive a single tick synchronously
// via runOnce. In production, prefer Start.
func (s *Service) Run(ctx context.Context) {
	ticker := time.NewTicker(s.cfg.Interval)
	defer ticker.Stop()

	s.runOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runOnce(ctx)
		}
	}
}

// runOnce performs a single sweep. Failures are logged; one failing step does
// not abort the others, and they never propagate up to crash the server.
func (s *Service) runOnce(ctx context.Context) {
	now := time.Now()

	if rows, err := s.store.PurgeWipeReactsBefore(ctx, now.Add(-s.cfg.WipeReactRetention)); err != nil {
		log.Printf("[backend] cleanup: purge wipe reacts failed: %v", err)
	} else if rows > 0 {
		log.Printf("[backend] cleanup: purged %d wipe react rows older than %s", rows, s.cfg.WipeReactRetention)
	}

	if rows, err := s.store.PurgeTrackAnalyticsSessionsCacheBefore(ctx, now.Add(-s.cfg.AnalyticsCacheRetention)); err != nil {
		log.Printf("[backend] cleanup: purge analytics cache failed: %v", err)
	} else if rows > 0 {
		log.Printf("[backend] cleanup: purged %d analytics cache rows older than %s", rows, s.cfg.AnalyticsCacheRetention)
	}

	if rows, err := s.store.PurgeCommandUsageBefore(ctx, now.Add(-s.cfg.CommandUsageRetention)); err != nil {
		log.Printf("[backend] cleanup: purge command usage failed: %v", err)
	} else if rows > 0 {
		log.Printf("[backend] cleanup: purged %d command usage rows older than %s", rows, s.cfg.CommandUsageRetention)
	}
}
