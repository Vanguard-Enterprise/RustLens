package httpapi

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

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
		TRUNCATE audit_events, app_audit_events, admin_user_roles, user_guilds, app_users RESTART IDENTITY CASCADE
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
		`INSERT INTO app_users (id, discord_id, discord_username, display_name)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (id) DO UPDATE SET display_name = EXCLUDED.display_name`,
		"usr_exporter", "d_exporter", "exporter", "Exporter"); err != nil {
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

func countAuditByAction(t *testing.T, pool *pgxpool.Pool, actionType string) int {
	t.Helper()
	var n int
	err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM audit_events WHERE action_type = $1`, actionType,
	).Scan(&n)
	if err != nil {
		t.Fatalf("count %s: %v", actionType, err)
	}
	return n
}

func fetchAuditRow(t *testing.T, pool *pgxpool.Pool, actionType string) struct {
	ResourceID   string
	TargetUserID *string
	Params       []byte
	Result       string
	IPAddress    *string
	ErrorMessage *string
} {
	t.Helper()
	var row struct {
		ResourceID   string
		TargetUserID *string
		Params       []byte
		Result       string
		IPAddress    *string
		ErrorMessage *string
	}
	err := pool.QueryRow(context.Background(),
		`SELECT COALESCE(resource_id,''), target_user_id::text, params,
		        result, ip_address::text, error_message
		 FROM audit_events WHERE action_type = $1
		 ORDER BY created_at DESC LIMIT 1`,
		actionType,
	).Scan(&row.ResourceID, &row.TargetUserID, &row.Params,
		&row.Result, &row.IPAddress, &row.ErrorMessage)
	if err != nil {
		t.Fatalf("fetch %s: %v", actionType, err)
	}
	return row
}

func TestExportAdminUsersCSV_HappyPath(t *testing.T) {
	h, s := newExportHandlerServer(t)
	seedUser(t, s.Pool(), "usr_a", "Alice", seedOpts{Discord: "alice", DiscordID: "d_alice"})
	seedUser(t, s.Pool(), "usr_b", "Bob", seedOpts{Discord: "bob", DiscordID: "d_bob"})
	seedUser(t, s.Pool(), "usr_c", "Carol", seedOpts{Discord: "carol", DiscordID: "d_carol"})

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
	if records[0][0] != "id" || records[0][1] != "display_name" || records[0][2] != "discord_id" || records[0][3] != "discord_username" {
		t.Errorf("unexpected header: %v", records[0])
	}
}

func TestExportAdminUsersCSV_IdsFilter(t *testing.T) {
	h, s := newExportHandlerServer(t)
	seedUser(t, s.Pool(), "usr_a", "Alice", seedOpts{Discord: "alice", DiscordID: "d_alice"})
	seedUser(t, s.Pool(), "usr_b", "Bob", seedOpts{Discord: "bob", DiscordID: "d_bob"})
	seedUser(t, s.Pool(), "usr_c", "Carol", seedOpts{Discord: "carol", DiscordID: "d_carol"})

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
	seedUser(t, s.Pool(), "usr_a", "Alice", seedOpts{Discord: "alice", DiscordID: "d_alice"})
	seedUser(t, s.Pool(), "usr_b", "Bob", seedOpts{Discord: "bob", DiscordID: "d_bob"})
	seedUser(t, s.Pool(), "usr_c", "Carol", seedOpts{Discord: "carol", DiscordID: "d_carol"})

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
		`INSERT INTO app_users (id, discord_id, discord_username, display_name)
		 VALUES ('usr_noperm', 'd_noperm', 'noperm', 'NoPerm')
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

func TestExportAdminUsersCSV_WritesPairedAuditEvents(t *testing.T) {
	h, s := newExportHandlerServer(t)
	seedUser(t, s.Pool(), "usr_a", "Alice", seedOpts{Discord: "alice", DiscordID: "d_alice"})

	// Pre-conditions: zero rows of each event type yet.
	if got := countAuditByAction(t, s.Pool(), "users.export.started"); got != 0 {
		t.Fatalf("expected 0 started rows pre-export, got %d", got)
	}
	if got := countAuditByAction(t, s.Pool(), "users.export.completed"); got != 0 {
		t.Fatalf("expected 0 completed rows pre-export, got %d", got)
	}

	req := httptest.NewRequest("GET", "/api/app/admin/users/export.csv?status=any", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	req.Header.Set("User-Agent", "test-agent")
	req = req.WithContext(appauth.InjectSessionForTest(req.Context(),
		&appauth.SessionUser{UserID: "usr_exporter", Roles: []string{"superadmin"}}))
	rr := httptest.NewRecorder()
	h.ExportAdminUsersCSV(rr, req)

	if rr.Code != 200 {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}

	if got := countAuditByAction(t, s.Pool(), "users.export.started"); got != 1 {
		t.Fatalf("expected 1 started row, got %d", got)
	}
	if got := countAuditByAction(t, s.Pool(), "users.export.completed"); got != 1 {
		t.Fatalf("expected 1 completed row, got %d", got)
	}

	started := fetchAuditRow(t, s.Pool(), "users.export.started")
	completed := fetchAuditRow(t, s.Pool(), "users.export.completed")

	if started.ResourceID == "" {
		t.Fatal("started.resource_id should be a non-empty exportId")
	}
	if started.ResourceID != completed.ResourceID {
		t.Errorf("export pair must share resource_id, got started=%q completed=%q",
			started.ResourceID, completed.ResourceID)
	}
	if started.Result != "success" {
		t.Errorf("started.result=%q want success", started.Result)
	}
	if completed.Result != "success" {
		t.Errorf("completed.result=%q want success", completed.Result)
	}

	var startedParams map[string]any
	if err := json.Unmarshal(started.Params, &startedParams); err != nil {
		t.Fatalf("unmarshal started params: %v", err)
	}
	if _, ok := startedParams["expectedRowCount"]; !ok {
		t.Errorf("started params missing expectedRowCount: %s", started.Params)
	}
	filters, _ := startedParams["filters"].(map[string]any)
	if filters == nil {
		t.Fatalf("started params missing filters: %s", started.Params)
	}
	if filters["status"] != "any" {
		t.Errorf("filters.status=%v want \"any\"", filters["status"])
	}

	var completedParams map[string]any
	if err := json.Unmarshal(completed.Params, &completedParams); err != nil {
		t.Fatalf("unmarshal completed params: %v", err)
	}
	if _, ok := completedParams["rowsWritten"]; !ok {
		t.Errorf("completed params missing rowsWritten: %s", completed.Params)
	}
	if _, ok := completedParams["durationMs"]; !ok {
		t.Errorf("completed params missing durationMs: %s", completed.Params)
	}
}

func TestExportAdminUsersCSV_SingleIDsTargetsUser(t *testing.T) {
	h, s := newExportHandlerServer(t)
	seedUser(t, s.Pool(), "usr_a", "Alice", seedOpts{Discord: "alice", DiscordID: "d_alice"})

	req := httptest.NewRequest("GET", "/api/app/admin/users/export.csv?ids=usr_a", nil)
	req = req.WithContext(appauth.InjectSessionForTest(req.Context(),
		&appauth.SessionUser{UserID: "usr_exporter", Roles: []string{"superadmin"}}))
	rr := httptest.NewRecorder()
	h.ExportAdminUsersCSV(rr, req)
	if rr.Code != 200 {
		t.Fatalf("status=%d", rr.Code)
	}

	if got := countAuditByAction(t, s.Pool(), "users.export.started"); got != 1 {
		t.Fatalf("expected exactly 1 started row, got %d", got)
	}
	started := fetchAuditRow(t, s.Pool(), "users.export.started")
	if started.TargetUserID == nil || *started.TargetUserID != "usr_a" {
		t.Errorf("target_user_id=%v want usr_a", started.TargetUserID)
	}
}

func TestExportAdminUsersCSV_MultipleIDsKeepsTargetNullButRecordsIDs(t *testing.T) {
	h, s := newExportHandlerServer(t)
	seedUser(t, s.Pool(), "usr_a", "Alice", seedOpts{Discord: "alice", DiscordID: "d_alice"})
	seedUser(t, s.Pool(), "usr_b", "Bob", seedOpts{Discord: "bob", DiscordID: "d_bob"})

	req := httptest.NewRequest("GET", "/api/app/admin/users/export.csv?ids=usr_a,usr_b", nil)
	req = req.WithContext(appauth.InjectSessionForTest(req.Context(),
		&appauth.SessionUser{UserID: "usr_exporter", Roles: []string{"superadmin"}}))
	rr := httptest.NewRecorder()
	h.ExportAdminUsersCSV(rr, req)
	if rr.Code != 200 {
		t.Fatalf("status=%d", rr.Code)
	}

	if got := countAuditByAction(t, s.Pool(), "users.export.started"); got != 1 {
		t.Fatalf("expected exactly 1 started row, got %d", got)
	}
	started := fetchAuditRow(t, s.Pool(), "users.export.started")
	if started.TargetUserID != nil {
		t.Errorf("target_user_id=%v want nil for multi-id export", *started.TargetUserID)
	}
	var p map[string]any
	if err := json.Unmarshal(started.Params, &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	filters, _ := p["filters"].(map[string]any)
	ids, _ := filters["ids"].([]any)
	if len(ids) != 2 {
		t.Fatalf("filters.ids len=%d want 2: %v", len(ids), ids)
	}
}
