package store

import (
	"context"
	"os"
	"testing"
)

func newTestPostgres(t *testing.T) *PostgresStore {
	t.Helper()
	dsn := os.Getenv("TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_URL not set")
	}
	store, err := NewPostgresStore(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(store.Close)
	if err := store.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	return store
}

func TestEnsureSchema_CreatesAdminTables(t *testing.T) {
	ctx := context.Background()
	store := newTestPostgres(t)
	tables := []string{
		"admin_permissions",
		"admin_roles",
		"admin_role_permissions",
		"admin_user_roles",
		"audit_events",
	}
	for _, table := range tables {
		var exists bool
		err := store.pool.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = $1)`,
			table,
		).Scan(&exists)
		if err != nil {
			t.Fatalf("query %s: %v", table, err)
		}
		if !exists {
			t.Errorf("expected table %q to exist", table)
		}
	}
}

func TestEnsureSchema_CreatesDiscordRoleConnectionsTable(t *testing.T) {
	ctx := context.Background()
	store := newTestPostgres(t)
	var exists bool
	if err := store.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = $1)`,
		"discord_role_connections",
	).Scan(&exists); err != nil {
		t.Fatalf("query: %v", err)
	}
	if !exists {
		t.Fatal("expected discord_role_connections table to exist")
	}
}
