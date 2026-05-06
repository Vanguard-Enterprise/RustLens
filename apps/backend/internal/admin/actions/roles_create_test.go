package actions_test

import (
	"context"
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

// newRolesCRUDHarness wires the real rbac.Resolver plus a fake dispatcher-level
// permission resolver. All four roles.* actions are registered here:
// roles.create, roles.delete, roles.update, and roles.permissions.set.
func newRolesCRUDHarness(t *testing.T) (*httptest.Server, *store.PostgresStore, *rbac.Store, func(map[string]bool)) {
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
	rb := rbac.NewStore(s.Pool())
	if err := rb.SeedPermissionsAndRoles(ctx); err != nil {
		t.Fatalf("seed: %v", err)
	}
	au := audit.NewStore(s.Pool())
	perms := &fakePermResolver{allow: map[string]bool{}}
	reg := actions.NewRegistry(rbac.IsValidPermission)
	if err := reg.Register(actions.NewRolesCreateAction(s.Pool())); err != nil {
		t.Fatalf("reg create: %v", err)
	}
	if err := reg.Register(actions.NewRolesUpdateAction(s.Pool())); err != nil {
		t.Fatalf("reg update: %v", err)
	}
	if err := reg.Register(actions.NewRolesDeleteAction(s.Pool())); err != nil {
		t.Fatalf("reg delete: %v", err)
	}
	if err := reg.Register(actions.NewRolesPermissionsSetAction(s.Pool())); err != nil {
		t.Fatalf("reg permissions set: %v", err)
	}
	disp := actions.NewDispatcher(reg, perms, auditSinkBridge{au}, fixedActor)
	r := chi.NewRouter()
	r.Post("/api/app/admin/actions/{type}", disp.ServeHTTP)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv, s, rb, func(a map[string]bool) { perms.allow = a }
}

func deleteRoleByKey(t *testing.T, s *store.PostgresStore, key string) {
	t.Helper()
	_, _ = s.Pool().Exec(context.Background(), `DELETE FROM admin_roles WHERE key=$1`, key)
}

func TestRolesCreate_HappyPath(t *testing.T) {
	srv, s, _, setPerms := newRolesCRUDHarness(t)
	setPerms(map[string]bool{rbac.PermAdminsGrant: true})
	seedAppUser(t, s, "usr_test_actor", "Actor", false)
	t.Cleanup(func() { deleteRoleByKey(t, s, "rcreate_happy") })

	body := mustJSON(t, map[string]any{
		"params": map[string]any{
			"key":         "rcreate_happy",
			"name":        "Created",
			"description": "for tests",
			"permissions": []string{rbac.PermUsersRead, rbac.PermUsersAuditRead},
		},
		"reason": "test",
	})
	resp := mustPOST(t, srv.URL+"/api/app/admin/actions/roles.create", body)
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d body=%s", resp.StatusCode, readBody(resp))
	}

	var isSystem bool
	if err := s.Pool().QueryRow(context.Background(),
		`SELECT is_system FROM admin_roles WHERE key=$1`, "rcreate_happy",
	).Scan(&isSystem); err != nil {
		t.Fatalf("created row missing: %v", err)
	}
	if isSystem {
		t.Errorf("created role should not be system")
	}

	var permCount int
	if err := s.Pool().QueryRow(context.Background(), `
		SELECT COUNT(*) FROM admin_role_permissions rp
		JOIN admin_roles r ON r.id=rp.role_id WHERE r.key=$1`, "rcreate_happy").Scan(&permCount); err != nil {
		t.Fatal(err)
	}
	if permCount != 2 {
		t.Errorf("perm count = %d, want 2", permCount)
	}
}

func TestRolesCreate_DuplicateKeyRejected(t *testing.T) {
	srv, s, _, setPerms := newRolesCRUDHarness(t)
	setPerms(map[string]bool{rbac.PermAdminsGrant: true})
	seedAppUser(t, s, "usr_test_actor", "Actor", false)

	body := mustJSON(t, map[string]any{
		"params": map[string]any{"key": rbac.RoleModerator, "name": "X", "description": "", "permissions": []string{}},
		"reason": "t",
	})
	resp := mustPOST(t, srv.URL+"/api/app/admin/actions/roles.create", body)
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("system-key collision should be 409; got %d body=%s", resp.StatusCode, readBody(resp))
	}
}

func TestRolesCreate_InvalidKeyFormat(t *testing.T) {
	srv, s, _, setPerms := newRolesCRUDHarness(t)
	setPerms(map[string]bool{rbac.PermAdminsGrant: true})
	seedAppUser(t, s, "usr_test_actor", "Actor", false)

	body := mustJSON(t, map[string]any{
		"params": map[string]any{"key": "Bad-Key!", "name": "X", "description": "", "permissions": []string{}},
		"reason": "t",
	})
	resp := mustPOST(t, srv.URL+"/api/app/admin/actions/roles.create", body)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("bad key format should be 400; got %d body=%s", resp.StatusCode, readBody(resp))
	}
}

func TestRolesCreate_BadPermissionRejected(t *testing.T) {
	srv, s, _, setPerms := newRolesCRUDHarness(t)
	setPerms(map[string]bool{rbac.PermAdminsGrant: true})
	seedAppUser(t, s, "usr_test_actor", "Actor", false)
	t.Cleanup(func() { deleteRoleByKey(t, s, "rcreate_badperm") })

	body := mustJSON(t, map[string]any{
		"params": map[string]any{"key": "rcreate_badperm", "name": "X", "description": "", "permissions": []string{"not.a.permission"}},
		"reason": "t",
	})
	resp := mustPOST(t, srv.URL+"/api/app/admin/actions/roles.create", body)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("bad permission should be 400; got %d body=%s", resp.StatusCode, readBody(resp))
	}
}

func TestRolesCreate_ReasonRequired(t *testing.T) {
	srv, s, _, setPerms := newRolesCRUDHarness(t)
	setPerms(map[string]bool{rbac.PermAdminsGrant: true})
	seedAppUser(t, s, "usr_test_actor", "Actor", false)

	body := mustJSON(t, map[string]any{
		"params": map[string]any{"key": "rcreate_noreason", "name": "X", "description": "", "permissions": []string{}},
	})
	resp := mustPOST(t, srv.URL+"/api/app/admin/actions/roles.create", body)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("missing reason should be 400; got %d", resp.StatusCode)
	}
}

func TestRolesCreate_PermissionDenied(t *testing.T) {
	srv, s, _, setPerms := newRolesCRUDHarness(t)
	setPerms(map[string]bool{}) // no admins.grant
	seedAppUser(t, s, "usr_test_actor", "Actor", false)

	body := mustJSON(t, map[string]any{
		"params": map[string]any{"key": "rcreate_no_perm", "name": "X", "description": "", "permissions": []string{}},
		"reason": "t",
	})
	resp := mustPOST(t, srv.URL+"/api/app/admin/actions/roles.create", body)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("missing admins.grant should be 403; got %d", resp.StatusCode)
	}
}
