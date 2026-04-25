package discord_test

import (
	"testing"
	"time"

	"rustlens/backend/internal/appauth"
)

func mustSerializeSessionForTest(t *testing.T, secret, userID string) string {
	t.Helper()
	tok, err := appauth.SerializeSession(appauth.SessionUser{
		UserID:    userID,
		Roles:     []string{"user"},
		Epoch:     0,
		ExpiresAt: time.Now().Add(time.Hour).UnixMilli(),
	}, secret)
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}
	return tok
}
