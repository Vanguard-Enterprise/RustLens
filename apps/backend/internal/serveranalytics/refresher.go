package serveranalytics

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"rustlens/backend/internal/battlemetrics"
	"rustlens/backend/internal/store"
)

const (
	dailyRefreshHourUTC   = 0
	dailyRefreshMinuteUTC = 1
	serverRefreshGap      = 30 * time.Second
)

type RedisLocker interface {
	TryAcquireServerAnalyticsRefreshLock(ctx context.Context, owner string, ttl time.Duration) (bool, error)
	ReleaseServerAnalyticsRefreshLock(ctx context.Context, owner string) error
}

type Refresher struct {
	client   *battlemetrics.Client
	postgres *store.PostgresStore
	redis    RedisLocker
	lockID   string
}

func NewRefresher(client *battlemetrics.Client, postgres *store.PostgresStore, redis RedisLocker) *Refresher {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "unknown-host"
	}
	return &Refresher{
		client:   client,
		postgres: postgres,
		redis:    redis,
		lockID:   fmt.Sprintf("%s:%d", host, os.Getpid()),
	}
}

func (r *Refresher) Start(ctx context.Context) {
	go r.loop(ctx)
}

func (r *Refresher) loop(ctx context.Context) {
	for {
		next := nextDailyRefresh(time.Now().UTC())
		timer := time.NewTimer(time.Until(next))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			r.run(ctx)
		}
	}
}

func (r *Refresher) run(ctx context.Context) {
	lockCtx, lockCancel := context.WithTimeout(ctx, 2*time.Second)
	acquired, err := r.redis.TryAcquireServerAnalyticsRefreshLock(lockCtx, r.lockID, 23*time.Hour)
	lockCancel()
	if err != nil {
		log.Printf("[server-analytics] failed to acquire refresh lock: %v", err)
		return
	}
	if !acquired {
		return
	}
	defer func() {
		releaseCtx, releaseCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer releaseCancel()
		if err := r.redis.ReleaseServerAnalyticsRefreshLock(releaseCtx, r.lockID); err != nil {
			log.Printf("[server-analytics] failed to release refresh lock: %v", err)
		}
	}()

	listCtx, listCancel := context.WithTimeout(ctx, 10*time.Second)
	servers, err := r.postgres.ListTrackedServers(listCtx)
	listCancel()
	if err != nil {
		log.Printf("[server-analytics] failed to list tracked servers: %v", err)
		return
	}
	if len(servers) == 0 {
		return
	}

	now := time.Now().UTC()
	start, stop := CompletedDayRange(now, CacheDays)
	log.Printf("[server-analytics] refreshing %d tracked servers for %s to %s", len(servers), start.Format(time.RFC3339), stop.Format(time.RFC3339))
	succeeded := 0
	failed := 0
	for i, server := range servers {
		if r.refreshServer(ctx, server, start, stop) {
			succeeded++
		} else {
			failed++
		}
		if i == len(servers)-1 {
			break
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(serverRefreshGap):
		}
	}
	log.Printf("[server-analytics] refresh complete: %d succeeded, %d failed, %d total", succeeded, failed, len(servers))
}

func (r *Refresher) refreshServer(ctx context.Context, server store.TrackedServer, start time.Time, stop time.Time) bool {
	points, err := r.client.FetchServerPlayerCountHistory(ctx, server.BmID, start, stop)
	if err != nil {
		log.Printf("[server-analytics] refresh failed for server %s (%s): %v", server.BmID, server.Name, err)
		return false
	}

	converted := make([]store.ServerPlayerCountPoint, 0, len(points))
	for _, point := range points {
		converted = append(converted, store.ServerPlayerCountPoint{
			TimestampUnix: point.Timestamp.UTC().Unix(),
			Min:           point.Min,
			Value:         point.Value,
			Max:           point.Max,
		})
	}

	upsertCtx, upsertCancel := context.WithTimeout(ctx, 10*time.Second)
	defer upsertCancel()
	if err := r.postgres.UpsertServerPlayerCountHistory(upsertCtx, server.BmID, converted); err != nil {
		log.Printf("[server-analytics] cache write failed for server %s (%s): %v", server.BmID, server.Name, err)
		return false
	}
	log.Printf("[server-analytics] refreshed server %s (%s): %d points", server.BmID, server.Name, len(converted))
	return true
}

func nextDailyRefresh(now time.Time) time.Time {
	now = now.UTC()
	next := time.Date(now.Year(), now.Month(), now.Day(), dailyRefreshHourUTC, dailyRefreshMinuteUTC, 0, 0, time.UTC)
	if !next.After(now) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}
