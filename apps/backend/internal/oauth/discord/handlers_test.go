package discord_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"rustlens/backend/internal/appauth"
	"rustlens/backend/internal/oauth/discord"
	"rustlens/backend/internal/store"
)

func TestInitiate_RedirectsToDiscord_SetsCookies(t *testing.T) {
	h := discord.NewHandler(discord.HandlerConfig{
		ClientID:      "the-client-id",
		AppOrigin:     "http://localhost",
		SessionSecret: "session-secret",
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/auth/discord", nil)
	h.Initiate(w, r)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", w.Code)
	}
	loc, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}
	if loc.Host != "discord.com" || loc.Path != "/oauth2/authorize" {
		t.Errorf("Location host/path: %s%s", loc.Host, loc.Path)
	}
	q := loc.Query()
	for _, want := range []string{"client_id", "response_type", "redirect_uri", "scope", "state", "code_challenge", "code_challenge_method"} {
		if q.Get(want) == "" {
			t.Errorf("missing query param %q", want)
		}
	}
	if q.Get("client_id") != "the-client-id" {
		t.Errorf("client_id = %q", q.Get("client_id"))
	}
	if q.Get("redirect_uri") != "http://localhost/api/auth/discord/callback" {
		t.Errorf("redirect_uri = %q", q.Get("redirect_uri"))
	}
	if q.Get("scope") != "identify guilds" {
		t.Errorf("scope = %q", q.Get("scope"))
	}
	if q.Get("code_challenge_method") != "S256" {
		t.Errorf("code_challenge_method = %q", q.Get("code_challenge_method"))
	}

	cookies := w.Result().Cookies()
	names := map[string]string{}
	for _, c := range cookies {
		names[c.Name] = c.Value
	}
	for _, want := range []string{discord.CookieState, discord.CookieVerifier} {
		if names[want] == "" {
			t.Errorf("missing cookie %q", want)
		}
	}
}

func TestInitiate_RefusesWhenSessionPresent(t *testing.T) {
	tok := mustSerializeSessionForTest(t, "session-secret", "usr_already")
	h := discord.NewHandler(discord.HandlerConfig{
		ClientID:      "id",
		AppOrigin:     "http://localhost",
		SessionSecret: "session-secret",
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/auth/discord", nil)
	r.AddCookie(&http.Cookie{Name: "rustlens_session", Value: tok})
	h.Initiate(w, r)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "/login?error=discord-already-signed-in") {
		t.Errorf("Location = %q, want ?error=discord-already-signed-in", loc)
	}
}

func discordTestPool(t *testing.T) (*pgxpool.Pool, func()) {
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
	if err := s.EnsureSchema(ctx); err != nil {
		s.Close()
		t.Fatalf("schema: %v", err)
	}
	return s.Pool(), s.Close
}

// fakeDiscord returns an httptest server that pretends to be Discord. The
// returned profile is whatever the caller wires in.
func fakeDiscord(t *testing.T, profileJSON string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/oauth2/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"tok","token_type":"Bearer"}`))
	})
	mux.HandleFunc("/api/users/@me", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(profileJSON))
	})
	return httptest.NewServer(mux)
}

// fakeDiscordTokenFails simulates a failed token exchange with Discord.
func fakeDiscordTokenFails(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/oauth2/token", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
	})
	return httptest.NewServer(mux)
}

func newCallbackRequest(state, verifier, code string) *http.Request {
	r := httptest.NewRequest("GET", "/api/auth/discord/callback?code="+code+"&state="+state, nil)
	r.AddCookie(&http.Cookie{Name: discord.CookieState, Value: state})
	r.AddCookie(&http.Cookie{Name: discord.CookieVerifier, Value: verifier})
	return r
}

func newCallbackHandler(t *testing.T, pool *pgxpool.Pool, baseURL string) *discord.Handler {
	t.Helper()
	repo := appauth.NewRepo(pool)
	c := discord.NewClient("id", "secret",
		discord.WithBaseURL(baseURL),
		discord.WithRedirectURI("http://localhost/api/auth/discord/callback"))
	return discord.NewHandler(discord.HandlerConfig{
		ClientID:      "id",
		ClientSecret:  "secret",
		AppOrigin:     "http://localhost",
		SessionSecret: "session-secret",
		Repo:          repo,
		Pool:          pool,
		Client:        c,
	})
}

func TestCallback_StateMismatch(t *testing.T) {
	pool, cleanup := discordTestPool(t)
	defer cleanup()
	srv := fakeDiscord(t, `{}`)
	defer srv.Close()
	h := newCallbackHandler(t, pool, srv.URL)

	w := httptest.NewRecorder()
	r := newCallbackRequest("client-state", "v", "code")
	r.URL.RawQuery = "code=code&state=other-state"
	h.Callback(w, r)
	if !strings.Contains(w.Header().Get("Location"), "discord-state") {
		t.Errorf("expected discord-state error, got %q", w.Header().Get("Location"))
	}
}

func TestCallback_TokenExchangeFailure(t *testing.T) {
	pool, cleanup := discordTestPool(t)
	defer cleanup()
	srv := fakeDiscordTokenFails(t)
	defer srv.Close()
	h := newCallbackHandler(t, pool, srv.URL)

	w := httptest.NewRecorder()
	r := newCallbackRequest("s", "v", "code")
	h.Callback(w, r)
	if !strings.Contains(w.Header().Get("Location"), "discord-token") {
		t.Errorf("expected discord-token error, got %q", w.Header().Get("Location"))
	}
}

func TestCallback_FindOrCreateUpsertsUser(t *testing.T) {
	pool, cleanup := discordTestPool(t)
	defer cleanup()
	ctx := context.Background()
	const discordID = "d_callback_signin_e2e"
	if _, err := pool.Exec(ctx, `DELETE FROM app_users WHERE discord_id=$1`, discordID); err != nil {
		t.Fatalf("clean: %v", err)
	}

	srv := fakeDiscord(t, `{"id":"`+discordID+`","username":"newbie","global_name":"Newbie","avatar":"abc123"}`)
	defer srv.Close()
	h := newCallbackHandler(t, pool, srv.URL)

	w := httptest.NewRecorder()
	r := newCallbackRequest("s", "v", "code")
	h.Callback(w, r)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d", w.Code)
	}
	// Success path: redirect to /dashboard with a session cookie.
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "/dashboard") {
		t.Errorf("expected redirect to /dashboard, got %q", loc)
	}
	foundSessionCookie := false
	for _, c := range w.Result().Cookies() {
		if c.Name == "rustlens_session" && c.Value != "" {
			foundSessionCookie = true
			break
		}
	}
	if !foundSessionCookie {
		t.Errorf("expected session cookie to be set, got cookies %v", w.Result().Cookies())
	}
	var username, globalName, avatar string
	if err := pool.QueryRow(ctx,
		`SELECT discord_username, COALESCE(discord_global_name,''), COALESCE(discord_avatar,'')
		 FROM app_users WHERE discord_id=$1`, discordID).Scan(&username, &globalName, &avatar); err != nil {
		t.Fatalf("read user row: %v", err)
	}
	if username != "newbie" {
		t.Errorf("discord_username = %q, want newbie", username)
	}
	if globalName != "Newbie" {
		t.Errorf("discord_global_name = %q, want Newbie", globalName)
	}
	if avatar != "abc123" {
		t.Errorf("discord_avatar = %q, want abc123 (raw hash)", avatar)
	}
}
