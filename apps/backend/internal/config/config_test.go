package config

import (
	"reflect"
	"testing"
)

func TestLoadParsesProxyConfig(t *testing.T) {
	t.Setenv("APP_API_INTERNAL_SECRET", "test-app-secret")
	t.Setenv("APP_SESSION_SECRET", "test-session-secret")
	t.Setenv("BOT_SERVICE_JWT_SECRET", "test-bot-secret")
	t.Setenv("PROXY_ENABLED", "true")
	t.Setenv("PROXY_ENDPOINTS", " http://proxy-one:8080,https://proxy-two:8443 ")

	cfg := Load()

	if !cfg.ProxyEnabled {
		t.Fatal("expected ProxyEnabled to be true")
	}

	want := []string{"http://proxy-one:8080", "https://proxy-two:8443"}
	if !reflect.DeepEqual(cfg.ProxyEndpoints, want) {
		t.Fatalf("ProxyEndpoints = %#v, want %#v", cfg.ProxyEndpoints, want)
	}
}

func TestLoadDefaultsProxyDisabled(t *testing.T) {
	t.Setenv("APP_API_INTERNAL_SECRET", "test-app-secret")
	t.Setenv("APP_SESSION_SECRET", "test-session-secret")
	t.Setenv("BOT_SERVICE_JWT_SECRET", "test-bot-secret")
	t.Setenv("PROXY_ENABLED", "false")
	t.Setenv("PROXY_ENDPOINTS", "http://proxy-one:8080")

	cfg := Load()

	if cfg.ProxyEnabled {
		t.Fatal("expected ProxyEnabled to be false")
	}
	if len(cfg.ProxyEndpoints) != 1 {
		t.Fatalf("expected proxy endpoints to still parse for config visibility, got %d", len(cfg.ProxyEndpoints))
	}
}
