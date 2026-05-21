package store

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/extra/redisotel/v9"
	"github.com/redis/go-redis/v9"
)

const (
	workersKey                    = "rustlens:bot:workers"
	leaseMapKey                   = "rustlens:bot:leases"
	workerHeartbeatPref           = "rustlens:bot:worker:heartbeat:"
	workerStatsPref               = "rustlens:bot:worker:stats:"
	trackNotificationClaimPref    = "rustlens:track:notification:claim:"
	battleMetricsPollerCadenceKey = "rustlens:battlemetrics:poller:cadence"
	battleMetricsPollerLockKey    = "rustlens:battlemetrics:poller:lock"
	serverAnalyticsRefreshLockKey = "rustlens:server-analytics:refresh:lock"
)

type RedisStore struct {
	client *redis.Client
}

func NewRedisStore(ctx context.Context, redisURL string) (*RedisStore, error) {
	options, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, err
	}

	client := redis.NewClient(options)
	if err := redisotel.InstrumentTracing(client); err != nil {
		return nil, fmt.Errorf("redis tracing: %w", err)
	}
	if err := redisotel.InstrumentMetrics(client); err != nil {
		return nil, fmt.Errorf("redis metrics: %w", err)
	}
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, err
	}

	return &RedisStore{client: client}, nil
}

func (r *RedisStore) Close() error {
	return r.client.Close()
}

func (r *RedisStore) Ping(ctx context.Context) error {
	return r.client.Ping(ctx).Err()
}

func (r *RedisStore) MarkWorker(ctx context.Context, workerID string, ttl time.Duration) error {
	pipe := r.client.TxPipeline()
	pipe.SAdd(ctx, workersKey, workerID)
	pipe.SetEx(ctx, workerHeartbeatPref+workerID, "1", ttl)
	_, err := pipe.Exec(ctx)
	return err
}

func (r *RedisStore) RefreshWorker(ctx context.Context, workerID string, ttl time.Duration) error {
	// Re-SAdd to workersKey on every heartbeat: if a worker's TTL key ever
	// expired (transient blip, GC pause, backend restart), ActiveWorkers would
	// have SREM'd the worker from the set. Without this re-add a single missed
	// heartbeat permanently removes the worker until the bot process restarts.
	pipe := r.client.TxPipeline()
	pipe.SAdd(ctx, workersKey, workerID)
	pipe.SetEx(ctx, workerHeartbeatPref+workerID, "1", ttl)
	_, err := pipe.Exec(ctx)
	return err
}

func (r *RedisStore) RemoveWorker(ctx context.Context, workerID string) error {
	pipe := r.client.TxPipeline()
	pipe.SRem(ctx, workersKey, workerID)
	pipe.Del(ctx, workerHeartbeatPref+workerID)
	pipe.Del(ctx, workerStatsPref+workerID)
	_, err := pipe.Exec(ctx)
	return err
}

func (r *RedisStore) SetWorkerStats(ctx context.Context, workerID string, payload []byte, ttl time.Duration) error {
	return r.client.Set(ctx, workerStatsPref+workerID, payload, ttl).Err()
}

func (r *RedisStore) GetWorkerStats(ctx context.Context, workerIDs []string) (map[string][]byte, error) {
	result := make(map[string][]byte, len(workerIDs))
	if len(workerIDs) == 0 {
		return result, nil
	}

	keys := make([]string, len(workerIDs))
	for i, id := range workerIDs {
		keys[i] = workerStatsPref + id
	}

	values, err := r.client.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, err
	}

	for i, value := range values {
		if value == nil {
			continue
		}
		switch v := value.(type) {
		case string:
			result[workerIDs[i]] = []byte(v)
		case []byte:
			result[workerIDs[i]] = v
		}
	}

	return result, nil
}

func (r *RedisStore) ActiveWorkers(ctx context.Context) ([]string, error) {
	workers, err := r.client.SMembers(ctx, workersKey).Result()
	if err != nil {
		return nil, err
	}

	active := make([]string, 0, len(workers))
	for _, workerID := range workers {
		exists, err := r.client.Exists(ctx, workerHeartbeatPref+workerID).Result()
		if err != nil {
			return nil, err
		}

		if exists == 0 {
			if err := r.RemoveWorker(ctx, workerID); err != nil {
				return nil, err
			}
			if err := r.RemoveLeasesByWorker(ctx, workerID); err != nil {
				return nil, err
			}
			continue
		}

		active = append(active, workerID)
	}

	sort.Strings(active)
	return active, nil
}

func (r *RedisStore) SetLeases(ctx context.Context, workerID string, shardIDs []int) error {
	if err := r.RemoveLeasesByWorker(ctx, workerID); err != nil {
		return err
	}

	if len(shardIDs) == 0 {
		return nil
	}

	pairs := make([]any, 0, len(shardIDs)*2)
	for _, shardID := range shardIDs {
		pairs = append(pairs, strconv.Itoa(shardID), workerID)
	}

	return r.client.HSet(ctx, leaseMapKey, pairs...).Err()
}

func (r *RedisStore) RemoveLeasesByWorker(ctx context.Context, workerID string) error {
	leases, err := r.client.HGetAll(ctx, leaseMapKey).Result()
	if err != nil {
		return err
	}

	keys := make([]string, 0)
	for shardID, owner := range leases {
		if owner == workerID {
			keys = append(keys, shardID)
		}
	}

	if len(keys) == 0 {
		return nil
	}

	return r.client.HDel(ctx, leaseMapKey, keys...).Err()
}

func (r *RedisStore) CurrentAssignments(ctx context.Context) (map[string][]int, error) {
	leases, err := r.client.HGetAll(ctx, leaseMapKey).Result()
	if err != nil {
		return nil, err
	}

	result := map[string][]int{}
	for shardID, workerID := range leases {
		shard, err := strconv.Atoi(strings.TrimSpace(shardID))
		if err != nil {
			continue
		}
		result[workerID] = append(result[workerID], shard)
	}

	for workerID := range result {
		sort.Ints(result[workerID])
	}

	return result, nil
}

func (r *RedisStore) TryAcquireBattleMetricsPollerLock(ctx context.Context, owner string, ttl time.Duration) (bool, error) {
	return r.client.SetNX(ctx, battleMetricsPollerLockKey, owner, ttl).Result()
}

func (r *RedisStore) TryReserveBattleMetricsPollerCycle(ctx context.Context, owner string, ttl time.Duration) (bool, error) {
	return r.client.SetNX(ctx, battleMetricsPollerCadenceKey, owner, ttl).Result()
}

func (r *RedisStore) ReleaseBattleMetricsPollerLock(ctx context.Context, owner string) error {
	const releaseScript = `
if redis.call("get", KEYS[1]) == ARGV[1] then
	return redis.call("del", KEYS[1])
end
return 0
`

	return r.client.Eval(ctx, releaseScript, []string{battleMetricsPollerLockKey}, owner).Err()
}

func (r *RedisStore) TryAcquireServerAnalyticsRefreshLock(ctx context.Context, owner string, ttl time.Duration) (bool, error) {
	return r.client.SetNX(ctx, serverAnalyticsRefreshLockKey, owner, ttl).Result()
}

func (r *RedisStore) ReleaseServerAnalyticsRefreshLock(ctx context.Context, owner string) error {
	const releaseScript = `
if redis.call("get", KEYS[1]) == ARGV[1] then
	return redis.call("del", KEYS[1])
end
return 0
`

	return r.client.Eval(ctx, releaseScript, []string{serverAnalyticsRefreshLockKey}, owner).Err()
}

func (r *RedisStore) TryClaimTrackNotification(ctx context.Context, groupID int64, steamID string, isOnline bool, onlineUnix int64, owner string, ttl time.Duration) (bool, error) {
	state := "offline"
	if isOnline {
		state = "online"
	}
	key := fmt.Sprintf("%s%d:%s:%s:%d", trackNotificationClaimPref, groupID, steamID, state, onlineUnix)
	return r.client.SetNX(ctx, key, owner, ttl).Result()
}

// Client returns the underlying go-redis client. Used by callers that need
// direct pub/sub access; the typed wrapper methods on RedisStore are still
// preferred for storage-style operations.
func (r *RedisStore) Client() *redis.Client {
	return r.client
}
