package actions_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/go-chi/chi/v5"

	"rustlens/backend/internal/admin/actions"
	"rustlens/backend/internal/admin/audit"
	"rustlens/backend/internal/admin/rbac"
	"rustlens/backend/internal/store"
)

func newBulkHarness(t *testing.T) (*httptest.Server, *store.PostgresStore, func(map[string]bool)) {
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
	if err := reg.Register(actions.NewUsersSuspendAction(s.Pool())); err != nil {
		t.Fatalf("register suspend: %v", err)
	}
	if err := reg.Register(actions.NewUsersUnsuspendAction(s.Pool())); err != nil {
		t.Fatalf("register unsuspend: %v", err)
	}
	disp := actions.NewDispatcher(reg, perms, auditSinkBridge{au}, fixedActor)
	if err := reg.Register(actions.NewUsersBulkAction(disp)); err != nil {
		t.Fatalf("register bulk: %v", err)
	}
	r := chi.NewRouter()
	r.Post("/api/app/admin/actions/{type}", disp.ServeHTTP)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv, s, func(a map[string]bool) { perms.allow = a }
}

func countAudit(t *testing.T, s *store.PostgresStore, actionType, resourceID string) int {
	t.Helper()
	var n int
	q := `SELECT COUNT(*) FROM audit_events WHERE action_type = $1`
	args := []any{actionType}
	if resourceID != "" {
		q += ` AND resource_id = $2`
		args = append(args, resourceID)
	}
	if err := s.Pool().QueryRow(context.Background(), q, args...).Scan(&n); err != nil {
		t.Fatalf("count audit: %v", err)
	}
	return n
}

func TestUsersBulk_SuspendHappyPath(t *testing.T) {
	srv, s, setPerms := newBulkHarness(t)
	setPerms(map[string]bool{rbac.PermUsersSuspend: true})
	seedAppUser(t, s, "usr_test_actor", "Actor", false)
	ids := []string{"usr_bulk_a1", "usr_bulk_a2", "usr_bulk_a3"}
	for _, id := range ids {
		seedAppUser(t, s, id, id, false)
		if _, err := s.Pool().Exec(context.Background(),
			`UPDATE app_users SET suspended_at = NULL, suspended_by = NULL, suspend_reason = NULL WHERE id = $1`, id); err != nil {
			t.Fatalf("reset: %v", err)
		}
		if _, err := s.Pool().Exec(context.Background(),
			`DELETE FROM audit_events WHERE target_user_id = $1 OR resource_id = $1`, id); err != nil {
			t.Fatalf("cleanup: %v", err)
		}
	}

	body := mustJSON(t, map[string]any{
		"params": map[string]any{
			"action":  "suspend",
			"userIds": ids,
		},
		"reason": "abuse",
	})
	resp := mustPOST(t, srv.URL+"/api/app/admin/actions/users.bulk", body)
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d body=%s", resp.StatusCode, readBody(resp))
	}
	var out struct {
		OK     bool `json:"ok"`
		Result struct {
			Succeeded int `json:"succeeded"`
			Failed    int `json:"failed"`
			Results   []struct {
				UserID string `json:"userId"`
				OK     bool   `json:"ok"`
				Error  string `json:"error"`
			} `json:"results"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Result.Succeeded != 3 || out.Result.Failed != 0 {
		t.Errorf("expected 3/0, got %d/%d results=%+v", out.Result.Succeeded, out.Result.Failed, out.Result.Results)
	}
	// 3 sub-action audit rows.
	for _, id := range ids {
		if n := countAudit(t, s, "users.suspend", id); n != 1 {
			t.Errorf("expected 1 users.suspend audit for %s, got %d", id, n)
		}
	}
	// Top-level bulk audit row.
	if n := countAudit(t, s, "users.bulk", "bulk"); n < 1 {
		t.Errorf("expected top-level users.bulk audit, got %d", n)
	}
}

func TestUsersBulk_MixedPartialFailure(t *testing.T) {
	srv, s, setPerms := newBulkHarness(t)
	setPerms(map[string]bool{rbac.PermUsersSuspend: true})
	seedAppUser(t, s, "usr_test_actor", "Actor", false)
	active := "usr_bulk_mix_active"
	alreadySus := "usr_bulk_mix_sus"
	seedAppUser(t, s, active, active, false)
	seedAppUser(t, s, alreadySus, alreadySus, false)
	if _, err := s.Pool().Exec(context.Background(),
		`UPDATE app_users SET suspended_at = NULL WHERE id = $1`, active); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if _, err := s.Pool().Exec(context.Background(),
		`UPDATE app_users SET suspended_at = now() WHERE id = $1`, alreadySus); err != nil {
		t.Fatalf("preseed: %v", err)
	}

	body := mustJSON(t, map[string]any{
		"params": map[string]any{
			"action":  "suspend",
			"userIds": []string{active, alreadySus},
		},
		"reason": "mix",
	})
	resp := mustPOST(t, srv.URL+"/api/app/admin/actions/users.bulk", body)
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d body=%s", resp.StatusCode, readBody(resp))
	}
	var out struct {
		Result struct {
			Succeeded int `json:"succeeded"`
			Failed    int `json:"failed"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Result.Succeeded != 1 || out.Result.Failed != 1 {
		t.Errorf("expected 1/1, got %d/%d", out.Result.Succeeded, out.Result.Failed)
	}
}

func TestUsersBulk_Validation(t *testing.T) {
	srv, s, setPerms := newBulkHarness(t)
	setPerms(map[string]bool{rbac.PermUsersSuspend: true})
	seedAppUser(t, s, "usr_test_actor", "Actor", false)
	body := mustJSON(t, map[string]any{
		"params": map[string]any{"action": "suspend", "userIds": []string{}},
		"reason": "x",
	})
	resp := mustPOST(t, srv.URL+"/api/app/admin/actions/users.bulk", body)
	if resp.StatusCode != 400 {
		t.Errorf("expected 400, got %d body=%s", resp.StatusCode, readBody(resp))
	}

	body = mustJSON(t, map[string]any{
		"params": map[string]any{"action": "nope", "userIds": []string{"u1"}},
	})
	resp = mustPOST(t, srv.URL+"/api/app/admin/actions/users.bulk", body)
	if resp.StatusCode != 400 {
		t.Errorf("expected 400 for bad action, got %d", resp.StatusCode)
	}
}
