package audit_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"

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
