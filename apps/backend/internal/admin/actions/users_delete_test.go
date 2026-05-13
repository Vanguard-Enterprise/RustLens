package actions_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"rustlens/backend/internal/admin/actions"
	"rustlens/backend/internal/admin/audit"
	"rustlens/backend/internal/admin/rbac"
	"rustlens/backend/internal/events"
	"rustlens/backend/internal/store"
)

func newDeleteHarness(t *testing.T) (*httptest.Server, *store.PostgresStore, *rbac.Store, func(map[string]bool)) {
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
	if err := reg.Register(actions.NewUsersDeleteAction(s.Pool(), events.NopPublisher{})); err != nil {
		t.Fatalf("register delete: %v", err)
	}
	disp := actions.NewDispatcher(reg, perms, auditSinkBridge{au}, fixedActor)
	r := chi.NewRouter()
	r.Post("/api/app/admin/actions/{type}", disp.ServeHTTP)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv, s, rb, func(a map[string]bool) { perms.allow = a }
}

func TestUsersDelete_HappyPath(t *testing.T) {
	srv, s, _, setPerms := newDeleteHarness(t)
	setPerms(map[string]bool{rbac.PermUsersDelete: true})
	seedAppUser(t, s, "usr_test_actor", "Actor", false)
	const tid = "usr_del_target"
	seedAppUser(t, s, tid, "Target", false)

	body := mustJSON(t, map[string]any{
		"params":            map[string]any{"userId": tid},
		"reason":            "cleanup",
		"typedConfirmation": tid,
	})
	resp := mustPOST(t, srv.URL+"/api/app/admin/actions/users.delete", body)
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d body=%s", resp.StatusCode, readBody(resp))
	}
	var id string
	err := s.Pool().QueryRow(context.Background(), `SELECT id FROM app_users WHERE id=$1`, tid).Scan(&id)
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("expected row gone; got err=%v id=%s", err, id)
	}
}

func TestUsersDelete_SelfBlocked(t *testing.T) {
	srv, s, _, setPerms := newDeleteHarness(t)
	setPerms(map[string]bool{rbac.PermUsersDelete: true})
	seedAppUser(t, s, "usr_test_actor", "Actor", false)

	body := mustJSON(t, map[string]any{
		"params":            map[string]any{"userId": "usr_test_actor"},
		"reason":            "self",
		"typedConfirmation": "usr_test_actor",
	})
	resp := mustPOST(t, srv.URL+"/api/app/admin/actions/users.delete", body)
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("status=%d; want 409", resp.StatusCode)
	}
}

func TestUsersDelete_LastSuperadminBlocked(t *testing.T) {
	srv, s, rb, setPerms := newDeleteHarness(t)
	setPerms(map[string]bool{rbac.PermUsersDelete: true})
	seedAppUser(t, s, "usr_test_actor", "Actor", false)
	if _, err := s.Pool().Exec(context.Background(),
		`DELETE FROM admin_user_roles WHERE role_id IN (SELECT id FROM admin_roles WHERE key='superadmin')`); err != nil {
		t.Fatalf("purge sa: %v", err)
	}
	const tid = "usr_del_lastsa"
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
	resp := mustPOST(t, srv.URL+"/api/app/admin/actions/users.delete", body)
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("status=%d; want 409 body=%s", resp.StatusCode, readBody(resp))
	}
}
