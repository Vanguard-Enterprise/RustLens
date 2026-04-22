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

func newEmailAddHarness(t *testing.T) (*httptest.Server, *store.PostgresStore, func(map[string]bool)) {
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
	if err := reg.Register(actions.NewUsersEmailAddAction(s.Pool())); err != nil {
		t.Fatalf("register: %v", err)
	}
	disp := actions.NewDispatcher(reg, perms, auditSinkBridge{au}, fixedActor)
	r := chi.NewRouter()
	r.Post("/api/app/admin/actions/{type}", disp.ServeHTTP)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv, s, func(a map[string]bool) { perms.allow = a }
}

func TestUsersEmailAdd_HappyPath(t *testing.T) {
	srv, s, setPerms := newEmailAddHarness(t)
	setPerms(map[string]bool{rbac.PermUsersEmailManage: true})
	seedAppUser(t, s, "usr_test_actor", "Actor", false)
	const tid = "usr_ea_ok"
	seedAppUser(t, s, tid, "Target", false)
	_, _ = s.Pool().Exec(context.Background(), `DELETE FROM user_emails WHERE user_id=$1`, tid)

	body := mustJSON(t, map[string]any{
		"params": map[string]any{"userId": tid, "email": "New-Mixed@Example.COM"},
	})
	resp := mustPOST(t, srv.URL+"/api/app/admin/actions/users.email.add", body)
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d body=%s", resp.StatusCode, readBody(resp))
	}
	var rowUser string
	var isPrimary, isVerified bool
	if err := s.Pool().QueryRow(context.Background(),
		`SELECT user_id, is_primary, is_verified FROM user_emails WHERE email=$1`,
		"new-mixed@example.com").Scan(&rowUser, &isPrimary, &isVerified); err != nil {
		t.Fatalf("query: %v", err)
	}
	if rowUser != tid || isPrimary || isVerified {
		t.Errorf("row = %s primary=%v verified=%v", rowUser, isPrimary, isVerified)
	}
}

func TestUsersEmailAdd_DuplicateSameUser(t *testing.T) {
	srv, s, setPerms := newEmailAddHarness(t)
	setPerms(map[string]bool{rbac.PermUsersEmailManage: true})
	seedAppUser(t, s, "usr_test_actor", "Actor", false)
	const tid = "usr_ea_dupself"
	seedAppUser(t, s, tid, "Target", false)
	_, _ = s.Pool().Exec(context.Background(), `DELETE FROM user_emails WHERE user_id=$1`, tid)
	seedEmailRow(t, s, tid, tid+"@example.com", true, true)

	body := mustJSON(t, map[string]any{
		"params": map[string]any{"userId": tid, "email": tid + "@example.com"},
	})
	resp := mustPOST(t, srv.URL+"/api/app/admin/actions/users.email.add", body)
	if resp.StatusCode != 409 {
		t.Errorf("status=%d want 409 body=%s", resp.StatusCode, readBody(resp))
	}
}

func TestUsersEmailAdd_DuplicateOtherUser(t *testing.T) {
	srv, s, setPerms := newEmailAddHarness(t)
	setPerms(map[string]bool{rbac.PermUsersEmailManage: true})
	seedAppUser(t, s, "usr_test_actor", "Actor", false)
	const owner = "usr_ea_owner"
	const other = "usr_ea_other"
	seedAppUser(t, s, owner, "Owner", false)
	seedAppUser(t, s, other, "Other", false)
	shared := "shared_ea@example.com"
	_, _ = s.Pool().Exec(context.Background(), `DELETE FROM user_emails WHERE email=$1`, shared)
	seedEmailRow(t, s, owner, shared, true, true)

	body := mustJSON(t, map[string]any{
		"params": map[string]any{"userId": other, "email": shared},
	})
	resp := mustPOST(t, srv.URL+"/api/app/admin/actions/users.email.add", body)
	if resp.StatusCode != 409 {
		t.Errorf("status=%d want 409 body=%s", resp.StatusCode, readBody(resp))
	}
}

func TestUsersEmailAdd_BadFormat(t *testing.T) {
	srv, s, setPerms := newEmailAddHarness(t)
	setPerms(map[string]bool{rbac.PermUsersEmailManage: true})
	seedAppUser(t, s, "usr_test_actor", "Actor", false)
	const tid = "usr_ea_bad"
	seedAppUser(t, s, tid, "Target", false)

	body := mustJSON(t, map[string]any{
		"params": map[string]any{"userId": tid, "email": "not-an-email"},
	})
	resp := mustPOST(t, srv.URL+"/api/app/admin/actions/users.email.add", body)
	if resp.StatusCode != 400 {
		t.Errorf("status=%d want 400 body=%s", resp.StatusCode, readBody(resp))
	}
}
