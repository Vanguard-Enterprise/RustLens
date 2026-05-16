package audit_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"rustlens/backend/internal/admin/audit"
	"rustlens/backend/internal/store"
)

func newStore(t *testing.T) *store.PostgresStore {
	t.Helper()
	dsn := os.Getenv("TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_URL not set")
	}
	s, err := store.NewPostgresStore(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(s.Close)
	if err := s.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	return s
}

func TestAudit_WriteAndQuery(t *testing.T) {
	s := newStore(t)
	a := audit.NewStore(s.Pool())
	ctx := context.Background()

	actor := "usr_audit_actor"
	target := "usr_audit_target"
	params, _ := json.Marshal(map[string]string{"reason_tag": "abuse"})

	// Clean residuals from prior runs (shared DB — audit_events is not truncated elsewhere).
	if _, err := s.Pool().Exec(ctx, `DELETE FROM audit_events WHERE actor_user_id::text = $1`, actor); err != nil {
		t.Fatalf("cleanup: %v", err)
	}

	err := a.Record(ctx, audit.Entry{
		ActionType:        "users.suspend",
		ActorUserID:       &actor,
		ActorRoleSnapshot: []string{"moderator"},
		ResourceType:      "user",
		ResourceID:        target,
		TargetUserID:      &target,
		Params:            params,
		Reason:            strPtr("spamming"),
		Result:            "success",
		IPAddress:         strPtr("10.0.0.1"),
		UserAgent:         strPtr("go-test"),
	})
	if err != nil {
		t.Fatalf("record: %v", err)
	}

	rows, err := a.Query(ctx, audit.QueryFilters{ActorUserID: &actor, Limit: 10})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].ActionType != "users.suspend" {
		t.Errorf("action_type = %q", rows[0].ActionType)
	}
	if rows[0].Result != "success" {
		t.Errorf("result = %q", rows[0].Result)
	}
}

func strPtr(s string) *string { return &s }

func TestRecordAndQueryWithViaAPIKey(t *testing.T) {
	s := newStore(t)
	a := audit.NewStore(s.Pool())
	ctx := context.Background()

	keyID := seedTestAPIKey(t, s.Pool())
	if err := a.Record(ctx, audit.Entry{
		ActionType:    "users.read",
		ActorUserID:   strPtr("u1"),
		Result:        "success",
		ViaAPIKeyID:   &keyID,
		ViaAPIKeyName: strPtr("deploy-bot"),
	}); err != nil {
		t.Fatalf("record: %v", err)
	}

	rows, err := a.Query(ctx, audit.QueryFilters{Limit: 10})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("want 1 row, got %d", len(rows))
	}
	r := rows[0]
	if r.ViaAPIKeyID == nil || *r.ViaAPIKeyID != keyID {
		t.Errorf("via_api_key_id: got %v want %s", r.ViaAPIKeyID, keyID)
	}
	if r.ViaAPIKeyName == nil || *r.ViaAPIKeyName != "deploy-bot" {
		t.Errorf("via_api_key_name: got %v", r.ViaAPIKeyName)
	}
}

func seedTestAPIKey(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	ctx := context.Background()
	var userID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO app_users (id, discord_id, discord_username, display_name)
		 VALUES (gen_random_uuid()::text, 'd1', 'tester', 'Tester')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	var id string
	if err := pool.QueryRow(ctx, `
		INSERT INTO api_keys (name, prefix, last4, secret_hash, created_by_user_id, permissions)
		VALUES ('k', 'rl_admin_seed', 'seed', '\x00', $1, ARRAY['users.read'])
		RETURNING id::text
	`, userID).Scan(&id); err != nil {
		t.Fatalf("seed key: %v", err)
	}
	return id
}
