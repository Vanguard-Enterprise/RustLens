package appauth_test

import (
	"context"
	"testing"

	"rustlens/backend/internal/appauth"
)

func TestFindOrCreateByDiscordID_CreatesNewUser(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	repo := appauth.NewRepo(pool)

	profile := appauth.DiscordProfile{
		ID:         "111111111111111111",
		Username:   "alice",
		GlobalName: "Alice A.",
		Avatar:     "abc123",
	}
	userID, created, err := repo.FindOrCreateByDiscordID(ctx, profile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !created {
		t.Fatalf("expected created=true on first call")
	}
	if userID == "" {
		t.Fatalf("expected non-empty user id")
	}
}

func TestFindOrCreateByDiscordID_ReturnsExistingUser(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	repo := appauth.NewRepo(pool)

	profile := appauth.DiscordProfile{ID: "222", Username: "bob"}
	first, _, err := repo.FindOrCreateByDiscordID(ctx, profile)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	second, created, err := repo.FindOrCreateByDiscordID(ctx, profile)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if created {
		t.Fatalf("expected created=false on returning user")
	}
	if second != first {
		t.Fatalf("expected same user id; got %q vs %q", first, second)
	}
}

func TestFindOrCreateByDiscordID_RefreshesProfileFields(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	repo := appauth.NewRepo(pool)

	initial := appauth.DiscordProfile{ID: "333", Username: "old_name"}
	userID, _, err := repo.FindOrCreateByDiscordID(ctx, initial)
	if err != nil {
		t.Fatalf("initial: %v", err)
	}

	updated := appauth.DiscordProfile{ID: "333", Username: "new_name", GlobalName: "New Name", Avatar: "newhash"}
	if _, _, err := repo.FindOrCreateByDiscordID(ctx, updated); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	var got string
	if err := pool.QueryRow(ctx, `SELECT discord_username FROM app_users WHERE id=$1`, userID).Scan(&got); err != nil {
		t.Fatalf("query: %v", err)
	}
	if got != "new_name" {
		t.Fatalf("expected profile refresh to update discord_username to %q, got %q", "new_name", got)
	}
}
