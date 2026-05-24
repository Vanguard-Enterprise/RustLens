package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"rustlens/backend/internal/store"
)

func TestHasDiscordPRORoleUsesEntitlement(t *testing.T) {
	ts := newTestHandlerServer(t)
	ctx := context.Background()

	const userID = "usr_pro_handler"
	const discordID = "100000000000000001"
	if _, err := ts.pool.Exec(ctx, `
		INSERT INTO app_users (id, discord_id, discord_username, display_name)
		VALUES ($1, $2, 'pro_handler', 'PRO Handler')
	`, userID, discordID); err != nil {
		t.Fatalf("insert app user: %v", err)
	}
	if err := ts.h.postgres.GrantUserEntitlement(ctx, userID, store.EntitlementPRO, ""); err != nil {
		t.Fatalf("grant pro entitlement: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/internal/bot/setup/discord/"+discordID+"/has-pro-role", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("discordID", discordID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rr := httptest.NewRecorder()
	ts.h.HasDiscordPRORole(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%s", rr.Code, rr.Body.String())
	}
	var body struct {
		HasRole bool `json:"hasRole"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !body.HasRole {
		t.Fatal("expected hasRole=true for PRO entitlement")
	}
}
