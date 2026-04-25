package appauth_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"rustlens/backend/internal/appauth"
	"rustlens/backend/internal/store"
)

func discordTestStore(t *testing.T) *store.PostgresStore {
	t.Helper()
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
	return s
}

func TestRepo_FindUserByDiscordID(t *testing.T) {
	s := discordTestStore(t)
	ctx := context.Background()
	repo := appauth.NewRepo(s.Pool())

	const userID = "usr_disc_find_1"
	if _, err := s.Pool().Exec(ctx,
		`INSERT INTO app_users (id, display_name) VALUES ($1, 'D') ON CONFLICT (id) DO NOTHING`, userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := s.Pool().Exec(ctx,
		`INSERT INTO user_connected_accounts (user_id, provider, provider_account_id, username)
		 VALUES ($1, 'discord', '999111', 'd_user')
		 ON CONFLICT (provider, provider_account_id) DO NOTHING`, userID); err != nil {
		t.Fatalf("seed conn: %v", err)
	}

	got, err := repo.FindUserByDiscordID(ctx, "999111")
	if err != nil {
		t.Fatalf("FindUserByDiscordID: %v", err)
	}
	if got != userID {
		t.Errorf("got %q, want %q", got, userID)
	}

	_, err = repo.FindUserByDiscordID(ctx, "no-such-discord-id")
	if !errors.Is(err, appauth.ErrUserNotFound) {
		t.Errorf("missing id: got %v, want ErrUserNotFound", err)
	}
}
