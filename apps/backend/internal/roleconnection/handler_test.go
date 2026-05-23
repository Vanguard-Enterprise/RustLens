package roleconnection

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"rustlens/backend/internal/appauth"
)

func TestHandler_Initiate_RequiresSession(t *testing.T) {
	h := NewHandler(HandlerConfig{
		ClientID: "cid", AppOrigin: "http://localhost",
	})
	req := httptest.NewRequest(http.MethodGet, "/api/auth/discord/role-connection", nil)
	rec := httptest.NewRecorder()
	h.Initiate(rec, req)
	if rec.Code != http.StatusFound {
		t.Errorf("status = %d, want 302", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "/login") {
		t.Errorf("expected redirect to /login, got %s", loc)
	}
}

func TestHandler_Initiate_WithSession_RedirectsToDiscord(t *testing.T) {
	h := NewHandler(HandlerConfig{
		ClientID: "cid", AppOrigin: "http://localhost",
	})
	req := httptest.NewRequest(http.MethodGet, "/api/auth/discord/role-connection", nil)
	ctx := appauth.InjectSessionForTest(context.Background(), &appauth.SessionUser{UserID: "uid-1"})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	h.Initiate(rec, req)
	if rec.Code != http.StatusFound {
		t.Errorf("status = %d, want 302", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "https://discord.com/oauth2/authorize") {
		t.Errorf("expected discord authorize URL, got %s", loc)
	}
	if !strings.Contains(loc, "scope=identify+role_connections.write") &&
		!strings.Contains(loc, "scope=identify%20role_connections.write") {
		t.Errorf("expected scopes in URL, got %s", loc)
	}
	cookies := rec.Result().Cookies()
	var sawState, sawVerifier bool
	for _, c := range cookies {
		if c.Name == "rl_rc_state" {
			sawState = true
		}
		if c.Name == "rl_rc_verifier" {
			sawVerifier = true
		}
	}
	if !sawState || !sawVerifier {
		t.Errorf("missing state/verifier cookies: %v", cookies)
	}
}
