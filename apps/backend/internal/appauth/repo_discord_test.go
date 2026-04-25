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

func TestRepo_RefreshDiscordConnection(t *testing.T) {
	s := discordTestStore(t)
	ctx := context.Background()
	repo := appauth.NewRepo(s.Pool())

	const userID = "usr_disc_refresh_1"
	if _, err := s.Pool().Exec(ctx,
		`INSERT INTO app_users (id, display_name) VALUES ($1, 'D') ON CONFLICT (id) DO NOTHING`, userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := s.Pool().Exec(ctx,
		`INSERT INTO user_connected_accounts (user_id, provider, provider_account_id, username, display_name, avatar_url, profile_email)
		 VALUES ($1, 'discord', '111222', 'old_name', 'Old Display', 'http://old/avatar', 'old@example.com')
		 ON CONFLICT (provider, provider_account_id) DO UPDATE
		 SET username='old_name', display_name='Old Display', avatar_url='http://old/avatar', profile_email='old@example.com'`, userID); err != nil {
		t.Fatalf("seed conn: %v", err)
	}

	err := repo.RefreshDiscordConnection(ctx, userID, appauth.DiscordProfile{
		ID:          "111222",
		Username:    "new_name",
		DisplayName: "New Display",
		AvatarURL:   "http://new/avatar",
		Email:       "new@example.com",
	})
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	var u, d, a, e string
	if err := s.Pool().QueryRow(ctx,
		`SELECT username, display_name, avatar_url, profile_email
		 FROM user_connected_accounts WHERE provider='discord' AND provider_account_id='111222'`).
		Scan(&u, &d, &a, &e); err != nil {
		t.Fatalf("read: %v", err)
	}
	if u != "new_name" || d != "New Display" || a != "http://new/avatar" || e != "new@example.com" {
		t.Errorf("row not refreshed: %q %q %q %q", u, d, a, e)
	}
}
