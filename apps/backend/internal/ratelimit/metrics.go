package ratelimit

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter            = otel.Meter("rustlens.ratelimit")
	rejectionCounter metric.Int64Counter
)

func init() {
	var err error

	if rejectionCounter, err = meter.Int64Counter(
		"rustlens.ratelimit.rejection_total",
		metric.WithDescription("Total number of rate-limit rejections"),
	); err != nil {
		panic(err)
	}
}
