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
	"rustlens/backend/internal/events"
	"rustlens/backend/internal/store"
)

// newRolesHarness wires the real rbac.Resolver (so grant's admins.grant
// self-check is real) plus a fake dispatcher-level permission resolver
// whose `allow` map we can toggle per test.
func newRolesHarness(t *testing.T) (*httptest.Server, *store.PostgresStore, *rbac.Store, func(map[string]bool), *events.RecordingPublisher) {
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
	// Do not TRUNCATE: shared DB across packages. Tests use unique ids.
	_ = ctx
	rb := rbac.NewStore(s.Pool())
	if err := rb.SeedPermissionsAndRoles(ctx); err != nil {
		t.Fatalf("seed: %v", err)
	}
	realResolver := rbac.NewResolver(rb)
	au := audit.NewStore(s.Pool())

	pub := &events.RecordingPublisher{}
	perms := &fakePermResolver{allow: map[string]bool{}}
	reg := actions.NewRegistry(rbac.IsValidPermission)
	if err := reg.Register(actions.NewUsersRolesGrantAction(s.Pool(), realResolver, pub)); err != nil {
		t.Fatalf("register grant: %v", err)
	}
	if err := reg.Register(actions.NewUsersRolesRevokeAction(s.Pool(), pub)); err != nil {
		t.Fatalf("register revoke: %v", err)
	}

	disp := actions.NewDispatcher(reg, perms, auditSinkBridge{au}, fixedActor)
	r := chi.NewRouter()
	r.Post("/api/app/admin/actions/{type}", disp.ServeHTTP)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv, s, rb, func(a map[string]bool) { perms.allow = a }, pub
}

func assignRole(t *testing.T, rb *rbac.Store, userID, roleKey string) {
	t.Helper()
	ctx := context.Background()
	id, err := rb.RoleIDByKey(ctx, roleKey)
	if err != nil {
		t.Fatalf("role id: %v", err)
	}
	if err := rb.AssignRole(ctx, userID, id, ""); err != nil {
		t.Fatalf("assign: %v", err)
	}
}

func countRoles(t *testing.T, s *store.PostgresStore, userID, roleKey string) int {
	t.Helper()
	var n int
	err := s.Pool().QueryRow(context.Background(),
		`SELECT COUNT(*) FROM admin_user_roles aur JOIN admin_roles r ON r.id=aur.role_id WHERE aur.user_id=$1 AND r.key=$2`,
		userID, roleKey).Scan(&n)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func TestUsersRolesGrant_HappyPathAndIdempotent(t *testing.T) {
	srv, s, _, setPerms, _ := newRolesHarness(t)
	setPerms(map[string]bool{rbac.PermUsersRolesAssign: true})
	seedAppUser(t, s, "usr_test_actor", "Actor", false)
	seedAppUser(t, s, "usr_target", "Target", false)

	body := mustJSON(t, map[string]any{
		"params": map[string]any{"userId": "usr_target", "roleKey": rbac.RoleModerator},
		"reason": "promotion",
	})
	resp := mustPOST(t, srv.URL+"/api/app/admin/actions/users.roles.grant", body)
	if resp.StatusCode != 200 {
		t.Fatalf("grant status=%d body=%s", resp.StatusCode, readBody(resp))
	}
	if n := countRoles(t, s, "usr_target", rbac.RoleModerator); n != 1 {
		t.Errorf("after grant count=%d", n)
	}

	// Re-grant is a no-op (still emits audit row).
	resp2 := mustPOST(t, srv.URL+"/api/app/admin/actions/users.roles.grant", body)
	if resp2.StatusCode != 200 {
		t.Fatalf("regrant status=%d body=%s", resp2.StatusCode, readBody(resp2))
	}
	if n := countRoles(t, s, "usr_target", rbac.RoleModerator); n != 1 {
		t.Errorf("after regrant count=%d", n)
	}
}

func TestUsersRolesGrant_UnknownRole(t *testing.T) {
	srv, s, _, setPerms, _ := newRolesHarness(t)
	setPerms(map[string]bool{rbac.PermUsersRolesAssign: true})
	seedAppUser(t, s, "usr_test_actor", "Actor", false)
	seedAppUser(t, s, "usr_target", "Target", false)

	body := mustJSON(t, map[string]any{
		"params": map[string]any{"userId": "usr_target", "roleKey": "does_not_exist"},
		"reason": "test",
	})
	resp := mustPOST(t, srv.URL+"/api/app/admin/actions/users.roles.grant", body)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status=%d; want 404", resp.StatusCode)
	}
}

func TestUsersRolesGrant_SuperadminRequiresAdminsGrant(t *testing.T) {
	srv, s, _, setPerms, _ := newRolesHarness(t)
	// Dispatcher-level perm OK, but real resolver will say actor lacks admins.grant.
	setPerms(map[string]bool{rbac.PermUsersRolesAssign: true})
	seedAppUser(t, s, "usr_test_actor", "Actor", false)
	seedAppUser(t, s, "usr_target", "Target", false)

	body := mustJSON(t, map[string]any{
		"params": map[string]any{"userId": "usr_target", "roleKey": rbac.RoleSuperadmin},
		"reason": "t",
	})
	resp := mustPOST(t, srv.URL+"/api/app/admin/actions/users.roles.grant", body)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status=%d; want 403 body=%s", resp.StatusCode, readBody(resp))
	}
}

func TestUsersRolesGrant_SuperadminAllowedWhenActorIsSuperadmin(t *testing.T) {
	srv, s, rb, setPerms, _ := newRolesHarness(t)
	setPerms(map[string]bool{rbac.PermUsersRolesAssign: true})
	seedAppUser(t, s, "usr_test_actor", "Actor", false)
	seedAppUser(t, s, "usr_target", "Target", false)
	// Give actor superadmin (implies admins.grant via rbac.PermissionsForUser).
	assignRole(t, rb, "usr_test_actor", rbac.RoleSuperadmin)

	body := mustJSON(t, map[string]any{
		"params": map[string]any{"userId": "usr_target", "roleKey": rbac.RoleSuperadmin},
		"reason": "t",
	})
	resp := mustPOST(t, srv.URL+"/api/app/admin/actions/users.roles.grant", body)
	if resp.StatusCode != 200 {
		t.Errorf("status=%d body=%s", resp.StatusCode, readBody(resp))
	}
	if n := countRoles(t, s, "usr_target", rbac.RoleSuperadmin); n != 1 {
		t.Errorf("target superadmin count=%d", n)
	}
}

func TestUsersRolesRevoke_HappyPath(t *testing.T) {
	srv, s, rb, setPerms, _ := newRolesHarness(t)
	setPerms(map[string]bool{rbac.PermUsersRolesAssign: true})
	seedAppUser(t, s, "usr_test_actor", "Actor", false)
	seedAppUser(t, s, "usr_target", "Target", false)
	assignRole(t, rb, "usr_target", rbac.RoleModerator)

	body := mustJSON(t, map[string]any{
		"params": map[string]any{"userId": "usr_target", "roleKey": rbac.RoleModerator},
		"reason": "cleanup",
	})
	resp := mustPOST(t, srv.URL+"/api/app/admin/actions/users.roles.revoke", body)
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d body=%s", resp.StatusCode, readBody(resp))
	}
	if n := countRoles(t, s, "usr_target", rbac.RoleModerator); n != 0 {
		t.Errorf("after revoke count=%d", n)
	}
}

func TestUsersRolesRevoke_LastSuperadminBlocked(t *testing.T) {
	srv, s, rb, setPerms, _ := newRolesHarness(t)
	setPerms(map[string]bool{rbac.PermUsersRolesAssign: true})
	// Purge all existing superadmin assignments so "only one" actually holds.
	if _, err := s.Pool().Exec(context.Background(),
		`DELETE FROM admin_user_roles WHERE role_id IN (SELECT id FROM admin_roles WHERE key=$1)`,
		rbac.RoleSuperadmin); err != nil {
		t.Fatalf("purge sa: %v", err)
	}
	seedAppUser(t, s, "usr_test_actor", "Actor", false)
	seedAppUser(t, s, "usr_the_only_sa", "Solo", false)
	assignRole(t, rb, "usr_the_only_sa", rbac.RoleSuperadmin)

	body := mustJSON(t, map[string]any{
		"params": map[string]any{"userId": "usr_the_only_sa", "roleKey": rbac.RoleSuperadmin},
		"reason": "boom",
	})
	resp := mustPOST(t, srv.URL+"/api/app/admin/actions/users.roles.revoke", body)
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("status=%d; want 409 body=%s", resp.StatusCode, readBody(resp))
	}
	if n := countRoles(t, s, "usr_the_only_sa", rbac.RoleSuperadmin); n != 1 {
		t.Errorf("superadmin still there expected; count=%d", n)
	}
}

func TestUsersRolesGrant_PublishesRolesChangedEvent(t *testing.T) {
	srv, s, _, setPerms, pub := newRolesHarness(t)
	setPerms(map[string]bool{rbac.PermUsersRolesAssign: true})
	seedAppUser(t, s, "usr_test_actor", "Actor", false)
	seedAppUser(t, s, "usr_pub_target", "Target", false)

	body := mustJSON(t, map[string]any{
		"params": map[string]any{"userId": "usr_pub_target", "roleKey": rbac.RoleModerator},
		"reason": "promotion",
	})
	resp := mustPOST(t, srv.URL+"/api/app/admin/actions/users.roles.grant", body)
	if resp.StatusCode != 200 {
		t.Fatalf("grant status=%d body=%s", resp.StatusCode, readBody(resp))
	}

	recs := pub.Snapshot()
	if len(recs) != 1 {
		t.Fatalf("expected 1 publish, got %d", len(recs))
	}
	if recs[0].UserID != "usr_pub_target" {
		t.Fatalf("publish userID = %q", recs[0].UserID)
	}
	if recs[0].Event.Type != events.TypeRolesChanged || recs[0].Event.Reason != events.ReasonGrant {
		t.Fatalf("publish event = %+v", recs[0].Event)
	}
}
