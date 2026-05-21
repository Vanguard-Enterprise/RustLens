package shards

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

type leaseStore interface {
	MarkWorker(ctx context.Context, workerID string, ttl time.Duration) error
	RefreshWorker(ctx context.Context, workerID string, ttl time.Duration) error
	RemoveWorker(ctx context.Context, workerID string) error
	ActiveWorkers(ctx context.Context) ([]string, error)
	SetLeases(ctx context.Context, workerID string, shardIDs []int) error
	CurrentAssignments(ctx context.Context) (map[string][]int, error)
	SetWorkerStats(ctx context.Context, workerID string, payload []byte, ttl time.Duration) error
	GetWorkerStats(ctx context.Context, workerIDs []string) (map[string][]byte, error)
	LastHeartbeats(ctx context.Context) (map[string]time.Time, error)
}

type Service struct {
	leases       leaseStore
	totalShards  int
	heartbeatTTL time.Duration
}

func NewService(leases leaseStore, totalShards int, heartbeatTTL time.Duration) *Service {
	return &Service{
		leases:       leases,
		totalShards:  totalShards,
		heartbeatTTL: heartbeatTTL,
	}
}

type ShardStatSnapshot struct {
	ShardID int    `json:"shardId"`
	Status  string `json:"status"`
	PingMs  int    `json:"pingMs"`
	Guilds  int    `json:"guilds"`
	Members int64  `json:"members"`
}

type WorkerStatSnapshot struct {
	UptimeSec int                 `json:"uptimeSec"`
	RSSBytes  int64               `json:"rssBytes"`
	Shards    []ShardStatSnapshot `json:"shards"`
}

type RegisterResponse struct {
	WorkerID        string `json:"workerId"`
	TargetShards    int    `json:"targetShards"`
	HeartbeatTTLSec int    `json:"heartbeatTtlSeconds"`
}

func (s *Service) Register(ctx context.Context, workerID string) (*RegisterResponse, error) {
	if workerID == "" {
		return nil, fmt.Errorf("worker id required")
	}

	if err := s.leases.MarkWorker(ctx, workerID, s.heartbeatTTL); err != nil {
		return nil, err
	}

	return &RegisterResponse{
		WorkerID:        workerID,
		TargetShards:    s.totalShards,
		HeartbeatTTLSec: int(s.heartbeatTTL / time.Second),
	}, nil
}

func (s *Service) Heartbeat(ctx context.Context, workerID string, shardIDs []int, snapshot *WorkerStatSnapshot) error {
	if workerID == "" {
		return fmt.Errorf("worker id required")
	}
	_ = shardIDs // health-only: canonical lease map is owned by Assignments.

	if err := s.leases.RefreshWorker(ctx, workerID, s.heartbeatTTL); err != nil {
		return err
	}

	if snapshot != nil {
		payload, err := json.Marshal(snapshot)
		if err != nil {
			return fmt.Errorf("marshal snapshot: %w", err)
		}
		if err := s.leases.SetWorkerStats(ctx, workerID, payload, s.heartbeatTTL); err != nil {
			return err
		}
	}

	return nil
}

func (s *Service) Release(ctx context.Context, workerID string) error {
	if workerID == "" {
		return fmt.Errorf("worker id required")
	}

	if err := s.leases.RemoveWorker(ctx, workerID); err != nil {
		return err
	}

	if err := s.leases.SetLeases(ctx, workerID, nil); err != nil {
		return err
	}

	return nil
}

func (s *Service) Assignments(ctx context.Context, workerID string) ([]int, int, error) {
	workers, err := s.leases.ActiveWorkers(ctx)
	if err != nil {
		return nil, 0, err
	}

	// Capture the distribution before writing new leases so we can detect
	// whether the assignment actually changed.
	prior, err := s.leases.CurrentAssignments(ctx)
	if err != nil {
		return nil, 0, err
	}

	assignments := AssignShards(s.totalShards, workers)
	for owner, shardIDs := range assignments {
		if err := s.leases.SetLeases(ctx, owner, shardIDs); err != nil {
			return nil, 0, err
		}
	}

	// Only count a rebalance when the distribution actually changed.
	if !assignmentsEqual(prior, assignments) {
		rebalanceCounter.Add(ctx, 1)
	}

	current, err := s.leases.CurrentAssignments(ctx)
	if err != nil {
		return nil, 0, err
	}

	owned := current[workerID]
	sort.Ints(owned)
	return owned, s.totalShards, nil
}

// assignmentsEqual reports whether two assignment maps are identical: same
// worker keys and the same sorted shard slice for every key.
func assignmentsEqual(a, b map[string][]int) bool {
	if len(a) != len(b) {
		return false
	}
	for workerID, shardsA := range a {
		shardsB, ok := b[workerID]
		if !ok || len(shardsA) != len(shardsB) {
			return false
		}
		sortedA := append([]int(nil), shardsA...)
		sortedB := append([]int(nil), shardsB...)
		sort.Ints(sortedA)
		sort.Ints(sortedB)
		for i := range sortedA {
			if sortedA[i] != sortedB[i] {
				return false
			}
		}
	}
	return true
}

// AssignedCounts returns the number of shards currently assigned to each
// active worker, keyed by worker ID.
func (s *Service) AssignedCounts(ctx context.Context) (map[string]int64, error) {
	current, err := s.leases.CurrentAssignments(ctx)
	if err != nil {
		return nil, err
	}

	counts := make(map[string]int64, len(current))
	for workerID, shards := range current {
		counts[workerID] = int64(len(shards))
	}
	return counts, nil
}

// ActiveWorkerStats returns the latest stat snapshot per active worker. Workers
// without a snapshot in Redis are skipped.
func (s *Service) ActiveWorkerStats(ctx context.Context) (map[string]WorkerStatSnapshot, error) {
	workers, err := s.leases.ActiveWorkers(ctx)
	if err != nil {
		return nil, err
	}

	if len(workers) == 0 {
		return map[string]WorkerStatSnapshot{}, nil
	}

	raw, err := s.leases.GetWorkerStats(ctx, workers)
	if err != nil {
		return nil, err
	}

	result := make(map[string]WorkerStatSnapshot, len(raw))
	for workerID, payload := range raw {
		if len(payload) == 0 {
			continue
		}
		var snapshot WorkerStatSnapshot
		if err := json.Unmarshal(payload, &snapshot); err != nil {
			return nil, fmt.Errorf("unmarshal snapshot for %s: %w", workerID, err)
		}
		result[workerID] = snapshot
	}

	return result, nil
}

// LastHeartbeats returns the last heartbeat time for every worker that
// currently has a heartbeat key in Redis.  It delegates to the underlying
// lease store and is the data source for the heartbeat-age observable gauge.
func (s *Service) LastHeartbeats(ctx context.Context) (map[string]time.Time, error) {
	return s.leases.LastHeartbeats(ctx)
}
