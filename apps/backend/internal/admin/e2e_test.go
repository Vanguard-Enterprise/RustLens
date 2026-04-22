package admin_test

import (
	"bytes"
	"context"
	"encoding/json"
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
