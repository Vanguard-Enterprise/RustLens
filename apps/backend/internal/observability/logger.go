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

// Logger returns a slog.Logger that writes to Loki via OTLP when observability
// is enabled, or slog.Default() otherwise, so log records are never silently
// dropped by the SDK no-op provider.
//
// ctx is accepted for API stability but is not used at construction time.
// trace_id / span_id correlation is performed by the OTel slog bridge at log
// time, not here: callers must pass the active context to the logger methods
// (e.g. logger.InfoContext(ctx, ...)) for span fields to appear on log records.
func Logger(ctx context.Context) *slog.Logger {
	if providerInitialised.Load() == 0 {
		return slog.Default()
	}
	lp := global.GetLoggerProvider()
	handler := otelslog.NewHandler("rustlens-backend", otelslog.WithLoggerProvider(lp))
	return slog.New(handler)
}
