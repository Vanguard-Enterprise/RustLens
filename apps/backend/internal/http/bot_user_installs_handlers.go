package httpapi

import (
	"encoding/json"
	"log"
	"net/http"
)

type botUserInstallSeenRequest struct {
	DiscordID string `json:"discordId"`
}

// TouchBotUserInstallSeen: POST /api/internal/bot/setup/bot-user-installs/seen
// Called by the bot whenever it processes an interaction whose
// authorizing_integration_owners["1"] equals a given user — confirms the
// user-app install is still active and bumps last_seen_at. Idempotent.
func (h *Handler) TouchBotUserInstallSeen(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var req botUserInstallSeenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !validateSnowflakeValue(req.DiscordID) {
		writeError(w, http.StatusBadRequest, "discordId must be a valid Discord snowflake")
		return
	}
	if err := h.postgres.TouchBotUserInstallSeen(r.Context(), req.DiscordID); err != nil {
		log.Printf("[backend] TouchBotUserInstallSeen error for %s: %v", req.DiscordID, err)
		writeError(w, http.StatusInternalServerError, "failed to touch user install")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
