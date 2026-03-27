package shards

import (
	"reflect"
	"testing"
	"time"
)

func TestActiveWorkersFiltersExpiredHeartbeats(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	records := []WorkerHeartbeat{
		{WorkerID: "bot-a", LastHeartbeat: now.Add(-10 * time.Second)},
		{WorkerID: "bot-b", LastHeartbeat: now.Add(-65 * time.Second)},
	}

	active := ActiveWorkers(records, now, 30*time.Second)
	expected := []string{"bot-a"}
	if !reflect.DeepEqual(active, expected) {
		t.Fatalf("active workers mismatch: got %#v expected %#v", active, expected)
	}
}
