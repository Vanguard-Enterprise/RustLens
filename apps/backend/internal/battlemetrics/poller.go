package battlemetrics

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"rustlens/backend/internal/observability"
	"rustlens/backend/internal/store"
)

type Poller struct {
	client   *Client
	postgres *store.PostgresStore
	redis    *store.RedisStore
	interval time.Duration
	lockID   string
}

const (
	highFrequencyPollBatchSize = 2
	lowFrequencyPollBatchSize  = 1
	totalPollBatchSize         = highFrequencyPollBatchSize + lowFrequencyPollBatchSize
	battleMetricsPollTimeout   = 15 * time.Second
	emptyPollsBeforeOffline    = 3
)

func NewPoller(client *Client, postgres *store.PostgresStore, redis *store.RedisStore, interval time.Duration) *Poller {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "unknown-host"
	}

	return &Poller{
		client:   client,
		postgres: postgres,
		redis:    redis,
		interval: interval,
		lockID:   fmt.Sprintf("%s:%d", host, os.Getpid()),
	}
}

func (p *Poller) Start(ctx context.Context) {
	if p.interval <= 0 {
		return
	}

	go p.loop(ctx)
}

func (p *Poller) loop(ctx context.Context) {
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	p.runCycle(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.runCycle(ctx)
		}
	}
}

func (p *Poller) runCycle(ctx context.Context) {
	cadenceTTL := p.interval
	if cadenceTTL <= 0 {
		return
	}

	cadenceCtx, cadenceCancel := context.WithTimeout(ctx, 2*time.Second)
	reserved, err := p.redis.TryReserveBattleMetricsPollerCycle(cadenceCtx, p.lockID, cadenceTTL)
	cadenceCancel()
	if err != nil {
		observability.Logger(ctx).ErrorContext(ctx, "failed to reserve poller cycle",
			slog.Any("error", err))
		return
	}
	if !reserved {
		return
	}

	lockTTL := 45 * time.Second
	if p.interval > lockTTL {
		lockTTL = p.interval + 30*time.Second
	}

	lockCtx, lockCancel := context.WithTimeout(ctx, 2*time.Second)
	acquired, err := p.redis.TryAcquireBattleMetricsPollerLock(lockCtx, p.lockID, lockTTL)
	lockCancel()
	if err != nil {
		observability.Logger(ctx).ErrorContext(ctx, "failed to acquire poller lock",
			slog.Any("error", err))
		return
	}
	if !acquired {
		return
	}
	defer func() {
		releaseCtx, releaseCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer releaseCancel()
		if err := p.redis.ReleaseBattleMetricsPollerLock(releaseCtx, p.lockID); err != nil {
			observability.Logger(releaseCtx).ErrorContext(releaseCtx, "failed to release poller lock",
				slog.Any("error", err))
		}
	}()

	servers, err := p.trackedServersToPoll(ctx)
	if err != nil {
		observability.Logger(ctx).ErrorContext(ctx, "failed to select next tracked server",
			slog.Any("error", err))
		return
	}
	if len(servers) == 0 {
		return
	}

	highFrequencyCount, lowFrequencyCount := countPollFrequencies(servers)
	observability.Logger(ctx).InfoContext(ctx, "selected battlemetrics poll batch",
		slog.Int("server_count", len(servers)),
		slog.Int("high_frequency_count", highFrequencyCount),
		slog.Int("low_frequency_count", lowFrequencyCount))

	var wg sync.WaitGroup
	wg.Add(len(servers))
	for _, server := range servers {
		server := server
		go func() {
			defer wg.Done()
			p.pollServer(ctx, server)
		}()
	}
	wg.Wait()
}

func (p *Poller) pollServer(ctx context.Context, server store.TrackedServer) {
	pollStart := time.Now()
	observedAt := pollStart.UTC()

	requestCtx, requestCancel := context.WithTimeout(ctx, battleMetricsPollTimeout)
	players, pollErr := p.client.FetchServerPlayers(requestCtx, server.BmID)
	requestCancel()

	if pollErr == nil {
		inputs := make([]store.PlayerPresenceInput, 0, len(players))
		for _, player := range players {
			inputs = append(inputs, store.PlayerPresenceInput{
				PlayerBmID: player.BmID,
				BmName:     player.Name,
			})
		}

		shouldSync := true
		if len(inputs) == 0 {
			stateCtx, stateCancel := context.WithTimeout(ctx, 5*time.Second)
			onlineCount, countErr := p.postgres.CountOnlinePlayerStates(stateCtx, server.BmID)
			stateCancel()
			if countErr != nil {
				pollErr = fmt.Errorf("count online player states: %w", countErr)
			} else if onlineCount > 0 {
				stateCtx, stateCancel = context.WithTimeout(ctx, 5*time.Second)
				emptyPolls, incrementErr := p.postgres.IncrementTrackedServerEmptyPolls(stateCtx, server.BmID)
				stateCancel()
				if incrementErr != nil {
					pollErr = fmt.Errorf("record empty player poll: %w", incrementErr)
				} else if emptyPolls < emptyPollsBeforeOffline {
					shouldSync = false
					observability.Logger(ctx).WarnContext(ctx, "delaying empty poll for server",
						slog.String("bm_id", server.BmID),
						slog.String("server_name", server.Name),
						slog.Int("empty_polls", emptyPolls),
						slog.Int("threshold", emptyPollsBeforeOffline))
				}
			}
		}

		if pollErr == nil && shouldSync {
			syncCtx, syncCancel := context.WithTimeout(ctx, 30*time.Second)
			err := p.postgres.SyncPlayerStates(syncCtx, server.BmID, inputs, observedAt)
			syncCancel()
			if err != nil {
				pollErr = fmt.Errorf("sync player states: %w", err)
			}
		}

		if pollErr == nil && (len(inputs) > 0 || shouldSync) {
			stateCtx, stateCancel := context.WithTimeout(ctx, 5*time.Second)
			err := p.postgres.ResetTrackedServerEmptyPolls(stateCtx, server.BmID)
			stateCancel()
			if err != nil {
				pollErr = fmt.Errorf("reset empty player polls: %w", err)
			}
		}
	}

	recordCtx, recordCancel := context.WithTimeout(ctx, 5*time.Second)
	defer recordCancel()
	pollErrorMessage := ""
	if pollErr != nil {
		pollErrorMessage = pollErr.Error()
	}
	if err := p.postgres.RecordTrackedServerPollResult(recordCtx, server.BmID, observedAt, pollErrorMessage); err != nil {
		observability.Logger(recordCtx).ErrorContext(recordCtx, "failed to record poll result",
			slog.String("bm_id", server.BmID),
			slog.Any("error", err))
	}

	elapsed := time.Since(pollStart).Seconds()
	if pollErr != nil {
		pollErrorCounter.Add(ctx, 1, metric.WithAttributes(attribute.String("reason", reasonFor(pollErr))))
		observability.Logger(ctx).ErrorContext(ctx, "poll failed for server",
			slog.String("bm_id", server.BmID),
			slog.String("server_name", server.Name),
			slog.Any("error", pollErr))
		return
	}

	pollDurationHist.Record(ctx, elapsed)
	observability.Logger(ctx).InfoContext(ctx, "polled server",
		slog.String("bm_id", server.BmID),
		slog.String("server_name", server.Name),
		slog.Int("player_count", len(players)))
}

// reasonFor classifies an error into a short label for the poll_error_total
// metric. Keep this simple: look for obvious substrings and default to "other".
func reasonFor(err error) string {
	if err == nil {
		return "other"
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline exceeded"):
		return "timeout"
	case strings.Contains(msg, "unauthorized") || strings.Contains(msg, "403") || strings.Contains(msg, "401"):
		return "auth"
	case strings.Contains(msg, "429") || strings.Contains(msg, "rate limit") || strings.Contains(msg, "too many requests"):
		return "rate_limit"
	default:
		return "other"
	}
}

func (p *Poller) trackedServersToPoll(ctx context.Context) ([]store.TrackedServer, error) {
	dbCtx, dbCancel := context.WithTimeout(ctx, 5*time.Second)
	highFrequencyServers, err := p.postgres.ListTrackedServersToPoll(dbCtx, true, totalPollBatchSize)
	dbCancel()
	if err != nil {
		return nil, err
	}

	dbCtx, dbCancel = context.WithTimeout(ctx, 5*time.Second)
	lowFrequencyServers, err := p.postgres.ListTrackedServersToPoll(dbCtx, false, totalPollBatchSize)
	dbCancel()
	if err != nil {
		return nil, err
	}

	servers := make([]store.TrackedServer, 0, totalPollBatchSize)
	highUsed := appendPollServers(&servers, highFrequencyServers, 0, highFrequencyPollBatchSize)
	lowUsed := appendPollServers(&servers, lowFrequencyServers, 0, lowFrequencyPollBatchSize)
	if remaining := totalPollBatchSize - len(servers); remaining > 0 {
		highUsed += appendPollServers(&servers, highFrequencyServers, highUsed, remaining)
	}
	if remaining := totalPollBatchSize - len(servers); remaining > 0 {
		appendPollServers(&servers, lowFrequencyServers, lowUsed, remaining)
	}
	return servers, nil
}

func appendPollServers(dst *[]store.TrackedServer, src []store.TrackedServer, start int, limit int) int {
	if limit <= 0 || start >= len(src) {
		return 0
	}

	added := 0
	for _, server := range src[start:] {
		if added >= limit {
			break
		}
		*dst = append(*dst, server)
		added++
	}
	return added
}

func countPollFrequencies(servers []store.TrackedServer) (int, int) {
	highFrequencyCount := 0
	lowFrequencyCount := 0
	for _, server := range servers {
		if server.HighFrequency {
			highFrequencyCount++
		} else {
			lowFrequencyCount++
		}
	}
	return highFrequencyCount, lowFrequencyCount
}
