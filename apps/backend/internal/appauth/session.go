package appauth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
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
	Epoch       int      `json:"epoch"`
	ExpiresAt   int64    `json:"expiresAt"` // milliseconds since epoch, matches JS Date.now()
}

var (
	ErrMalformedToken  = errors.New("malformed session token")
	ErrBadSignature    = errors.New("invalid session signature")
	ErrExpired         = errors.New("session expired")
	ErrSessionRevoked  = errors.New("session revoked")
)

// ParseSession verifies the HMAC signature on a token produced by
// apps/frontend/lib/session.ts serializeSession() and returns the payload.
// Does NOT perform the epoch revocation check — use ValidateSession for that.
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

// ValidateSession parses+verifies the token, then additionally checks that
// the embedded epoch is not below the current session_epoch in the database.
// Returns ErrSessionRevoked if the token has been revoked.
func ValidateSession(ctx context.Context, token, secret string, pool *pgxpool.Pool) (*SessionUser, error) {
	s, err := ParseSession(token, secret)
	if err != nil {
		return nil, err
	}
	if pool == nil {
		return s, nil
	}
	var current int
	if err := pool.QueryRow(ctx, `SELECT session_epoch FROM app_users WHERE id = $1`, s.UserID).Scan(&current); err != nil {
		// If the user no longer exists, treat the session as revoked.
		return nil, ErrSessionRevoked
	}
	if s.Epoch < current {
		return nil, ErrSessionRevoked
	}
	return s, nil
}

// CurrentSessionEpoch returns the session_epoch for the given user id.
func CurrentSessionEpoch(ctx context.Context, pool *pgxpool.Pool, userID string) (int, error) {
	var epoch int
	err := pool.QueryRow(ctx, `SELECT session_epoch FROM app_users WHERE id = $1`, userID).Scan(&epoch)
	return epoch, err
}

func sign(encodedPayload, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(encodedPayload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// SerializeSession is exposed primarily for tests; it builds a signed token
// from a SessionUser using the same format the frontend emits.
func SerializeSession(s SessionUser, secret string) (string, error) {
	raw, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(raw)
	return encoded + "." + sign(encoded, secret), nil
}
