package httpapi

import (
	"encoding/json"
	"log"
	"net/http"

	"rustlens/backend/internal/store"
)

type botUserInstallSeenRequest struct {
	DiscordID  string  `json:"discordId"`
	Username   string  `json:"username,omitempty"`
	GlobalName *string `json:"globalName,omitempty"`
	AvatarHash *string `json:"avatarHash,omitempty"`
}

// TouchBotUserInstallSeen: POST /api/internal/bot/setup/bot-user-installs/seen
// Called by the bot whenever it processes an interaction whose
// authorizing_integration_owners["1"] equals a given user — confirms the
// user-app install is still active and bumps last_seen_at. The bot also
// forwards the user's username/global_name/avatar from the interaction so the
// row gets populated even if we never received an APPLICATION_AUTHORIZED
// webhook for this user. Idempotent.
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
	if err := h.postgres.TouchBotUserInstallSeen(r.Context(), store.TouchBotUserInstallSeenInput{
		DiscordID:  req.DiscordID,
		Username:   req.Username,
		GlobalName: req.GlobalName,
		AvatarHash: req.AvatarHash,
	}); err != nil {
		log.Printf("[backend] TouchBotUserInstallSeen error for %s: %v", req.DiscordID, err)
		writeError(w, http.StatusInternalServerError, "failed to touch user install")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
