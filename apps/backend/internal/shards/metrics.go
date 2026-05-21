package shards

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter            = otel.Meter("rustlens.shards")
	rebalanceCounter metric.Int64Counter
)

func init() {
	var err error
	if rebalanceCounter, err = meter.Int64Counter(
		"rustlens.shard.rebalance_total",
		metric.WithDescription("Number of shard rebalances performed"),
	); err != nil {
		panic(err)
	}
}

// RegisterObservableGauges registers observable gauges that are scraped on
// each collection cycle:
//   - rustlens.shard.assigned_count: current shard count per active worker.
//   - rustlens.shard.heartbeat_age_seconds: seconds since the last heartbeat
//     per worker, derived from timestamps stored in Redis.  The alert fires
//     when this exceeds 120 s; because the gauge is observed continuously it
//     will rise even after heartbeats stop arriving.
func RegisterObservableGauges(s *Service) {
	_, err := meter.Int64ObservableGauge(
		"rustlens.shard.assigned_count",
		metric.WithDescription("Currently assigned shards per worker"),
		metric.WithInt64Callback(func(ctx context.Context, o metric.Int64Observer) error {
			counts, err := s.AssignedCounts(ctx)
			if err != nil {
				slog.Warn("shard assigned-count callback failed", slog.Any("error", err))
				return nil
			}
			for worker, n := range counts {
				o.Observe(n, metric.WithAttributes(attribute.String("worker_id", worker)))
			}
			return nil
		}),
	)
	if err != nil {
		panic(err)
	}

	_, err = meter.Float64ObservableGauge(
		"rustlens.shard.heartbeat_age_seconds",
		metric.WithDescription("Seconds since the last heartbeat was received from each worker"),
		metric.WithUnit("s"),
		metric.WithFloat64Callback(func(ctx context.Context, o metric.Float64Observer) error {
			heartbeats, err := s.LastHeartbeats(ctx)
			if err != nil {
				slog.Warn("shard heartbeat-age callback failed", slog.Any("error", err))
				return nil
			}
			now := time.Now()
			for worker, lastTime := range heartbeats {
				age := now.Sub(lastTime).Seconds()
				o.Observe(age, metric.WithAttributes(attribute.String("worker_id", worker)))
			}
			return nil
		}),
	)
	if err != nil {
		panic(err)
	}
}
