# Admin Discord / Guilds Page Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a "Discord" section to the `/admin` sidebar with a single child page at `/admin/guilds` that lists every guild the bot has been installed in (`bot_guilds` table), with server-side pagination and a single search box matching name/guild ID/owner username.

**Architecture:** Repurpose the existing (unused) `GET /api/app/admin/guilds` endpoint to read paginated rows from `bot_guilds` LEFT JOIN `guild_settings` LEFT JOIN `guild_tracked_servers`-count. Add a new `app/admin/guilds/page.tsx` server component plus a client `<GuildsTable>` that drives search + pagination through URL params. Replace the dashboard placeholder nav with a clean admin-area `<NavGroup label="Discord">` when `area="admin"`.

**Tech Stack:** Go 1.24 (chi, pgx) backend, Next.js 16 App Router (React 19, TypeScript) frontend, Tailwind v4 + shadcn/ui (`Sidebar`, `Table`, `Badge`, `Select`, `Input`, `Avatar`, `Button`).

**Spec:** `docs/superpowers/specs/2026-05-02-admin-discord-guilds-design.md`

---

## File Structure

**New files:**
- `apps/frontend/app/admin/guilds/page.tsx` — server component (session gate, search-param parsing, sidebar shell)
- `apps/frontend/app/admin/guilds/_components/guilds-table.tsx` — client component (search input + table + pagination footer)

**Modified files:**
- `apps/backend/internal/store/bot_guilds.go` — add `ListAdminGuilds`
- `apps/backend/internal/http/admin_read_handlers.go` — rewrite `ListAdminGuilds` handler + reshape `adminGuildItem`
- `apps/backend/internal/http/admin_read_handlers_test.go` — extend tests
- `apps/backend/internal/http/openapi.go` — update `/api/app/admin/guilds` path entry + response schema
- `apps/frontend/lib/rustlens-api.ts` — replace `AdminGuildItem` type and `getAdminGuilds()` signature
- `apps/frontend/components/dashboard-app-sidebar.tsx` — admin-area branch rendering `<NavGroup label="Discord">`
- `AGENTS.md` — refresh `app/admin/...` line, add `/admin/guilds`, update `/api/app/admin/guilds` description

---

## Task 1: Backend store — `ListAdminGuilds`

**Files:**
- Modify: `apps/backend/internal/store/bot_guilds.go`

- [ ] **Step 1: Append the new types and method to `bot_guilds.go`**

Append at the end of `apps/backend/internal/store/bot_guilds.go`:

```go
// AdminGuildRow is one row returned by ListAdminGuilds.
type AdminGuildRow struct {
	GuildID                string
	Name                   string
	IconHash               *string
	OwnerDiscordID         *string
	OwnerUsername          *string
	OwnerGlobalName        *string
	OwnerAvatarHash        *string
	ApproximateMemberCount *int32
	IsInstalled            bool
	LeftAt                 *time.Time
	IsConfigured           bool
	TrackedServerCount     int32
}

// ListAdminGuildsOpts are query parameters for ListAdminGuilds.
//
// Q is a free-text search applied to bot_guilds.name (substring),
// bot_guilds.guild_id (prefix), and bot_guilds.owner_username (substring), all
// case-insensitive. Empty string disables the WHERE clause.
type ListAdminGuildsOpts struct {
	Q        string
	Page     int // 1-based; values <= 0 are treated as 1
	PageSize int // values <= 0 or > 100 are clamped to 20
}

// ListAdminGuilds returns one paginated page of bot_guilds rows joined with
// guild_settings (configured flag) and guild_tracked_servers (count). Total is
// the COUNT(*) of matched rows before LIMIT/OFFSET.
func (p *PostgresStore) ListAdminGuilds(ctx context.Context, opts ListAdminGuildsOpts) ([]AdminGuildRow, int, error) {
	page := opts.Page
	if page < 1 {
		page = 1
	}
	pageSize := opts.PageSize
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize

	rows, err := p.pool.Query(ctx, `
		SELECT bg.guild_id, bg.name, bg.icon_hash,
		       bg.owner_discord_id, bg.owner_username, bg.owner_global_name, bg.owner_avatar_hash,
		       bg.approximate_member_count, bg.is_installed, bg.left_at,
		       (gs.guild_id IS NOT NULL) AS is_configured,
		       COALESCE(gts.cnt, 0)      AS tracked_server_count,
		       COUNT(*) OVER ()          AS total
		FROM bot_guilds bg
		LEFT JOIN guild_settings gs ON gs.guild_id = bg.guild_id
		LEFT JOIN (
			SELECT guild_id, COUNT(*)::int AS cnt
			FROM guild_tracked_servers
			GROUP BY guild_id
		) gts ON gts.guild_id = bg.guild_id
		WHERE ($1 = ''
		       OR bg.name ILIKE '%' || $1 || '%'
		       OR bg.guild_id ILIKE $1 || '%'
		       OR bg.owner_username ILIKE '%' || $1 || '%')
		ORDER BY bg.name ASC, bg.guild_id ASC
		LIMIT $2 OFFSET $3
	`, opts.Q, pageSize, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := make([]AdminGuildRow, 0)
	total := 0
	for rows.Next() {
		var r AdminGuildRow
		var rowTotal int64
		if err := rows.Scan(
			&r.GuildID, &r.Name, &r.IconHash,
			&r.OwnerDiscordID, &r.OwnerUsername, &r.OwnerGlobalName, &r.OwnerAvatarHash,
			&r.ApproximateMemberCount, &r.IsInstalled, &r.LeftAt,
			&r.IsConfigured, &r.TrackedServerCount, &rowTotal,
		); err != nil {
			return nil, 0, err
		}
		total = int(rowTotal)
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd apps/backend && go build ./internal/store/...`
Expected: no output, exit 0.

- [ ] **Step 3: Commit**

```bash
git add apps/backend/internal/store/bot_guilds.go
git commit -m "feat(backend): add store.ListAdminGuilds for admin guilds page"
```

---

## Task 2: Backend handler — rewrite `ListAdminGuilds`

**Files:**
- Modify: `apps/backend/internal/http/admin_read_handlers.go:43-49,208-246`

- [ ] **Step 1: Replace the `adminGuildItem` struct (lines 43-49)**

Find:
```go
type adminGuildItem struct {
	GuildID        string `json:"guildId"`
	ActiveMembers  int    `json:"activeMembers"`
	LinkedMembers  int    `json:"linkedMembers"`
	TrackedServers int    `json:"trackedServers"`
	AdminRoleID    string `json:"adminRoleId"`
}
```

Replace with:
```go
type adminGuildItem struct {
	GuildID                string  `json:"guildId"`
	Name                   string  `json:"name"`
	IconHash               *string `json:"iconHash"`
	OwnerDiscordID         *string `json:"ownerDiscordId"`
	OwnerUsername          *string `json:"ownerUsername"`
	OwnerGlobalName        *string `json:"ownerGlobalName"`
	OwnerAvatarHash        *string `json:"ownerAvatarHash"`
	ApproximateMemberCount *int32  `json:"approximateMemberCount"`
	IsInstalled            bool    `json:"isInstalled"`
	LeftAt                 *string `json:"leftAt"`
	IsConfigured           bool    `json:"isConfigured"`
	TrackedServerCount     int     `json:"trackedServerCount"`
}
```

- [ ] **Step 2: Replace the `ListAdminGuilds` handler (lines 208-246)**

Find the entire `// ListAdminGuilds: GET /api/app/admin/guilds` function (from the comment through the closing brace on line 246).

Replace with:
```go
// ListAdminGuilds: GET /api/app/admin/guilds
// Query:
//   q=<substring>            matches bot_guilds.name (substring), guild_id (prefix), owner_username (substring); ILIKE
//   page=1 (default)         1-based
//   pageSize=20 (default)    capped at 100
func (h *Handler) ListAdminGuilds(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	page := 1
	pageSize := 20
	if v := q.Get("page"); v != "" {
		parseLimit(v, &page)
		if page < 1 {
			page = 1
		}
	}
	if v := q.Get("pageSize"); v != "" {
		parseLimit(v, &pageSize)
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	rows, total, err := h.postgres.ListAdminGuilds(ctx, store.ListAdminGuildsOpts{
		Q:        q.Get("q"),
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	items := make([]adminGuildItem, 0, len(rows))
	for _, r := range rows {
		it := adminGuildItem{
			GuildID:                r.GuildID,
			Name:                   r.Name,
			IconHash:               r.IconHash,
			OwnerDiscordID:         r.OwnerDiscordID,
			OwnerUsername:          r.OwnerUsername,
			OwnerGlobalName:        r.OwnerGlobalName,
			OwnerAvatarHash:        r.OwnerAvatarHash,
			ApproximateMemberCount: r.ApproximateMemberCount,
			IsInstalled:            r.IsInstalled,
			IsConfigured:           r.IsConfigured,
			TrackedServerCount:     int(r.TrackedServerCount),
		}
		if r.LeftAt != nil {
			s := r.LeftAt.Format(time.RFC3339)
			it.LeftAt = &s
		}
		items = append(items, it)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items":    items,
		"total":    total,
		"page":     page,
		"pageSize": pageSize,
	})
}
```

- [ ] **Step 3: Add the `store` import if missing**

Open `apps/backend/internal/http/admin_read_handlers.go` and confirm the import block has `"rustlens/backend/internal/store"`. If not, add it. (Run `go build` in next step to surface this.)

- [ ] **Step 4: Verify it compiles**

Run: `cd apps/backend && go build ./...`
Expected: no output, exit 0.

If you see "undefined: store", add `"rustlens/backend/internal/store"` to the import group at the top of `admin_read_handlers.go` and re-run.

- [ ] **Step 5: Commit**

```bash
git add apps/backend/internal/http/admin_read_handlers.go
git commit -m "feat(backend): rewrite ListAdminGuilds to read bot_guilds with pagination/search"
```

---

## Task 3: Backend tests — `ListAdminGuilds`

**Files:**
- Modify: `apps/backend/internal/http/admin_read_handlers_test.go`

- [ ] **Step 1: Append the new test types and helpers**

Append at the end of `apps/backend/internal/http/admin_read_handlers_test.go`:

```go
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

	// Insert 30 guilds with names that sort deterministically.
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

	// name substring (case-insensitive)
	r := ts.getGuilds(t, "/api/app/admin/guilds?q=brav")
	if r.Total != 1 || r.Items[0].Name != "Bravo" {
		t.Errorf("name search: want Bravo only, got total=%d items=%v", r.Total, r.Items)
	}

	// guild_id prefix
	r = ts.getGuilds(t, "/api/app/admin/guilds?q=22222")
	if r.Total != 1 || r.Items[0].GuildID != "222222222222222222" {
		t.Errorf("id search: want guild 222..., got total=%d items=%v", r.Total, r.Items)
	}

	// owner_username substring
	r = ts.getGuilds(t, "/api/app/admin/guilds?q=carol")
	if r.Total != 1 || r.Items[0].Name != "Charlie" {
		t.Errorf("owner search: want Charlie, got total=%d items=%v", r.Total, r.Items)
	}

	// no match
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
```

- [ ] **Step 2: Run the new tests**

Run: `cd apps/backend && TEST_POSTGRES_URL=postgres://rustlens:rustlens@localhost:5432/rustlens?sslmode=disable go test ./internal/http/ -run TestListAdminGuilds -v`

Expected: all four tests pass. If they skip with "TEST_POSTGRES_URL not set", make sure local infra is up (`make infra-up`). If a `TRUNCATE` fails because of foreign-key constraints, expand the `wipeGuildTables` truncate list with the conflicting table.

- [ ] **Step 3: Run full backend test suite**

Run: `cd apps/backend && TEST_POSTGRES_URL=postgres://rustlens:rustlens@localhost:5432/rustlens?sslmode=disable go test ./...`

Expected: all packages pass.

- [ ] **Step 4: Commit**

```bash
git add apps/backend/internal/http/admin_read_handlers_test.go
git commit -m "test(backend): cover ListAdminGuilds pagination/search/configured/tracked"
```

---

## Task 4: Backend OpenAPI — refresh `/api/app/admin/guilds` schema

**Files:**
- Modify: `apps/backend/internal/http/openapi.go:2081-2098,2994-3014`

- [ ] **Step 1: Update the path entry (lines 2081-2098)**

Find:
```go
"/api/app/admin/guilds": map[string]any{
    "get": map[string]any{
        "summary":     "List admin guilds",
        "description": "Returns internal admin overview data for guild activity, linked members, and tracked servers.",
        "security":    []map[string][]string{{"appSecretAuth": {}, "appSessionAuth": {}}},
        "responses": map[string]any{
            "200": map[string]any{
                "description": "Admin guilds returned",
                "content": map[string]any{
                    "application/json": map[string]any{
                        "schema": map[string]any{"$ref": "#/components/schemas/AdminGuildOverviewListResponse"},
                    },
                },
            },
            "401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
            "403": map[string]any{"$ref": "#/components/responses/Forbidden"},
        },
    },
},
```

Replace with:
```go
"/api/app/admin/guilds": map[string]any{
    "get": map[string]any{
        "summary":     "List admin guilds",
        "description": "Returns paginated bot_guilds rows with name/icon/owner metadata, plus join-derived isConfigured (guild_settings) and trackedServerCount (guild_tracked_servers). Supports free-text search across name, guild_id prefix, and owner username.",
        "security":    []map[string][]string{{"appSecretAuth": {}, "appSessionAuth": {}}},
        "parameters": []map[string]any{
            {
                "name":        "q",
                "in":          "query",
                "required":    false,
                "description": "Substring/prefix search across bot_guilds.name, bot_guilds.guild_id (prefix), and bot_guilds.owner_username.",
                "schema":      map[string]any{"type": "string"},
            },
            {
                "name":        "page",
                "in":          "query",
                "required":    false,
                "description": "1-based page number. Default 1.",
                "schema":      map[string]any{"type": "integer", "minimum": 1},
            },
            {
                "name":        "pageSize",
                "in":          "query",
                "required":    false,
                "description": "Page size, default 20, capped at 100.",
                "schema":      map[string]any{"type": "integer", "minimum": 1, "maximum": 100},
            },
        },
        "responses": map[string]any{
            "200": map[string]any{
                "description": "Admin guilds returned",
                "content": map[string]any{
                    "application/json": map[string]any{
                        "schema": map[string]any{"$ref": "#/components/schemas/AdminGuildListResponse"},
                    },
                },
            },
            "401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
            "403": map[string]any{"$ref": "#/components/responses/Forbidden"},
        },
    },
},
```

- [ ] **Step 2: Replace the schema definitions (lines 2994-3014)**

Find:
```go
"AdminGuildOverview": map[string]any{
    "type": "object",
    "properties": map[string]any{
        "guildId":        map[string]any{"type": "string"},
        "activeMembers":  map[string]any{"type": "integer"},
        "linkedMembers":  map[string]any{"type": "integer"},
        "trackedServers": map[string]any{"type": "integer"},
        "adminRoleId":    map[string]any{"type": "string"},
    },
    "required": []string{"guildId", "activeMembers", "linkedMembers", "trackedServers", "adminRoleId"},
},
"AdminGuildOverviewListResponse": map[string]any{
    "type": "object",
    "properties": map[string]any{
        "items": map[string]any{
            "type":  "array",
            "items": map[string]any{"$ref": "#/components/schemas/AdminGuildOverview"},
        },
    },
    "required": []string{"items"},
},
```

Replace with:
```go
"AdminGuildItem": map[string]any{
    "type": "object",
    "properties": map[string]any{
        "guildId":                map[string]any{"type": "string"},
        "name":                   map[string]any{"type": "string"},
        "iconHash":               map[string]any{"type": "string", "nullable": true},
        "ownerDiscordId":         map[string]any{"type": "string", "nullable": true},
        "ownerUsername":          map[string]any{"type": "string", "nullable": true},
        "ownerGlobalName":        map[string]any{"type": "string", "nullable": true},
        "ownerAvatarHash":        map[string]any{"type": "string", "nullable": true},
        "approximateMemberCount": map[string]any{"type": "integer", "nullable": true},
        "isInstalled":            map[string]any{"type": "boolean"},
        "leftAt":                 map[string]any{"type": "string", "format": "date-time", "nullable": true},
        "isConfigured":           map[string]any{"type": "boolean"},
        "trackedServerCount":     map[string]any{"type": "integer"},
    },
    "required": []string{
        "guildId", "name", "iconHash",
        "ownerDiscordId", "ownerUsername", "ownerGlobalName", "ownerAvatarHash",
        "approximateMemberCount", "isInstalled", "leftAt",
        "isConfigured", "trackedServerCount",
    },
},
"AdminGuildListResponse": map[string]any{
    "type": "object",
    "properties": map[string]any{
        "items": map[string]any{
            "type":  "array",
            "items": map[string]any{"$ref": "#/components/schemas/AdminGuildItem"},
        },
        "total":    map[string]any{"type": "integer"},
        "page":     map[string]any{"type": "integer"},
        "pageSize": map[string]any{"type": "integer"},
    },
    "required": []string{"items", "total", "page", "pageSize"},
},
```

- [ ] **Step 3: Verify the backend still builds**

Run: `cd apps/backend && go build ./...`
Expected: no output.

If you see "AdminGuildOverview not used" or similar, search for any other references in `openapi.go` and remove them (the schema is renamed to `AdminGuildItem`/`AdminGuildListResponse`).

- [ ] **Step 4: Commit**

```bash
git add apps/backend/internal/http/openapi.go
git commit -m "docs(backend): refresh /api/app/admin/guilds OpenAPI schema"
```

---

## Task 5: Frontend API helper — replace `getAdminGuilds`

**Files:**
- Modify: `apps/frontend/lib/rustlens-api.ts:115-121,358-365`

- [ ] **Step 1: Replace the `AdminGuildItem` type (lines 115-121)**

Find:
```ts
export type AdminGuildItem = {
	guildId: string;
	activeMembers: number;
	linkedMembers: number;
	trackedServers: number;
	adminRoleId: string;
};
```

Replace with:
```ts
export type AdminGuildItem = {
	guildId: string;
	name: string;
	iconHash: string | null;
	ownerDiscordId: string | null;
	ownerUsername: string | null;
	ownerGlobalName: string | null;
	ownerAvatarHash: string | null;
	approximateMemberCount: number | null;
	isInstalled: boolean;
	leftAt: string | null;
	isConfigured: boolean;
	trackedServerCount: number;
};

export type AdminGuildsQuery = {
	q?: string;
	page?: number;
	pageSize?: number;
};

export type AdminGuildsPage = {
	items: AdminGuildItem[];
	total: number;
	page: number;
	pageSize: number;
};
```

- [ ] **Step 2: Replace the `getAdminGuilds` function (lines 358-365)**

Find:
```ts
export async function getAdminGuilds() {
	const payload = await backendFetch<{ items: AdminGuildItem[] }>(
		"/api/app/admin/guilds",
		undefined,
		{ requireSession: true },
	);
	return payload.items;
}
```

Replace with:
```ts
export async function getAdminGuilds(params: AdminGuildsQuery = {}): Promise<AdminGuildsPage> {
	const sp = new URLSearchParams();
	if (params.q) sp.set("q", params.q);
	if (params.page) sp.set("page", String(params.page));
	if (params.pageSize) sp.set("pageSize", String(params.pageSize));
	const qs = sp.toString();
	return backendFetch<AdminGuildsPage>(
		`/api/app/admin/guilds${qs ? `?${qs}` : ""}`,
		undefined,
		{ requireSession: true },
	);
}
```

- [ ] **Step 3: Type-check the frontend**

Run: `pnpm --filter @rustlens/frontend lint`
Expected: no type errors related to `AdminGuildItem` / `getAdminGuilds`. (If a previously-unrelated `getAdminGuilds()` caller existed somewhere, the compiler will flag it; the spec confirmed no live callers, so this should be clean.)

- [ ] **Step 4: Commit**

```bash
git add apps/frontend/lib/rustlens-api.ts
git commit -m "feat(frontend): paginated AdminGuildItem and getAdminGuilds() signature"
```

---

## Task 6: Frontend sidebar — admin-area `Discord` group

**Files:**
- Modify: `apps/frontend/components/dashboard-app-sidebar.tsx`

- [ ] **Step 1: Update imports**

Open `apps/frontend/components/dashboard-app-sidebar.tsx`. Find the lucide import block (lines 24-39) and add `UsersRoundIcon` and `Hash` (Hash is for the Discord-section icon — the existing `NavGroup` doesn't render a section-level icon, but we want a clear group label, so `UsersRoundIcon` for the **Guilds** menu item is enough. We can skip importing extra icons.)

Concretely, just ensure `UsersRoundIcon` is imported. If it isn't, add it:

Find the line:
```ts
	UsersIcon,
} from "lucide-react";
```

Replace with:
```ts
	UsersIcon,
	UsersRoundIcon,
} from "lucide-react";
```

Also add `NavGroup` import. Find:
```ts
import { NavDocuments } from "@/components/nav-documents";
import { NavMain } from "@/components/nav-main";
import { NavSecondary } from "@/components/nav-secondary";
```

Replace with:
```ts
import { NavDocuments } from "@/components/nav-documents";
import { NavGroup } from "@/components/nav-group";
import { NavMain } from "@/components/nav-main";
import { NavSecondary } from "@/components/nav-secondary";
```

- [ ] **Step 2: Branch the rendered content on `area`**

Find the `<SidebarContent>` block (around line 177):
```tsx
			<SidebarContent>
				<NavMain items={navMain} />
				<NavDocuments items={DOCUMENTS} />
				<NavSecondary items={NAV_SECONDARY} className="mt-auto" />
			</SidebarContent>
```

Replace with:
```tsx
			<SidebarContent>
				<NavMain items={navMain} />
				{area === "admin" ? (
					<NavGroup
						label="Discord"
						items={[
							{
								title: "Guilds",
								path: "/admin/guilds",
								icon: UsersRoundIcon,
								isActive: pathname === "/admin/guilds" || pathname.startsWith("/admin/guilds/"),
							},
						]}
					/>
				) : (
					<>
						<NavDocuments items={DOCUMENTS} />
						<NavSecondary items={NAV_SECONDARY} className="mt-auto" />
					</>
				)}
			</SidebarContent>
```

- [ ] **Step 3: Type-check the frontend**

Run: `pnpm --filter @rustlens/frontend lint`
Expected: no type errors. The `SidebarNavItem` type from `app-shared.tsx:47-52` matches (`title`, `path`, `icon: LucideIcon`, optional `isActive`).

- [ ] **Step 4: Commit**

```bash
git add apps/frontend/components/dashboard-app-sidebar.tsx
git commit -m "feat(frontend): admin sidebar shows Discord > Guilds nav group"
```

---

## Task 7: Frontend page — `/admin/guilds` server component

**Files:**
- Create: `apps/frontend/app/admin/guilds/page.tsx`

- [ ] **Step 1: Create the file**

Create `apps/frontend/app/admin/guilds/page.tsx` with:

```tsx
import { AppSidebar } from "../_components/app-sidebar";
import { GuildsTable } from "./_components/guilds-table";
import { SiteHeader } from "@/components/site-header";
import { SidebarInset, SidebarProvider } from "@/components/ui/sidebar";
import { getCsrfToken } from "@/lib/csrf";
import { getAdminGuilds, getCurrentUserSafe } from "@/lib/rustlens-api";
import { requireAdminSession } from "@/lib/session";

const DEFAULT_PAGE_SIZE = 20;
const PAGE_SIZE_OPTIONS = [10, 20, 30, 40, 50];

function parsePositiveInt(value: string | string[] | undefined, fallback: number): number {
  const raw = Array.isArray(value) ? value[0] : value;
  if (!raw) return fallback;
  const n = Number.parseInt(raw, 10);
  return Number.isFinite(n) && n > 0 ? n : fallback;
}

function clampPageSize(value: number): number {
  return PAGE_SIZE_OPTIONS.includes(value) ? value : DEFAULT_PAGE_SIZE;
}

export default async function AdminGuildsPage({
  searchParams,
}: {
  searchParams: Promise<Record<string, string | string[] | undefined>>;
}) {
  const session = await requireAdminSession();
  const sp = await searchParams;

  const qRaw = Array.isArray(sp.q) ? sp.q[0] : sp.q;
  const q = qRaw ?? "";
  const page = parsePositiveInt(sp.page, 1);
  const pageSize = clampPageSize(parsePositiveInt(sp.pageSize, DEFAULT_PAGE_SIZE));

  const [csrfToken, currentUser, guilds] = await Promise.all([
    getCsrfToken(),
    getCurrentUserSafe(),
    getAdminGuilds({ q: q || undefined, page, pageSize }),
  ]);

  const user = {
    displayName: session.displayName,
    avatarUrl: session.avatarUrl || undefined,
    discordUsername: currentUser?.user.discordUsername,
    isInternalAdmin:
      (session.adminRoles?.length ?? 0) > 0 ||
      session.roles.includes("internal_admin"),
  };

  return (
    <SidebarProvider
      style={
        {
          "--sidebar-width": "calc(var(--spacing) * 72)",
          "--header-height": "calc(var(--spacing) * 12)",
        } as React.CSSProperties
      }
    >
      <AppSidebar csrfToken={csrfToken} user={user} variant="inset" />
      <SidebarInset>
        <SiteHeader />
        <div className="flex flex-1 flex-col">
          <div className="@container/main flex flex-1 flex-col gap-2">
            <div className="flex flex-col gap-4 py-4 md:gap-6 md:py-6 px-4 lg:px-6">
              <div>
                <h1 className="text-2xl font-semibold">Guilds</h1>
                <p className="text-sm text-muted-foreground">
                  All Discord guilds RustLens has been installed in.
                </p>
              </div>
              <GuildsTable
                initialQuery={q}
                initialPage={page}
                initialPageSize={pageSize}
                pageSizeOptions={PAGE_SIZE_OPTIONS}
                data={guilds}
              />
            </div>
          </div>
        </div>
      </SidebarInset>
    </SidebarProvider>
  );
}
```

- [ ] **Step 2: Verify the file compiles (page builds)**

Skip a separate compile here — Task 8 introduces the imported `GuildsTable`. The lint/build verification happens at the end of Task 8.

- [ ] **Step 3: (Defer commit)**

Do not commit yet — `GuildsTable` doesn't exist; Task 8 adds it and the commit covers both files.

---

## Task 8: Frontend client component — `GuildsTable`

**Files:**
- Create: `apps/frontend/app/admin/guilds/_components/guilds-table.tsx`

- [ ] **Step 1: Create the file**

Create `apps/frontend/app/admin/guilds/_components/guilds-table.tsx` with:

```tsx
"use client";

import * as React from "react";
import { useRouter, useSearchParams } from "next/navigation";
import {
  ChevronLeftIcon,
  ChevronRightIcon,
  ChevronsLeftIcon,
  ChevronsRightIcon,
} from "lucide-react";

import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { guildIconUrl, userAvatarUrl } from "@/lib/discord-cdn";
import type { AdminGuildItem, AdminGuildsPage } from "@/lib/rustlens-api";

type GuildsTableProps = {
  initialQuery: string;
  initialPage: number;
  initialPageSize: number;
  pageSizeOptions: number[];
  data: AdminGuildsPage;
};

const SEARCH_DEBOUNCE_MS = 300;

function formatNumber(n: number | null): string {
  if (n === null) return "—";
  return n.toLocaleString();
}

function formatRelativeLeftAt(iso: string | null): string {
  if (!iso) return "";
  const ts = new Date(iso).getTime();
  if (!Number.isFinite(ts)) return "";
  const diff = Date.now() - ts;
  const days = Math.floor(diff / (1000 * 60 * 60 * 24));
  if (days < 1) return "today";
  if (days === 1) return "1 day ago";
  if (days < 30) return `${days} days ago`;
  const months = Math.floor(days / 30);
  if (months === 1) return "1 month ago";
  if (months < 12) return `${months} months ago`;
  const years = Math.floor(days / 365);
  return years === 1 ? "1 year ago" : `${years} years ago`;
}

function ownerLabel(g: AdminGuildItem): string {
  return g.ownerGlobalName || g.ownerUsername || "—";
}

function ownerInitial(g: AdminGuildItem): string {
  const label = ownerLabel(g);
  return label === "—" ? "?" : label.charAt(0).toUpperCase();
}

export function GuildsTable({
  initialQuery,
  initialPage,
  initialPageSize,
  pageSizeOptions,
  data,
}: GuildsTableProps) {
  const router = useRouter();
  const searchParams = useSearchParams();
  const [searchValue, setSearchValue] = React.useState(initialQuery);

  const totalPages = Math.max(1, Math.ceil(data.total / data.pageSize));
  const canPrev = data.page > 1;
  const canNext = data.page < totalPages;

  const pushParams = React.useCallback(
    (updates: Record<string, string | null>) => {
      const next = new URLSearchParams(searchParams?.toString() ?? "");
      for (const [k, v] of Object.entries(updates)) {
        if (v === null || v === "") next.delete(k);
        else next.set(k, v);
      }
      const qs = next.toString();
      router.push(qs ? `?${qs}` : "?");
    },
    [router, searchParams],
  );

  // Debounce search → URL push
  React.useEffect(() => {
    if (searchValue === initialQuery) return;
    const t = window.setTimeout(() => {
      pushParams({ q: searchValue || null, page: null });
    }, SEARCH_DEBOUNCE_MS);
    return () => window.clearTimeout(t);
  }, [searchValue, initialQuery, pushParams]);

  const goToPage = (p: number) => pushParams({ page: String(p) });
  const setPageSize = (size: number) =>
    pushParams({ pageSize: String(size), page: null });

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center gap-2">
        <Input
          placeholder="Search by name, guild ID, or owner username…"
          value={searchValue}
          onChange={(e) => setSearchValue(e.target.value)}
          className="max-w-md"
          aria-label="Search guilds"
        />
        <span className="text-sm text-muted-foreground">
          {data.total} {data.total === 1 ? "guild" : "guilds"}
        </span>
      </div>

      <div className="overflow-hidden rounded-lg border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="w-12"></TableHead>
              <TableHead>Name</TableHead>
              <TableHead>Guild ID</TableHead>
              <TableHead>Owner</TableHead>
              <TableHead className="text-right">Members</TableHead>
              <TableHead>Installed</TableHead>
              <TableHead>Configured</TableHead>
              <TableHead className="text-right">Tracked</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {data.items.length === 0 ? (
              <TableRow>
                <TableCell colSpan={8} className="h-24 text-center">
                  No guilds found.
                </TableCell>
              </TableRow>
            ) : (
              data.items.map((g) => {
                const iconUrl = guildIconUrl(g.guildId, g.iconHash, 64);
                const ownerAvatar = g.ownerDiscordId
                  ? userAvatarUrl(g.ownerDiscordId, g.ownerAvatarHash, 64)
                  : null;
                return (
                  <TableRow key={g.guildId}>
                    <TableCell>
                      <Avatar className="size-8">
                        {iconUrl && <AvatarImage src={iconUrl} alt={g.name} />}
                        <AvatarFallback>
                          {g.name ? g.name.charAt(0).toUpperCase() : "?"}
                        </AvatarFallback>
                      </Avatar>
                    </TableCell>
                    <TableCell className="font-medium">{g.name || "—"}</TableCell>
                    <TableCell>
                      <button
                        type="button"
                        onClick={() => navigator.clipboard?.writeText(g.guildId)}
                        className="font-mono text-xs text-muted-foreground hover:text-foreground"
                        title="Click to copy"
                      >
                        {g.guildId}
                      </button>
                    </TableCell>
                    <TableCell>
                      <div className="flex items-center gap-2">
                        <Avatar className="size-6">
                          {ownerAvatar && (
                            <AvatarImage src={ownerAvatar} alt={ownerLabel(g)} />
                          )}
                          <AvatarFallback className="text-[10px]">
                            {ownerInitial(g)}
                          </AvatarFallback>
                        </Avatar>
                        <span className="text-sm">{ownerLabel(g)}</span>
                      </div>
                    </TableCell>
                    <TableCell className="text-right tabular-nums">
                      {formatNumber(g.approximateMemberCount)}
                    </TableCell>
                    <TableCell>
                      {g.isInstalled ? (
                        <Badge variant="default">Installed</Badge>
                      ) : (
                        <Badge variant="secondary" title={g.leftAt ?? undefined}>
                          {g.leftAt ? `Left ${formatRelativeLeftAt(g.leftAt)}` : "Left"}
                        </Badge>
                      )}
                    </TableCell>
                    <TableCell>
                      {g.isConfigured ? <Badge variant="outline">Configured</Badge> : <span className="text-muted-foreground">—</span>}
                    </TableCell>
                    <TableCell
                      className={`text-right tabular-nums ${g.trackedServerCount === 0 ? "text-muted-foreground" : ""}`}
                    >
                      {g.trackedServerCount}
                    </TableCell>
                  </TableRow>
                );
              })
            )}
          </TableBody>
        </Table>
      </div>

      <div className="flex items-center justify-between px-4">
        <div className="hidden flex-1 text-sm text-muted-foreground lg:flex" />
        <div className="flex w-full items-center gap-8 lg:w-fit">
          <div className="hidden items-center gap-2 lg:flex">
            <Label htmlFor="rows-per-page" className="text-sm font-medium">
              Rows per page
            </Label>
            <Select
              value={String(data.pageSize)}
              onValueChange={(value) => setPageSize(Number(value))}
            >
              <SelectTrigger size="sm" className="w-20" id="rows-per-page">
                <SelectValue placeholder={data.pageSize} />
              </SelectTrigger>
              <SelectContent side="top">
                <SelectGroup>
                  {pageSizeOptions.map((size) => (
                    <SelectItem key={size} value={String(size)}>
                      {size}
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
          </div>
          <div className="flex w-fit items-center justify-center text-sm font-medium">
            Page {data.page} of {totalPages}
          </div>
          <div className="ml-auto flex items-center gap-2 lg:ml-0">
            <Button
              variant="outline"
              className="hidden h-8 w-8 p-0 lg:flex"
              onClick={() => goToPage(1)}
              disabled={!canPrev}
            >
              <span className="sr-only">Go to first page</span>
              <ChevronsLeftIcon />
            </Button>
            <Button
              variant="outline"
              className="size-8"
              size="icon"
              onClick={() => goToPage(data.page - 1)}
              disabled={!canPrev}
            >
              <span className="sr-only">Go to previous page</span>
              <ChevronLeftIcon />
            </Button>
            <Button
              variant="outline"
              className="size-8"
              size="icon"
              onClick={() => goToPage(data.page + 1)}
              disabled={!canNext}
            >
              <span className="sr-only">Go to next page</span>
              <ChevronRightIcon />
            </Button>
            <Button
              variant="outline"
              className="hidden size-8 lg:flex"
              size="icon"
              onClick={() => goToPage(totalPages)}
              disabled={!canNext}
            >
              <span className="sr-only">Go to last page</span>
              <ChevronsRightIcon />
            </Button>
          </div>
        </div>
      </div>
    </div>
  );
}
```

- [ ] **Step 2: Type-check + lint the frontend**

Run: `pnpm --filter @rustlens/frontend lint`
Expected: clean. If a shadcn primitive (`avatar`, `select`, `table`, `badge`, `input`, `label`) is missing, install it via `pnpm --filter @rustlens/frontend dlx shadcn@latest add avatar` (etc.) and re-run.

- [ ] **Step 3: Build the frontend**

Run: `pnpm --filter @rustlens/frontend build`
Expected: build succeeds and the new route compiles. (The page is dynamic because it uses `searchParams` — that's fine.)

- [ ] **Step 4: Manual smoke test**

Start the stack with `make dev`. Sign in as an admin (Discord OAuth). Visit `http://localhost/admin/guilds`. Verify:
- The sidebar shows a "Discord" group with "Guilds" highlighted.
- The page header reads "Guilds".
- The table renders rows for the bot's installed guilds with icons, IDs, and member counts.
- Typing in the search box updates the URL `?q=…` after ~300 ms and refreshes the table.
- The "Rows per page" dropdown changes `?pageSize=…` and the page resets.
- First / Prev / Next / Last buttons work and disable at the boundaries.
- "Page X of Y" reflects `total / pageSize`.
- Visiting `/admin` (overview) does NOT show the "Discord" group is broken — i.e., it now also uses the new admin-only sidebar (Documents/NavSecondary placeholders gone).

If you can't test the UI in a browser, say so explicitly rather than claiming success.

- [ ] **Step 5: Commit (covers Task 7 + Task 8)**

```bash
git add apps/frontend/app/admin/guilds/page.tsx apps/frontend/app/admin/guilds/_components/guilds-table.tsx
git commit -m "feat(frontend): add /admin/guilds page with search and pagination"
```

---

## Task 9: Refresh `AGENTS.md`

**Files:**
- Modify: `AGENTS.md`

- [ ] **Step 1: Update the admin app routes section**

Find:
```
    admin/page.tsx                      # Internal ops dashboard guarded by app-user internal_admin role
```

Replace with:
```
    admin/page.tsx                      # Internal ops dashboard guarded by app-user internal_admin role
    admin/guilds/page.tsx               # Admin Discord/Guilds list (paginated bot_guilds with name+ID+owner search)
```

- [ ] **Step 2: Update the `/api/app/admin/guilds` description in the backend section**

Find the bullet describing admin RBAC mounting:
```
- App-facing routes under `/api/app/*` are protected by an internal shared-secret header used only by Next.js server-side code. Session-authenticated `/api/app/*` routes additionally validate the signed frontend session token with `APP_SESSION_SECRET` and derive the actor from that session before resolving current-user/admin authorization through `app_users` roles. Admin RBAC routes are mounted under `/api/app/admin/*` and back the internal-admin dashboard (roles, users, guilds, servers, audit, action dispatcher, CSV export).
```

Append a new bullet immediately after it:
```
- The admin `GET /api/app/admin/guilds` route reads paginated rows from `bot_guilds` LEFT JOIN `guild_settings` LEFT JOIN `guild_tracked_servers` (count). Query params: `q` (substring on `name`/`owner_username`, prefix on `guild_id`), `page` (default 1), `pageSize` (default 20, capped at 100). Response is `{items, total, page, pageSize}`.
```

- [ ] **Step 3: Commit**

```bash
git add AGENTS.md
git commit -m "docs: document /admin/guilds page and admin guilds API shape"
```

---

## Self-Review Notes

**Spec coverage:**
- Sidebar admin-area branch with `Discord` group → Task 6
- Backend endpoint repurposing with `q/page/pageSize` → Tasks 1, 2, 4
- Backend test coverage (empty / pagination / search / configured+tracked) → Task 3
- Frontend `getAdminGuilds` paginated signature + types → Task 5
- `/admin/guilds` server component → Task 7
- `<GuildsTable>` client component (search + table + pagination footer) → Task 8
- AGENTS.md refresh → Task 9

**Type consistency:** `AdminGuildItem` shape matches across backend (`adminGuildItem`), OpenAPI (`AdminGuildItem`), frontend type (`AdminGuildItem`), and the SQL projection. `ListAdminGuildsOpts` field names (`Q`, `Page`, `PageSize`) match the handler call site. `pageSizeOptions={[10,20,30,40,50]}` and `DEFAULT_PAGE_SIZE=20` match backend default.
