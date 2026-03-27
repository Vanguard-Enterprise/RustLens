package shards

import (
	"sort"
	"time"
)

type WorkerHeartbeat struct {
	WorkerID      string
	LastHeartbeat time.Time
}

func ActiveWorkers(records []WorkerHeartbeat, now time.Time, ttl time.Duration) []string {
	active := make([]string, 0, len(records))
	for _, record := range records {
		if now.Sub(record.LastHeartbeat) <= ttl {
			active = append(active, record.WorkerID)
		}
	}

	sort.Strings(active)
	return active
}
