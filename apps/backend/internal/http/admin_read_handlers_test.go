package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"rustlens/backend/internal/store"
)

type listAdminUsersResp struct {
	Items    []adminUserItem `json:"items"`
	Total    int             `json:"total"`
	Page     int             `json:"page"`
	PageSize int             `json:"pageSize"`
}

type testHandlerServer struct {
	h    *Handler
	pool *pgxpool.Pool
	mux  *http.ServeMux
}

func (ts *testHandlerServer) get(t *testing.T, path string) listAdminUsersResp {
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

func (ts *testHandlerServer) getCode(t *testing.T, path string) int {
	t.Helper()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", path, nil)
	ts.h.ListAdminUsers(rr, req)
	return rr.Code
}

func newTestHandlerServer(t *testing.T) *testHandlerServer {
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
	// Wipe user-related tables so each test starts fresh.
	if _, err := s.Pool().Exec(ctx, `
		TRUNCATE admin_user_roles, user_emails, user_connected_accounts, user_guilds, app_users RESTART IDENTITY CASCADE
	`); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	h := &Handler{postgres: s}
	return &testHandlerServer{h: h, pool: s.Pool()}
}

type seedOpts struct {
	PrimaryEmail  string
	Verified      bool
	Discord       string // discord username
	DiscordID     string
	Suspended     bool
	Deleted       bool
	LinkSteam     bool
	AdminRoleKeys []string
}

func seedUser(t *testing.T, pool *pgxpool.Pool, id, displayName string, opts seedOpts) {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx,
		`INSERT INTO app_users (id, display_name) VALUES ($1, $2)
		 ON CONFLICT (id) DO UPDATE SET display_name = EXCLUDED.display_name`,
		id, displayName,
	); err != nil {
		t.Fatalf("seed app_user %s: %v", id, err)
	}
	if opts.Suspended {
		if _, err := pool.Exec(ctx, `UPDATE app_users SET suspended_at = NOW() WHERE id = $1`, id); err != nil {
			t.Fatalf("suspend: %v", err)
		}
	}
	if opts.Deleted {
		if _, err := pool.Exec(ctx, `UPDATE app_users SET deleted_at = NOW() WHERE id = $1`, id); err != nil {
			t.Fatalf("delete: %v", err)
		}
	}
	if opts.PrimaryEmail != "" {
		if _, err := pool.Exec(ctx,
			`INSERT INTO user_emails (email, user_id, is_primary, is_verified) VALUES ($1, $2, true, $3)
			 ON CONFLICT (email) DO UPDATE SET user_id = EXCLUDED.user_id, is_primary = true, is_verified = EXCLUDED.is_verified`,
			opts.PrimaryEmail, id, opts.Verified,
		); err != nil {
			t.Fatalf("seed email: %v", err)
		}
	}
	if opts.Discord != "" || opts.DiscordID != "" {
		discordID := opts.DiscordID
		if discordID == "" {
			discordID = fmt.Sprintf("d_%s", id)
		}
		if _, err := pool.Exec(ctx,
			`INSERT INTO user_connected_accounts (user_id, provider, provider_account_id, username) VALUES ($1, 'discord', $2, $3)
			 ON CONFLICT (provider, provider_account_id) DO UPDATE SET user_id = EXCLUDED.user_id, username = EXCLUDED.username`,
			id, discordID, opts.Discord,
		); err != nil {
			t.Fatalf("seed discord: %v", err)
		}
	}
	if opts.LinkSteam {
		if _, err := pool.Exec(ctx,
			`INSERT INTO user_connected_accounts (user_id, provider, provider_account_id) VALUES ($1, 'steam', $2)
			 ON CONFLICT (provider, provider_account_id) DO NOTHING`,
			id, "steam_"+id,
		); err != nil {
			t.Fatalf("seed steam: %v", err)
		}
	}
}

func TestListAdminUsers_FiltersAndPagination(t *testing.T) {
	ts := newTestHandlerServer(t)

	seedUser(t, ts.pool, "usr_aaaa", "Alice", seedOpts{PrimaryEmail: "a@x", Verified: true, Discord: "a#1", DiscordID: "d_alice"})
	seedUser(t, ts.pool, "usr_bbbb", "Bob", seedOpts{PrimaryEmail: "b@x", Verified: false})
	seedUser(t, ts.pool, "usr_cccc", "Carol", seedOpts{Suspended: true})
	seedUser(t, ts.pool, "usr_dddd", "Dan", seedOpts{Deleted: true})

	// default: status=active hides suspended + deleted
	resp := ts.get(t, "/api/app/admin/users")
	if resp.Total != 2 {
		t.Errorf("active total: want 2, got %d", resp.Total)
	}

	if r := ts.get(t, "/api/app/admin/users?q=alice"); r.Total != 1 {
		t.Errorf("q=alice: want 1 got %d", r.Total)
	}

	if r := ts.get(t, "/api/app/admin/users?status=suspended"); r.Total != 1 {
		t.Errorf("suspended: want 1 got %d", r.Total)
	}

	if r := ts.get(t, "/api/app/admin/users?discord=linked"); r.Total != 1 {
		t.Errorf("discord=linked: want 1 got %d", r.Total)
	}

	if r := ts.get(t, "/api/app/admin/users?emailVerified=yes"); r.Total != 1 {
		t.Errorf("emailVerified=yes: want 1 got %d", r.Total)
	}

	r := ts.get(t, "/api/app/admin/users?status=any&pageSize=2&page=1")
	if len(r.Items) != 2 || r.Total != 4 {
		t.Errorf("page1: want len=2 total=4, got len=%d total=%d", len(r.Items), r.Total)
	}

	r = ts.get(t, "/api/app/admin/users?status=any&sort=displayName")
	if len(r.Items) == 0 || r.Items[0].User.DisplayName != "Alice" {
		t.Errorf("sort asc: want Alice first, got %+v", r.Items)
	}

	if code := ts.getCode(t, "/api/app/admin/users?sort=bogus"); code != 400 {
		t.Errorf("bogus sort: want 400 got %d", code)
	}
}
