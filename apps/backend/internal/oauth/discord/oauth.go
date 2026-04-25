package discord

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"strings"
)

// Cookie names. All set with HttpOnly, SameSite=Lax, 10-minute MaxAge during
// initiate; cleared (MaxAge: -1) on callback regardless of outcome.
const (
	CookieState    = "rustlens_discord_oauth_state"
	CookieVerifier = "rustlens_discord_oauth_verifier"
	CookieIntent   = "rustlens_discord_oauth_intent"
)

// Intent cookie values.
const (
	IntentSignin = "signin"
	IntentLink   = "link"
)

// CookieMaxAgeSeconds is the lifetime of the per-flow cookies set on initiate.
const CookieMaxAgeSeconds = 60 * 10

// NewPkceVerifier returns a fresh base64url-encoded random string suitable as
// a PKCE code_verifier (RFC 7636). 32 random bytes encode to 43 chars.
func NewPkceVerifier() (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf[:]), nil
}

// PkceChallenge returns the S256 challenge for a verifier.
func PkceChallenge(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

// NewState returns a fresh random state value. 16 bytes encoded base64url.
func NewState() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf[:]), nil
}

// RedirectURI builds the OAuth callback URL given an APP_ORIGIN.
// Normalizes a single trailing slash on origin.
func RedirectURI(appOrigin string) string {
	return strings.TrimRight(appOrigin, "/") + "/api/auth/discord/callback"
}
