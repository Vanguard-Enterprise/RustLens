package appauth_test

import (
	"context"
	"errors"
	"testing"

	"rustlens/backend/internal/appauth"
)

func TestRepo_FindByDiscordID(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	repo := appauth.NewRepo(pool)

	if _, err := pool.Exec(ctx, `
		INSERT INTO app_users (id, discord_id, discord_username, display_name)
		VALUES ('usr_findbd', '999', 'finder', 'finder')
	`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	id, err := repo.FindByDiscordID(ctx, "999")
	if err != nil {
		t.Fatalf("FindByDiscordID: %v", err)
	}
	if id != "usr_findbd" {
		t.Fatalf("expected usr_findbd got %q", id)
	}

	if _, err := repo.FindByDiscordID(ctx, "nonexistent"); !errors.Is(err, appauth.ErrAppUserNotFound) {
		t.Fatalf("expected ErrAppUserNotFound got %v", err)
	}
}
