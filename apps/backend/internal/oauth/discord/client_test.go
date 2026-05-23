package discord_test

import (
	"context"
	"errors"
	"io"
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
	if p.ID != "777" || p.Username != "alice" || p.GlobalName != "Alice" {
		t.Errorf("profile mismatch: %+v", p)
	}
	if p.AvatarHash != "abcd" {
		t.Errorf("AvatarHash = %q, want %q", p.AvatarHash, "abcd")
	}
}

func TestClient_FetchUserGuilds_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/users/@me/guilds" || r.Method != http.MethodGet {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if got := r.URL.Query().Get("with_counts"); got != "true" {
			t.Errorf("with_counts = %q, want true", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tok-abc" {
			t.Errorf("Authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"id":"100","name":"Alpha","icon":"icon-a"},
			{"id":"200","name":"Bravo","icon":null}
		]`))
	}))
	defer srv.Close()
	c := discord.NewClient("id", "secret", discord.WithBaseURL(srv.URL), discord.WithRedirectURI("x"))
	guilds, err := c.FetchUserGuilds(context.Background(), "tok-abc")
	if err != nil {
		t.Fatalf("FetchUserGuilds: %v", err)
	}
	if len(guilds) != 2 {
		t.Fatalf("len = %d, want 2", len(guilds))
	}
	if guilds[0].ID != "100" || guilds[0].Name != "Alpha" || guilds[0].IconHash != "icon-a" {
		t.Errorf("guilds[0] = %+v", guilds[0])
	}
	if guilds[1].ID != "200" || guilds[1].IconHash != "" {
		t.Errorf("guilds[1] = %+v", guilds[1])
	}
}

func TestClient_FetchUserGuilds_NonOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"401: Unauthorized"}`))
	}))
	defer srv.Close()
	c := discord.NewClient("id", "secret", discord.WithBaseURL(srv.URL), discord.WithRedirectURI("x"))
	if _, err := c.FetchUserGuilds(context.Background(), "tok"); err == nil {
		t.Fatalf("expected error on 401")
	}
}

func TestClient_FetchProfile_NoAvatar(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"1","username":"x","avatar":null}`))
	}))
	defer srv.Close()
	c := discord.NewClient("id", "secret", discord.WithBaseURL(srv.URL), discord.WithRedirectURI("x"))
	p, err := c.FetchProfile(context.Background(), "tok")
	if err != nil {
		t.Fatalf("FetchProfile: %v", err)
	}
	if p.AvatarHash != "" {
		t.Errorf("AvatarHash with null avatar = %q, want empty", p.AvatarHash)
	}
}

func TestRefreshToken_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/api/oauth2/token" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm: %v", err)
		}
		if got := r.Form.Get("grant_type"); got != "refresh_token" {
			t.Errorf("grant_type = %s", got)
		}
		if got := r.Form.Get("refresh_token"); got != "rt-old" {
			t.Errorf("refresh_token = %s", got)
		}
		_, _ = w.Write([]byte(`{
			"access_token": "at-new", "refresh_token": "rt-new",
			"expires_in": 604800, "token_type": "Bearer", "scope": "identify role_connections.write"
		}`))
	}))
	defer srv.Close()

	c := discord.NewClient("cid", "csecret", discord.WithBaseURL(srv.URL))
	access, refresh, expiresIn, err := c.RefreshToken(context.Background(), "rt-old")
	if err != nil {
		t.Fatalf("RefreshToken: %v", err)
	}
	if access != "at-new" || refresh != "rt-new" || expiresIn != 604800 {
		t.Fatalf("got %q %q %d", access, refresh, expiresIn)
	}
}

func TestRefreshToken_InvalidGrant(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer srv.Close()
	c := discord.NewClient("cid", "csecret", discord.WithBaseURL(srv.URL))
	_, _, _, err := c.RefreshToken(context.Background(), "rt-old")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, discord.ErrRefreshRevoked) {
		t.Fatalf("err = %v, want ErrRefreshRevoked", err)
	}
}

func TestPutRoleConnection_Success(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("method = %s", r.Method)
		}
		if r.URL.Path != "/api/users/@me/applications/APP-ID/role-connection" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer at-1" {
			t.Errorf("auth = %s", got)
		}
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"platform_name":"RustLens"}`))
	}))
	defer srv.Close()
	c := discord.NewClient("cid", "csecret", discord.WithBaseURL(srv.URL))
	body := discord.RoleConnectionBody{
		PlatformName:     "RustLens",
		PlatformUsername: "sem",
		Metadata: map[string]string{
			"is_pro":    "1",
			"pro_since": "2026-05-23T12:00:00+00:00",
		},
	}
	if err := c.PutRoleConnection(context.Background(), "at-1", "APP-ID", body); err != nil {
		t.Fatalf("PutRoleConnection: %v", err)
	}
	if !strings.Contains(string(gotBody), `"is_pro":"1"`) {
		t.Errorf("body missing is_pro: %s", string(gotBody))
	}
}

func TestPutRoleConnection_Unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"401: Unauthorized","code":0}`))
	}))
	defer srv.Close()
	c := discord.NewClient("cid", "csecret", discord.WithBaseURL(srv.URL))
	err := c.PutRoleConnection(context.Background(), "expired", "APP-ID", discord.RoleConnectionBody{PlatformName: "x"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, discord.ErrAccessTokenInvalid) {
		t.Fatalf("err = %v, want ErrAccessTokenInvalid", err)
	}
}
