package httpapi

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"rustlens/backend/internal/appauth"
	"rustlens/backend/internal/store"
)

const maxErrorCodeLen = 256
const maxOptionsBytes = 16 * 1024

var validInteractionTypes = map[string]struct{}{
	"command":       {},
	"button":        {},
	"modal":         {},
	"string_select": {},
}

var validOutcomes = map[string]struct{}{
	"success": {},
	"error":   {},
	"banned":  {},
	"denied":  {},
}

type recordCommandUsageRequest struct {
	DiscordID       string          `json:"discordId"`
	GuildID         *string         `json:"guildId,omitempty"`
	ChannelID       *string         `json:"channelId,omitempty"`
	InteractionType string          `json:"interactionType"`
	Command         string          `json:"command"`
	Subcommand      *string         `json:"subcommand,omitempty"`
	Target          *string         `json:"target,omitempty"`
	Options         json.RawMessage `json:"options"`
	Outcome         string          `json:"outcome"`
	ErrorCode       *string         `json:"errorCode,omitempty"`
	LatencyMs       int             `json:"latencyMs"`
}

// RecordCommandUsage: POST /api/internal/bot/setup/command-usage
//
// JWT-protected. Logs a single Discord interaction by the bot. Resolves
// app_user_id from discord_id (the bot's ensureAppUser hook guarantees the
// row exists by the time any handler runs). If the lookup misses (race),
// the write is silently dropped with a log line and 204 is returned —
// the bot's response path is never blocked.
func (h *Handler) RecordCommandUsage(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var req recordCommandUsageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !validateSnowflakeValue(req.DiscordID) {
		writeError(w, http.StatusBadRequest, "discordId must be a valid Discord snowflake")
		return
	}
	if _, ok := validInteractionTypes[req.InteractionType]; !ok {
		writeError(w, http.StatusBadRequest, "interactionType invalid")
		return
	}
	if _, ok := validOutcomes[req.Outcome]; !ok {
		writeError(w, http.StatusBadRequest, "outcome invalid")
		return
	}
	if req.Command == "" || len(req.Command) > 128 {
		writeError(w, http.StatusBadRequest, "command must be 1..128 chars")
		return
	}
	if req.LatencyMs < 0 {
		req.LatencyMs = 0
	}
	if len(req.Options) > maxOptionsBytes {
		writeError(w, http.StatusBadRequest, "options payload too large")
		return
	}
	if req.ErrorCode != nil && len(*req.ErrorCode) > maxErrorCodeLen {
		trimmed := (*req.ErrorCode)[:maxErrorCodeLen]
		req.ErrorCode = &trimmed
	}
	if h.appauth == nil {
		writeError(w, http.StatusInternalServerError, "appauth repo not configured")
		return
	}

	appUserID, err := h.appauth.FindByDiscordID(r.Context(), req.DiscordID)
	if err != nil {
		if errors.Is(err, appauth.ErrAppUserNotFound) {
			log.Printf("[backend] RecordCommandUsage: no app_users row for discord_id=%s (race); dropping write", req.DiscordID)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		log.Printf("[backend] RecordCommandUsage FindByDiscordID error for %s: %v", req.DiscordID, err)
		writeError(w, http.StatusInternalServerError, "failed to resolve user")
		return
	}

	if err := h.postgres.InsertCommandUsage(r.Context(), store.CommandUsageInsert{
		AppUserID:       appUserID,
		DiscordID:       req.DiscordID,
		GuildID:         req.GuildID,
		ChannelID:       req.ChannelID,
		InteractionType: req.InteractionType,
		Command:         req.Command,
		Subcommand:      req.Subcommand,
		Target:          req.Target,
		Options:         req.Options,
		Outcome:         req.Outcome,
		ErrorCode:       req.ErrorCode,
		LatencyMs:       req.LatencyMs,
	}); err != nil {
		log.Printf("[backend] RecordCommandUsage insert error: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to record usage")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
