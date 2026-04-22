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

func newConnectionsUnlinkHarness(t *testing.T) (*httptest.Server, *store.PostgresStore, func(map[string]bool)) {
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
	if err := reg.Register(actions.NewUsersConnectionsUnlinkAction(s.Pool())); err != nil {
		t.Fatalf("register: %v", err)
	}
	disp := actions.NewDispatcher(reg, perms, auditSinkBridge{au}, fixedActor)
	r := chi.NewRouter()
	r.Post("/api/app/admin/actions/{type}", disp.ServeHTTP)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv, s, func(a map[string]bool) { perms.allow = a }
}

func seedConnection(t *testing.T, s *store.PostgresStore, userID, provider, providerAccountID string) {
	t.Helper()
	if _, err := s.Pool().Exec(context.Background(), `
		INSERT INTO user_connected_accounts (user_id, provider, provider_account_id, username)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (provider, provider_account_id)
		DO UPDATE SET user_id = EXCLUDED.user_id
	`, userID, provider, providerAccountID, "user"); err != nil {
		t.Fatalf("seed conn: %v", err)
	}
}

func TestUsersConnectionsUnlink_HappyPath(t *testing.T) {
	srv, s, setPerms := newConnectionsUnlinkHarness(t)
	setPerms(map[string]bool{rbac.PermUsersConnectionsManage: true})
	seedAppUser(t, s, "usr_test_actor", "Actor", false)
	const tid = "usr_cu_ok"
	seedAppUser(t, s, tid, "Target", false)
	_, _ = s.Pool().Exec(context.Background(), `DELETE FROM user_connected_accounts WHERE user_id=$1`, tid)
	seedConnection(t, s, tid, "discord", "dc_123")

	body := mustJSON(t, map[string]any{
		"params": map[string]any{
			"userId":            tid,
			"provider":          "discord",
			"providerAccountId": "dc_123",
		},
		"reason":            "cleanup",
		"typedConfirmation": tid,
	})
	resp := mustPOST(t, srv.URL+"/api/app/admin/actions/users.connections.unlink", body)
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d body=%s", resp.StatusCode, readBody(resp))
	}
	var cnt int
	if err := s.Pool().QueryRow(context.Background(),
		`SELECT COUNT(*) FROM user_connected_accounts WHERE user_id=$1`, tid).Scan(&cnt); err != nil {
		t.Fatalf("q: %v", err)
	}
	if cnt != 0 {
		t.Errorf("remaining = %d", cnt)
	}
}

func TestUsersConnectionsUnlink_UnknownProvider(t *testing.T) {
	srv, s, setPerms := newConnectionsUnlinkHarness(t)
	setPerms(map[string]bool{rbac.PermUsersConnectionsManage: true})
	seedAppUser(t, s, "usr_test_actor", "Actor", false)
	const tid = "usr_cu_up"
	seedAppUser(t, s, tid, "Target", false)

	body := mustJSON(t, map[string]any{
		"params": map[string]any{
			"userId":            tid,
			"provider":          "twitter",
			"providerAccountId": "tw_1",
		},
		"reason":            "x",
		"typedConfirmation": tid,
	})
	resp := mustPOST(t, srv.URL+"/api/app/admin/actions/users.connections.unlink", body)
	if resp.StatusCode != 400 {
		t.Errorf("status=%d want 400 body=%s", resp.StatusCode, readBody(resp))
	}
}

func TestUsersConnectionsUnlink_Missing(t *testing.T) {
	srv, s, setPerms := newConnectionsUnlinkHarness(t)
	setPerms(map[string]bool{rbac.PermUsersConnectionsManage: true})
	seedAppUser(t, s, "usr_test_actor", "Actor", false)
	const tid = "usr_cu_miss"
	seedAppUser(t, s, tid, "Target", false)
	_, _ = s.Pool().Exec(context.Background(), `DELETE FROM user_connected_accounts WHERE user_id=$1`, tid)

	body := mustJSON(t, map[string]any{
		"params": map[string]any{
			"userId":            tid,
			"provider":          "steam",
			"providerAccountId": "st_nope",
		},
		"reason":            "x",
		"typedConfirmation": tid,
	})
	resp := mustPOST(t, srv.URL+"/api/app/admin/actions/users.connections.unlink", body)
	if resp.StatusCode != 404 {
		t.Errorf("status=%d want 404 body=%s", resp.StatusCode, readBody(resp))
	}
}
