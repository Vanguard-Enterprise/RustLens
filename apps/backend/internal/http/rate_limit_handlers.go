package httpapi

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"rustlens/backend/internal/fraud"
	"rustlens/backend/internal/ratelimit"
)

// playerRateLimitConfig is the escalating-cooldown config used for the
// /player slash command and any other command routed through the
// "player" rate-limit name. 5 calls per 60s; overflow triggers a 30s
// cooldown that doubles per repeat violation, capped at 1h. The
// violation counter decays after 1h of clean usage.
var playerRateLimitConfig = ratelimit.Config{
	Quota:          5,
	Window:         60 * time.Second,
	BaseCooldown:   30 * time.Second,
	MaxCooldown:    1 * time.Hour,
	ViolationDecay: 1 * time.Hour,
}

// playerFraudConfig: alert when a guild with <25 approximate members
// triggers >=50 /player invocations within a rolling hour. Alerts are
// suppressed per (guild, command) for 6h to avoid spam.
var playerFraudConfig = fraud.Config{
	Window:         1 * time.Hour,
	Threshold:      50,
	MaxMemberCount: 25,
	AlertCooldown:  6 * time.Hour,
}

var rateLimitCommandConfigs = map[string]ratelimit.Config{
	"player": playerRateLimitConfig,
}

var fraudCommandConfigs = map[string]fraud.Config{
	"player": playerFraudConfig,
}

type rateLimitConsumeRequest struct {
	DiscordID   string `json:"discordId"`
	Command     string `json:"command"`
	GuildID     string `json:"guildId,omitempty"`
	MemberCount int    `json:"memberCount,omitempty"`
}

type rateLimitFraudPayload struct {
	GuildID     string `json:"guildId"`
	Command     string `json:"command"`
	Count       int    `json:"count"`
	MemberCount int    `json:"memberCount"`
	WindowSecs  int    `json:"windowSecs"`
}

type rateLimitConsumeResponse struct {
	Allowed           bool                   `json:"allowed"`
	RetryAfterSeconds int                    `json:"retryAfterSeconds"`
	ViolationLevel    int                    `json:"violationLevel,omitempty"`
	FraudAlert        *rateLimitFraudPayload `json:"fraudAlert,omitempty"`
}

// ConsumeRateLimit: POST /api/internal/bot/setup/rate-limit/consume
// Bot calls this once per rate-limited slash-command invocation.
// Returns whether the call should proceed, and (when applicable) a
// fraud alert payload the bot should post to its alert channel.
func (h *Handler) ConsumeRateLimit(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var req rateLimitConsumeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !validateSnowflakeValue(req.DiscordID) {
		writeError(w, http.StatusBadRequest, "discordId must be a valid Discord snowflake")
		return
	}
	cmd := strings.TrimSpace(strings.ToLower(req.Command))
	if cmd == "" {
		writeError(w, http.StatusBadRequest, "command is required")
		return
	}
	cfg, ok := rateLimitCommandConfigs[cmd]
	if !ok {
		writeError(w, http.StatusBadRequest, "unknown rate-limit command")
		return
	}
	if req.GuildID != "" && !validateSnowflakeValue(req.GuildID) {
		writeError(w, http.StatusBadRequest, "guildId must be a valid Discord snowflake")
		return
	}

	limiter := ratelimit.New(h.redis.Client())
	detector := fraud.New(h.redis.Client())

	key := req.DiscordID + ":" + cmd
	res, err := limiter.Consume(r.Context(), key, cfg)
	if err != nil {
		log.Printf("[backend] rate-limit consume error user=%s cmd=%s: %v", req.DiscordID, cmd, err)
		writeError(w, http.StatusInternalServerError, "rate limit unavailable")
		return
	}

	resp := rateLimitConsumeResponse{
		Allowed:           res.Allowed,
		RetryAfterSeconds: res.RetryAfterSeconds,
		ViolationLevel:    res.ViolationLevel,
	}

	if res.Allowed {
		if fcfg, ok := fraudCommandConfigs[cmd]; ok {
			alert, err := detector.Record(r.Context(), req.GuildID, cmd, req.MemberCount, fcfg)
			if err != nil {
				log.Printf("[backend] fraud detector error guild=%s cmd=%s: %v", req.GuildID, cmd, err)
			} else if alert.Triggered {
				resp.FraudAlert = &rateLimitFraudPayload{
					GuildID:     alert.GuildID,
					Command:     alert.Command,
					Count:       alert.Count,
					MemberCount: alert.MemberCount,
					WindowSecs:  alert.WindowSecs,
				}
			}
		}
	}

	writeJSON(w, http.StatusOK, resp)
}
