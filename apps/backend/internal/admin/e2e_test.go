package admin_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/go-chi/chi/v5"

	"rustlens/backend/internal/admin/actions"
	"rustlens/backend/internal/admin/audit"
	"rustlens/backend/internal/admin/rbac"
	"rustlens/backend/internal/store"
)

type noop struct{}

func (noop) Type() string                                                { return "test.ping" }
func (noop) Permission() string                                          { return "audit.read" }
func (noop) RequiresReason() bool                                        { return false }
func (noop) Dangerous() bool                                             { return false }
func (noop) SensitiveParamKeys() []string                                { return nil }
func (noop) ResourceType() string                                        { return "system" }
func (noop) Validate(_ json.RawMessage) (actions.ValidatedParams, error) { return actions.ValidatedParams{ResourceID: "ping"}, nil }
func (noop) Execute(_ context.Context, _ actions.ActionInput) (actions.ActionResult, error) {
	return actions.ActionResult{ResourceID: "ping"}, nil
}

// seedDiscordUser inserts an app_users row with a Discord identity that
// satisfies the post-migration NOT NULL/UNIQUE constraints.
func seedDiscordUser(ctx context.Context, t *testing.T, s *store.PostgresStore, id string) {
	t.Helper()
	if _, err := s.Pool().Exec(ctx,
		`INSERT INTO app_users (id, discord_id, discord_username)
		 VALUES ($1, $2, $3) ON CONFLICT (id) DO NOTHING`,
		id, "d_"+id, "u_"+id); err != nil {
		t.Fatalf("seed user %s: %v", id, err)
	}
}

func TestAdminFoundation_E2E(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_URL not set")
	}
	ctx := context.Background()
	s, err := store.NewPostgresStore(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer s.Close()

	if err := s.EnsureSchema(ctx); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	rb := rbac.NewStore(s.Pool())
	if err := rb.SeedPermissionsAndRoles(ctx); err != nil {
		t.Fatalf("seed: %v", err)
	}
	resolver := rbac.NewResolver(rb)
	au := audit.NewStore(s.Pool())

	adapter := struct{ *audit.Store }{au}
	type sink struct{ *audit.Store }
	_ = adapter

	reg := actions.NewRegistry(rbac.IsValidPermission)
	if err := reg.Register(noop{}); err != nil {
		t.Fatalf("register: %v", err)
	}

	disp := actions.NewDispatcher(reg, resolver, auditBridge{au}, func(r *http.Request) (string, []string, error) {
		return r.Header.Get("X-Admin-User-ID"), []string{"moderator"}, nil
	})

	r := chi.NewRouter()
	r.Post("/api/app/admin/actions/{type}", disp.ServeHTTP)

	userID := "usr_e2e_actor"

	// Seed app_users row — admin_user_roles.user_id FK references app_users(id).
	seedDiscordUser(ctx, t, s, userID)
	// Drop any residual audit rows for this actor from prior runs (shared DB).
	if _, err := s.Pool().Exec(ctx, `DELETE FROM audit_events WHERE actor_user_id::text = $1`, userID); err != nil {
		t.Fatalf("cleanup audit: %v", err)
	}

	modID, err := rb.RoleIDByKey(ctx, rbac.RoleModerator)
	if err != nil {
		t.Fatalf("role id: %v", err)
	}
	if err := rb.AssignRole(ctx, userID, modID, ""); err != nil {
		t.Fatalf("assign: %v", err)
	}

	req := httptest.NewRequest("POST", "/api/app/admin/actions/test.ping", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("X-Admin-User-ID", userID)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}

	rows, err := au.Query(ctx, audit.QueryFilters{ActorUserID: &userID, Limit: 10})
	if err != nil {
		t.Fatalf("query audit: %v", err)
	}
	if len(rows) != 1 || rows[0].ActionType != "test.ping" || rows[0].Result != "success" {
		t.Errorf("audit rows = %+v", rows)
	}
}

// TestAdminUsers_BulkSuspendE2E exercises the admin foundation end-to-end:
// pre-seed two Discord users, bulk-suspend them, and assert the audit trail.
// The legacy invite-via-email flow is gone, so users are seeded directly.
func TestAdminUsers_BulkSuspendE2E(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_URL not set")
	}
	ctx := context.Background()
	s, err := store.NewPostgresStore(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer s.Close()
	if err := s.EnsureSchema(ctx); err != nil {
		t.Fatalf("schema: %v", err)
	}
	rb := rbac.NewStore(s.Pool())
	if err := rb.SeedPermissionsAndRoles(ctx); err != nil {
		t.Fatalf("seed: %v", err)
	}
	resolver := rbac.NewResolver(rb)
	au := audit.NewStore(s.Pool())

	actorID := "usr_bulk_e2e_actor"
	seedDiscordUser(ctx, t, s, actorID)
	// Drop prior audit rows for this actor so repeated runs don't inflate counts.
	if _, err := s.Pool().Exec(ctx, `DELETE FROM audit_events WHERE actor_user_id::text = $1`, actorID); err != nil {
		t.Fatalf("cleanup audit: %v", err)
	}
	adminID, err := rb.RoleIDByKey(ctx, rbac.RoleUserAdmin)
	if err != nil {
		t.Fatalf("role id: %v", err)
	}
	if err := rb.AssignRole(ctx, actorID, adminID, ""); err != nil {
		t.Fatalf("assign user_admin: %v", err)
	}

	reg := actions.NewRegistry(rbac.IsValidPermission)
	if err := reg.Register(actions.NewUsersSuspendAction(s.Pool())); err != nil {
		t.Fatalf("register suspend: %v", err)
	}
	if err := reg.Register(actions.NewUsersUnsuspendAction(s.Pool())); err != nil {
		t.Fatalf("register unsuspend: %v", err)
	}
	disp := actions.NewDispatcher(reg, resolver, auditBridge{au}, func(r *http.Request) (string, []string, error) {
		return actorID, []string{"user_admin"}, nil
	})
	if err := reg.Register(actions.NewUsersBulkAction(disp)); err != nil {
		t.Fatalf("register bulk: %v", err)
	}

	r := chi.NewRouter()
	r.Post("/api/app/admin/actions/{type}", disp.ServeHTTP)

	// Seed two unique target users.
	u1 := fmt.Sprintf("usr_bulk_e2e_t1_%d", randSuffixCounter())
	u2 := fmt.Sprintf("usr_bulk_e2e_t2_%d", randSuffixCounter())
	seedDiscordUser(ctx, t, s, u1)
	seedDiscordUser(ctx, t, s, u2)
	// Make sure repeated runs against a shared DB start with a fresh
	// (un-suspended) target — seed uses ON CONFLICT DO NOTHING.
	if _, err := s.Pool().Exec(ctx,
		`UPDATE app_users SET suspended_at = NULL, suspended_by = NULL, suspend_reason = NULL, deleted_at = NULL WHERE id IN ($1, $2)`,
		u1, u2); err != nil {
		t.Fatalf("reset users: %v", err)
	}

	// Bulk-suspend both.
	bulkBody, _ := json.Marshal(map[string]any{
		"params": map[string]any{
			"action":  "suspend",
			"userIds": []string{u1, u2},
		},
		"reason": "bulk e2e",
	})
	req := httptest.NewRequest("POST", "/api/app/admin/actions/users.bulk", bytes.NewReader(bulkBody))
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("bulk: status=%d body=%s", rr.Code, rr.Body.String())
	}
	var bulkResp struct {
		Result struct {
			Succeeded int `json:"succeeded"`
			Failed    int `json:"failed"`
		} `json:"result"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &bulkResp); err != nil {
		t.Fatalf("decode bulk: %v", err)
	}
	if bulkResp.Result.Succeeded != 2 || bulkResp.Result.Failed != 0 {
		t.Errorf("expected 2/0, got %d/%d body=%s", bulkResp.Result.Succeeded, bulkResp.Result.Failed, rr.Body.String())
	}

	// Both users should be suspended now.
	for _, id := range []string{u1, u2} {
		var suspended *string
		if err := s.Pool().QueryRow(ctx, `SELECT suspended_at::text FROM app_users WHERE id = $1`, id).Scan(&suspended); err != nil {
			t.Fatalf("query %s: %v", id, err)
		}
		if suspended == nil {
			t.Errorf("expected %s suspended", id)
		}
	}

	// Audit trail: at least 2 users.suspend + 1 users.bulk for this actor.
	rows, err := au.Query(ctx, audit.QueryFilters{ActorUserID: &actorID, Limit: 50})
	if err != nil {
		t.Fatalf("audit query: %v", err)
	}
	counts := map[string]int{}
	for _, row := range rows {
		counts[row.ActionType]++
	}
	if counts["users.suspend"] < 2 {
		t.Errorf("expected >=2 users.suspend audit rows, got %d", counts["users.suspend"])
	}
	if counts["users.bulk"] < 1 {
		t.Errorf("expected >=1 users.bulk audit row, got %d", counts["users.bulk"])
	}
}

// randSuffixCounter returns a process-unique monotonic int suitable for making
// test fixtures unique within a single run. Time.UnixNano() works too but
// producing duplicates inside the same nanosecond is possible on Windows;
// this is simpler and deterministic.
var e2eCounter int64

func randSuffixCounter() int64 {
	e2eCounter++
	return e2eCounter
}

type auditBridge struct{ s *audit.Store }

func (a auditBridge) RecordEntry(ctx context.Context, e actions.AuditEntryInput) error {
	return a.s.Record(ctx, audit.Entry{
		ActionType:        e.ActionType,
		ActorUserID:       &e.ActorUserID,
		ActorRoleSnapshot: e.ActorRoleSnapshot,
		ResourceType:      e.ResourceType,
		ResourceID:        e.ResourceID,
		TargetUserID:      e.TargetUserID,
		Params:            e.Params,
		Reason:            e.Reason,
		Result:            e.Result,
		ErrorMessage:      e.ErrorMessage,
		IPAddress:         e.IPAddress,
		UserAgent:         e.UserAgent,
	})
}
