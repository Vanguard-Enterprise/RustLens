package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// WithOTel wraps the router so every request produces an HTTP server span and
// the http.server.* metric family, with http.route set from chi's resolved
// route pattern (rather than the raw URL, which would explode cardinality).
//
// The middleware runs after chi has resolved the route pattern by deferring
// the attribute annotation to after next.ServeHTTP returns. At that point chi
// has populated the route context, and the otelhttp span is still active on
// r.Context(), so we can attach http.route directly to the span.
func WithOTel(serviceName string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		wrapped := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r)
			// After next returns, chi has resolved the route pattern.
			// The otelhttp span is still active on r.Context() — annotate it.
			if rctx := chi.RouteContext(r.Context()); rctx != nil && rctx.RoutePattern() != "" {
				pattern := rctx.RoutePattern()
				span := trace.SpanFromContext(r.Context())
				span.SetAttributes(attribute.String("http.route", pattern))
				// Also label the metric attributes for cardinality-bounded histograms.
				if labeler, ok := otelhttp.LabelerFromContext(r.Context()); ok {
					labeler.Add(attribute.String("http.route", pattern))
				}
			}
		})
		return otelhttp.NewHandler(
			wrapped,
			"",
			otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
				if rctx := chi.RouteContext(r.Context()); rctx != nil && rctx.RoutePattern() != "" {
					return r.Method + " " + rctx.RoutePattern()
				}
				return r.Method + " " + r.URL.Path
			}),
		)
	}
}
