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
	ts.h.ListAdministrators(rr, req)
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
	ts.h.ListAdministrators(rr, req)
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
	// Wipe user-related tables so each test starts fresh. The Discord-only
	// migration removed user_emails / user_connected_accounts; identity now
	// lives directly on app_users.
	if _, err := s.Pool().Exec(ctx, `
		TRUNCATE admin_user_roles, user_guilds, app_users RESTART IDENTITY CASCADE
	`); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	h := &Handler{postgres: s}
	return &testHandlerServer{h: h, pool: s.Pool()}
}

type seedOpts struct {
	Discord       string // discord username
	DiscordID     string
	GlobalName    string
	Suspended     bool
	Deleted       bool
	AdminRoleKeys []string
}

func seedUser(t *testing.T, pool *pgxpool.Pool, id, displayName string, opts seedOpts) {
	t.Helper()
	ctx := context.Background()
	discordID := opts.DiscordID
	if discordID == "" {
		discordID = fmt.Sprintf("d_%s", id)
	}
	discordUsername := opts.Discord
	if discordUsername == "" {
		discordUsername = "u_" + id
	}
	globalName := opts.GlobalName
	if _, err := pool.Exec(ctx,
		`INSERT INTO app_users (id, discord_id, discord_username, discord_global_name, display_name)
		 VALUES ($1, $2, $3, NULLIF($4, ''), $5)
		 ON CONFLICT (id) DO UPDATE SET
		   discord_id = EXCLUDED.discord_id,
		   discord_username = EXCLUDED.discord_username,
		   discord_global_name = EXCLUDED.discord_global_name,
		   display_name = EXCLUDED.display_name`,
		id, discordID, discordUsername, globalName, displayName,
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
}

func TestListAdministrators_FiltersAndPagination(t *testing.T) {
	ts := newTestHandlerServer(t)

	seedUser(t, ts.pool, "usr_aaaa", "Alice", seedOpts{Discord: "alice", DiscordID: "d_alice"})
	seedUser(t, ts.pool, "usr_bbbb", "Bob", seedOpts{Discord: "bob", DiscordID: "d_bob"})
	seedUser(t, ts.pool, "usr_cccc", "Carol", seedOpts{Suspended: true})
	seedUser(t, ts.pool, "usr_dddd", "Dan", seedOpts{Deleted: true})

	// default: status=active hides suspended + deleted
	resp := ts.get(t, "/api/app/admin/administrators")
	if resp.Total != 2 {
		t.Errorf("active total: want 2, got %d", resp.Total)
	}

	if r := ts.get(t, "/api/app/admin/administrators?q=alice"); r.Total != 1 {
		t.Errorf("q=alice: want 1 got %d", r.Total)
	}

	if r := ts.get(t, "/api/app/admin/administrators?status=suspended"); r.Total != 1 {
		t.Errorf("suspended: want 1 got %d", r.Total)
	}

	// Every active user has discord_id intrinsically; "linked" returns active count.
	if r := ts.get(t, "/api/app/admin/administrators?discord=linked"); r.Total != 2 {
		t.Errorf("discord=linked: want 2 got %d", r.Total)
	}

	r := ts.get(t, "/api/app/admin/administrators?status=any&pageSize=2&page=1")
	if len(r.Items) != 2 || r.Total != 4 {
		t.Errorf("page1: want len=2 total=4, got len=%d total=%d", len(r.Items), r.Total)
	}

	r = ts.get(t, "/api/app/admin/administrators?status=any&sort=displayName")
	if len(r.Items) == 0 || r.Items[0].User.DisplayName != "Alice" {
		t.Errorf("sort asc: want Alice first, got %+v", r.Items)
	}

	if code := ts.getCode(t, "/api/app/admin/administrators?sort=bogus"); code != 400 {
		t.Errorf("bogus sort: want 400 got %d", code)
	}
}

// --- ListAdminGuilds tests ---

type listAdminGuildsResp struct {
	Items    []adminGuildItem `json:"items"`
	Total    int              `json:"total"`
	Page     int              `json:"page"`
	PageSize int              `json:"pageSize"`
}

func (ts *testHandlerServer) getGuilds(t *testing.T, path string) listAdminGuildsResp {
	t.Helper()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", path, nil)
	ts.h.ListAdminGuilds(rr, req)
	if rr.Code != 200 {
		t.Fatalf("GET %s: status=%d body=%s", path, rr.Code, rr.Body.String())
	}
	var resp listAdminGuildsResp
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, rr.Body.String())
	}
	return resp
}

func wipeGuildTables(t *testing.T, ts *testHandlerServer) {
	t.Helper()
	if _, err := ts.pool.Exec(context.Background(), `
		TRUNCATE bot_guilds, guild_settings, guild_tracked_servers, tracked_servers RESTART IDENTITY CASCADE
	`); err != nil {
		t.Fatalf("truncate guilds: %v", err)
	}
}

func insertBotGuild(t *testing.T, ts *testHandlerServer, guildID, name, ownerUsername string) {
	t.Helper()
	if _, err := ts.pool.Exec(context.Background(), `
		INSERT INTO bot_guilds (guild_id, name, owner_username, is_installed, installed_at, metadata_updated_at)
		VALUES ($1, $2, $3, TRUE, NOW(), NOW())
	`, guildID, name, ownerUsername); err != nil {
		t.Fatalf("insert bot_guilds: %v", err)
	}
}

func insertBotGuildWithMeta(t *testing.T, ts *testHandlerServer, guildID, name string, members int, installedAt string) {
	t.Helper()
	if _, err := ts.pool.Exec(context.Background(), `
		INSERT INTO bot_guilds (
			guild_id, name, owner_username, approximate_member_count,
			is_installed, installed_at, metadata_updated_at
		)
		VALUES ($1, $2, 'owner', $3, TRUE, $4::timestamptz, NOW())
	`, guildID, name, members, installedAt); err != nil {
		t.Fatalf("insert bot_guilds meta: %v", err)
	}
}

func insertGuildSettings(t *testing.T, ts *testHandlerServer, guildID string) {
	t.Helper()
	if _, err := ts.pool.Exec(context.Background(), `
		INSERT INTO guild_settings (guild_id) VALUES ($1)
	`, guildID); err != nil {
		t.Fatalf("insert guild_settings: %v", err)
	}
}

func insertGuildTrackedServer(t *testing.T, ts *testHandlerServer, guildID, bmID string) {
	t.Helper()
	// guild_tracked_servers references tracked_servers.bm_id — insert a parent row first.
	if _, err := ts.pool.Exec(context.Background(), `
		INSERT INTO tracked_servers (bm_id, name) VALUES ($1, 'test') ON CONFLICT (bm_id) DO NOTHING
	`, bmID); err != nil {
		t.Fatalf("insert tracked_servers: %v", err)
	}
	if _, err := ts.pool.Exec(context.Background(), `
		INSERT INTO guild_tracked_servers (guild_id, server_bm_id) VALUES ($1, $2)
	`, guildID, bmID); err != nil {
		t.Fatalf("insert guild_tracked_servers: %v", err)
	}
}

func TestListAdminGuilds_Empty(t *testing.T) {
	ts := newTestHandlerServer(t)
	wipeGuildTables(t, ts)

	resp := ts.getGuilds(t, "/api/app/admin/guilds")
	if resp.Total != 0 {
		t.Errorf("total: want 0, got %d", resp.Total)
	}
	if len(resp.Items) != 0 {
		t.Errorf("items: want 0, got %d", len(resp.Items))
	}
	if resp.Page != 1 {
		t.Errorf("page: want 1, got %d", resp.Page)
	}
	if resp.PageSize != 20 {
		t.Errorf("pageSize: want 20 default, got %d", resp.PageSize)
	}
}

func TestListAdminGuilds_Pagination(t *testing.T) {
	ts := newTestHandlerServer(t)
	wipeGuildTables(t, ts)

	for i := 0; i < 30; i++ {
		insertBotGuild(t, ts, fmt.Sprintf("100000000000000%03d", i), fmt.Sprintf("Guild %02d", i), "alice")
	}

	page1 := ts.getGuilds(t, "/api/app/admin/guilds?page=1&pageSize=10")
	if page1.Total != 30 {
		t.Errorf("page1 total: want 30, got %d", page1.Total)
	}
	if len(page1.Items) != 10 {
		t.Errorf("page1 items: want 10, got %d", len(page1.Items))
	}
	if page1.Items[0].Name != "Guild 00" {
		t.Errorf("page1[0]: want %q, got %q", "Guild 00", page1.Items[0].Name)
	}

	page2 := ts.getGuilds(t, "/api/app/admin/guilds?page=2&pageSize=10")
	if len(page2.Items) != 10 {
		t.Errorf("page2 items: want 10, got %d", len(page2.Items))
	}
	if page2.Items[0].Name != "Guild 10" {
		t.Errorf("page2[0]: want %q, got %q", "Guild 10", page2.Items[0].Name)
	}

	page4 := ts.getGuilds(t, "/api/app/admin/guilds?page=4&pageSize=10")
	if len(page4.Items) != 0 {
		t.Errorf("page4 items: want 0 (off the end), got %d", len(page4.Items))
	}
	if page4.Total != 30 {
		t.Errorf("page4 total: want 30, got %d", page4.Total)
	}
}

func TestListAdminGuilds_Search(t *testing.T) {
	ts := newTestHandlerServer(t)
	wipeGuildTables(t, ts)

	insertBotGuild(t, ts, "111111111111111111", "Alpha", "alice")
	insertBotGuild(t, ts, "222222222222222222", "Bravo", "bob")
	insertBotGuild(t, ts, "333333333333333333", "Charlie", "carol")

	r := ts.getGuilds(t, "/api/app/admin/guilds?q=brav")
	if r.Total != 1 || r.Items[0].Name != "Bravo" {
		t.Errorf("name search: want Bravo only, got total=%d items=%v", r.Total, r.Items)
	}

	r = ts.getGuilds(t, "/api/app/admin/guilds?q=22222")
	if r.Total != 1 || r.Items[0].GuildID != "222222222222222222" {
		t.Errorf("id search: want guild 222..., got total=%d items=%v", r.Total, r.Items)
	}

	r = ts.getGuilds(t, "/api/app/admin/guilds?q=carol")
	if r.Total != 1 || r.Items[0].Name != "Charlie" {
		t.Errorf("owner search: want Charlie, got total=%d items=%v", r.Total, r.Items)
	}

	r = ts.getGuilds(t, "/api/app/admin/guilds?q=zzz_no_match")
	if r.Total != 0 || len(r.Items) != 0 {
		t.Errorf("no match: want empty, got total=%d items=%v", r.Total, r.Items)
	}
}

func TestListAdminGuilds_ConfiguredAndTrackedCount(t *testing.T) {
	ts := newTestHandlerServer(t)
	wipeGuildTables(t, ts)

	insertBotGuild(t, ts, "111111111111111111", "WithSettings", "alice")
	insertBotGuild(t, ts, "222222222222222222", "Bare", "bob")

	insertGuildSettings(t, ts, "111111111111111111")
	insertGuildTrackedServer(t, ts, "111111111111111111", "bm-1")
	insertGuildTrackedServer(t, ts, "111111111111111111", "bm-2")

	resp := ts.getGuilds(t, "/api/app/admin/guilds")
	if resp.Total != 2 {
		t.Fatalf("total: want 2, got %d", resp.Total)
	}

	byID := map[string]adminGuildItem{}
	for _, it := range resp.Items {
		byID[it.GuildID] = it
	}

	if got := byID["111111111111111111"]; !got.IsConfigured || got.TrackedServerCount != 2 {
		t.Errorf("WithSettings: want isConfigured=true count=2, got %+v", got)
	}
	if got := byID["222222222222222222"]; got.IsConfigured || got.TrackedServerCount != 0 {
		t.Errorf("Bare: want isConfigured=false count=0, got %+v", got)
	}
}

func TestListAdminGuilds_SortAndJoinedFilter(t *testing.T) {
	ts := newTestHandlerServer(t)
	wipeGuildTables(t, ts)

	insertBotGuildWithMeta(t, ts, "111111111111111111", "Alpha", 10, "2026-01-01T00:00:00Z")
	insertBotGuildWithMeta(t, ts, "222222222222222222", "Bravo", 30, "2026-02-01T00:00:00Z")
	insertBotGuildWithMeta(t, ts, "333333333333333333", "Charlie", 20, "2026-03-01T00:00:00Z")

	byMembers := ts.getGuilds(t, "/api/app/admin/guilds?sort=-members")
	if byMembers.Items[0].GuildID != "222222222222222222" {
		t.Fatalf("sort=-members first = %s, want Bravo", byMembers.Items[0].GuildID)
	}

	byJoined := ts.getGuilds(t, "/api/app/admin/guilds?sort=joined")
	if byJoined.Items[0].GuildID != "111111111111111111" || byJoined.Items[0].InstalledAt == "" {
		t.Fatalf("sort=joined first = %+v, want Alpha with installedAt", byJoined.Items[0])
	}

	filtered := ts.getGuilds(t, "/api/app/admin/guilds?joinedSince=2026-02-01&joinedBefore=2026-03-01")
	if filtered.Total != 1 || filtered.Items[0].GuildID != "222222222222222222" {
		t.Fatalf("joined filter got total=%d items=%+v, want Bravo only", filtered.Total, filtered.Items)
	}
}
