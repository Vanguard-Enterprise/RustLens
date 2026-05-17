package store

import (
	"context"
	"testing"
)

func TestAPIKeysTableExists(t *testing.T) {
	pool := newTestPostgres(t)
	defer pool.Close()
	ctx := context.Background()

	var exists bool
	if err := pool.pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM information_schema.tables
			WHERE table_name = 'api_keys'
		)
	`).Scan(&exists); err != nil {
		t.Fatalf("query: %v", err)
	}
	if !exists {
		t.Fatal("expected api_keys table to exist after EnsureSchema")
	}
}

func TestAPIKeysHasExpectedColumns(t *testing.T) {
	pool := newTestPostgres(t)
	defer pool.Close()
	ctx := context.Background()

	want := map[string]bool{
		"id": true, "name": true, "prefix": true, "last4": true,
		"secret_hash": true, "created_by_user_id": true,
		"permissions": true, "expires_at": true,
		"revoked_at": true, "revoked_reason": true,
		"last_used_at": true, "last_used_ip": true,
		"created_at": true, "updated_at": true,
	}
	rows, err := pool.pool.Query(ctx, `
		SELECT column_name FROM information_schema.columns
		WHERE table_name = 'api_keys'
	`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	got := map[string]bool{}
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got[c] = true
	}
	for col := range want {
		if !got[col] {
			t.Errorf("missing column %q", col)
		}
	}
}
