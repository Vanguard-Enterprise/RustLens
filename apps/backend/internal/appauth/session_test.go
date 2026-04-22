package appauth_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"rustlens/backend/internal/appauth"
	"rustlens/backend/internal/store"
)

func TestValidateSession_EpochRevocation(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_URL not set")
	}
	ctx := context.Background()
	s, err := store.NewPostgresStore(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(s.Close)
	if err := s.EnsureSchema(ctx); err != nil {
		t.Fatalf("schema: %v", err)
	}

	const userID = "usr_sess_epoch_1"
	if _, err := s.Pool().Exec(ctx,
		`INSERT INTO app_users (id, display_name) VALUES ($1, 'Test')
		 ON CONFLICT (id) DO UPDATE SET session_epoch = 0`, userID); err != nil {
		t.Fatalf("seed: %v", err)
	}

	secret := "test-secret"
	session := appauth.SessionUser{
		UserID:    userID,
		Roles:     []string{},
		Epoch:     0,
		ExpiresAt: time.Now().Add(1 * time.Hour).UnixMilli(),
	}
	token, err := appauth.SerializeSession(session, secret)
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}

	// Fresh token validates OK.
	got, err := appauth.ValidateSession(ctx, token, secret, s.Pool())
	if err != nil {
		t.Fatalf("fresh validate: %v", err)
	}
	if got.UserID != userID {
		t.Errorf("got userID %q", got.UserID)
	}

	// Bump the epoch -> existing token must be rejected.
	if _, err := s.Pool().Exec(ctx,
		`UPDATE app_users SET session_epoch = session_epoch + 1 WHERE id = $1`, userID); err != nil {
		t.Fatalf("bump epoch: %v", err)
	}

	_, err = appauth.ValidateSession(ctx, token, secret, s.Pool())
	if !errors.Is(err, appauth.ErrSessionRevoked) {
		t.Errorf("after bump expected ErrSessionRevoked, got %v", err)
	}
}

func TestValidateSession_BadSignature(t *testing.T) {
	ctx := context.Background()
	secret := "s"
	session := appauth.SessionUser{
		UserID:    "anyone",
		Epoch:     0,
		ExpiresAt: time.Now().Add(1 * time.Hour).UnixMilli(),
	}
	tok, err := appauth.SerializeSession(session, secret)
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}
	// Flip a character in the signature.
	bad := tok[:len(tok)-1] + "X"
	if _, err := appauth.ValidateSession(ctx, bad, secret, nil); !errors.Is(err, appauth.ErrBadSignature) {
		t.Errorf("got %v, want ErrBadSignature", err)
	}
}
