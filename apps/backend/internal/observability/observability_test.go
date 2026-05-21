package observability

import (
	"context"
	"testing"

	"rustlens/backend/internal/config"
)

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
