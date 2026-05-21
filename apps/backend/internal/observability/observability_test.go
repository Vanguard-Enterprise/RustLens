package observability

import (
	"context"
	"log/slog"
	"testing"

	"rustlens/backend/internal/config"
)

// TestLoggerFallsBackToSlogDefaultWhenNoProvider must run before any test that
// calls Init with ObservabilityEnabled:true (which would set providerInitialised).
// The existing tests in this file never reach initLogger, so ordering is safe.
func TestLoggerFallsBackToSlogDefaultWhenNoProvider(t *testing.T) {
	// After init has not run, Logger(ctx) should return slog.Default() so log
	// calls still reach the stdout handler instead of being silently dropped
	// by the SDK no-op provider.
	got := Logger(context.Background())
	if got != slog.Default() {
		t.Fatalf("Logger() before Init must return slog.Default(); got %T", got.Handler())
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
