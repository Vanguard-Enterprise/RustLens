package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

var (
	ErrInvalidAppSession = errors.New("invalid app session")
	ErrExpiredAppSession = errors.New("expired app session")
)

type AppSessionClaims struct {
	UserID    string `json:"userId"`
	ExpiresAt int64  `json:"expiresAt"`
}

type AppSessionValidator struct {
	secret []byte
}

func NewAppSessionValidator(secret string) *AppSessionValidator {
	return &AppSessionValidator{secret: []byte(secret)}
}

func (v *AppSessionValidator) Parse(token string) (*AppSessionClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, ErrInvalidAppSession
	}

	expectedSignature := signAppSession(parts[0], v.secret)
	if subtle.ConstantTimeCompare([]byte(parts[1]), []byte(expectedSignature)) != 1 {
		return nil, ErrInvalidAppSession
	}

	payloadJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, ErrInvalidAppSession
	}

	var claims AppSessionClaims
	if err := json.Unmarshal(payloadJSON, &claims); err != nil {
		return nil, ErrInvalidAppSession
	}
	if strings.TrimSpace(claims.UserID) == "" || claims.ExpiresAt <= 0 {
		return nil, ErrInvalidAppSession
	}
	if time.UnixMilli(claims.ExpiresAt).Before(time.Now()) {
		return nil, ErrExpiredAppSession
	}

	return &claims, nil
}

func signAppSession(encodedPayload string, secret []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(encodedPayload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
