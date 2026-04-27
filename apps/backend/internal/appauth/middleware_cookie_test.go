package appauth_test

import (
	"net/http"
	"testing"
	"time"

	"rustlens/backend/internal/appauth"
)

func TestSessionFromCookie_ValidToken(t *testing.T) {
	secret := "test-secret"
	in := appauth.SessionUser{
		UserID:    "usr_cookie_1",
		Roles:     []string{"user"},
		Epoch:     0,
		ExpiresAt: time.Now().Add(time.Hour).UnixMilli(),
	}
	tok, err := appauth.SerializeSession(in, secret)
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}
	r, _ := http.NewRequest("GET", "/", nil)
	r.AddCookie(&http.Cookie{Name: appauth.SessionCookieName, Value: tok})
	got, err := appauth.SessionFromCookie(r, secret)
	if err != nil {
		t.Fatalf("SessionFromCookie: %v", err)
	}
	if got == nil || got.UserID != "usr_cookie_1" {
		t.Errorf("got %+v", got)
	}
}

func TestSessionFromCookie_Missing(t *testing.T) {
	r, _ := http.NewRequest("GET", "/", nil)
	if _, err := appauth.SessionFromCookie(r, "secret"); err == nil {
		t.Fatalf("expected error on missing cookie")
	}
}
