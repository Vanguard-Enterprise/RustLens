package httpapi

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
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
