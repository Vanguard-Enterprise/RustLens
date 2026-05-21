package outboundhttp

import (
	"net/url"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter               = otel.Meter("rustlens.outbound")
	autoDisabledCounter metric.Int64Counter
)

func init() {
	var err error

	if autoDisabledCounter, err = meter.Int64Counter(
		"rustlens.outbound.auto_disabled_total",
		metric.WithDescription("Total number of proxy endpoints that were automatically disabled due to high error rate"),
	); err != nil {
		panic(err)
	}
}

// classifyEndpoint extracts the hostname from an endpoint URL for use as a
// metric attribute. Falls back to "unknown" when the URL cannot be parsed or
// has no host.
func classifyEndpoint(endpoint string) string {
	if endpoint == "" {
		return "unknown"
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Host == "" {
		return "unknown"
	}
	return parsed.Hostname()
}
