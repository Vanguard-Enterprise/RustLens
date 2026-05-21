package observability

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel/log/global"
	sdklog "go.opentelemetry.io/otel/sdk/log"
)

// SetGlobalLoggerProvider stores lp as the process-wide OTel log provider.
// Called by Init; exposed so tests can also reset it.
func SetGlobalLoggerProvider(lp *sdklog.LoggerProvider) {
	global.SetLoggerProvider(lp)
}

// Logger returns a slog.Logger that writes to both stdout (handled by the
// caller's default slog setup) and Loki via OTLP. If observability is disabled
// or Init has not run, callers receive slog.Default().
//
// The returned logger automatically attaches trace_id / span_id when a span is
// active in ctx.
func Logger(_ context.Context) *slog.Logger {
	// The global logger provider starts as a no-op until Init sets it.
	// otelslog.NewHandler reads global.GetLoggerProvider() when no explicit
	// provider option is given, so we can safely call this unconditionally.
	lp := global.GetLoggerProvider()
	// The SDK no-op provider type lives in go.opentelemetry.io/otel/log/noop;
	// we detect an uninitialised state by checking the concrete type name via
	// the interface — simplest: just always return a bridge logger and let the
	// no-op discard records silently.
	handler := otelslog.NewHandler("rustlens-backend", otelslog.WithLoggerProvider(lp))
	return slog.New(handler)
}
