package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"rustlens/backend/internal/appauth"
	"rustlens/backend/internal/store"
)

type ensureAppUserResp struct {
	UserID  string `json:"userId"`
	Created bool   `json:"created"`
}

func newEnsureTestHandler(t *testing.T) *Handler {
	t.Helper()
	dsn := os.Getenv("TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_URL not set")
	}
	ctx := context.Background()
	s, err := store.NewPostgresStore(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(s.Close)
	if err := s.EnsureSchema(ctx); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	if _, err := s.Pool().Exec(ctx, `TRUNCATE app_users RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	return &Handler{postgres: s, appauth: appauth.NewRepo(s.Pool())}
}

func TestEnsureAppUser_CreatesThenReturnsExisting(t *testing.T) {
	h := newEnsureTestHandler(t)

	body, _ := json.Marshal(map[string]any{
		"discordId":  "111222333444555666",
		"username":   "alice",
		"globalName": "Alice",
		"avatarHash": "deadbeef",
	})

	// First call: created=true.
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/internal/bot/setup/users/ensure", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	h.EnsureAppUser(rr, req)
	if rr.Code != 200 {
		t.Fatalf("first call: status=%d body=%s", rr.Code, rr.Body.String())
	}
	var resp ensureAppUserResp
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !resp.Created || resp.UserID == "" {
		t.Errorf("first call: want created=true userId!=\"\", got %+v", resp)
	}
	firstID := resp.UserID

	// Second call with same discordId: created=false, same userId.
	rr2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("POST", "/api/internal/bot/setup/users/ensure", bytes.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	h.EnsureAppUser(rr2, req2)
	if rr2.Code != 200 {
		t.Fatalf("second call: status=%d body=%s", rr2.Code, rr2.Body.String())
	}
	var resp2 ensureAppUserResp
	if err := json.Unmarshal(rr2.Body.Bytes(), &resp2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp2.Created {
		t.Errorf("second call: want created=false, got true")
	}
	if resp2.UserID != firstID {
		t.Errorf("second call: userId changed: first=%s second=%s", firstID, resp2.UserID)
	}
}

func TestEnsureAppUser_RejectsInvalidSnowflake(t *testing.T) {
	h := &Handler{}

	body, _ := json.Marshal(map[string]any{
		"discordId":  "not-a-snowflake",
		"username":   "alice",
		"globalName": "",
		"avatarHash": "",
	})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/internal/bot/setup/users/ensure", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	h.EnsureAppUser(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d body=%s", rr.Code, rr.Body.String())
	}
}
