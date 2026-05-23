package store

import (
	"bytes"
	"context"
	"testing"
	"time"
)

func TestDiscordRoleConnection_UpsertAndGet(t *testing.T) {
	ctx := context.Background()
	s := newTestPostgres(t)

	userID := seedTestUser(t, s)
	discordID := userID + "-discord"

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
	if !bytes.Equal(got.AccessTokenNonce, row.AccessTokenNonce) {
		t.Errorf("AccessTokenNonce mismatch")
	}
	if !bytes.Equal(got.RefreshTokenCT, row.RefreshTokenCT) {
		t.Errorf("RefreshTokenCT mismatch")
	}
	if !bytes.Equal(got.RefreshTokenNonce, row.RefreshTokenNonce) {
		t.Errorf("RefreshTokenNonce mismatch")
	}
	if got.Scopes != row.Scopes {
		t.Errorf("Scopes = %q, want %q", got.Scopes, row.Scopes)
	}
	if got.LinkedAt.IsZero() {
		t.Error("LinkedAt should be set")
	}
	if !got.AccessExpiresAt.Equal(expires) {
		t.Errorf("AccessExpiresAt = %v, want %v", got.AccessExpiresAt, expires)
	}
	if got.RevokedAt != nil {
		t.Errorf("RevokedAt = %v, want nil", got.RevokedAt)
	}
}
