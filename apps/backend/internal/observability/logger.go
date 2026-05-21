package observability

import (
	"context"
	"log/slog"
	"sync/atomic"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel/log/global"
	sdklog "go.opentelemetry.io/otel/sdk/log"
)

// providerInitialised is set to 1 by SetGlobalLoggerProvider once a real SDK
// provider has been registered. Logger uses it to decide whether to return the
// OTel bridge logger or slog.Default().
var providerInitialised atomic.Uint32

// SetGlobalLoggerProvider stores lp as the process-wide OTel log provider.
// Called by Init; exposed so tests can also reset it.
func SetGlobalLoggerProvider(lp *sdklog.LoggerProvider) {
	global.SetLoggerProvider(lp)
	providerInitialised.Store(1)
}

// Logger returns a slog.Logger that writes to both stdout (handled by the
// caller's default slog setup) and Loki via OTLP. If observability is disabled
// or Init has not run, callers receive slog.Default() so log records are not
// silently dropped by the SDK no-op provider.
//
// The returned logger automatically attaches trace_id / span_id when a span is
// active in ctx.
func Logger(_ context.Context) *slog.Logger {
	if providerInitialised.Load() == 0 {
		return slog.Default()
	}
	lp := global.GetLoggerProvider()
	handler := otelslog.NewHandler("rustlens-backend", otelslog.WithLoggerProvider(lp))
	return slog.New(handler)
}
