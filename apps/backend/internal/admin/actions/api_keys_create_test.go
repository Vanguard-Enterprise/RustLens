package actions

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAPIKeysCreateValidate(t *testing.T) {
	a := NewAPIKeysCreateAction(nil, &stubPermLoader{perms: []string{"users.read"}})
	cases := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{"missing name", `{"permissions":["users.read"]}`, true},
		{"empty perms ok", `{"name":"k","permissions":[]}`, false},
		{"long name rejected", `{"name":"` + strings.Repeat("x", 65) + `","permissions":[]}`, true},
		{"invalid perm rejected", `{"name":"k","permissions":["nope.invalid"]}`, true},
		{"happy path", `{"name":"k","permissions":["users.read"]}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := a.Validate(json.RawMessage(tc.raw))
			if (err != nil) != tc.wantErr {
				t.Fatalf("got err=%v wantErr=%v", err, tc.wantErr)
			}
		})
	}
}

func TestAPIKeysCreateRejectsEscalation(t *testing.T) {
	pool := newTestPool(t)
	defer pool.Close()
	a := NewAPIKeysCreateAction(pool, &stubPermLoader{perms: []string{"users.read"}})
	_, err := a.Execute(context.Background(), ActionInput{
		ActorUserID: "u1",
		Params:      json.RawMessage(`{"name":"k","permissions":["users.delete"]}`),
		Reason:      "test",
	})
	if err == nil || !strings.Contains(err.Error(), "users.delete") {
		t.Fatalf("expected permission_denied on escalation, got %v", err)
	}
}

func TestAPIKeysCreateHappyPath(t *testing.T) {
	pool := newTestPool(t)
	defer pool.Close()
	userID := seedTestUser(t, pool)
	a := NewAPIKeysCreateAction(pool, &stubPermLoader{perms: []string{"users.read"}})
	result, err := a.Execute(context.Background(), ActionInput{
		ActorUserID: userID,
		Params:      json.RawMessage(`{"name":"k","permissions":["users.read"]}`),
		Reason:      "test",
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	payload, _ := result.Payload.(map[string]any)
	secret, _ := payload["secret"].(string)
	if !strings.HasPrefix(secret, "rl_admin_") || len(secret) < 16 {
		t.Errorf("unexpected secret: %q", secret)
	}
}

// stubPermLoader satisfies the actor-perms loader the action needs.
type stubPermLoader struct{ perms []string }

func (s *stubPermLoader) PermissionsForUser(_ context.Context, _ string) ([]string, error) {
	return s.perms, nil
}

// newTestPool returns a *pgxpool.Pool connected to TEST_POSTGRES_URL.
// It skips if the env var is absent. The pool is NOT the owner of the schema —
// the DB is expected to be pre-migrated by the test environment.
func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	return pool
}

// seedTestUser inserts a minimal app_users row and returns its id.
// app_users requires: id TEXT PK, discord_id TEXT UNIQUE NOT NULL,
// discord_username TEXT NOT NULL, display_name TEXT NOT NULL DEFAULT ''.
func seedTestUser(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	id := fmt.Sprintf("usr_akc_%d", time.Now().UnixNano())
	_, err := pool.Exec(context.Background(),
		`INSERT INTO app_users (id, discord_id, discord_username, display_name)
		 VALUES ($1, $2, $3, $4)`,
		id, "d_"+id, "u_"+id, "Test User",
	)
	if err != nil {
		t.Fatalf("seedTestUser: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM app_users WHERE id = $1`, id)
	})
	return id
}
