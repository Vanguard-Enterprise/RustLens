package auth

import (
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestExtractBearerToken(t *testing.T) {
	token, err := ExtractBearerToken("Bearer abc123")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if token != "abc123" {
		t.Fatalf("unexpected token: %s", token)
	}
}

func TestValidatorRejectsWrongServiceClaim(t *testing.T) {
	validator := NewValidator("secret", "issuer", "aud")
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, Claims{
		Service: "frontend",
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "issuer",
			Audience:  []string{"aud"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Minute)),
		},
	})
	tokenString, err := token.SignedString([]byte("secret"))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	_, err = validator.Parse(tokenString)
	if err == nil || !strings.Contains(err.Error(), "invalid service") {
		t.Fatalf("expected invalid service claim error, got %v", err)
	}
}
