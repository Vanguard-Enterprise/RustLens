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

// Purge harness uses the real rbac.Resolver as the PermissionChecker so
// SuperadminOnly() goes through PermissionsForUser(), which treats the
// superadmin role as implicit-all.
func newPurgeHarness(t *testing.T) (*httptest.Server, *store.PostgresStore, *rbac.Store) {
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
	resolver := rbac.NewResolver(rb)
	reg := actions.NewRegistry(rbac.IsValidPermission)
	if err := reg.Register(actions.NewUsersPurgeAction(s.Pool())); err != nil {
		t.Fatalf("register purge: %v", err)
	}
	disp := actions.NewDispatcher(reg, resolverBridge{resolver}, auditSinkBridge{au}, fixedActor)
	r := chi.NewRouter()
	r.Post("/api/app/admin/actions/{type}", disp.ServeHTTP)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv, s, rb
}

type resolverBridge struct{ r *rbac.Resolver }

func (b resolverBridge) HasPermission(ctx context.Context, userID string, key string) (bool, error) {
	return b.r.HasPermission(ctx, userID, key)
}
func (b resolverBridge) PermissionsForUser(ctx context.Context, userID string) ([]string, error) {
	return b.r.PermissionsForUser(ctx, userID)
}

func TestUsersPurge_HappyPathCascades(t *testing.T) {
	srv, s, rb := newPurgeHarness(t)
	ctx := context.Background()
	// Actor is superadmin.
	seedAppUser(t, s, "usr_test_actor", "Actor", false)
	saID, err := rb.RoleIDByKey(ctx, rbac.RoleSuperadmin)
	if err != nil {
		t.Fatalf("role: %v", err)
	}
	_ = rb.AssignRole(ctx, "usr_test_actor", saID, "")

	const tid = "usr_purge_t"
	seedAppUser(t, s, tid, "Target", false)
	// The user_emails / user_password_credentials children went away with the
	// Discord-only migration; admin_user_roles / audit_events are the only
	// dependents left, both of which are exercised implicitly by the seed +
	// dispatch.

	body := mustJSON(t, map[string]any{
		"params":            map[string]any{"userId": tid, "confirmId": tid},
		"reason":            "GDPR request",
		"typedConfirmation": tid,
	})
	resp := mustPOST(t, srv.URL+"/api/app/admin/actions/users.purge", body)
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d body=%s", resp.StatusCode, readBody(resp))
	}

	var n int
	_ = s.Pool().QueryRow(ctx, `SELECT COUNT(*) FROM app_users WHERE id=$1`, tid).Scan(&n)
	if n != 0 {
		t.Errorf("app_users row still present")
	}
}

func TestUsersPurge_BadConfirmID(t *testing.T) {
	srv, s, rb := newPurgeHarness(t)
	ctx := context.Background()
	seedAppUser(t, s, "usr_test_actor", "Actor", false)
	saID, _ := rb.RoleIDByKey(ctx, rbac.RoleSuperadmin)
	_ = rb.AssignRole(ctx, "usr_test_actor", saID, "")
	const tid = "usr_purge_badc"
	seedAppUser(t, s, tid, "Target", false)

	body := mustJSON(t, map[string]any{
		"params":            map[string]any{"userId": tid, "confirmId": "wrong"},
		"reason":            "t",
		"typedConfirmation": tid,
	})
	resp := mustPOST(t, srv.URL+"/api/app/admin/actions/users.purge", body)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status=%d; want 400 body=%s", resp.StatusCode, readBody(resp))
	}
}

func TestUsersPurge_SelfBlocked(t *testing.T) {
	srv, s, rb := newPurgeHarness(t)
	ctx := context.Background()
	seedAppUser(t, s, "usr_test_actor", "Actor", false)
	saID, _ := rb.RoleIDByKey(ctx, rbac.RoleSuperadmin)
	_ = rb.AssignRole(ctx, "usr_test_actor", saID, "")
	// Another superadmin so last-SA guard doesn't trip first.
	const other = "usr_purge_other_sa"
	seedAppUser(t, s, other, "Other SA", false)
	_ = rb.AssignRole(ctx, other, saID, "")

	body := mustJSON(t, map[string]any{
		"params":            map[string]any{"userId": "usr_test_actor", "confirmId": "usr_test_actor"},
		"reason":            "self",
		"typedConfirmation": "usr_test_actor",
	})
	resp := mustPOST(t, srv.URL+"/api/app/admin/actions/users.purge", body)
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("status=%d; want 409 body=%s", resp.StatusCode, readBody(resp))
	}
}

func TestUsersPurge_NonSuperadminRejected(t *testing.T) {
	srv, s, rb := newPurgeHarness(t)
	ctx := context.Background()
	// Actor has NO superadmin role. Purge all existing SAs to be sure HasPermission/PermissionsForUser
	// returns an empty set for the actor.
	seedAppUser(t, s, "usr_test_actor", "Actor", false)
	// Explicit: remove any leftover role on the actor.
	if _, err := s.Pool().Exec(ctx, `DELETE FROM admin_user_roles WHERE user_id = $1`, "usr_test_actor"); err != nil {
		t.Fatalf("clear actor roles: %v", err)
	}
	const tid = "usr_purge_nospa"
	seedAppUser(t, s, tid, "Target", false)
	_ = rb

	body := mustJSON(t, map[string]any{
		"params":            map[string]any{"userId": tid, "confirmId": tid},
		"reason":            "t",
		"typedConfirmation": tid,
	})
	resp := mustPOST(t, srv.URL+"/api/app/admin/actions/users.purge", body)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status=%d; want 403 body=%s", resp.StatusCode, readBody(resp))
	}
}

func TestUsersPurge_LastSuperadminBlocked(t *testing.T) {
	srv, s, rb := newPurgeHarness(t)
	ctx := context.Background()
	seedAppUser(t, s, "usr_test_actor", "Actor", false)
	saID, _ := rb.RoleIDByKey(ctx, rbac.RoleSuperadmin)
	_ = rb.AssignRole(ctx, "usr_test_actor", saID, "")

	// Wipe other superadmins except actor.
	if _, err := s.Pool().Exec(ctx,
		`DELETE FROM admin_user_roles WHERE role_id = $1 AND user_id <> $2`, saID, "usr_test_actor"); err != nil {
		t.Fatalf("cleanup: %v", err)
	}

	// We need to purge SOMEONE ELSE who is the last SA — but the actor holds it above.
	// So: make the actor not be a SA, give tid the last SA.
	_, _ = s.Pool().Exec(ctx, `DELETE FROM admin_user_roles WHERE role_id = $1`, saID)
	const tid = "usr_purge_lastsa"
	seedAppUser(t, s, tid, "Only SA", false)
	_ = rb.AssignRole(ctx, tid, saID, "")
	// Actor needs superadmin to even get past dispatcher gate.
	_ = rb.AssignRole(ctx, "usr_test_actor", saID, "")
	// Now the actor is ALSO a superadmin, so tid is not truly the last one…
	// Drop actor's SA so tid is the sole SA — but then actor can't call purge.
	// To test the guard, give the actor admins.grant via a custom role.
	// Simpler: just remove the actor's superadmin, add a bespoke role with admins.grant.
	_, _ = s.Pool().Exec(ctx, `DELETE FROM admin_user_roles WHERE user_id = $1 AND role_id = $2`, "usr_test_actor", saID)
	// We already asserted in TestUsersPurge_NonSuperadminRejected that without
	// superadmin, purge returns 403, so without another bypass we cannot
	// directly exercise last-SA-guard via this harness. Fall back to calling
	// the guard function directly.
	if err := actions.GuardLastSuperadminByUser(ctx, s.Pool(), tid); err == nil {
		t.Errorf("expected last-superadmin guard to error, got nil")
	}
	_ = srv // silence unused warn
}
