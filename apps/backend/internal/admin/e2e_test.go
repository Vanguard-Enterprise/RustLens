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
	"time"

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
	if _, err := s.Pool().Exec(ctx, `INSERT INTO app_users (id) VALUES ($1) ON CONFLICT (id) DO NOTHING`, userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
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

// TestAdminUsers_BulkSuspendE2E exercises the admin foundation end-to-end for
// plan 5: invite two users, bulk-suspend them, and assert the audit trail.
// CSV export is covered at the handler layer in httpapi (requires rbacResolver
// wiring that this thin e2e harness intentionally omits).
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
	if _, err := s.Pool().Exec(ctx, `INSERT INTO app_users (id) VALUES ($1) ON CONFLICT (id) DO NOTHING`, actorID); err != nil {
		t.Fatalf("seed actor: %v", err)
	}
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
	if err := reg.Register(actions.NewUsersInviteAction(s.Pool(), "https://example.com")); err != nil {
		t.Fatalf("register invite: %v", err)
	}
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

	invite := func(email string) string {
		t.Helper()
		body, _ := json.Marshal(map[string]any{
			"params":            map[string]any{"email": email, "displayName": email},
			"reason":            "invite",
			"typedConfirmation": email,
		})
		req := httptest.NewRequest("POST", "/api/app/admin/actions/users.invite", bytes.NewReader(body))
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		if rr.Code != 200 {
			t.Fatalf("invite %s: status=%d body=%s", email, rr.Code, rr.Body.String())
		}
		var resp struct {
			Result struct {
				UserID string `json:"userId"`
			} `json:"result"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode invite: %v", err)
		}
		if resp.Result.UserID == "" {
			t.Fatalf("invite result missing userId: %s", rr.Body.String())
		}
		return resp.Result.UserID
	}
	// Use a nanosecond-suffixed email so repeated test runs against a shared
	// Postgres don't collide on user_emails.email uniqueness.
	suffix := time.Now().UnixNano()
	u1 := invite(fmt.Sprintf("bulk-e2e-1-%d@example.com", suffix))
	u2 := invite(fmt.Sprintf("bulk-e2e-2-%d@example.com", suffix))

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
		t.Errorf("expected 2/0, got %d/%d", bulkResp.Result.Succeeded, bulkResp.Result.Failed)
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

	// Audit trail: at least 2 users.suspend + 1 users.bulk + 2 users.invite for this actor.
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
	if counts["users.invite"] < 2 {
		t.Errorf("expected >=2 users.invite audit rows, got %d", counts["users.invite"])
	}
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
