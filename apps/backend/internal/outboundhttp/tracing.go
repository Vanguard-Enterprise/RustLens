package outboundhttp

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// WithTarget annotates the current span (typically the HTTP handler span)
// with the logical outbound target. Call this once per request you make to
// an external service. Safe when no span is active.
func WithTarget(ctx context.Context, target string) {
	if span := trace.SpanFromContext(ctx); span.IsRecording() {
		span.SetAttributes(attribute.String("rustlens.outbound.target", target))
	}
}
