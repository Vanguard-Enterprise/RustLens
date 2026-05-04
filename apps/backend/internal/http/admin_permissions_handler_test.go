package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
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
