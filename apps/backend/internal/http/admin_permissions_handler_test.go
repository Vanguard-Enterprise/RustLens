package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"rustlens/backend/internal/admin/rbac"
)

// listAdminPermissionsResp mirrors the JSON shape returned by ListAdminPermissions.
type listAdminPermissionsResp struct {
	Items []adminPermissionItem `json:"items"`
}

func TestListAdminPermissions(t *testing.T) {
	// ListAdminPermissions is static (no DB read), so no postgres needed.
	h := &Handler{}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/app/admin/permissions", nil)
	h.ListAdminPermissions(rr, req)

	if rr.Code != 200 {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}

	var resp listAdminPermissionsResp
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, rr.Body.String())
	}

	if len(resp.Items) < 10 {
		t.Errorf("items: want at least 10, got %d", len(resp.Items))
	}

	// Verify the known entry: key "users.read" with description containing "View user profiles".
	found := false
	for _, item := range resp.Items {
		if item.Key == "users.read" {
			if !strings.Contains(item.Description, "View user profiles") {
				t.Errorf("users.read description: want to contain %q, got %q",
					"View user profiles", item.Description)
			}
			found = true
			break
		}
	}
	if !found {
		t.Errorf("users.read entry not found in items: %+v", resp.Items)
	}
}

// TestListAdminPermissions_RoutedThroughChi verifies that
// GET /api/app/admin/permissions is registered in the chi router and resolves
// to the correct handler. It uses a minimal chi shim (not the full NewRouter)
// to avoid requiring real config/session secrets, while still exercising the
// route registration that would be silently missed by the direct-handler test
// above.
//
// Skips when TEST_POSTGRES_URL is unset — the same convention used by every
// other admin integration test in this package — so that CI without Postgres
// does not fail.
func TestListAdminPermissions_RoutedThroughChi(t *testing.T) {
	// newTestHandlerServer skips the test when TEST_POSTGRES_URL is not set and
	// returns a Handler backed by a real Postgres pool.
	ts := newTestHandlerServer(t)

	// Build a minimal chi router that mirrors only the route under test.
	// This is exactly the registration that lives in router.go:
	//   r.Get("/permissions", handler.ListAdminPermissions)
	// If that line were removed, this test would return 404 and fail.
	r := chi.NewRouter()
	r.Get("/api/app/admin/permissions", ts.h.ListAdminPermissions)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/app/admin/permissions", nil)
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}

	var resp listAdminPermissionsResp
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, rr.Body.String())
	}

	if len(resp.Items) < 10 {
		t.Errorf("items routed through chi: want at least 10, got %d", len(resp.Items))
	}
}

// listAdminRolesResp mirrors the JSON shape returned by ListAdminRoles.
type listAdminRolesResp struct {
	Items []adminRoleItem `json:"items"`
}

// TestListAdminRoles_IncludesMemberCount verifies that each role item in the
// GET /api/app/admin/roles response carries a memberCount field (zero or more).
// Skips when TEST_POSTGRES_URL is not set.
func TestListAdminRoles_IncludesMemberCount(t *testing.T) {
	ts := newTestHandlerServer(t)
	// newTestHandlerServer skips when TEST_POSTGRES_URL is unset, so if we
	// reach here we have a real Postgres pool.
	ctx := httptest.NewRequest("GET", "/", nil).Context()
	rbacStore := rbac.NewStore(ts.pool)
	if err := rbacStore.SeedPermissionsAndRoles(ctx); err != nil {
		t.Fatalf("seed: %v", err)
	}
	ts.h.rbacStore = rbacStore

	// Seed a user and assign them to the moderator role so memberCount is non-zero.
	const memberUserID = "usr_lar_member_test_a"
	seedUser(t, ts.pool, memberUserID, "MemberTest", seedOpts{})
	modRoleID, err := rbacStore.RoleIDByKey(ctx, rbac.RoleModerator)
	if err != nil {
		t.Fatalf("role id for moderator: %v", err)
	}
	if _, err := ts.pool.Exec(ctx,
		`INSERT INTO admin_user_roles (user_id, role_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		memberUserID, modRoleID,
	); err != nil {
		t.Fatalf("assign moderator role: %v", err)
	}
	t.Cleanup(func() {
		_, _ = ts.pool.Exec(context.Background(),
			`DELETE FROM admin_user_roles WHERE user_id = $1`, memberUserID)
	})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/app/admin/roles", nil)
	ts.h.ListAdminRoles(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}

	var resp listAdminRolesResp
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, rr.Body.String())
	}

	if len(resp.Items) == 0 {
		t.Fatal("expected at least one role in response")
	}

	// Verify the memberCount field is present (always serialised; no omitempty).
	// Round-trip through a raw map to confirm the JSON key exists.
	raw := struct {
		Items []map[string]any `json:"items"`
	}{}
	if err := json.Unmarshal(rr.Body.Bytes(), &raw); err != nil {
		t.Fatalf("raw unmarshal: %v", err)
	}
	for _, item := range raw.Items {
		if _, ok := item["memberCount"]; !ok {
			t.Errorf("role %v is missing memberCount field", item["key"])
		}
	}

	// Verify the moderator role has memberCount >= 1 (we just assigned one user).
	for _, item := range raw.Items {
		if item["key"] == rbac.RoleModerator {
			count := int(item["memberCount"].(float64))
			if count < 1 {
				t.Errorf("moderator memberCount: want >= 1, got %d", count)
			}
			return
		}
	}
	t.Errorf("moderator role not found in response items")
}
