package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Port                string
	PostgresURL         string
	RedisURL            string
	BotServiceJWT       string
	BotTargetShards     int
	BotHeartbeatTTL     time.Duration
	SteamAPIKey         string
	BattleMetricsAPIKey string
	BattleMetricsPoll   time.Duration
	AllowedJWTIssuer    string
	AllowedJWTAudience  string
	ProxyEnabled        bool
	ProxyEndpoints      []string
	AppAPIInternalSecret string
	AppSessionSecret     string
	AppBaseURL           string
}

func Load() Config {
	targetShards := getEnvInt("BOT_TARGET_SHARDS", 4)
	heartbeatTTL := getEnvInt("BOT_HEARTBEAT_TTL_SECONDS", 45)
	battleMetricsPoll := getEnvInt("BATTLEMETRICS_SERVER_POLL_INTERVAL_SECONDS", 10)
	proxyEnabled := getEnvBool("PROXY_ENABLED", false)

	return Config{
		Port:                getEnv("PORT", "8080"),
		PostgresURL:         getEnv("POSTGRES_URL", "postgres://rustlens:rustlens@localhost:5432/rustlens?sslmode=disable"),
		RedisURL:            getEnv("REDIS_URL", "redis://localhost:6379/0"),
		BotServiceJWT:       getEnv("BOT_SERVICE_JWT_SECRET", "dev-bot-secret"),
		BotTargetShards:     targetShards,
		BotHeartbeatTTL:     time.Duration(heartbeatTTL) * time.Second,
		SteamAPIKey:         getEnv("STEAM_API_KEY", ""),
		BattleMetricsAPIKey: getEnv("BATTLEMETRICS_API_KEY", ""),
		BattleMetricsPoll:   time.Duration(battleMetricsPoll) * time.Second,
		AllowedJWTIssuer:    getEnv("BOT_JWT_ISSUER", "rustlens-bot"),
		AllowedJWTAudience:  getEnv("BOT_JWT_AUDIENCE", "rustlens-backend"),
		ProxyEnabled:        proxyEnabled,
		ProxyEndpoints:      getEnvCSV("PROXY_ENDPOINTS"),
		AppAPIInternalSecret: getEnv("APP_API_INTERNAL_SECRET", "dev-app-internal-secret"),
		AppSessionSecret:     getEnv("APP_SESSION_SECRET", "dev-app-session-secret"),
		AppBaseURL:           getEnv("APP_BASE_URL", "http://localhost:3000"),
	}
}

func getEnv(key string, fallback string) string {
	if value, ok := os.LookupEnv(key); ok && value != "" {
		return value
	}
	return fallback
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
