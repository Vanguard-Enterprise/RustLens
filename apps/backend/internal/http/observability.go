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
// chi v5 sets r.Pattern (the stdlib field) after routing, and otelhttp v0.68+
// calls the span-name formatter a second time post-handler when r.Pattern is
// non-empty. We therefore provide a formatter that reads r.Pattern (chi's
// resolved route) and returns "METHOD /route/pattern", giving low-cardinality
// span names in Tempo. The inner wrapped handler additionally sets the
// http.route attribute and the metric labeler for cardinality-bounded
// histograms while the span is still active.
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
				span.SetName(r.Method + " " + pattern)
				// Also label the metric attributes for cardinality-bounded histograms.
				if labeler, ok := otelhttp.LabelerFromContext(r.Context()); ok {
					labeler.Add(attribute.String("http.route", pattern))
				}
			}
		})
		return otelhttp.NewHandler(
			wrapped,
			"",
			otelhttp.WithServerName(serviceName),
			// chi v5 sets r.Pattern (stdlib field) once routing completes.
			// otelhttp calls this formatter post-handler when r.Pattern != "".
			// Using r.Pattern here produces low-cardinality "METHOD /route"
			// span names rather than the raw URL path.
			otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
				if r.Pattern != "" {
					return r.Method + " " + r.Pattern
				}
				return r.Method + " " + r.URL.Path
			}),
		)
	}
}
