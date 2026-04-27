package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestUpsertBotGuild_RejectsInvalidGuildID(t *testing.T) {
	h := &Handler{}
	body, _ := json.Marshal(map[string]any{"guildId": "not-a-snowflake", "name": "x"})
	req := httptest.NewRequest(http.MethodPost, "/api/internal/bot/setup/bot-guilds/upsert", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.UpsertBotGuild(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestMarkBotGuildLeft_RejectsInvalidGuildID(t *testing.T) {
	h := &Handler{}
	body, _ := json.Marshal(map[string]any{"guildId": "abc"})
	req := httptest.NewRequest(http.MethodPost, "/api/internal/bot/setup/bot-guilds/leave", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.MarkBotGuildLeft(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestReconcileBotGuilds_RejectsBadShardCount(t *testing.T) {
	h := &Handler{}
	body, _ := json.Marshal(map[string]any{"shardId": 0, "shardCount": 0, "guildIds": []string{}})
	req := httptest.NewRequest(http.MethodPost, "/api/internal/bot/setup/bot-guilds/reconcile", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ReconcileBotGuilds(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}
