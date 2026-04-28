package battlemetrics

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"rustlens/backend/internal/store"
)

type Poller struct {
	client             *Client
	postgres           *store.PostgresStore
	redis              *store.RedisStore
	interval           time.Duration
	lockID             string
	highFrequencyPolls int
}

const highFrequencyPollsBeforeLowFrequency = 3

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
	lockTTL := 45 * time.Second
	if p.interval > lockTTL {
		lockTTL = p.interval + 30*time.Second
	}

	lockCtx, lockCancel := context.WithTimeout(ctx, 2*time.Second)
	acquired, err := p.redis.TryAcquireBattleMetricsPollerLock(lockCtx, p.lockID, lockTTL)
	lockCancel()
	if err != nil {
		log.Printf("[battlemetrics] failed to acquire poller lock: %v", err)
		return
	}
	if !acquired {
		return
	}
	defer func() {
		releaseCtx, releaseCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer releaseCancel()
		if err := p.redis.ReleaseBattleMetricsPollerLock(releaseCtx, p.lockID); err != nil {
			log.Printf("[battlemetrics] failed to release poller lock: %v", err)
		}
	}()

	dbCtx, dbCancel := context.WithTimeout(ctx, 5*time.Second)
	server, err := p.nextTrackedServerToPoll(dbCtx)
	dbCancel()
	if err != nil {
		log.Printf("[battlemetrics] failed to select next tracked server: %v", err)
		return
	}
	if server == nil {
		return
	}

	observedAt := time.Now().UTC()
	players, pollErr := p.client.FetchServerPlayers(ctx, server.BmID)
	if pollErr == nil {
		inputs := make([]store.PlayerPresenceInput, 0, len(players))
		for _, player := range players {
			inputs = append(inputs, store.PlayerPresenceInput{
				PlayerBmID: player.BmID,
				BmName:     player.Name,
			})
		}

		syncCtx, syncCancel := context.WithTimeout(ctx, 30*time.Second)
		err = p.postgres.SyncPlayerStates(syncCtx, server.BmID, inputs, observedAt)
		syncCancel()
		if err != nil {
			pollErr = fmt.Errorf("sync player states: %w", err)
		}
	}

	recordCtx, recordCancel := context.WithTimeout(ctx, 5*time.Second)
	defer recordCancel()
	pollErrorMessage := ""
	if pollErr != nil {
		pollErrorMessage = pollErr.Error()
	}
	if err := p.postgres.RecordTrackedServerPollResult(recordCtx, server.BmID, observedAt, pollErrorMessage); err != nil {
		log.Printf("[battlemetrics] failed to record poll result for server %s: %v", server.BmID, err)
	}

	if pollErr != nil {
		log.Printf("[battlemetrics] poll failed for server %s (%s): %v", server.BmID, server.Name, pollErr)
		return
	}

	log.Printf("[battlemetrics] polled server %s (%s): %d players", server.BmID, server.Name, len(players))
}

func (p *Poller) nextTrackedServerToPoll(ctx context.Context) (*store.TrackedServer, error) {
	targetHighFrequency := p.highFrequencyPolls < highFrequencyPollsBeforeLowFrequency
	server, err := p.postgres.NextTrackedServerToPoll(ctx, targetHighFrequency)
	if err != nil {
		return nil, err
	}
	if server == nil {
		server, err = p.postgres.NextTrackedServerToPoll(ctx, !targetHighFrequency)
		if err != nil {
			return nil, err
		}
	}
	if server == nil {
		return nil, nil
	}

	if targetHighFrequency {
		p.highFrequencyPolls++
	} else {
		p.highFrequencyPolls = 0
	}
	return server, nil
}
