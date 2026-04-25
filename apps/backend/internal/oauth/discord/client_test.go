package discord_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"rustlens/backend/internal/oauth/discord"
)

func TestClient_ExchangeCode_Success(t *testing.T) {
	var gotForm string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/oauth2/token" || r.Method != http.MethodPost {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		_ = r.ParseForm()
		gotForm = r.Form.Encode()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"tok-abc","token_type":"Bearer"}`))
	}))
	defer srv.Close()

	c := discord.NewClient("client-id", "client-secret", discord.WithBaseURL(srv.URL), discord.WithRedirectURI("http://localhost/api/auth/discord/callback"))
	tok, err := c.ExchangeCode(context.Background(), "the-code", "the-verifier")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if tok != "tok-abc" {
		t.Errorf("access_token = %q", tok)
	}
	for _, want := range []string{"client_id=client-id", "client_secret=client-secret", "grant_type=authorization_code", "code=the-code", "code_verifier=the-verifier", "redirect_uri="} {
		if !strings.Contains(gotForm, want) {
			t.Errorf("body missing %q; got %q", want, gotForm)
		}
	}
}

func TestClient_ExchangeCode_NonOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer srv.Close()
	c := discord.NewClient("id", "secret", discord.WithBaseURL(srv.URL), discord.WithRedirectURI("x"))
	if _, err := c.ExchangeCode(context.Background(), "code", "verifier"); err == nil {
		t.Fatalf("expected error on 400")
	}
}

func TestClient_FetchProfile_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/users/@me" || r.Method != http.MethodGet {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tok-abc" {
			t.Errorf("Authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "777",
			"username": "alice",
			"global_name": "Alice",
			"avatar": "abcd",
			"email": "alice@example.com",
			"verified": true
		}`))
	}))
	defer srv.Close()
	c := discord.NewClient("id", "secret", discord.WithBaseURL(srv.URL), discord.WithRedirectURI("x"))
	p, err := c.FetchProfile(context.Background(), "tok-abc")
	if err != nil {
		t.Fatalf("FetchProfile: %v", err)
	}
	if p.ID != "777" || p.Username != "alice" || p.GlobalName != "Alice" || p.Email != "alice@example.com" || !p.Verified {
		t.Errorf("profile mismatch: %+v", p)
	}
	wantAvatar := "https://cdn.discordapp.com/avatars/777/abcd.png?size=256"
	if p.AvatarURL != wantAvatar {
		t.Errorf("AvatarURL = %q, want %q", p.AvatarURL, wantAvatar)
	}
}

func TestClient_FetchProfile_NoAvatar(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"1","username":"x","avatar":null,"verified":false}`))
	}))
	defer srv.Close()
	c := discord.NewClient("id", "secret", discord.WithBaseURL(srv.URL), discord.WithRedirectURI("x"))
	p, err := c.FetchProfile(context.Background(), "tok")
	if err != nil {
		t.Fatalf("FetchProfile: %v", err)
	}
	if p.AvatarURL != "" {
		t.Errorf("AvatarURL with null avatar = %q, want empty", p.AvatarURL)
	}
}
