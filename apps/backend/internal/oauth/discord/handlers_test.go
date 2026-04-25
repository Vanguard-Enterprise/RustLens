package discord_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"rustlens/backend/internal/oauth/discord"
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
