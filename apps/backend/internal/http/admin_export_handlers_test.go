package httpapi

import (
	"context"
	"encoding/csv"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"rustlens/backend/internal/admin/rbac"
	"rustlens/backend/internal/appauth"
	"rustlens/backend/internal/store"
)

func newExportHandlerServer(t *testing.T) (*Handler, *store.PostgresStore) {
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
		t.Fatalf("ensure schema: %v", err)
	}
	if _, err := s.Pool().Exec(ctx, `
		TRUNCATE admin_user_roles, user_emails, user_connected_accounts, user_guilds, app_users RESTART IDENTITY CASCADE
	`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	rbStore := rbac.NewStore(s.Pool())
	if err := rbStore.SeedPermissionsAndRoles(ctx); err != nil {
		t.Fatalf("rbac seed: %v", err)
	}
	resolver := rbac.NewResolver(rbStore)

	// Seed a superadmin caller so the HasPermission check passes.
	if _, err := s.Pool().Exec(ctx,
		`INSERT INTO app_users (id, display_name) VALUES ($1, $2)
		 ON CONFLICT (id) DO UPDATE SET display_name = EXCLUDED.display_name`,
		"usr_exporter", "Exporter"); err != nil {
		t.Fatalf("seed caller: %v", err)
	}
	saID, err := rbStore.RoleIDByKey(ctx, rbac.RoleSuperadmin)
	if err != nil {
		t.Fatalf("role id: %v", err)
	}
	if err := rbStore.AssignRole(ctx, "usr_exporter", saID, ""); err != nil {
		t.Fatalf("assign sa: %v", err)
	}

	h := &Handler{postgres: s, rbacResolver: resolver, rbacStore: rbStore}
	return h, s
}

func TestExportAdminUsersCSV_HappyPath(t *testing.T) {
	h, s := newExportHandlerServer(t)
	seedUser(t, s.Pool(), "usr_a", "Alice", seedOpts{PrimaryEmail: "a@example.com", Verified: true})
	seedUser(t, s.Pool(), "usr_b", "Bob", seedOpts{PrimaryEmail: "b@example.com", Verified: false})
	seedUser(t, s.Pool(), "usr_c", "Carol", seedOpts{PrimaryEmail: "c@example.com", Verified: true})

	req := httptest.NewRequest("GET", "/api/app/admin/users/export.csv", nil)
	req = req.WithContext(appauth.InjectSessionForTest(req.Context(), &appauth.SessionUser{UserID: "usr_exporter", Roles: []string{"superadmin"}}))
	rr := httptest.NewRecorder()

	h.ExportAdminUsersCSV(rr, req)
	if rr.Code != 200 {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
		t.Errorf("Content-Type=%q", ct)
	}
	if cd := rr.Header().Get("Content-Disposition"); !strings.Contains(cd, "attachment") {
		t.Errorf("Content-Disposition=%q", cd)
	}

	r := csv.NewReader(strings.NewReader(rr.Body.String()))
	records, err := r.ReadAll()
	if err != nil {
		t.Fatalf("parse csv: %v", err)
	}
	// 3 seeded users + the exporter caller = 4 data rows + 1 header.
	if len(records) != 5 {
		t.Fatalf("expected 1 header + 4 rows, got %d", len(records))
	}
	if records[0][0] != "id" || records[0][1] != "display_name" {
		t.Errorf("unexpected header: %v", records[0])
	}
}

func TestExportAdminUsersCSV_IdsFilter(t *testing.T) {
	h, s := newExportHandlerServer(t)
	seedUser(t, s.Pool(), "usr_a", "Alice", seedOpts{PrimaryEmail: "a@example.com"})
	seedUser(t, s.Pool(), "usr_b", "Bob", seedOpts{PrimaryEmail: "b@example.com"})
	seedUser(t, s.Pool(), "usr_c", "Carol", seedOpts{PrimaryEmail: "c@example.com"})

	req := httptest.NewRequest("GET", "/api/app/admin/users/export.csv?ids=usr_a,usr_c", nil)
	req = req.WithContext(appauth.InjectSessionForTest(req.Context(), &appauth.SessionUser{UserID: "usr_exporter", Roles: []string{"superadmin"}}))
	rr := httptest.NewRecorder()
	h.ExportAdminUsersCSV(rr, req)
	if rr.Code != 200 {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	r := csv.NewReader(strings.NewReader(rr.Body.String()))
	records, err := r.ReadAll()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(records) != 3 {
		t.Fatalf("expected header + 2 rows, got %d", len(records))
	}
	gotIDs := map[string]bool{records[1][0]: true, records[2][0]: true}
	if !gotIDs["usr_a"] || !gotIDs["usr_c"] {
		t.Errorf("wrong ids in output: %v", gotIDs)
	}
}

func TestExportAdminUsersCSV_RowCapEnforced(t *testing.T) {
	h, s := newExportHandlerServer(t)
	seedUser(t, s.Pool(), "usr_a", "Alice", seedOpts{PrimaryEmail: "a@example.com"})
	seedUser(t, s.Pool(), "usr_b", "Bob", seedOpts{PrimaryEmail: "b@example.com"})
	seedUser(t, s.Pool(), "usr_c", "Carol", seedOpts{PrimaryEmail: "c@example.com"})

	prev := adminUsersExportRowCap
	adminUsersExportRowCap = 1
	t.Cleanup(func() { adminUsersExportRowCap = prev })

	req := httptest.NewRequest("GET", "/api/app/admin/users/export.csv", nil)
	req = req.WithContext(appauth.InjectSessionForTest(req.Context(), &appauth.SessionUser{UserID: "usr_exporter", Roles: []string{"superadmin"}}))
	rr := httptest.NewRecorder()
	h.ExportAdminUsersCSV(rr, req)
	if rr.Code != 413 {
		t.Errorf("expected 413, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestExportAdminUsersCSV_PermissionDenied(t *testing.T) {
	h, s := newExportHandlerServer(t)
	// Seed a non-privileged caller.
	ctx := context.Background()
	if _, err := s.Pool().Exec(ctx,
		`INSERT INTO app_users (id, display_name) VALUES ('usr_noperm', 'NoPerm')
		 ON CONFLICT (id) DO NOTHING`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/app/admin/users/export.csv", nil)
	req = req.WithContext(appauth.InjectSessionForTest(req.Context(), &appauth.SessionUser{UserID: "usr_noperm", Roles: []string{}}))
	rr := httptest.NewRecorder()
	h.ExportAdminUsersCSV(rr, req)
	if rr.Code != 403 {
		t.Errorf("expected 403, got %d body=%s", rr.Code, rr.Body.String())
	}
}
