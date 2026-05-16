package store

import (
	"context"
	"crypto/sha256"
	"fmt"
	"testing"
	"time"
)

func seedTestUser(t *testing.T, s *PostgresStore) string {
	t.Helper()
	id := fmt.Sprintf("test-user-%d", time.Now().UnixNano())
	_, err := s.pool.Exec(context.Background(), `
		INSERT INTO app_users (id, discord_id, discord_username, display_name)
		VALUES ($1, $2, $3, $4)
	`, id, id+"-discord", "testuser", "Test User")
	if err != nil {
		t.Fatalf("seedTestUser: %v", err)
	}
	return id
}

func TestAPIKeyStoreCreateAndGetByPrefix(t *testing.T) {
	pg := newTestPostgres(t)
	defer pg.Close()
	ctx := context.Background()
	s := NewAPIKeyStore(pg.pool)
	userID := seedTestUser(t, pg)

	hash := sha256.Sum256([]byte("rl_admin_abc123xyz"))
	key, err := s.Create(ctx, APIKeyCreate{
		Name:            "deploy-bot",
		Prefix:          "rl_admin_abc",
		Last4:           "1xyz",
		SecretHash:      hash[:],
		CreatedByUserID: userID,
		Permissions:     []string{"users.read"},
		ExpiresAt:       nil,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if key.ID == "" {
		t.Fatal("expected non-empty id")
	}

	got, err := s.GetByPrefix(ctx, "rl_admin_abc")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != key.ID {
		t.Errorf("id mismatch")
	}
}

func TestAPIKeyStoreRevoke(t *testing.T) {
	pg := newTestPostgres(t)
	defer pg.Close()
	ctx := context.Background()
	s := NewAPIKeyStore(pg.pool)
	userID := seedTestUser(t, pg)
	hash := sha256.Sum256([]byte("rl_admin_revoketestxyz"))
	key, _ := s.Create(ctx, APIKeyCreate{
		Name: "tmp", Prefix: "rl_admin_rev", Last4: "txyz",
		SecretHash: hash[:], CreatedByUserID: userID,
		Permissions: []string{"users.read"},
	})

	if err := s.Revoke(ctx, key.ID, "manual"); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	got, _ := s.GetByPrefix(ctx, "rl_admin_rev")
	if got.RevokedAt == nil {
		t.Fatal("expected revoked_at set")
	}
	if got.RevokedReason == nil || *got.RevokedReason != "manual" {
		t.Errorf("revoked_reason: got %v, want manual", got.RevokedReason)
	}

	// Second revoke is idempotent and does not bump revoked_at.
	first := *got.RevokedAt
	time.Sleep(10 * time.Millisecond)
	if err := s.Revoke(ctx, key.ID, "manual"); err != nil {
		t.Fatalf("revoke again: %v", err)
	}
	got, _ = s.GetByPrefix(ctx, "rl_admin_rev")
	if !got.RevokedAt.Equal(first) {
		t.Errorf("revoked_at bumped on idempotent revoke")
	}
}

func TestAPIKeyStoreRotate(t *testing.T) {
	pg := newTestPostgres(t)
	defer pg.Close()
	ctx := context.Background()
	s := NewAPIKeyStore(pg.pool)
	userID := seedTestUser(t, pg)
	hash := sha256.Sum256([]byte("rl_admin_old123xyz"))
	key, _ := s.Create(ctx, APIKeyCreate{
		Name: "k", Prefix: "rl_admin_old", Last4: "3xyz",
		SecretHash: hash[:], CreatedByUserID: userID,
		Permissions: []string{"users.read"},
	})

	newHash := sha256.Sum256([]byte("rl_admin_new789zzz"))
	if err := s.Rotate(ctx, key.ID, "rl_admin_new", "9zzz", newHash[:]); err != nil {
		t.Fatalf("rotate: %v", err)
	}
	got, _ := s.GetByPrefix(ctx, "rl_admin_new")
	if got.ID != key.ID {
		t.Error("rotate must preserve id")
	}
	if string(got.Permissions[0]) != "users.read" {
		t.Error("rotate must preserve permissions")
	}
}

func TestAPIKeyStoreUpdateLastUsed(t *testing.T) {
	pg := newTestPostgres(t)
	defer pg.Close()
	ctx := context.Background()
	s := NewAPIKeyStore(pg.pool)
	userID := seedTestUser(t, pg)
	hash := sha256.Sum256([]byte("rl_admin_lastused"))
	key, _ := s.Create(ctx, APIKeyCreate{
		Name: "k", Prefix: "rl_admin_lus", Last4: "used",
		SecretHash: hash[:], CreatedByUserID: userID,
		Permissions: []string{"users.read"},
	})

	if err := s.UpdateLastUsed(ctx, key.ID, "203.0.113.7"); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ := s.GetByPrefix(ctx, "rl_admin_lus")
	if got.LastUsedAt == nil {
		t.Fatal("expected last_used_at set")
	}
	if got.LastUsedIP == nil || *got.LastUsedIP != "203.0.113.7" {
		t.Errorf("last_used_ip: %v", got.LastUsedIP)
	}
}
