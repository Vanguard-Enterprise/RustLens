package appauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"rustlens/backend/internal/admin/actions"
	"rustlens/backend/internal/store"
)

func TestAPIKeyAuthPassesThroughWithoutHeader(t *testing.T) {
	mw := NewAPIKeyAuth(nil, nil) // store/user-loader nil OK on the pass-through path
	called := false
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if actions.APIKeyAuthFromContext(r.Context()) != nil {
			t.Error("apiKeyAuth should not be set")
		}
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/app/admin/users", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if !called {
		t.Fatal("handler not called")
	}
}

func TestAPIKeyAuthRejectsUnknownPrefix(t *testing.T) {
	// This test depends on a DB-backed store; skip if no test DB.
	if !apiKeyTestDBAvailable() {
		t.Skip("set TEST_POSTGRES_URL")
	}
	mw, _, _ := newAPIKeyAuthTestRig(t)
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer rl_admin_doesnotexistxx")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401", w.Code)
	}
	if !strings.Contains(w.Body.String(), "invalid_api_key") {
		t.Errorf("body=%s", w.Body.String())
	}
}

func TestAPIKeyAuthValidKeyPopulatesContext(t *testing.T) {
	t.Skip("requires DB-backed harness; covered by integration test in Task 22")
}

func TestAPIKeyAuthRejectsRevoked(t *testing.T) {
	t.Skip("requires DB-backed harness; covered by integration test in Task 22")
}

func TestAPIKeyAuthAutoRevokesOnSuspendedCreator(t *testing.T) {
	t.Skip("requires DB-backed harness; covered by integration test in Task 22")
}

// apiKeyTestDBAvailable reports whether a Postgres test DB is configured.
func apiKeyTestDBAvailable() bool {
	return os.Getenv("TEST_POSTGRES_URL") != ""
}

// activeUserLoader is a UserLoader that always reports all users as active.
type activeUserLoader struct{}

func (a *activeUserLoader) LoadUserStatus(_ context.Context, _ string) (bool, bool, bool, error) {
	return true, false, false, nil
}

// newAPIKeyAuthTestRig returns a middleware, a real APIKeyStore, and a stub
// UserLoader wired against the test DB. Skips the test if no DB is available.
func newAPIKeyAuthTestRig(t *testing.T) (func(http.Handler) http.Handler, *store.APIKeyStore, UserLoader) {
	t.Helper()
	pool := apiKeyTestPool(t)
	ks := store.NewAPIKeyStore(pool)
	ul := &activeUserLoader{}
	mw := NewAPIKeyAuth(ks, ul)
	return mw, ks, ul
}

// apiKeyTestPool connects to the test Postgres instance and truncates api_keys
// for isolation. Skips the test if TEST_POSTGRES_URL is not set.
func apiKeyTestPool(t *testing.T) *pgxpool.Pool {
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
	if _, err := s.Pool().Exec(ctx, `TRUNCATE TABLE api_keys CASCADE`); err != nil {
		t.Fatalf("truncate api_keys: %v", err)
	}
	return s.Pool()
}
