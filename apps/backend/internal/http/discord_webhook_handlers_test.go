package httpapi

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"rustlens/backend/internal/config"
)

// freshTimestamp returns the current Unix timestamp as a string. The webhook
// handler rejects timestamps outside a ±5 minute window so test signatures
// must be re-signed against "now".
func freshTimestamp() string {
	return strconv.FormatInt(time.Now().Unix(), 10)
}

func newHandlerWithDiscordKey(t *testing.T) (*Handler, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate ed25519 key: %v", err)
	}
	return &Handler{cfg: config.Config{DiscordAppPublicKey: hex.EncodeToString(pub)}}, priv
}

func signed(t *testing.T, priv ed25519.PrivateKey, ts string, body []byte) string {
	t.Helper()
	var msg bytes.Buffer
	msg.WriteString(ts)
	msg.Write(body)
	return hex.EncodeToString(ed25519.Sign(priv, msg.Bytes()))
}

func TestDiscordWebhookEvents_AcksPingWithValidSignature(t *testing.T) {
	h, priv := newHandlerWithDiscordKey(t)

	body := []byte(`{"version":1,"application_id":"123","type":0}`)
	ts := freshTimestamp()
	sig := signed(t, priv, ts, body)

	req := httptest.NewRequest(http.MethodPost, "/api/discord/webhook-events", bytes.NewReader(body))
	req.Header.Set("X-Signature-Ed25519", sig)
	req.Header.Set("X-Signature-Timestamp", ts)
	w := httptest.NewRecorder()
	h.DiscordWebhookEvents(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", w.Code)
	}
}

func TestDiscordWebhookEvents_RejectsBadSignature(t *testing.T) {
	h, _ := newHandlerWithDiscordKey(t)

	body := []byte(`{"version":1,"application_id":"123","type":0}`)
	req := httptest.NewRequest(http.MethodPost, "/api/discord/webhook-events", bytes.NewReader(body))
	// Valid hex shape (64 bytes = 128 hex chars) but signed by the wrong key.
	req.Header.Set("X-Signature-Ed25519",
		"00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000")
	req.Header.Set("X-Signature-Timestamp", "1700000000")
	w := httptest.NewRecorder()
	h.DiscordWebhookEvents(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestDiscordWebhookEvents_RejectsMissingHeaders(t *testing.T) {
	h, _ := newHandlerWithDiscordKey(t)

	body := []byte(`{}`)
	req := httptest.NewRequest(http.MethodPost, "/api/discord/webhook-events", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.DiscordWebhookEvents(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestDiscordWebhookEvents_FailsWithoutPublicKey(t *testing.T) {
	h := &Handler{} // no DiscordAppPublicKey configured

	body := []byte(`{}`)
	req := httptest.NewRequest(http.MethodPost, "/api/discord/webhook-events", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.DiscordWebhookEvents(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}
}

func TestDiscordWebhookEvents_IgnoresGuildInstallAuthorizedEvent(t *testing.T) {
	h, priv := newHandlerWithDiscordKey(t)

	// integration_type=0 (guild install) — should be ack'd 204 without
	// touching the DB. With a nil postgres this is the only way the test
	// won't panic.
	body := []byte(`{
		"version":1,"application_id":"123","type":1,
		"event":{
			"type":"APPLICATION_AUTHORIZED","timestamp":"2026-01-01T00:00:00Z",
			"data":{"integration_type":0,"scopes":["applications.commands"],
			        "user":{"id":"123456789012345678","username":"x"},
			        "guild":{"id":"999","name":"g","icon":null}}
		}
	}`)
	ts := freshTimestamp()
	sig := signed(t, priv, ts, body)

	req := httptest.NewRequest(http.MethodPost, "/api/discord/webhook-events", bytes.NewReader(body))
	req.Header.Set("X-Signature-Ed25519", sig)
	req.Header.Set("X-Signature-Timestamp", ts)
	w := httptest.NewRecorder()
	h.DiscordWebhookEvents(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", w.Code)
	}
}
