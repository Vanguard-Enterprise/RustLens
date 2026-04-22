package actions_test

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

func newUsersEditHarness(t *testing.T) (*httptest.Server, *store.PostgresStore, func(allow map[string]bool)) {
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
	if _, err := s.Pool().Exec(ctx, `TRUNCATE admin_user_roles, audit_events, app_users RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	rb := rbac.NewStore(s.Pool())
	if err := rb.SeedPermissionsAndRoles(ctx); err != nil {
		t.Fatalf("seed: %v", err)
	}
	au := audit.NewStore(s.Pool())

	perms := &fakePermResolver{allow: map[string]bool{}}
	reg := actions.NewRegistry(rbac.IsValidPermission)
	if err := reg.Register(actions.NewUsersEditAction(s.Pool())); err != nil {
		t.Fatalf("register: %v", err)
	}

	disp := actions.NewDispatcher(reg, perms, auditSinkBridge{au}, fixedActor)
	r := chi.NewRouter()
	r.Post("/api/app/admin/actions/{type}", disp.ServeHTTP)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv, s, func(a map[string]bool) { perms.allow = a }
}

type auditSinkBridge struct{ s *audit.Store }

func (b auditSinkBridge) RecordEntry(ctx context.Context, e actions.AuditEntryInput) error {
	return b.s.Record(ctx, audit.Entry{
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

func seedAppUser(t *testing.T, s *store.PostgresStore, id, name string, deleted bool) {
	t.Helper()
	ctx := context.Background()
	if _, err := s.Pool().Exec(ctx,
		`INSERT INTO app_users (id, display_name) VALUES ($1, $2) ON CONFLICT (id) DO UPDATE SET display_name = EXCLUDED.display_name`,
		id, name); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if deleted {
		if _, err := s.Pool().Exec(ctx, `UPDATE app_users SET deleted_at = now() WHERE id = $1`, id); err != nil {
			t.Fatalf("delete: %v", err)
		}
	}
}

func TestUsersEdit_HappyPath(t *testing.T) {
	srv, s, setPerms := newUsersEditHarness(t)
	setPerms(map[string]bool{rbac.PermUsersEdit: true})
	// Actor also needs an app_users row for the audit_events FK.
	seedAppUser(t, s, "usr_test_actor", "Actor", false)
	seedAppUser(t, s, "usr_target_1", "Old Name", false)

	newName := "New Name"
	body := mustJSON(t, map[string]any{
		"params": map[string]any{"userId": "usr_target_1", "displayName": newName},
	})
	resp := mustPOST(t, srv.URL+"/api/app/admin/actions/users.edit", body)
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d body=%s", resp.StatusCode, readBody(resp))
	}

	var row struct{ DisplayName string }
	if err := s.Pool().QueryRow(context.Background(),
		`SELECT display_name FROM app_users WHERE id=$1`, "usr_target_1").Scan(&row.DisplayName); err != nil {
		t.Fatalf("query: %v", err)
	}
	if row.DisplayName != newName {
		t.Errorf("display_name = %q; want %q", row.DisplayName, newName)
	}

	// One success audit_events row with action_type=users.edit.
	var count int
	if err := s.Pool().QueryRow(context.Background(),
		`SELECT COUNT(*) FROM audit_events WHERE action_type='users.edit' AND result='success'`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("audit rows = %d", count)
	}
}

func TestUsersEdit_ValidationFailures(t *testing.T) {
	srv, s, setPerms := newUsersEditHarness(t)
	setPerms(map[string]bool{rbac.PermUsersEdit: true})
	seedAppUser(t, s, "usr_test_actor", "Actor", false)
	seedAppUser(t, s, "usr_v_1", "v1", false)

	cases := []struct {
		name string
		body map[string]any
	}{
		{"empty userId", map[string]any{"params": map[string]any{"userId": "", "displayName": "x"}}},
		{"no fields", map[string]any{"params": map[string]any{"userId": "usr_v_1"}}},
		{"overlong name", map[string]any{"params": map[string]any{"userId": "usr_v_1", "displayName": string(make([]byte, 81))}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := mustPOST(t, srv.URL+"/api/app/admin/actions/users.edit", mustJSON(t, tc.body))
			if resp.StatusCode != 400 {
				t.Errorf("status=%d body=%s", resp.StatusCode, readBody(resp))
			}
		})
	}
}

func TestUsersEdit_ConflictOnDeletedUser(t *testing.T) {
	srv, s, setPerms := newUsersEditHarness(t)
	setPerms(map[string]bool{rbac.PermUsersEdit: true})
	seedAppUser(t, s, "usr_test_actor", "Actor", false)
	seedAppUser(t, s, "usr_deleted_1", "Del", true)

	body := mustJSON(t, map[string]any{
		"params": map[string]any{"userId": "usr_deleted_1", "displayName": "new"},
	})
	resp := mustPOST(t, srv.URL+"/api/app/admin/actions/users.edit", body)
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("status=%d body=%s; want 409", resp.StatusCode, readBody(resp))
	}
}

func TestUsersEdit_PermissionDenied(t *testing.T) {
	srv, s, setPerms := newUsersEditHarness(t)
	setPerms(map[string]bool{}) // no perms
	seedAppUser(t, s, "usr_target_2", "t", false)

	body := mustJSON(t, map[string]any{
		"params": map[string]any{"userId": "usr_target_2", "displayName": "new"},
	})
	resp := mustPOST(t, srv.URL+"/api/app/admin/actions/users.edit", body)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status=%d body=%s; want 403", resp.StatusCode, readBody(resp))
	}
}

// keep the import graph clean by referencing json/bytes explicitly
var _ = bytes.NewReader
var _ = json.Marshal
