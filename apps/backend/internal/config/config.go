package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Port                 string
	PostgresURL          string
	RedisURL             string
	BotServiceJWT        string
	AppAPIInternalSecret string
	AppSessionSecret     string
	BotTargetShards      int
	BotHeartbeatTTL      time.Duration
	SteamAPIKey          string
	BattleMetricsPoll    time.Duration
	BMMatchURL           string
	BMMatchToken         string
	PlatformSyncToken    string
	AllowedJWTIssuer     string
	AllowedJWTAudience   string
	AppAdminDiscordIDs   []string
	ProxyEnabled         bool
	ProxyEndpoints       []string
	ProxyEncryptionKey   string
	AppBaseURL           string
	DiscordClientID      string
	DiscordClientSecret  string
	DiscordAppPublicKey  string
	AppOrigin            string
	TrustProxyHeaders    bool
	TrustProxyIPHeader   string
	TrustProxyGeoHeader  string

	// Observability.
	ObservabilityEnabled bool
	OTLPEndpoint         string
	PyroscopeAddress     string
	ServiceName          string
	ServiceNamespace     string
	DeploymentEnv        string
	TraceSampleRatio     float64
}

func Load() Config {
	targetShards := getEnvInt("BOT_TARGET_SHARDS", 4)
	heartbeatTTL := getEnvInt("BOT_HEARTBEAT_TTL_SECONDS", 45)
	battleMetricsPoll := getEnvInt("BATTLEMETRICS_SERVER_POLL_INTERVAL_SECONDS", 30)
	proxyEnabled := getEnvBool("PROXY_ENABLED", false)

	return Config{
		Port:                 getEnv("PORT", "8080"),
		PostgresURL:          getEnv("POSTGRES_URL", "postgres://rustlens:rustlens@localhost:5432/rustlens?sslmode=disable"),
		RedisURL:             getEnv("REDIS_URL", "redis://localhost:6379/0"),
		BotServiceJWT:        getRequiredEnv("BOT_SERVICE_JWT_SECRET"),
		AppAPIInternalSecret: getRequiredEnv("APP_API_INTERNAL_SECRET"),
		AppSessionSecret:     getRequiredEnv("APP_SESSION_SECRET"),
		BotTargetShards:      targetShards,
		BotHeartbeatTTL:      time.Duration(heartbeatTTL) * time.Second,
		SteamAPIKey:          getEnv("STEAM_API_KEY", ""),
		BattleMetricsPoll:    time.Duration(battleMetricsPoll) * time.Second,
		BMMatchURL:           strings.TrimSpace(getEnv("BM_MATCH_URL", "")),
		BMMatchToken:         strings.TrimSpace(getEnv("BM_MATCH_TOKEN", "")),
		PlatformSyncToken:    strings.TrimSpace(getEnv("PLATFORMSYNC_TOKEN", "")),
		AllowedJWTIssuer:     getEnv("BOT_JWT_ISSUER", "rustlens-bot"),
		AllowedJWTAudience:   getEnv("BOT_JWT_AUDIENCE", "rustlens-backend"),
		AppAdminDiscordIDs:   getEnvCSV("APP_ADMIN_DISCORD_IDS"),
		ProxyEnabled:         proxyEnabled,
		ProxyEndpoints:       getEnvCSV("PROXY_ENDPOINTS"),
		ProxyEncryptionKey:   strings.TrimSpace(getEnv("PROXY_ENCRYPTION_KEY", "")),
		AppBaseURL:           getEnv("APP_BASE_URL", "http://localhost:3000"),
		DiscordClientID:      getEnv("DISCORD_CLIENT_ID", ""),
		DiscordClientSecret:  getEnv("DISCORD_CLIENT_SECRET", ""),
		DiscordAppPublicKey:  strings.TrimSpace(getEnv("DISCORD_APP_PUBLIC_KEY", "")),
		AppOrigin:            getEnv("APP_ORIGIN", "http://localhost"),
		TrustProxyHeaders:    getEnvBool("TRUST_PROXY_HEADERS", false),
		TrustProxyIPHeader:   strings.TrimSpace(getEnv("TRUST_PROXY_IP_HEADER", "CF-Connecting-IP")),
		TrustProxyGeoHeader:  strings.TrimSpace(getEnv("TRUST_PROXY_GEO_HEADER", "CF-IPCountry")),
		ObservabilityEnabled: getEnvBool("OBSERVABILITY_ENABLED", false),
		OTLPEndpoint:         strings.TrimSpace(getEnv("OTEL_EXPORTER_OTLP_ENDPOINT", "")),
		PyroscopeAddress:     strings.TrimSpace(getEnv("PYROSCOPE_SERVER_ADDRESS", "")),
		ServiceName:          getEnv("OTEL_SERVICE_NAME", "rustlens-backend"),
		ServiceNamespace:     getEnv("OTEL_SERVICE_NAMESPACE", "rustlens"),
		DeploymentEnv:        getEnv("DEPLOYMENT_ENV", "local"),
		TraceSampleRatio:     getEnvFloat("OTEL_TRACES_SAMPLER_ARG", 0.1),
	}
}

func getEnv(key string, fallback string) string {
	if value, ok := os.LookupEnv(key); ok && value != "" {
		return value
	}
	return fallback
}

func getRequiredEnv(key string) string {
	value, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(value) == "" {
		panic("missing required environment variable: " + key)
	}
	return value
}

func getEnvInt(key string, fallback int) int {
	if value, ok := os.LookupEnv(key); ok {
		parsed, err := strconv.Atoi(value)
		if err == nil {
			return parsed
		}
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	if value, ok := os.LookupEnv(key); ok {
		parsed, err := strconv.ParseBool(value)
		if err == nil {
			return parsed
		}
	}
	return fallback
}

func getEnvFloat(key string, fallback float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return fallback
}

func getEnvCSV(key string) []string {
	value, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(value) == "" {
		return nil
	}

	parts := strings.Split(value, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		values = append(values, trimmed)
	}

	if len(values) == 0 {
		return nil
	}
	return values
}
