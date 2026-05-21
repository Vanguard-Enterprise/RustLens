package events

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter            = otel.Meter("rustlens.events")
	publishedCounter metric.Int64Counter
)

func init() {
	var err error

	if publishedCounter, err = meter.Int64Counter(
		"rustlens.events.published_total",
		metric.WithDescription("Total number of events published to the Hub"),
	); err != nil {
		panic(err)
	}

	// TODO: rustlens.events.subscribers (Int64ObservableGauge) — the Hub stores
	// subscribers as a map[userID][]chan keyed per user; there is no single
	// integer representing the total subscriber count across all users.
	// Implement Subscribers() int64 on Hub (iterate h.subs and sum slice lengths)
	// and wire it into a callback here once reviewed.
}
