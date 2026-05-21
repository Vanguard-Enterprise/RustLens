package fraud

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter         = otel.Meter("rustlens.fraud")
	signalCounter metric.Int64Counter
)

func init() {
	var err error

	if signalCounter, err = meter.Int64Counter(
		"rustlens.fraud.signal_total",
		metric.WithDescription("Total number of fraud signals recorded (one per Record call, regardless of threshold)"),
	); err != nil {
		panic(err)
	}
}
