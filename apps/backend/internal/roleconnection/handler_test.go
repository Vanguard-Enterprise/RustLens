package roleconnection

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"rustlens/backend/internal/appauth"
	"rustlens/backend/internal/oauth/discord"
	"rustlens/backend/internal/store"
)

func TestHandler_Callback_HappyPath(t *testing.T) {
	discordSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/oauth2/token":
			_, _ = w.Write([]byte(`{
				"access_token":"at","refresh_token":"rt","expires_in":604800,
				"token_type":"Bearer","scope":"identify role_connections.write"
			}`))
		case "/api/users/@me":
			_, _ = w.Write([]byte(`{"id":"d1","username":"sem","global_name":"Sem","avatar":null}`))
		case "/api/users/@me/applications/APP/role-connection":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		default:
			t.Errorf("unexpected discord call: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer discordSrv.Close()

	cipher := TokenCipher{Crypto: newTestCrypto(t)}
	s := &fakeStore{displayName: "Sem"}
	disc := discord.NewClient("cid", "csec",
		discord.WithBaseURL(discordSrv.URL),
		discord.WithRedirectURI("http://localhost/api/auth/discord/role-connection/callback"),
	)
	syncer := NewSyncer(SyncerConfig{
		Store: s, Discord: disc, ApplicationID: "APP", Cipher: cipher,
	})
	h := NewHandler(HandlerConfig{
		ClientID: "cid", ClientSecret: "csec",
		AppOrigin: "http://localhost", Syncer: syncer, DiscordAPI: disc,
	})

	req := httptest.NewRequest(http.MethodGet,
		"/api/auth/discord/role-connection/callback?code=ok&state=s1", nil)
	req.AddCookie(&http.Cookie{Name: "rl_rc_state", Value: "s1"})
	req.AddCookie(&http.Cookie{Name: "rl_rc_return", Value: "dashboard"})
	ctx := appauth.InjectSessionForTest(req.Context(), &appauth.SessionUser{UserID: "u1", DiscordID: "d1"})
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	h.Callback(rec, req)
	if rec.Code != http.StatusFound {
		t.Errorf("status = %d, want 302", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "/dashboard?notice=discord-linked") {
		t.Errorf("loc = %s", loc)
	}
	if s.upsertCalled != 1 {
		t.Errorf("expected 1 upsert, got %d", s.upsertCalled)
	}
}

func TestHandler_Callback_DiscordIDMismatch(t *testing.T) {
	discordSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/oauth2/token":
			_, _ = w.Write([]byte(`{"access_token":"at","refresh_token":"rt","expires_in":604800,"token_type":"Bearer","scope":"identify role_connections.write"}`))
		case "/api/users/@me":
			_, _ = w.Write([]byte(`{"id":"OTHER","username":"x","global_name":"X","avatar":null}`))
		}
	}))
	defer discordSrv.Close()
	cipher := TokenCipher{Crypto: newTestCrypto(t)}
	s := &fakeStore{}
	disc := discord.NewClient("cid", "csec",
		discord.WithBaseURL(discordSrv.URL),
		discord.WithRedirectURI("http://localhost/api/auth/discord/role-connection/callback"),
	)
	syncer := NewSyncer(SyncerConfig{Store: s, Discord: disc, ApplicationID: "APP", Cipher: cipher})
	h := NewHandler(HandlerConfig{
		ClientID: "cid", ClientSecret: "csec", AppOrigin: "http://localhost",
		Syncer: syncer, DiscordAPI: disc,
	})
	req := httptest.NewRequest(http.MethodGet,
		"/api/auth/discord/role-connection/callback?code=ok&state=s1", nil)
	req.AddCookie(&http.Cookie{Name: "rl_rc_state", Value: "s1"})
	req.AddCookie(&http.Cookie{Name: "rl_rc_return", Value: "dashboard"})
	ctx := appauth.InjectSessionForTest(req.Context(), &appauth.SessionUser{UserID: "u1", DiscordID: "d1"})
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	h.Callback(rec, req)
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "error=discord-mismatch") {
		t.Errorf("loc = %s", loc)
	}
	if s.upsertCalled != 0 {
		t.Errorf("upsert should not be called on mismatch, got %d", s.upsertCalled)
	}
}

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
	var sawState bool
	for _, c := range cookies {
		if c.Name == "rl_rc_state" {
			sawState = true
		}
	}
	if !sawState {
		t.Errorf("missing state cookie: %v", cookies)
	}
}

func TestHandler_Disconnect_DeletesRowAndPushesEmpty(t *testing.T) {
	var pushCalled int
	discordSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/users/@me/applications/APP/role-connection":
			pushCalled++
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer discordSrv.Close()
	cipher := TokenCipher{Crypto: newTestCrypto(t)}
	at, atNonce, _ := cipher.Encrypt("at")
	rt, rtNonce, _ := cipher.Encrypt("rt")
	s := &fakeStore{
		row: &store.DiscordRoleConnection{
			UserID: "u1", DiscordID: "d1",
			AccessTokenCT: at, AccessTokenNonce: atNonce,
			RefreshTokenCT: rt, RefreshTokenNonce: rtNonce,
			AccessExpiresAt: time.Now().Add(time.Hour),
			Scopes:          "identify role_connections.write",
		},
		displayName: "sem",
	}
	disc := discord.NewClient("cid", "csec", discord.WithBaseURL(discordSrv.URL))
	syncer := NewSyncer(SyncerConfig{Store: s, Discord: disc, ApplicationID: "APP", Cipher: cipher})
	h := NewHandler(HandlerConfig{Syncer: syncer, DiscordAPI: disc, AppOrigin: "http://localhost"})

	req := httptest.NewRequest(http.MethodDelete, "/api/app/me/discord-role-connection", nil)
	ctx := appauth.InjectSessionForTest(req.Context(), &appauth.SessionUser{UserID: "u1", DiscordID: "d1"})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	h.Disconnect(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d", rec.Code)
	}
	if s.deleteCalled != 1 {
		t.Errorf("delete called %d times", s.deleteCalled)
	}
	if pushCalled != 1 {
		t.Errorf("expected best-effort empty push, got %d", pushCalled)
	}
}
