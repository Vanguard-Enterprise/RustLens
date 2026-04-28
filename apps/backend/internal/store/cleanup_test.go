package store

import (
	"context"
	"testing"
	"time"
)

func TestPurgeWipeReactsBefore_DeletesOnlyOldRows(t *testing.T) {
	ctx := context.Background()
	s := newTestPostgres(t)

	now := time.Now()
	old := now.Add(-10 * 24 * time.Hour)   // 10 days ago — should be deleted
	recent := now.Add(-2 * 24 * time.Hour) // 2 days ago — should be kept

	insertWipeReact(t, s, "msg-old", old.Unix())
	insertWipeReact(t, s, "msg-recent", recent.Unix())

	cutoff := now.Add(-7 * 24 * time.Hour)
	rows, err := s.PurgeWipeReactsBefore(ctx, cutoff)
	if err != nil {
		t.Fatalf("PurgeWipeReactsBefore: %v", err)
	}
	if rows != 1 {
		t.Errorf("expected 1 row deleted, got %d", rows)
	}

	if !rowExists(t, s, "wipe_reacts", "message_id", "msg-recent") {
		t.Error("recent wipe react was deleted")
	}
	if rowExists(t, s, "wipe_reacts", "message_id", "msg-old") {
		t.Error("old wipe react was not deleted")
	}
}

func TestPurgeTrackAnalyticsSessionsCacheBefore_DeletesOnlyStale(t *testing.T) {
	ctx := context.Background()
	s := newTestPostgres(t)

	old := time.Now().Add(-72 * time.Hour)
	fresh := time.Now().Add(-1 * time.Hour)

	insertAnalyticsCache(t, s, "p-old", "srv-1", old)
	insertAnalyticsCache(t, s, "p-fresh", "srv-1", fresh)

	cutoff := time.Now().Add(-48 * time.Hour)
	rows, err := s.PurgeTrackAnalyticsSessionsCacheBefore(ctx, cutoff)
	if err != nil {
		t.Fatalf("PurgeTrackAnalyticsSessionsCacheBefore: %v", err)
	}
	if rows != 1 {
		t.Errorf("expected 1 row deleted, got %d", rows)
	}
}

func TestPurgeExpiredAuthTokensBefore_DeletesConsumedAndExpired(t *testing.T) {
	ctx := context.Background()
	s := newTestPostgres(t)

	// Set up a user/email to satisfy FK constraints.
	if _, err := s.pool.Exec(ctx, `INSERT INTO app_users (id, status) VALUES ($1, 'active') ON CONFLICT DO NOTHING`, "user-cleanup"); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO user_emails (email, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, "cleanup@example.com", "user-cleanup"); err != nil {
		t.Fatalf("insert email: %v", err)
	}

	now := time.Now()
	insertAuthToken(t, s, "password_reset_tokens", "tok-expired", "user-cleanup", "cleanup@example.com", now.Add(-2*24*time.Hour), nil)
	consumedAt := now.Add(-3 * time.Hour)
	insertAuthToken(t, s, "password_reset_tokens", "tok-consumed", "user-cleanup", "cleanup@example.com", now.Add(48*time.Hour), &consumedAt)
	insertAuthToken(t, s, "password_reset_tokens", "tok-active", "user-cleanup", "cleanup@example.com", now.Add(48*time.Hour), nil)

	rows, err := s.PurgeExpiredAuthTokensBefore(ctx, now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("PurgeExpiredAuthTokensBefore: %v", err)
	}
	if rows != 2 {
		t.Errorf("expected 2 rows deleted, got %d", rows)
	}
	if !rowExists(t, s, "password_reset_tokens", "token_hash", "tok-active") {
		t.Error("active reset token was deleted")
	}
	if rowExists(t, s, "password_reset_tokens", "token_hash", "tok-expired") {
		t.Error("expired reset token was not deleted")
	}
	if rowExists(t, s, "password_reset_tokens", "token_hash", "tok-consumed") {
		t.Error("consumed reset token was not deleted")
	}
}

func insertWipeReact(t *testing.T, s *PostgresStore, messageID string, scheduledUnix int64) {
	t.Helper()
	_, err := s.pool.Exec(context.Background(), `
		INSERT INTO wipe_reacts (
			message_id, guild_id, channel_id, creator_id,
			name, time_zone, scheduled_unix, embed_timestamp
		) VALUES ($1, '1', '2', '3', 'test', 'UTC', $2, '2026-01-01T00:00:00Z')
	`, messageID, scheduledUnix)
	if err != nil {
		t.Fatalf("insert wipe react %s: %v", messageID, err)
	}
}

func insertAnalyticsCache(t *testing.T, s *PostgresStore, playerBmID, serverBmID string, fetchedAt time.Time) {
	t.Helper()
	_, err := s.pool.Exec(context.Background(), `
		INSERT INTO track_analytics_sessions_cache (player_bm_id, server_bm_id, sessions, fetched_at)
		VALUES ($1, $2, '[]'::jsonb, $3)
	`, playerBmID, serverBmID, fetchedAt)
	if err != nil {
		t.Fatalf("insert analytics cache: %v", err)
	}
}

func insertAuthToken(t *testing.T, s *PostgresStore, table, tokenHash, userID, email string, expiresAt time.Time, consumedAt *time.Time) {
	t.Helper()
	query := `INSERT INTO ` + table + ` (token_hash, user_id, email, expires_at, consumed_at) VALUES ($1, $2, $3, $4, $5)`
	_, err := s.pool.Exec(context.Background(), query, tokenHash, userID, email, expiresAt, consumedAt)
	if err != nil {
		t.Fatalf("insert auth token %s: %v", tokenHash, err)
	}
}

func rowExists(t *testing.T, s *PostgresStore, table, column, value string) bool {
	t.Helper()
	var exists bool
	err := s.pool.QueryRow(context.Background(),
		`SELECT EXISTS(SELECT 1 FROM `+table+` WHERE `+column+` = $1)`,
		value,
	).Scan(&exists)
	if err != nil {
		t.Fatalf("rowExists %s.%s=%s: %v", table, column, value, err)
	}
	return exists
}
