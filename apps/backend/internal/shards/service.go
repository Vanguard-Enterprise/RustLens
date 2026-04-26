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
}

type eventStore interface {
	InsertWorkerEvent(ctx context.Context, workerID string, eventType string, payload any) error
}

type Service struct {
	leases       leaseStore
	events       eventStore
	totalShards  int
	heartbeatTTL time.Duration
}

func NewService(leases leaseStore, events eventStore, totalShards int, heartbeatTTL time.Duration) *Service {
	return &Service{
		leases:       leases,
		events:       events,
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

	_ = s.events.InsertWorkerEvent(ctx, workerID, "register", map[string]any{"targetShards": s.totalShards})

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

	if err := s.leases.RefreshWorker(ctx, workerID, s.heartbeatTTL); err != nil {
		return err
	}

	sorted := append([]int(nil), shardIDs...)
	sort.Ints(sorted)
	if err := s.leases.SetLeases(ctx, workerID, sorted); err != nil {
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

	_ = s.events.InsertWorkerEvent(ctx, workerID, "heartbeat", map[string]any{"shardIds": sorted})
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

	_ = s.events.InsertWorkerEvent(ctx, workerID, "release", map[string]any{})
	return nil
}

func (s *Service) Assignments(ctx context.Context, workerID string) ([]int, int, error) {
	workers, err := s.leases.ActiveWorkers(ctx)
	if err != nil {
		return nil, 0, err
	}

	assignments := AssignShards(s.totalShards, workers)
	for owner, shardIDs := range assignments {
		if err := s.leases.SetLeases(ctx, owner, shardIDs); err != nil {
			return nil, 0, err
		}
	}

	current, err := s.leases.CurrentAssignments(ctx)
	if err != nil {
		return nil, 0, err
	}

	owned := current[workerID]
	sort.Ints(owned)
	return owned, s.totalShards, nil
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
