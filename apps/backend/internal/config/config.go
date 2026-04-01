package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	Port                string
	PostgresURL         string
	RedisURL            string
	BotServiceJWT       string
	BotTargetShards     int
	BotHeartbeatTTL     time.Duration
	BattleMetricsAPIKey string
	BattleMetricsPoll   time.Duration
	AllowedJWTIssuer    string
	AllowedJWTAudience  string
}

func Load() Config {
	targetShards := getEnvInt("BOT_TARGET_SHARDS", 4)
	heartbeatTTL := getEnvInt("BOT_HEARTBEAT_TTL_SECONDS", 45)
	battleMetricsPoll := getEnvInt("BATTLEMETRICS_SERVER_POLL_INTERVAL_SECONDS", 10)

	return Config{
		Port:                getEnv("PORT", "8080"),
		PostgresURL:         getEnv("POSTGRES_URL", "postgres://rustlens:rustlens@localhost:5432/rustlens?sslmode=disable"),
		RedisURL:            getEnv("REDIS_URL", "redis://localhost:6379/0"),
		BotServiceJWT:       getEnv("BOT_SERVICE_JWT_SECRET", "dev-bot-secret"),
		BotTargetShards:     targetShards,
		BotHeartbeatTTL:     time.Duration(heartbeatTTL) * time.Second,
		BattleMetricsAPIKey: getEnv("BATTLEMETRICS_API_KEY", ""),
		BattleMetricsPoll:   time.Duration(battleMetricsPoll) * time.Second,
		AllowedJWTIssuer:    getEnv("BOT_JWT_ISSUER", "rustlens-bot"),
		AllowedJWTAudience:  getEnv("BOT_JWT_AUDIENCE", "rustlens-backend"),
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
