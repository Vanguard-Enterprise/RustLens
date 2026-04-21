package appauth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// SessionUser mirrors the TypeScript SessionUser produced by the frontend.
// The payload is a base64url(JSON) dot signature — signature is
// HMAC-SHA256(encodedPayload, secret), base64url, unpadded.
type SessionUser struct {
	UserID      string   `json:"userId"`
	DisplayName string   `json:"displayName"`
	AvatarURL   string   `json:"avatarUrl"`
	Roles       []string `json:"roles"`
	Permissions []string `json:"permissions,omitempty"`
	DiscordID   string   `json:"discordId,omitempty"`
	Email       string   `json:"email,omitempty"`
	ExpiresAt   int64    `json:"expiresAt"` // milliseconds since epoch, matches JS Date.now()
}

var (
	ErrMalformedToken = errors.New("malformed session token")
	ErrBadSignature   = errors.New("invalid session signature")
	ErrExpired        = errors.New("session expired")
)

// ParseSession verifies the HMAC signature on a token produced by
// apps/frontend/lib/session.ts serializeSession() and returns the payload.
func ParseSession(token, secret string) (*SessionUser, error) {
	dot := strings.IndexByte(token, '.')
	if dot <= 0 || dot == len(token)-1 {
		return nil, ErrMalformedToken
	}
	encoded := token[:dot]
	sig := token[dot+1:]

	expected := sign(encoded, secret)
	if !hmac.Equal([]byte(sig), []byte(expected)) {
		return nil, ErrBadSignature
	}

	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, ErrMalformedToken
	}
	var s SessionUser
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, ErrMalformedToken
	}
	if s.UserID == "" {
		return nil, ErrMalformedToken
	}
	if time.Now().UnixMilli() > s.ExpiresAt {
		return nil, ErrExpired
	}
	return &s, nil
}

func sign(encodedPayload, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(encodedPayload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
