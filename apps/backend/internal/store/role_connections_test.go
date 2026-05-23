package store

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"
)

func insertTestAppUser(t *testing.T, s *PostgresStore, userID, discordID string) {
	t.Helper()
	if _, err := s.pool.Exec(context.Background(), `
		INSERT INTO app_users (id, discord_id, discord_username, display_name)
		VALUES ($1, $2, $3, $3)
	`, userID, discordID, "test_"+userID); err != nil {
		t.Fatalf("insert app_user: %v", err)
	}
}

func TestDiscordRoleConnection_UpsertAndGet(t *testing.T) {
	ctx := context.Background()
	s := newTestPostgres(t)

	suffix := time.Now().UnixNano() % 1_000_000_000
	userID := fmt.Sprintf("usr_rc_%d", suffix)
	discordID := fmt.Sprintf("3000000%010d", suffix)
	insertTestAppUser(t, s, userID, discordID)

	expires := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	row := DiscordRoleConnection{
		UserID:            userID,
		DiscordID:         discordID,
		AccessTokenCT:     []byte("ct-access"),
		AccessTokenNonce:  []byte("nonce-access"),
		RefreshTokenCT:    []byte("ct-refresh"),
		RefreshTokenNonce: []byte("nonce-refresh"),
		AccessExpiresAt:   expires,
		Scopes:            "identify role_connections.write",
	}
	if err := s.UpsertDiscordRoleConnection(ctx, row); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got, err := s.GetDiscordRoleConnection(ctx, userID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil {
		t.Fatal("expected row, got nil")
	}
	if got.DiscordID != discordID {
		t.Errorf("DiscordID = %q, want %q", got.DiscordID, discordID)
	}
	if !bytes.Equal(got.AccessTokenCT, row.AccessTokenCT) {
		t.Errorf("AccessTokenCT mismatch")
	}
	if !got.AccessExpiresAt.Equal(expires) {
		t.Errorf("AccessExpiresAt = %v, want %v", got.AccessExpiresAt, expires)
	}
	if got.RevokedAt != nil {
		t.Errorf("RevokedAt = %v, want nil", got.RevokedAt)
	}
}
