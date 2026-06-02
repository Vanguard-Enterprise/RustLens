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

func newUsersBanSetHarness(t *testing.T) (*httptest.Server, *store.PostgresStore, func(map[string]bool)) {
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
	if err := reg.Register(actions.NewUsersBanSetAction(s.Pool())); err != nil {
		t.Fatalf("register ban set: %v", err)
	}

	disp := actions.NewDispatcher(reg, perms, auditSinkBridge{au}, fixedActor)
	r := chi.NewRouter()
	r.Post("/api/app/admin/actions/{type}", disp.ServeHTTP)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv, s, func(a map[string]bool) { perms.allow = a }
}

func readUserBanCategories(t *testing.T, s *store.PostgresStore, discordID string) []string {
	t.Helper()
	ban, err := s.GetUserBan(context.Background(), discordID)
	if err != nil {
		t.Fatalf("get user ban: %v", err)
	}
	return ban.Categories
}

func TestUsersBanSet_UpsertsCategories(t *testing.T) {
	srv, s, setPerms := newUsersBanSetHarness(t)
	setPerms(map[string]bool{rbac.PermUsersSuspend: true})
	seedAppUser(t, s, "usr_test_actor", "Actor", false)
	seedAppUser(t, s, "usr_ban_target", "Target", false)

	body := mustJSON(t, map[string]any{
		"params": map[string]any{
			"userId":     "usr_ban_target",
			"categories": []string{"lookup", "activity"},
		},
		"reason": "abuse",
	})
	resp := mustPOST(t, srv.URL+"/api/app/admin/actions/users.ban.set", body)
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d body=%s", resp.StatusCode, readBody(resp))
	}

	got := readUserBanCategories(t, s, "d_usr_ban_target")
	if len(got) != 2 || got[0] != "lookup" || got[1] != "activity" {
		t.Fatalf("categories = %v, want [lookup activity]", got)
	}
}

func TestUsersBanSet_EmptyCategoriesClearsBan(t *testing.T) {
	srv, s, setPerms := newUsersBanSetHarness(t)
	setPerms(map[string]bool{rbac.PermUsersSuspend: true})
	seedAppUser(t, s, "usr_test_actor", "Actor", false)
	seedAppUser(t, s, "usr_ban_clear", "Target", false)
	if _, err := s.Pool().Exec(context.Background(), `
		INSERT INTO user_bans (discord_id, categories, updated_by)
		VALUES ('d_usr_ban_clear', ARRAY['lookup']::text[], 'seed')
		ON CONFLICT (discord_id) DO UPDATE SET categories = EXCLUDED.categories
	`); err != nil {
		t.Fatalf("seed ban: %v", err)
	}

	body := mustJSON(t, map[string]any{
		"params": map[string]any{
			"userId":     "usr_ban_clear",
			"categories": []string{},
		},
		"reason": "appeal accepted",
	})
	resp := mustPOST(t, srv.URL+"/api/app/admin/actions/users.ban.set", body)
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d body=%s", resp.StatusCode, readBody(resp))
	}

	got := readUserBanCategories(t, s, "d_usr_ban_clear")
	if len(got) != 0 {
		t.Fatalf("categories = %v, want empty", got)
	}
}

func TestUsersBanSet_InvalidCategoryRejected(t *testing.T) {
	srv, s, setPerms := newUsersBanSetHarness(t)
	setPerms(map[string]bool{rbac.PermUsersSuspend: true})
	seedAppUser(t, s, "usr_test_actor", "Actor", false)
	seedAppUser(t, s, "usr_ban_invalid", "Target", false)

	body := mustJSON(t, map[string]any{
		"params": map[string]any{
			"userId":     "usr_ban_invalid",
			"categories": []string{"lookup", "not_real"},
		},
		"reason": "bad category",
	})
	resp := mustPOST(t, srv.URL+"/api/app/admin/actions/users.ban.set", body)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", resp.StatusCode, readBody(resp))
	}
}
