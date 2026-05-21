package wipereactscheduler

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter           = otel.Meter("rustlens.wipe_scheduler")
	runDurationHist metric.Float64Histogram
	runErrorCounter metric.Int64Counter
)

func init() {
	var err error

	if runDurationHist, err = meter.Float64Histogram(
		"rustlens.wipe_scheduler.run_duration_seconds",
		metric.WithDescription("Duration of a single wipe-react scheduler tick"),
		metric.WithUnit("s"),
	); err != nil {
		panic(err)
	}

	if runErrorCounter, err = meter.Int64Counter(
		"rustlens.wipe_scheduler.run_error_total",
		metric.WithDescription("Total number of wipe-react scheduler tick errors"),
	); err != nil {
		panic(err)
	}
}
