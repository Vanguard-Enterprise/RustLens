package battlemetrics

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter             = otel.Meter("rustlens.battlemetrics")
	pollDurationHist  metric.Float64Histogram
	pollErrorCounter  metric.Int64Counter
)

func init() {
	var err error

	if pollDurationHist, err = meter.Float64Histogram(
		"rustlens.battlemetrics.poll_duration_seconds",
		metric.WithDescription("Duration of a single BattleMetrics server poll cycle"),
		metric.WithUnit("s"),
	); err != nil {
		panic(err)
	}

	if pollErrorCounter, err = meter.Int64Counter(
		"rustlens.battlemetrics.poll_error_total",
		metric.WithDescription("Total number of BattleMetrics poll errors"),
	); err != nil {
		panic(err)
	}
}

// RegisterObservableGauges wires up the observable gauges that require a
// live store reference. Call once after the Poller is constructed.
//
// TODO: rustlens.battlemetrics.servers_polled — the Poller does not hold a
// count of currently tracked servers in memory. To implement this gauge
// properly we need either (a) a cached count on the Poller updated after each
// NextTrackedServerToPoll call, or (b) a periodic background query. Skipping
// the gauge for now rather than adding unreviewed DB calls inside an
// observable callback.
func RegisterObservableGauges(_ *Poller) {
	// No-op until the above TODO is resolved.
}
