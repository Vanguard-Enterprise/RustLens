package config

import (
	"reflect"
	"testing"
)

func TestLoad_DiscordOAuth(t *testing.T) {
	t.Setenv("DISCORD_CLIENT_ID", "id-123")
	t.Setenv("DISCORD_CLIENT_SECRET", "secret-456")
	t.Setenv("APP_ORIGIN", "http://localhost")
	cfg := Load()
	if cfg.DiscordClientID != "id-123" {
		t.Errorf("DiscordClientID = %q, want id-123", cfg.DiscordClientID)
	}
	if cfg.DiscordClientSecret != "secret-456" {
		t.Errorf("DiscordClientSecret = %q, want secret-456", cfg.DiscordClientSecret)
	}
	if cfg.AppOrigin != "http://localhost" {
		t.Errorf("AppOrigin = %q, want http://localhost", cfg.AppOrigin)
	}
}

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
