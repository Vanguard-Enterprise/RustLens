package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"rustlens/backend/internal/admin/rbac"
)

func TestGetAdminUser_HappyPath(t *testing.T) {
	ts := newTestHandlerServer(t)
	ctx := context.Background()

	seedUser(t, ts.pool, "usr_detail_1", "Alice", seedOpts{
		Discord:    "alice",
		GlobalName: "Alice Wonderland",
		DiscordID:  "d_alice",
	})

	// Seed admin_user_roles via rbac store so we have the FK right.
	rb := rbac.NewStore(ts.pool)
	if err := rb.SeedPermissionsAndRoles(ctx); err != nil {
		t.Fatalf("seed rbac: %v", err)
	}
	modID, err := rb.RoleIDByKey(ctx, rbac.RoleModerator)
	if err != nil {
		t.Fatalf("role id: %v", err)
	}
	if err := rb.AssignRole(ctx, "usr_detail_1", modID, ""); err != nil {
		t.Fatalf("assign role: %v", err)
	}
	if _, err := ts.pool.Exec(ctx, `
		INSERT INTO user_bans (discord_id, categories, updated_by)
		VALUES ($1, ARRAY['setup', 'lookup']::text[], 'seed')
	`, "d_alice"); err != nil {
		t.Fatalf("seed user_bans: %v", err)
	}

	// Seed an audit_events row targeting this user
	targetUser := "usr_detail_1"
	_, err = ts.pool.Exec(ctx, `
		INSERT INTO audit_events (action_type, actor_user_id, resource_type, resource_id, target_user_id, params, result)
		VALUES ($1, $2, $3, $4, $5, $6, 'success')
	`, "users.edit", "usr_actor_1", "user", targetUser, targetUser, `{"foo":"bar"}`)
	if err != nil {
		t.Fatalf("seed audit: %v", err)
	}

	// Call handler with chi URL param populated.
	r := chi.NewRouter()
	r.Get("/users/{id}", ts.h.GetAdminUser)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/users/usr_detail_1", nil)
	r.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var d adminUserDetail
	if err := json.Unmarshal(rr.Body.Bytes(), &d); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, rr.Body.String())
	}
	if d.User.ID != "usr_detail_1" {
		t.Errorf("user.id = %q", d.User.ID)
	}
	if d.DiscordID != "d_alice" {
		t.Errorf("discordId = %q", d.DiscordID)
	}
	if d.DiscordUsername != "alice" {
		t.Errorf("discordUsername = %q", d.DiscordUsername)
	}
	if d.DiscordGlobalName != "Alice Wonderland" {
		t.Errorf("discordGlobalName = %q", d.DiscordGlobalName)
	}
	if len(d.BanCategories) != 2 || d.BanCategories[0] != "setup" || d.BanCategories[1] != "lookup" {
		t.Errorf("banCategories = %v, want [setup lookup]", d.BanCategories)
	}
	foundMod := false
	for _, k := range d.AdminRoles {
		if k == rbac.RoleModerator {
			foundMod = true
		}
	}
	if !foundMod {
		t.Errorf("adminRoles missing moderator: %+v", d.AdminRoles)
	}
	if len(d.RecentAudit) < 1 {
		t.Errorf("recentAudit empty")
	} else {
		if d.RecentAudit[0].EventType != "users.edit" {
			t.Errorf("recentAudit[0].EventType = %q", d.RecentAudit[0].EventType)
		}
		// ID is the stringified UUID
		if _, perr := uuid.Parse(d.RecentAudit[0].ID); perr != nil {
			t.Errorf("recentAudit[0].ID not uuid: %q", d.RecentAudit[0].ID)
		}
	}
}

func TestGetAdminUser_NotFound(t *testing.T) {
	ts := newTestHandlerServer(t)
	r := chi.NewRouter()
	r.Get("/users/{id}", ts.h.GetAdminUser)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/users/usr_does_not_exist", nil)
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestListAdminUserAudit_PaginatesByBefore(t *testing.T) {
	ts := newTestHandlerServer(t)
	ctx := context.Background()
	seedUser(t, ts.pool, "usr_audit_1", "Aud", seedOpts{})

	for i := 0; i < 3; i++ {
		if _, err := ts.pool.Exec(ctx, `
			INSERT INTO audit_events (action_type, actor_user_id, resource_type, resource_id, target_user_id, params, result, created_at)
			VALUES ('users.edit', 'usr_actor', 'user', $1, $1, '{}', 'success', now() - ($2 || ' seconds')::interval)
		`, "usr_audit_1", fmt.Sprintf("%d", i)); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	r := chi.NewRouter()
	r.Get("/users/{id}/audit", ts.h.ListAdminUserAudit)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/users/usr_audit_1/audit?limit=2", nil)
	r.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Items []auditEventItem `json:"items"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Items) != 2 {
		t.Fatalf("limit=2 returned %d", len(resp.Items))
	}
}
