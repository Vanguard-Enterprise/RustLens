package discord_test

import (
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"

	"rustlens/backend/internal/oauth/discord"
)

func TestNewPkceVerifier_LengthAndCharset(t *testing.T) {
	v, err := discord.NewPkceVerifier()
	if err != nil {
		t.Fatalf("NewPkceVerifier: %v", err)
	}
	if len(v) < 43 || len(v) > 128 {
		t.Errorf("verifier length %d outside 43..128", len(v))
	}
	for _, r := range v {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_') {
			t.Errorf("verifier char %q not base64url-safe", r)
		}
	}
}

func TestPkceChallenge_MatchesS256(t *testing.T) {
	v := "test-verifier-stable-input-1234567890abcdef"
	want := base64.RawURLEncoding.EncodeToString(func() []byte {
		h := sha256.Sum256([]byte(v))
		return h[:]
	}())
	got := discord.PkceChallenge(v)
	if got != want {
		t.Errorf("PkceChallenge mismatch:\n got %s\nwant %s", got, want)
	}
}

func TestNewState_NonEmpty_Unique(t *testing.T) {
	a, err := discord.NewState()
	if err != nil {
		t.Fatalf("NewState a: %v", err)
	}
	b, err := discord.NewState()
	if err != nil {
		t.Fatalf("NewState b: %v", err)
	}
	if a == "" || b == "" || a == b {
		t.Errorf("expected non-empty distinct values, got %q %q", a, b)
	}
}

func TestRedirectURI_BuildsCallback(t *testing.T) {
	got := discord.RedirectURI("http://localhost")
	want := "http://localhost/api/auth/discord/callback"
	if got != want {
		t.Errorf("RedirectURI = %q, want %q", got, want)
	}
	if discord.RedirectURI("http://localhost/") != want {
		t.Errorf("trailing slash should be normalized")
	}
}

func TestIntentCookie_Constants(t *testing.T) {
	if discord.IntentSignin != "signin" || discord.IntentLink != "link" {
		t.Errorf("intent constants drifted: signin=%q link=%q", discord.IntentSignin, discord.IntentLink)
	}
	for _, name := range []string{discord.CookieState, discord.CookieVerifier, discord.CookieIntent} {
		if !strings.HasPrefix(name, "rustlens_discord_oauth_") {
			t.Errorf("cookie name %q lacks expected prefix", name)
		}
	}
}
