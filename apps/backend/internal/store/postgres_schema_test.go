package store

import (
	"context"
	"testing"
)

func TestEnsureSchema_Idempotent(t *testing.T) {
	ctx := context.Background()
	s := newTestPostgres(t)
	if err := s.EnsureSchema(ctx); err != nil {
		t.Fatalf("second EnsureSchema: %v", err)
	}
	if err := s.EnsureSchema(ctx); err != nil {
		t.Fatalf("third EnsureSchema: %v", err)
	}
	// Spot-check the new lifecycle columns exist on app_users.
	var count int
	err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM information_schema.columns
		WHERE table_name = 'app_users'
		  AND column_name IN ('suspended_at','deleted_at','session_epoch')
	`).Scan(&count)
	if err != nil {
		t.Fatalf("count columns: %v", err)
	}
	if count != 3 {
		t.Fatalf("expected 3 lifecycle columns on app_users, got %d", count)
	}
}

func TestEnsureSchema_UserNotificationPreferences(t *testing.T) {
	ctx := context.Background()
	s := newTestPostgres(t)
	if err := s.EnsureSchema(ctx); err != nil {
		t.Fatalf("first EnsureSchema: %v", err)
	}
	if err := s.EnsureSchema(ctx); err != nil {
		t.Fatalf("second EnsureSchema: %v", err)
	}
	var count int
	err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM information_schema.columns
		WHERE table_name = 'user_notification_preferences'
		  AND column_name IN ('user_id','notify_in_app','notify_discord_announcements','updated_at')
	`).Scan(&count)
	if err != nil {
		t.Fatalf("count columns: %v", err)
	}
	if count != 4 {
		t.Fatalf("expected 4 columns on user_notification_preferences, got %d", count)
	}
}
