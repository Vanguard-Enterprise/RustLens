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

func TestDiscordRoleConnection_Delete(t *testing.T) {
	ctx := context.Background()
	s := newTestPostgres(t)
	userID := seedTestUser(t, s)
	if err := s.UpsertDiscordRoleConnection(ctx, DiscordRoleConnection{
		UserID: userID, DiscordID: userID + "-discord",
		AccessTokenCT: []byte("x"), AccessTokenNonce: []byte("y"),
		RefreshTokenCT: []byte("x"), RefreshTokenNonce: []byte("y"),
		AccessExpiresAt: time.Now().Add(time.Hour), Scopes: "identify",
	}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if err := s.DeleteDiscordRoleConnection(ctx, userID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	got, err := s.GetDiscordRoleConnection(ctx, userID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil after delete")
	}
}

func TestDiscordRoleConnection_MarkRevoked(t *testing.T) {
	ctx := context.Background()
	s := newTestPostgres(t)
	userID := seedTestUser(t, s)
	if err := s.UpsertDiscordRoleConnection(ctx, DiscordRoleConnection{
		UserID: userID, DiscordID: userID + "-discord",
		AccessTokenCT: []byte("x"), AccessTokenNonce: []byte("y"),
		RefreshTokenCT: []byte("x"), RefreshTokenNonce: []byte("y"),
		AccessExpiresAt: time.Now().Add(time.Hour), Scopes: "identify",
	}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if err := s.MarkDiscordRoleConnectionRevoked(ctx, userID, "invalid_grant"); err != nil {
		t.Fatalf("MarkRevoked: %v", err)
	}
	got, err := s.GetDiscordRoleConnection(ctx, userID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil || got.RevokedAt == nil {
		t.Fatal("expected RevokedAt to be set")
	}
	if got.LastPushError == nil || *got.LastPushError != "invalid_grant" {
		t.Fatalf("LastPushError = %v, want 'invalid_grant'", got.LastPushError)
	}
}

func TestDiscordRoleConnection_RecordPushed(t *testing.T) {
	ctx := context.Background()
	s := newTestPostgres(t)
	userID := seedTestUser(t, s)
	if err := s.UpsertDiscordRoleConnection(ctx, DiscordRoleConnection{
		UserID: userID, DiscordID: userID + "-discord",
		AccessTokenCT: []byte("x"), AccessTokenNonce: []byte("y"),
		RefreshTokenCT: []byte("x"), RefreshTokenNonce: []byte("y"),
		AccessExpiresAt: time.Now().Add(time.Hour), Scopes: "identify",
	}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if err := s.RecordDiscordRoleConnectionPushed(ctx, userID, "hash-abc"); err != nil {
		t.Fatalf("RecordPushed: %v", err)
	}
	got, err := s.GetDiscordRoleConnection(ctx, userID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.LastPushedHash == nil || *got.LastPushedHash != "hash-abc" {
		t.Fatalf("LastPushedHash = %v", got.LastPushedHash)
	}
	if got.LastPushedAt == nil {
		t.Fatal("expected LastPushedAt to be set")
	}
	if got.LastPushError != nil {
		t.Fatalf("LastPushError should be cleared, got %v", got.LastPushError)
	}
}

func TestDiscordRoleConnection_ListActive(t *testing.T) {
	ctx := context.Background()
	s := newTestPostgres(t)
	activeID := seedTestUser(t, s)
	revokedID := seedTestUser(t, s)
	upsert := func(uid string) {
		if err := s.UpsertDiscordRoleConnection(ctx, DiscordRoleConnection{
			UserID: uid, DiscordID: uid + "-discord",
			AccessTokenCT: []byte("x"), AccessTokenNonce: []byte("y"),
			RefreshTokenCT: []byte("x"), RefreshTokenNonce: []byte("y"),
			AccessExpiresAt: time.Now().Add(time.Hour), Scopes: "identify",
		}); err != nil {
			t.Fatalf("upsert %s: %v", uid, err)
		}
	}
	upsert(activeID)
	upsert(revokedID)
	if err := s.MarkDiscordRoleConnectionRevoked(ctx, revokedID, "test"); err != nil {
		t.Fatalf("MarkRevoked: %v", err)
	}
	rows, err := s.ListActiveDiscordRoleConnections(ctx)
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	var sawActive, sawRevoked bool
	for _, r := range rows {
		if r.UserID == activeID {
			sawActive = true
		}
		if r.UserID == revokedID {
			sawRevoked = true
		}
	}
	if !sawActive {
		t.Error("expected active row in ListActive result")
	}
	if sawRevoked {
		t.Error("revoked row should be excluded")
	}
}

func TestGetDiscordRoleConnectionSummary(t *testing.T) {
	ctx := context.Background()
	s := newTestPostgres(t)
	userID := seedTestUser(t, s)

	got, err := s.GetDiscordRoleConnectionSummary(ctx, userID)
	if err != nil {
		t.Fatalf("Summary(absent): %v", err)
	}
	if got.Linked {
		t.Error("expected Linked=false when row missing")
	}

	if err := s.UpsertDiscordRoleConnection(ctx, DiscordRoleConnection{
		UserID: userID, DiscordID: "d",
		AccessTokenCT: []byte("x"), AccessTokenNonce: []byte("y"),
		RefreshTokenCT: []byte("x"), RefreshTokenNonce: []byte("y"),
		AccessExpiresAt: time.Now().Add(time.Hour), Scopes: "identify",
	}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	got, err = s.GetDiscordRoleConnectionSummary(ctx, userID)
	if err != nil {
		t.Fatalf("Summary(present): %v", err)
	}
	if !got.Linked || got.DiscordID == nil || *got.DiscordID != "d" {
		t.Errorf("summary = %+v", got)
	}
}

func TestGetUserProGrantedAt(t *testing.T) {
	ctx := context.Background()
	s := newTestPostgres(t)
	proID := seedTestUser(t, s)
	nonProID := seedTestUser(t, s)
	if err := s.GrantUserEntitlement(ctx, proID, EntitlementPRO, ""); err != nil {
		t.Fatalf("grant pro: %v", err)
	}
	gotPro, err := s.GetUserProGrantedAt(ctx, proID)
	if err != nil {
		t.Fatalf("GetUserProGrantedAt(pro): %v", err)
	}
	if gotPro == nil {
		t.Fatal("expected non-nil for pro user")
	}
	gotNon, err := s.GetUserProGrantedAt(ctx, nonProID)
	if err != nil {
		t.Fatalf("GetUserProGrantedAt(non-pro): %v", err)
	}
	if gotNon != nil {
		t.Fatalf("expected nil for non-pro user, got %v", gotNon)
	}
}
