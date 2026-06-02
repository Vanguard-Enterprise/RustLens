package httpapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func grantAdminRole(t *testing.T, pool *pgxpool.Pool, userID, roleKey string) {
	t.Helper()
	ctx := context.Background()
	// Ensure a role with at least one permission exists, then grant.
	if _, err := pool.Exec(ctx, `
		INSERT INTO admin_roles (key, name, description, is_system)
		VALUES ($1, $1, '', FALSE)
		ON CONFLICT (key) DO NOTHING`, roleKey); err != nil {
		t.Fatalf("seed admin_role %s: %v", roleKey, err)
	}
	// Use any existing permission. seed_permissions runs in EnsureSchema; pick the first.
	var permKey string
	if err := pool.QueryRow(ctx, `SELECT key FROM admin_permissions ORDER BY key LIMIT 1`).Scan(&permKey); err != nil {
		t.Fatalf("read permission: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO admin_role_permissions (role_id, permission_key)
		SELECT id, $2 FROM admin_roles WHERE key = $1
		ON CONFLICT DO NOTHING`, roleKey, permKey); err != nil {
		t.Fatalf("seed role permission: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO admin_user_roles (user_id, role_id)
		SELECT $1, id FROM admin_roles WHERE key = $2
		ON CONFLICT DO NOTHING`, userID, roleKey); err != nil {
		t.Fatalf("grant admin role %s to %s: %v", roleKey, userID, err)
	}
}

func (ts *testHandlerServer) getUsersList(t *testing.T, path string) listAdminUsersResp {
	t.Helper()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", path, nil)
	ts.h.ListAdminUsers(rr, req)
	if rr.Code != 200 {
		t.Fatalf("GET %s: status=%d body=%s", path, rr.Code, rr.Body.String())
	}
	var resp listAdminUsersResp
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, rr.Body.String())
	}
	return resp
}

func TestListAdminUsers_ExcludesAdmins(t *testing.T) {
	ts := newTestHandlerServer(t)

	seedUser(t, ts.pool, "usr_admin1", "Andy Admin", seedOpts{Discord: "andy"})
	seedUser(t, ts.pool, "usr_mem1", "Mona Member", seedOpts{Discord: "mona"})
	seedUser(t, ts.pool, "usr_mem2", "Mike Member", seedOpts{Discord: "mike"})

	// Fixed role key; TRUNCATE in newTestHandlerServer doesn't touch admin_roles,
	// but `ON CONFLICT (key) DO NOTHING` in grantAdminRole keeps this idempotent
	// across re-runs.
	grantAdminRole(t, ts.pool, "usr_admin1", "members_test_admin")

	resp := ts.getUsersList(t, "/api/app/admin/users")
	if resp.Total != 2 {
		t.Errorf("members total: want 2, got %d", resp.Total)
	}
	for _, item := range resp.Items {
		if item.User.ID == "usr_admin1" {
			t.Errorf("members listing leaked admin user: %s", item.User.ID)
		}
	}
}

func TestListAdminUsers_SearchAndPagination(t *testing.T) {
	ts := newTestHandlerServer(t)

	seedUser(t, ts.pool, "usr_a", "Alice", seedOpts{Discord: "alice"})
	seedUser(t, ts.pool, "usr_b", "Bob", seedOpts{Discord: "bob"})
	seedUser(t, ts.pool, "usr_c", "Carol", seedOpts{Discord: "carol"})

	if r := ts.getUsersList(t, "/api/app/admin/users?q=alice"); r.Total != 1 {
		t.Errorf("q=alice: want 1 got %d", r.Total)
	}

	r := ts.getUsersList(t, "/api/app/admin/users?pageSize=2&page=1")
	if len(r.Items) != 2 || r.Total != 3 {
		t.Errorf("page1: want len=2 total=3, got len=%d total=%d", len(r.Items), r.Total)
	}
}

func TestListAdminUsers_IncludesBanCategories(t *testing.T) {
	ts := newTestHandlerServer(t)

	seedUser(t, ts.pool, "usr_banned_member", "Banned Member", seedOpts{
		Discord:   "banned",
		DiscordID: "discord_banned_member",
	})
	if _, err := ts.pool.Exec(context.Background(), `
		INSERT INTO user_bans (discord_id, categories, updated_by)
		VALUES ($1, ARRAY['lookup', 'activity']::text[], 'seed')
	`, "discord_banned_member"); err != nil {
		t.Fatalf("seed user_bans: %v", err)
	}

	resp := ts.getUsersList(t, "/api/app/admin/users?q=banned")
	if resp.Total != 1 {
		t.Fatalf("total: want 1, got %d", resp.Total)
	}
	got := resp.Items[0].BanCategories
	if len(got) != 2 || got[0] != "lookup" || got[1] != "activity" {
		t.Fatalf("banCategories = %v, want [lookup activity]", got)
	}
}
