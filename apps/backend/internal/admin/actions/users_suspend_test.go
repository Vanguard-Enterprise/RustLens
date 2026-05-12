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

func newSuspendHarness(t *testing.T) (*httptest.Server, *store.PostgresStore, *rbac.Store, func(map[string]bool)) {
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
	if err := reg.Register(actions.NewUsersSuspendAction(s.Pool(), events.NopPublisher{})); err != nil {
		t.Fatalf("register suspend: %v", err)
	}
	if err := reg.Register(actions.NewUsersUnsuspendAction(s.Pool())); err != nil {
		t.Fatalf("register unsuspend: %v", err)
	}

	disp := actions.NewDispatcher(reg, perms, auditSinkBridge{au}, fixedActor)
	r := chi.NewRouter()
	r.Post("/api/app/admin/actions/{type}", disp.ServeHTTP)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv, s, rb, func(a map[string]bool) { perms.allow = a }
}

func readSuspended(t *testing.T, s *store.PostgresStore, userID string) (bool, int) {
	t.Helper()
	var suspended *string
	var epoch int
	if err := s.Pool().QueryRow(context.Background(),
		`SELECT suspended_at::text, session_epoch FROM app_users WHERE id=$1`, userID).
		Scan(&suspended, &epoch); err != nil {
		t.Fatalf("query: %v", err)
	}
	return suspended != nil, epoch
}

func TestUsersSuspend_HappyPathBumpsEpoch(t *testing.T) {
	srv, s, _, setPerms := newSuspendHarness(t)
	setPerms(map[string]bool{rbac.PermUsersSuspend: true})
	seedAppUser(t, s, "usr_test_actor", "Actor", false)
	const tid = "usr_susp_target"
	seedAppUser(t, s, tid, "Target", false)
	// Ensure target is not already suspended from a prior run.
	if _, err := s.Pool().Exec(context.Background(),
		`UPDATE app_users SET suspended_at = NULL, suspended_by = NULL, suspend_reason = NULL WHERE id = $1`, tid); err != nil {
		t.Fatalf("reset: %v", err)
	}
	_, epochBefore := readSuspended(t, s, tid)

	body := mustJSON(t, map[string]any{
		"params":            map[string]any{"userId": tid},
		"reason":            "abuse",
		"typedConfirmation": tid,
	})
	resp := mustPOST(t, srv.URL+"/api/app/admin/actions/users.suspend", body)
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d body=%s", resp.StatusCode, readBody(resp))
	}
	suspended, epochAfter := readSuspended(t, s, tid)
	if !suspended {
		t.Errorf("expected suspended_at set")
	}
	if epochAfter <= epochBefore {
		t.Errorf("epoch not bumped: before=%d after=%d", epochBefore, epochAfter)
	}
}

func TestUsersSuspend_SelfSuspendBlocked(t *testing.T) {
	srv, s, _, setPerms := newSuspendHarness(t)
	setPerms(map[string]bool{rbac.PermUsersSuspend: true})
	// fixedActor returns usr_test_actor
	seedAppUser(t, s, "usr_test_actor", "Actor", false)

	body := mustJSON(t, map[string]any{
		"params":            map[string]any{"userId": "usr_test_actor"},
		"reason":            "self",
		"typedConfirmation": "usr_test_actor",
	})
	resp := mustPOST(t, srv.URL+"/api/app/admin/actions/users.suspend", body)
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("status=%d; want 409 body=%s", resp.StatusCode, readBody(resp))
	}
}

func TestUsersSuspend_LastSuperadminBlocked(t *testing.T) {
	srv, s, rb, setPerms := newSuspendHarness(t)
	setPerms(map[string]bool{rbac.PermUsersSuspend: true})
	seedAppUser(t, s, "usr_test_actor", "Actor", false)
	// Purge existing superadmins to isolate the "last one" scenario.
	if _, err := s.Pool().Exec(context.Background(),
		`DELETE FROM admin_user_roles WHERE role_id IN (SELECT id FROM admin_roles WHERE key='superadmin')`); err != nil {
		t.Fatalf("purge sa: %v", err)
	}
	const tid = "usr_susp_lastsa"
	seedAppUser(t, s, tid, "Solo SA", false)
	saID, err := rb.RoleIDByKey(context.Background(), rbac.RoleSuperadmin)
	if err != nil {
		t.Fatalf("role id: %v", err)
	}
	if err := rb.AssignRole(context.Background(), tid, saID, ""); err != nil {
		t.Fatalf("assign: %v", err)
	}

	body := mustJSON(t, map[string]any{
		"params":            map[string]any{"userId": tid},
		"reason":            "boom",
		"typedConfirmation": tid,
	})
	resp := mustPOST(t, srv.URL+"/api/app/admin/actions/users.suspend", body)
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("status=%d; want 409 body=%s", resp.StatusCode, readBody(resp))
	}
}

func TestUsersSuspend_AlreadySuspended(t *testing.T) {
	srv, s, _, setPerms := newSuspendHarness(t)
	setPerms(map[string]bool{rbac.PermUsersSuspend: true})
	seedAppUser(t, s, "usr_test_actor", "Actor", false)
	const tid = "usr_susp_dup"
	seedAppUser(t, s, tid, "Target", false)
	if _, err := s.Pool().Exec(context.Background(),
		`UPDATE app_users SET suspended_at = now() WHERE id = $1`, tid); err != nil {
		t.Fatalf("preseed: %v", err)
	}

	body := mustJSON(t, map[string]any{
		"params":            map[string]any{"userId": tid},
		"reason":            "dup",
		"typedConfirmation": tid,
	})
	resp := mustPOST(t, srv.URL+"/api/app/admin/actions/users.suspend", body)
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("status=%d; want 409", resp.StatusCode)
	}
}

func TestUsersUnsuspend_Clears(t *testing.T) {
	srv, s, _, setPerms := newSuspendHarness(t)
	setPerms(map[string]bool{rbac.PermUsersSuspend: true})
	seedAppUser(t, s, "usr_test_actor", "Actor", false)
	const tid = "usr_unsusp_t"
	seedAppUser(t, s, tid, "Target", false)
	if _, err := s.Pool().Exec(context.Background(),
		`UPDATE app_users SET suspended_at = now(), suspended_by = $2, suspend_reason = 'x' WHERE id = $1`,
		tid, "usr_test_actor"); err != nil {
		t.Fatalf("preseed: %v", err)
	}

	body := mustJSON(t, map[string]any{
		"params": map[string]any{"userId": tid},
	})
	resp := mustPOST(t, srv.URL+"/api/app/admin/actions/users.unsuspend", body)
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d body=%s", resp.StatusCode, readBody(resp))
	}
	suspended, _ := readSuspended(t, s, tid)
	if suspended {
		t.Errorf("expected suspended_at cleared")
	}
}
