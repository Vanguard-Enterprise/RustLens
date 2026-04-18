package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"
)

func buildAppSessionToken(t *testing.T, secret string, claims AppSessionClaims) string {
	t.Helper()

	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}

	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(encodedPayload))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	return encodedPayload + "." + signature
}

func TestAppSessionValidatorParse(t *testing.T) {
	validator := NewAppSessionValidator("test-secret")
	token := buildAppSessionToken(t, "test-secret", AppSessionClaims{
		UserID:    "user_123",
		ExpiresAt: time.Now().Add(5 * time.Minute).UnixMilli(),
	})

	claims, err := validator.Parse(token)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if claims.UserID != "user_123" {
		t.Fatalf("Parse() userId = %q, want %q", claims.UserID, "user_123")
	}
}

func TestAppSessionValidatorRejectsExpiredToken(t *testing.T) {
	validator := NewAppSessionValidator("test-secret")
	token := buildAppSessionToken(t, "test-secret", AppSessionClaims{
		UserID:    "user_123",
		ExpiresAt: time.Now().Add(-1 * time.Minute).UnixMilli(),
	})

	_, err := validator.Parse(token)
	if err != ErrExpiredAppSession {
		t.Fatalf("Parse() error = %v, want %v", err, ErrExpiredAppSession)
	}
}

func TestAppSessionValidatorRejectsBadSignature(t *testing.T) {
	validator := NewAppSessionValidator("test-secret")
	token := buildAppSessionToken(t, "different-secret", AppSessionClaims{
		UserID:    "user_123",
		ExpiresAt: time.Now().Add(5 * time.Minute).UnixMilli(),
	})

	_, err := validator.Parse(token)
	if err != ErrInvalidAppSession {
		t.Fatalf("Parse() error = %v, want %v", err, ErrInvalidAppSession)
	}
}
