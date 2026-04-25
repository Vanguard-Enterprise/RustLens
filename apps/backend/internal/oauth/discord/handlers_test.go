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
	if q.Get("scope") != "identify email" {
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
	for _, want := range []string{discord.CookieState, discord.CookieVerifier, discord.CookieIntent} {
		if names[want] == "" {
			t.Errorf("missing cookie %q", want)
		}
	}
	if names[discord.CookieIntent] != discord.IntentSignin {
		t.Errorf("intent cookie = %q", names[discord.CookieIntent])
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

func TestInitiateLink_RequiresSession(t *testing.T) {
	h := discord.NewHandler(discord.HandlerConfig{
		ClientID:      "id",
		AppOrigin:     "http://localhost",
		SessionSecret: "session-secret",
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/auth/discord/link", nil)
	h.InitiateLink(w, r)
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d", w.Code)
	}
	if got := w.Header().Get("Location"); !strings.Contains(got, "/login?error=session") {
		t.Errorf("Location = %q", got)
	}
}

func TestInitiateLink_SetsIntentLink(t *testing.T) {
	tok := mustSerializeSessionForTest(t, "session-secret", "usr_link")
	h := discord.NewHandler(discord.HandlerConfig{
		ClientID:      "id",
		AppOrigin:     "http://localhost",
		SessionSecret: "session-secret",
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/auth/discord/link", nil)
	r.AddCookie(&http.Cookie{Name: "rustlens_session", Value: tok})
	h.InitiateLink(w, r)
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d", w.Code)
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == discord.CookieIntent && c.Value == discord.IntentLink {
			return
		}
	}
	t.Errorf("expected intent=link cookie")
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

func newCallbackRequest(state, verifier, intent, code string) *http.Request {
	r := httptest.NewRequest("GET", "/api/auth/discord/callback?code="+code+"&state="+state, nil)
	r.AddCookie(&http.Cookie{Name: discord.CookieState, Value: state})
	r.AddCookie(&http.Cookie{Name: discord.CookieVerifier, Value: verifier})
	r.AddCookie(&http.Cookie{Name: discord.CookieIntent, Value: intent})
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

func TestCallback_Signin_StateMismatch(t *testing.T) {
	pool, cleanup := discordTestPool(t)
	defer cleanup()
	srv := fakeDiscord(t, `{}`)
	defer srv.Close()
	h := newCallbackHandler(t, pool, srv.URL)

	w := httptest.NewRecorder()
	r := newCallbackRequest("client-state", "v", discord.IntentSignin, "code")
	r.URL.RawQuery = "code=code&state=other-state"
	h.Callback(w, r)
	if !strings.Contains(w.Header().Get("Location"), "discord-state") {
		t.Errorf("expected discord-state error, got %q", w.Header().Get("Location"))
	}
}

func TestCallback_Signin_NoEmail(t *testing.T) {
	pool, cleanup := discordTestPool(t)
	defer cleanup()
	srv := fakeDiscord(t, `{"id":"d_no_email_1","username":"u","verified":false}`)
	defer srv.Close()
	h := newCallbackHandler(t, pool, srv.URL)

	w := httptest.NewRecorder()
	r := newCallbackRequest("s", "v", discord.IntentSignin, "code")
	h.Callback(w, r)
	if !strings.Contains(w.Header().Get("Location"), "discord-no-email") {
		t.Errorf("expected discord-no-email, got %q", w.Header().Get("Location"))
	}
}

func TestCallback_Signin_ExistingDiscordRefreshesAndMintsSession(t *testing.T) {
	pool, cleanup := discordTestPool(t)
	defer cleanup()
	ctx := context.Background()
	const userID = "usr_callback_existing"
	if _, err := pool.Exec(ctx,
		`INSERT INTO app_users (id, display_name) VALUES ($1, 'Existing') ON CONFLICT (id) DO NOTHING`, userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO user_connected_accounts (user_id, provider, provider_account_id, username)
		 VALUES ($1, 'discord', 'd_callback_existing', 'old')
		 ON CONFLICT (provider, provider_account_id) DO UPDATE SET username='old'`, userID); err != nil {
		t.Fatalf("seed conn: %v", err)
	}
	srv := fakeDiscord(t, `{"id":"d_callback_existing","username":"new_name","global_name":"New","email":"e@x.com","verified":true,"avatar":"abc"}`)
	defer srv.Close()
	h := newCallbackHandler(t, pool, srv.URL)

	w := httptest.NewRecorder()
	r := newCallbackRequest("s", "v", discord.IntentSignin, "code")
	h.Callback(w, r)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d", w.Code)
	}
	if got := w.Header().Get("Location"); !strings.HasSuffix(got, "/dashboard") {
		t.Errorf("Location = %q, want suffix /dashboard", got)
	}
	var sessionFound bool
	for _, c := range w.Result().Cookies() {
		if c.Name == "rustlens_session" && c.Value != "" {
			sessionFound = true
		}
	}
	if !sessionFound {
		t.Errorf("expected rustlens_session cookie set")
	}
	var u string
	if err := pool.QueryRow(ctx,
		`SELECT username FROM user_connected_accounts WHERE provider='discord' AND provider_account_id='d_callback_existing'`).Scan(&u); err != nil {
		t.Fatalf("read: %v", err)
	}
	if u != "new_name" {
		t.Errorf("username not refreshed: %q", u)
	}
}

func TestCallback_Signin_AutoLinkVerifiedEmail(t *testing.T) {
	pool, cleanup := discordTestPool(t)
	defer cleanup()
	ctx := context.Background()
	const userID = "usr_callback_autolink"
	if _, err := pool.Exec(ctx,
		`INSERT INTO app_users (id, display_name) VALUES ($1, 'AutoLink') ON CONFLICT (id) DO NOTHING`, userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO user_emails (email, user_id, is_primary, is_verified)
		 VALUES ('autolink@example.com', $1, true, true)
		 ON CONFLICT (email) DO UPDATE SET user_id=$1, is_verified=true`, userID); err != nil {
		t.Fatalf("seed email: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`DELETE FROM user_connected_accounts WHERE provider='discord' AND provider_account_id='d_autolink'`); err != nil {
		t.Fatalf("clean: %v", err)
	}
	srv := fakeDiscord(t, `{"id":"d_autolink","username":"al","email":"autolink@example.com","verified":true}`)
	defer srv.Close()
	h := newCallbackHandler(t, pool, srv.URL)

	w := httptest.NewRecorder()
	r := newCallbackRequest("s", "v", discord.IntentSignin, "code")
	h.Callback(w, r)
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d", w.Code)
	}
	if !strings.HasSuffix(w.Header().Get("Location"), "/dashboard") {
		t.Errorf("Location = %q", w.Header().Get("Location"))
	}
	var linkedUser string
	if err := pool.QueryRow(ctx,
		`SELECT user_id FROM user_connected_accounts WHERE provider='discord' AND provider_account_id='d_autolink'`).Scan(&linkedUser); err != nil {
		t.Fatalf("read: %v", err)
	}
	if linkedUser != userID {
		t.Errorf("auto-link target = %q, want %q", linkedUser, userID)
	}
}

func TestCallback_Signin_EmailInUse_Unverified(t *testing.T) {
	pool, cleanup := discordTestPool(t)
	defer cleanup()
	ctx := context.Background()
	const userID = "usr_callback_unverified"
	if _, err := pool.Exec(ctx,
		`INSERT INTO app_users (id, display_name) VALUES ($1, 'U') ON CONFLICT (id) DO NOTHING`, userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO user_emails (email, user_id, is_primary, is_verified)
		 VALUES ('inuse@example.com', $1, true, false)
		 ON CONFLICT (email) DO UPDATE SET user_id=$1, is_verified=false`, userID); err != nil {
		t.Fatalf("seed email: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`DELETE FROM user_connected_accounts WHERE provider='discord' AND provider_account_id='d_inuse'`); err != nil {
		t.Fatalf("clean: %v", err)
	}
	srv := fakeDiscord(t, `{"id":"d_inuse","username":"u","email":"inuse@example.com","verified":true}`)
	defer srv.Close()
	h := newCallbackHandler(t, pool, srv.URL)

	w := httptest.NewRecorder()
	r := newCallbackRequest("s", "v", discord.IntentSignin, "code")
	h.Callback(w, r)
	if !strings.Contains(w.Header().Get("Location"), "discord-email-in-use") {
		t.Errorf("Location = %q, want discord-email-in-use", w.Header().Get("Location"))
	}
}

func TestCallback_Signin_NewUserCreated(t *testing.T) {
	pool, cleanup := discordTestPool(t)
	defer cleanup()
	ctx := context.Background()
	if _, err := pool.Exec(ctx,
		`DELETE FROM user_emails WHERE email='brand_new@example.com'`); err != nil {
		t.Fatalf("clean email: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`DELETE FROM user_connected_accounts WHERE provider='discord' AND provider_account_id='d_new'`); err != nil {
		t.Fatalf("clean conn: %v", err)
	}
	srv := fakeDiscord(t, `{"id":"d_new","username":"newbie","email":"brand_new@example.com","verified":true}`)
	defer srv.Close()
	h := newCallbackHandler(t, pool, srv.URL)

	w := httptest.NewRecorder()
	r := newCallbackRequest("s", "v", discord.IntentSignin, "code")
	h.Callback(w, r)
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d", w.Code)
	}
	if !strings.HasSuffix(w.Header().Get("Location"), "/dashboard") {
		t.Errorf("Location = %q", w.Header().Get("Location"))
	}
	var newID string
	if err := pool.QueryRow(ctx,
		`SELECT user_id FROM user_connected_accounts WHERE provider='discord' AND provider_account_id='d_new'`).Scan(&newID); err != nil {
		t.Fatalf("read conn: %v", err)
	}
	if newID == "" {
		t.Errorf("expected new user")
	}
}
