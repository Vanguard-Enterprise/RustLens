package auth

import (
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type Claims struct {
	Service  string `json:"service"`
	WorkerID string `json:"worker_id"`
	jwt.RegisteredClaims
}

type Validator struct {
	secret   []byte
	issuer   string
	audience string
}

func NewValidator(secret string, issuer string, audience string) *Validator {
	return &Validator{secret: []byte(secret), issuer: issuer, audience: audience}
}

func (v *Validator) Parse(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (any, error) {
		if token.Method.Alg() != jwt.SigningMethodHS256.Alg() {
			return nil, fmt.Errorf("unexpected signing method: %s", token.Method.Alg())
		}
		return v.secret, nil
	}, jwt.WithIssuer(v.issuer), jwt.WithAudience(v.audience), jwt.WithLeeway(2*time.Second))
	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(*Claims)
	if !ok {
		return nil, fmt.Errorf("unexpected token claims")
	}

	if claims.Service != "bot" {
		return nil, fmt.Errorf("invalid service claim")
	}

	return claims, nil
}

func ExtractBearerToken(headerValue string) (string, error) {
	parts := strings.SplitN(strings.TrimSpace(headerValue), " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", fmt.Errorf("invalid authorization header")
	}

	if parts[1] == "" {
		return "", fmt.Errorf("missing bearer token")
	}

	return parts[1], nil
}
