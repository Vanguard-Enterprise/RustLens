package httpapi

import (
	"encoding/json"
	"log"
	"net/http"

	"rustlens/backend/internal/appauth"
)

type ensureAppUserRequest struct {
	DiscordID  string `json:"discordId"`
	Username   string `json:"username"`
	GlobalName string `json:"globalName"`
	AvatarHash string `json:"avatarHash"`
}

// EnsureAppUser: POST /api/internal/bot/setup/users/ensure
//
// Idempotent. Inserts an app_users row keyed on Discord ID if one does not
// exist, otherwise refreshes cached profile fields and returns the existing
// row. Called by the bot at the top of every interaction so that every
// Discord user who touches the platform has a stable RustLens user id.
func (h *Handler) EnsureAppUser(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var req ensureAppUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !validateSnowflakeValue(req.DiscordID) {
		writeError(w, http.StatusBadRequest, "discordId must be a valid Discord snowflake")
		return
	}
	if h.appauth == nil {
		writeError(w, http.StatusInternalServerError, "appauth repo not configured")
		return
	}
	userID, created, err := h.appauth.FindOrCreateByDiscordID(r.Context(), appauth.DiscordProfile{
		ID:         req.DiscordID,
		Username:   req.Username,
		GlobalName: req.GlobalName,
		AvatarHash: req.AvatarHash,
	})
	if err != nil {
		log.Printf("[backend] EnsureAppUser FindOrCreateByDiscordID error for %s: %v", req.DiscordID, err)
		writeError(w, http.StatusInternalServerError, "failed to ensure user")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"userId":  userID,
		"created": created,
	})
}
