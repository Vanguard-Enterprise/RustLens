package actions_test

import (
	"context"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/go-chi/chi/v5"

	"rustlens/backend/internal/admin/actions"
	"rustlens/backend/internal/admin/audit"
	"rustlens/backend/internal/admin/rbac"
	"rustlens/backend/internal/store"
)

func newEmailSetPrimaryHarness(t *testing.T) (*httptest.Server, *store.PostgresStore, func(map[string]bool)) {
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
	if err := reg.Register(actions.NewUsersEmailSetPrimaryAction(s.Pool())); err != nil {
		t.Fatalf("register: %v", err)
	}
	disp := actions.NewDispatcher(reg, perms, auditSinkBridge{au}, fixedActor)
	r := chi.NewRouter()
	r.Post("/api/app/admin/actions/{type}", disp.ServeHTTP)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv, s, func(a map[string]bool) { perms.allow = a }
}

func TestUsersEmailSetPrimary_HappyPath(t *testing.T) {
	srv, s, setPerms := newEmailSetPrimaryHarness(t)
	setPerms(map[string]bool{rbac.PermUsersEmailManage: true})
	seedAppUser(t, s, "usr_test_actor", "Actor", false)
	const tid = "usr_sp_ok"
	seedAppUser(t, s, tid, "Target", false)
	_, _ = s.Pool().Exec(context.Background(), `DELETE FROM user_emails WHERE user_id=$1`, tid)
	seedEmailRow(t, s, tid, tid+"-a@example.com", true, true)
	seedEmailRow(t, s, tid, tid+"-b@example.com", false, true)

	body := mustJSON(t, map[string]any{
		"params": map[string]any{"userId": tid, "email": tid + "-b@example.com"},
	})
	resp := mustPOST(t, srv.URL+"/api/app/admin/actions/users.email.setPrimary", body)
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d body=%s", resp.StatusCode, readBody(resp))
	}
	var primaryEmail string
	if err := s.Pool().QueryRow(context.Background(),
		`SELECT email FROM user_emails WHERE user_id=$1 AND is_primary=true`, tid).Scan(&primaryEmail); err != nil {
		t.Fatalf("query primary: %v", err)
	}
	if primaryEmail != tid+"-b@example.com" {
		t.Errorf("primary = %q", primaryEmail)
	}
}

func TestUsersEmailSetPrimary_UnverifiedConflict(t *testing.T) {
	srv, s, setPerms := newEmailSetPrimaryHarness(t)
	setPerms(map[string]bool{rbac.PermUsersEmailManage: true})
	seedAppUser(t, s, "usr_test_actor", "Actor", false)
	const tid = "usr_sp_unv"
	seedAppUser(t, s, tid, "Target", false)
	_, _ = s.Pool().Exec(context.Background(), `DELETE FROM user_emails WHERE user_id=$1`, tid)
	seedEmailRow(t, s, tid, tid+"-a@example.com", true, true)
	seedEmailRow(t, s, tid, tid+"-b@example.com", false, false)

	body := mustJSON(t, map[string]any{
		"params": map[string]any{"userId": tid, "email": tid + "-b@example.com"},
	})
	resp := mustPOST(t, srv.URL+"/api/app/admin/actions/users.email.setPrimary", body)
	if resp.StatusCode != 409 {
		t.Errorf("status=%d want 409 body=%s", resp.StatusCode, readBody(resp))
	}
}

func TestUsersEmailSetPrimary_Missing(t *testing.T) {
	srv, s, setPerms := newEmailSetPrimaryHarness(t)
	setPerms(map[string]bool{rbac.PermUsersEmailManage: true})
	seedAppUser(t, s, "usr_test_actor", "Actor", false)
	const tid = "usr_sp_miss"
	seedAppUser(t, s, tid, "Target", false)
	_, _ = s.Pool().Exec(context.Background(), `DELETE FROM user_emails WHERE user_id=$1`, tid)

	body := mustJSON(t, map[string]any{
		"params": map[string]any{"userId": tid, "email": "nope@example.com"},
	})
	resp := mustPOST(t, srv.URL+"/api/app/admin/actions/users.email.setPrimary", body)
	if resp.StatusCode != 404 {
		t.Errorf("status=%d want 404 body=%s", resp.StatusCode, readBody(resp))
	}
}
