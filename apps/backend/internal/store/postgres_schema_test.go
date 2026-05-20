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

func TestEnsureSchema_CreatesCommandUsageTable(t *testing.T) {
	ctx := context.Background()
	s := newTestPostgres(t)
	if err := s.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	var exists bool
	if err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			WHERE table_name = 'command_usage'
		)`).Scan(&exists); err != nil {
		t.Fatalf("query: %v", err)
	}
	if !exists {
		t.Fatal("command_usage table not created")
	}

	// indexes
	for _, idx := range []string{"command_usage_user_created_idx", "command_usage_created_idx"} {
		var ok bool
		if err := s.pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM pg_indexes
				WHERE schemaname = 'public' AND indexname = $1
			)`, idx).Scan(&ok); err != nil {
			t.Fatalf("index %s query: %v", idx, err)
		}
		if !ok {
			t.Fatalf("expected index %s", idx)
		}
	}
}
