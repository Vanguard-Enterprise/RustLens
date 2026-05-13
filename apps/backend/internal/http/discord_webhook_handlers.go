package httpapi

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"

	"rustlens/backend/internal/store"
)

// Discord Webhook Events (Dev Portal "Events" tab). Two payload types share
// this endpoint:
//   type=0 PING       — Discord verifies the endpoint at registration.
//   type=1 EVENT      — APPLICATION_AUTHORIZED / APPLICATION_DEAUTHORIZED.
//
// Auth is ed25519 over (timestamp || raw body) using the application's public
// key from the Dev Portal — same scheme as interactions.

const (
	discordWebhookTypePing  = 0
	discordWebhookTypeEvent = 1

	discordIntegrationTypeUser = 1

	eventAppAuthorized   = "APPLICATION_AUTHORIZED"
	eventAppDeauthorized = "APPLICATION_DEAUTHORIZED"
)

type discordWebhookPayload struct {
	Version       int                  `json:"version"`
	ApplicationID string               `json:"application_id"`
	Type          int                  `json:"type"`
	Event         *discordWebhookEvent `json:"event,omitempty"`
}

type discordWebhookEvent struct {
	Type      string          `json:"type"`
	Timestamp string          `json:"timestamp"`
	Data      json.RawMessage `json:"data"`
}

type discordWebhookUser struct {
	ID         string  `json:"id"`
	Username   string  `json:"username"`
	GlobalName *string `json:"global_name,omitempty"`
	Avatar     *string `json:"avatar,omitempty"`
}

type appAuthorizedData struct {
	IntegrationType *int               `json:"integration_type,omitempty"`
	Scopes          []string           `json:"scopes"`
	User            discordWebhookUser `json:"user"`
}

type appDeauthorizedData struct {
	User discordWebhookUser `json:"user"`
}

// DiscordWebhookEvents handles Discord's "Webhook Events" POSTs at
// /api/discord/webhook-events. Public route — auth is signature-based.
func (h *Handler) DiscordWebhookEvents(w http.ResponseWriter, r *http.Request) {
	if h.cfg.DiscordAppPublicKey == "" {
		writeError(w, http.StatusServiceUnavailable, "discord webhook public key not configured")
		return
	}
	pubKey, err := hex.DecodeString(h.cfg.DiscordAppPublicKey)
	if err != nil || len(pubKey) != ed25519.PublicKeySize {
		log.Printf("[backend] discord webhook: invalid DISCORD_APP_PUBLIC_KEY")
		writeError(w, http.StatusServiceUnavailable, "discord webhook public key invalid")
		return
	}

	sigHeader := r.Header.Get("X-Signature-Ed25519")
	tsHeader := r.Header.Get("X-Signature-Timestamp")
	if sigHeader == "" || tsHeader == "" {
		writeError(w, http.StatusUnauthorized, "missing signature headers")
		return
	}
	sig, err := hex.DecodeString(sigHeader)
	if err != nil || len(sig) != ed25519.SignatureSize {
		writeError(w, http.StatusUnauthorized, "invalid signature encoding")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read body")
		return
	}

	var signedMsg bytes.Buffer
	signedMsg.WriteString(tsHeader)
	signedMsg.Write(body)
	if !ed25519.Verify(pubKey, signedMsg.Bytes(), sig) {
		writeError(w, http.StatusUnauthorized, "invalid signature")
		return
	}

	var p discordWebhookPayload
	if err := json.Unmarshal(body, &p); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}

	// PING handshake — respond 204 so Discord considers the endpoint valid.
	if p.Type == discordWebhookTypePing {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if p.Type != discordWebhookTypeEvent || p.Event == nil {
		// Unknown / unsupported payload — ack so Discord stops retrying.
		w.WriteHeader(http.StatusNoContent)
		return
	}

	switch p.Event.Type {
	case eventAppAuthorized:
		var d appAuthorizedData
		if err := json.Unmarshal(p.Event.Data, &d); err != nil {
			writeError(w, http.StatusBadRequest, "invalid authorized data")
			return
		}
		// Only user installs land here. Guild installs come through the bot
		// gateway flow (UpsertBotGuild).
		if d.IntegrationType == nil || *d.IntegrationType != discordIntegrationTypeUser {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if !validateSnowflakeValue(d.User.ID) {
			writeError(w, http.StatusBadRequest, "user.id must be a Discord snowflake")
			return
		}
		if err := h.postgres.UpsertBotUserInstall(r.Context(), store.UpsertBotUserInstallInput{
			DiscordID:  d.User.ID,
			Username:   d.User.Username,
			GlobalName: d.User.GlobalName,
			AvatarHash: d.User.Avatar,
			Scopes:     d.Scopes,
		}); err != nil {
			log.Printf("[backend] UpsertBotUserInstall error for %s: %v", d.User.ID, err)
			writeError(w, http.StatusInternalServerError, "failed to upsert user install")
			return
		}

	case eventAppDeauthorized:
		var d appDeauthorizedData
		if err := json.Unmarshal(p.Event.Data, &d); err != nil {
			writeError(w, http.StatusBadRequest, "invalid deauthorized data")
			return
		}
		if !validateSnowflakeValue(d.User.ID) {
			writeError(w, http.StatusBadRequest, "user.id must be a Discord snowflake")
			return
		}
		if err := h.postgres.MarkBotUserInstallRemoved(r.Context(), d.User.ID); err != nil {
			log.Printf("[backend] MarkBotUserInstallRemoved error for %s: %v", d.User.ID, err)
			writeError(w, http.StatusInternalServerError, "failed to mark user install removed")
			return
		}

	default:
		// Unknown event — ack to stop retries.
	}

	w.WriteHeader(http.StatusNoContent)
}
