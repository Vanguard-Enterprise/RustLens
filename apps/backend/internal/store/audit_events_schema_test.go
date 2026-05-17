package store

import (
	"context"
	"testing"
)

func TestAuditEventsHasViaAPIKeyColumns(t *testing.T) {
	store := newTestPostgres(t)
	ctx := context.Background()

	want := map[string]string{
		"via_api_key_id":   "uuid",
		"via_api_key_name": "text",
	}
	rows, err := store.pool.Query(ctx, `
		SELECT column_name, data_type FROM information_schema.columns
		WHERE table_name = 'audit_events'
	`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	got := map[string]string{}
	for rows.Next() {
		var name, dtype string
		if err := rows.Scan(&name, &dtype); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got[name] = dtype
	}
	for col, dtype := range want {
		if got[col] != dtype {
			t.Errorf("column %q: got data_type=%q, want %q", col, got[col], dtype)
		}
	}
}
