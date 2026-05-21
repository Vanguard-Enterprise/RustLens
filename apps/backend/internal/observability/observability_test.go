package observability

import (
	"context"
	"log/slog"
	"testing"

	sdklog "go.opentelemetry.io/otel/sdk/log"

	"rustlens/backend/internal/config"
)

// TestLoggerFallsBackToSlogDefaultWhenNoProvider verifies that Logger returns
// slog.Default() when no SDK provider has been registered.
func TestLoggerFallsBackToSlogDefaultWhenNoProvider(t *testing.T) {
	// Ensure the flag is clear going in and restore it on exit so other tests
	// are not affected regardless of execution order.
	providerInitialised.Store(0)
	t.Cleanup(func() { providerInitialised.Store(0) })

	// After init has not run, Logger(ctx) should return slog.Default() so log
	// calls still reach the stdout handler instead of being silently dropped
	// by the SDK no-op provider.
	got := Logger(context.Background())
	if got != slog.Default() {
		t.Fatalf("Logger() before Init must return slog.Default(); got %T", got.Handler())
	}
}

func TestLoggerUsesBridgeAfterProviderRegistered(t *testing.T) {
	// Construct a real SDK logger provider with no exporters (records still
	// flow through the bridge, just don't get sent anywhere).
	lp := sdklog.NewLoggerProvider()
	t.Cleanup(func() { _ = lp.Shutdown(context.Background()) })

	SetGlobalLoggerProvider(lp)
	t.Cleanup(func() {
		// Reset flag so subsequent tests see the pre-Init state.
		providerInitialised.Store(0)
	})

	got := Logger(context.Background())
	if got == slog.Default() {
		t.Fatal("expected bridge logger after SetGlobalLoggerProvider, got slog.Default")
	}
}

func TestInitDisabledReturnsNoopShutdown(t *testing.T) {
	cfg := config.Config{ObservabilityEnabled: false}
	shutdown, err := Init(context.Background(), cfg)
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	if shutdown == nil {
		t.Fatal("shutdown was nil")
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}

func TestInitEnabledWithMissingEndpointFails(t *testing.T) {
	cfg := config.Config{ObservabilityEnabled: true, OTLPEndpoint: ""}
	if _, err := Init(context.Background(), cfg); err == nil {
		t.Fatal("expected error when enabled without endpoint")
	}
}
