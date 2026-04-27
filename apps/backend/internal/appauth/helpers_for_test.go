package appauth_test

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"rustlens/backend/internal/store"
)

// testPool returns a connected pool to the test Postgres instance. Each
// call truncates app_users CASCADE for isolation. Do NOT use t.Parallel()
// in tests that call testPool unless TEST_POSTGRES_URL points at a per-test
// database — concurrent truncates would race.
//
// Skips the test if TEST_POSTGRES_URL is not set.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_URL not set")
	}
	ctx := context.Background()
	s, err := store.NewPostgresStore(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(s.Close)
	if err := s.EnsureSchema(ctx); err != nil {
		t.Fatalf("schema: %v", err)
	}
	if _, err := s.Pool().Exec(ctx, `TRUNCATE TABLE app_users CASCADE`); err != nil {
		t.Fatalf("truncate app_users: %v", err)
	}
	return s.Pool()
}
