package shards

import (
	"context"
	"maps"
	"reflect"
	"slices"
	"sort"
	"testing"
	"time"
)

// fakeLeaseStore is an in-memory leaseStore implementation suitable for
// exercising Service in isolation.
type fakeLeaseStore struct {
	now       func() time.Time
	workers   map[string]time.Time // worker -> lease expiry
	leases    map[string][]int     // worker -> assigned shard IDs
	stats     map[string][]byte
	refreshes []string
}

func newFakeLeaseStore(now func() time.Time) *fakeLeaseStore {
	return &fakeLeaseStore{
		now:     now,
		workers: map[string]time.Time{},
		leases:  map[string][]int{},
		stats:   map[string][]byte{},
	}
}

func (s *fakeLeaseStore) MarkWorker(_ context.Context, workerID string, ttl time.Duration) error {
	s.workers[workerID] = s.now().Add(ttl)
	return nil
}

func (s *fakeLeaseStore) RefreshWorker(_ context.Context, workerID string, ttl time.Duration) error {
	s.refreshes = append(s.refreshes, workerID)
	s.workers[workerID] = s.now().Add(ttl)
	return nil
}

func (s *fakeLeaseStore) RemoveWorker(_ context.Context, workerID string) error {
	delete(s.workers, workerID)
	return nil
}

func (s *fakeLeaseStore) ActiveWorkers(_ context.Context) ([]string, error) {
	out := []string{}
	now := s.now()
	for id, expiry := range s.workers {
		if expiry.After(now) {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out, nil
}

func (s *fakeLeaseStore) SetLeases(_ context.Context, workerID string, shardIDs []int) error {
	if len(shardIDs) == 0 {
		delete(s.leases, workerID)
		return nil
	}
	cp := append([]int(nil), shardIDs...)
	s.leases[workerID] = cp
	return nil
}

func (s *fakeLeaseStore) CurrentAssignments(_ context.Context) (map[string][]int, error) {
	out := map[string][]int{}
	for id, shards := range s.leases {
		out[id] = append([]int(nil), shards...)
	}
	return out, nil
}

func (s *fakeLeaseStore) SetWorkerStats(_ context.Context, workerID string, payload []byte, _ time.Duration) error {
	s.stats[workerID] = append([]byte(nil), payload...)
	return nil
}

func (s *fakeLeaseStore) GetWorkerStats(_ context.Context, workerIDs []string) (map[string][]byte, error) {
	out := map[string][]byte{}
	for _, id := range workerIDs {
		if v, ok := s.stats[id]; ok {
			out[id] = v
		}
	}
	return out, nil
}

func (s *fakeLeaseStore) LastHeartbeats(_ context.Context) (map[string]time.Time, error) {
	out := map[string]time.Time{}
	maps.Copy(out, s.workers)
	return out, nil
}

// TestAssignments_RefreshesCallerLease_PreventsShardDuplication verifies that
// when a worker's lease has just lapsed but it's still calling in,
// Assignments() refreshes the caller's lease so it remains in the active set
// — preventing the backend from redistributing the caller's shards to another
// worker (which would then run the same Discord shard concurrently and cause
// the request-fan-out the audit found).
func TestAssignments_RefreshesCallerLease_PreventsShardDuplication(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := &now
	store := newFakeLeaseStore(func() time.Time { return *clock })
	svc := NewService(store, 2, 45*time.Second)
	ctx := context.Background()

	// Worker A registers and claims both shards (only worker in the cluster).
	if _, err := svc.Register(ctx, "worker-a"); err != nil {
		t.Fatalf("register a: %v", err)
	}
	ownedA, _, err := svc.Assignments(ctx, "worker-a")
	if err != nil {
		t.Fatalf("assignments a: %v", err)
	}
	if !reflect.DeepEqual(ownedA, []int{0, 1}) {
		t.Fatalf("expected worker-a to own [0 1], got %v", ownedA)
	}

	// Worker B registers; cluster now has two workers, so we expect each to
	// take one shard.
	if _, err := svc.Register(ctx, "worker-b"); err != nil {
		t.Fatalf("register b: %v", err)
	}

	// Simulate worker-a's lease lapsing right before its next reconcile —
	// e.g. its previous heartbeat was delayed past TTL. Without the
	// caller-active refresh in Assignments(), worker-a would not appear in
	// ActiveWorkers(), and the backend would hand both shards to worker-b
	// while worker-a was still actively running them.
	store.workers["worker-a"] = now.Add(-time.Second)

	ownedA2, _, err := svc.Assignments(ctx, "worker-a")
	if err != nil {
		t.Fatalf("assignments a (post-lapse): %v", err)
	}
	// worker-a must still receive a shard — the fix promotes it back to
	// active because it just called in.
	if len(ownedA2) == 0 {
		t.Fatalf("worker-a was excluded from its own Assignments() call; got %v", ownedA2)
	}

	// The lease must have been refreshed for worker-a as part of the call.
	if !slices.Contains(store.refreshes, "worker-a") {
		t.Fatalf("expected RefreshWorker to be called for worker-a; refreshes=%v", store.refreshes)
	}

	// Both workers combined should hold every shard exactly once — no shard
	// is assigned to two workers.
	ownedB, _, err := svc.Assignments(ctx, "worker-b")
	if err != nil {
		t.Fatalf("assignments b: %v", err)
	}
	combined := append([]int{}, ownedA2...)
	combined = append(combined, ownedB...)
	sort.Ints(combined)
	if !reflect.DeepEqual(combined, []int{0, 1}) {
		t.Fatalf("shards split across workers should be {0,1} once each; got %v (a=%v b=%v)", combined, ownedA2, ownedB)
	}
}
