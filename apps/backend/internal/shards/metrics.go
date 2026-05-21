package shards

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter            = otel.Meter("rustlens.shards")
	heartbeatAgeHist metric.Float64Histogram
	rebalanceCounter metric.Int64Counter
)

func init() {
	var err error
	if heartbeatAgeHist, err = meter.Float64Histogram(
		"rustlens.shard.heartbeat_age_seconds",
		metric.WithDescription("Age of the most recent shard heartbeat"),
		metric.WithUnit("s"),
	); err != nil {
		panic(err)
	}
	if rebalanceCounter, err = meter.Int64Counter(
		"rustlens.shard.rebalance_total",
		metric.WithDescription("Number of shard rebalances performed"),
	); err != nil {
		panic(err)
	}
}

// RegisterObservableGauges registers an observable gauge that reports the
// number of shards currently assigned to each active worker.
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
}
