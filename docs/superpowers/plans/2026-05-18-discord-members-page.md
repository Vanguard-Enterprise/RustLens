# Discord Members Page — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship a read-only `/admin/members` page (under the Discord sidebar group) that lists every `app_users` row with zero admin role grants, and ensure an `app_users` row exists for any Discord user who triggers a bot interaction.

**Architecture:**

1. Backend adds two endpoints: `POST /api/internal/bot/setup/users/ensure` (bot-only, atomic upsert via `appauth.Repo.FindOrCreateByDiscordID`) and `GET /api/app/admin/members` (reuses `buildUsersQuery` with an additional `NOT EXISTS` clause that excludes any user holding a role with permissions).
2. Bot adds an `ensureAppUser` helper that fires-and-forgets a POST to the new endpoint at the top of `interactionCreate`, deduped by a 5-minute in-memory TTL cache.
3. Frontend adds a new route `/admin/members`, a `MembersTable` client component, and a sidebar entry. The detail view reuses the existing `/admin/users/[userId]` page.

**Tech Stack:** Go 1.25 (chi router, pgxpool), Node/TypeScript (discord.js, vitest), Next.js 16 App Router, shadcn/ui, Tailwind, lucide-react.

**Spec:** `docs/superpowers/specs/2026-05-18-discord-members-page-design.md`

---

## File Structure

**Backend (Go):**
- Modify `apps/backend/internal/http/handlers.go` — add `appauth *appauth.Repo` to `Handler` struct + `NewHandler` constructor.
- Modify `apps/backend/internal/http/router.go` — move existing `appAuthRepo` construction up so `NewHandler` can receive it, mount new ensure route + new admin members route. (Public signature of `NewRouter` is unchanged.)
- Create `apps/backend/internal/http/bot_users_handlers.go` — `EnsureAppUser` HTTP handler.
- Create `apps/backend/internal/http/bot_users_handlers_test.go` — handler tests.
- Modify `apps/backend/internal/http/admin_read_handlers.go` — add `ListAdminMembers` handler at the bottom of the file (next to `ListAdminUsers` block).
- Modify `apps/backend/internal/http/admin_users_query.go` — add exported `MembersOnlyClause` helper (string fragment + no extra args).
- Create `apps/backend/internal/http/admin_members_test.go` — handler test for `ListAdminMembers`.

**Bot (TypeScript):**
- Modify `apps/bot/src/backend-client.ts` — add `ensureAppUser` method.
- Modify `apps/bot/src/backend-client.test.ts` — assert the new method serializes correctly.
- Create `apps/bot/src/ensure-app-user.ts` — exported `ensureAppUser(interaction)` helper with TTL dedupe.
- Create `apps/bot/src/ensure-app-user.test.ts` — dedupe test.
- Modify `apps/bot/src/shard-worker.ts` — add one call line inside the `interactionCreate` handler.

**Frontend (TypeScript/React):**
- Modify `apps/frontend/lib/rustlens-api.ts` — add `getAdminMembers` (mirrors `getAdminUsers`).
- Create `apps/frontend/app/admin/members/page.tsx` — server component route.
- Create `apps/frontend/app/admin/members/_components/members-table.tsx` — client table.
- Create `apps/frontend/app/admin/members/_components/__tests__/members-table.test.tsx` — Vitest component test.
- Modify `apps/frontend/components/dashboard-app-sidebar.tsx` — add `Members` nav item under the Discord group.

**Test commands per slice:**
- Backend: `cd apps/backend && go test ./internal/http/... -run '<TestName>'` (handler integration tests require `TEST_POSTGRES_URL`; tests `t.Skip` when unset, which is fine in CI without that var).
- Bot: `cd apps/bot && npx vitest run <pattern>`.
- Frontend: `cd apps/frontend && npx vitest run <pattern>`.

---

## Task 1: Wire `appauth.Repo` into the HTTP handler

**Goal:** Pure plumbing — give the HTTP `Handler` access to `appauth.Repo` so the next task can call `FindOrCreateByDiscordID`. `NewRouter` already constructs `appAuthRepo` at `router.go:228` for the OAuth handler; we just move that construction up a few lines and pass it into `NewHandler`. No `main.go` changes needed.

**Files:**
- Modify: `apps/backend/internal/http/handlers.go`
- Modify: `apps/backend/internal/http/router.go`

- [ ] **Step 1: Add `appauth` field + constructor parameter to `Handler`**

Edit `apps/backend/internal/http/handlers.go`. Add this import to the existing import block (alphabetical order — it goes near the top of the `rustlens/backend/internal/*` group):

```go
"rustlens/backend/internal/appauth"
```

Add a new struct field at the bottom of `Handler`:

```go
type Handler struct {
    cfg                 config.Config
    postgres            *store.PostgresStore
    // … existing fields unchanged …
    players             *players.Service
    appauth             *appauth.Repo
}
```

Add the parameter to `NewHandler` (after `publisher events.Publisher`):

```go
func NewHandler(
    cfg config.Config,
    postgres *store.PostgresStore,
    redis *store.RedisStore,
    shardService *shards.Service,
    steamClient *steamapi.Client,
    steamSummaryClient *steamapi.Client,
    battleMetricsClient *battlemetrics.Client,
    bmMatch *bmmatch.Client,
    platformSync *platformsync.Client,
    outboundProvider *outboundhttp.Provider,
    proxyCrypto *proxycrypto.Crypto,
    publisher events.Publisher,
    appauthRepo *appauth.Repo,
) *Handler {
```

In the `return &Handler{...}` literal, add the field assignment as the last entry:

```go
    players:             players.NewService(postgres, steamClient, bmMatch),
    appauth:             appauthRepo,
}
```

- [ ] **Step 2: Move the `appAuthRepo` construction up and pass it to `NewHandler`**

Edit `apps/backend/internal/http/router.go`. Today, `appAuthRepo := appauth.NewRepo(postgres.Pool())` lives at `router.go:228` — after the `NewHandler` call at line 69. Move that line up so it's defined before `NewHandler`, then thread it through.

Concretely:

1. Delete the existing `appAuthRepo := appauth.NewRepo(postgres.Pool())` line at line ~228 (keep everything that uses it — `appauth.NewHandler(appAuthRepo)`, `discord.HandlerConfig{Repo: appAuthRepo, …}` — they will still resolve to the same variable, just defined earlier).
2. Add it immediately before the `NewHandler` call (around line 69):

```go
appAuthRepo := appauth.NewRepo(postgres.Pool())
handler := NewHandler(cfg, postgres, redis, shardService, steamClient, steamSummaryClient, battleMetricsClient, bmMatch, platformSync, outboundProvider, proxyCrypto, publisher, appAuthRepo)
```

No other changes — `NewRouter` keeps its existing public signature. The `appauth` import is already present in this file.

- [ ] **Step 3: Build the backend**

```bash
cd apps/backend && go build ./...
```

Expected: exit 0, no output.

- [ ] **Step 4: Run existing backend tests to confirm no regression**

```bash
cd apps/backend && go test ./internal/http/... -count=1
```

Expected: PASS or `SKIP` (Postgres-dependent tests skip when `TEST_POSTGRES_URL` is unset; that's fine).

- [ ] **Step 5: Commit**

```bash
git add apps/backend/internal/http/handlers.go apps/backend/internal/http/router.go
git commit -m "refactor(backend): thread appauth.Repo into http Handler"
```

---

## Task 2: Add `EnsureAppUser` HTTP handler (TDD)

**Goal:** A new internal endpoint `POST /api/internal/bot/setup/users/ensure` that takes a Discord profile and calls `FindOrCreateByDiscordID`, returning `{ userId, created }`.

**Files:**
- Create: `apps/backend/internal/http/bot_users_handlers.go`
- Create: `apps/backend/internal/http/bot_users_handlers_test.go`
- Modify: `apps/backend/internal/http/router.go`

- [ ] **Step 1: Write the failing handler test**

Create `apps/backend/internal/http/bot_users_handlers_test.go`:

```go
package httpapi

import (
    "bytes"
    "context"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "os"
    "testing"

    "rustlens/backend/internal/appauth"
    "rustlens/backend/internal/store"
)

type ensureAppUserResp struct {
    UserID  string `json:"userId"`
    Created bool   `json:"created"`
}

func newEnsureTestHandler(t *testing.T) *Handler {
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
    if _, err := s.Pool().Exec(ctx, `TRUNCATE app_users RESTART IDENTITY CASCADE`); err != nil {
        t.Fatalf("truncate: %v", err)
    }
    return &Handler{postgres: s, appauth: appauth.NewRepo(s.Pool())}
}

func TestEnsureAppUser_CreatesThenReturnsExisting(t *testing.T) {
    h := newEnsureTestHandler(t)

    body, _ := json.Marshal(map[string]any{
        "discordId":  "111222333444555666",
        "username":   "alice",
        "globalName": "Alice",
        "avatarHash": "deadbeef",
    })

    // First call: created=true.
    rr := httptest.NewRecorder()
    req := httptest.NewRequest("POST", "/api/internal/bot/setup/users/ensure", bytes.NewReader(body))
    req.Header.Set("Content-Type", "application/json")
    h.EnsureAppUser(rr, req)
    if rr.Code != 200 {
        t.Fatalf("first call: status=%d body=%s", rr.Code, rr.Body.String())
    }
    var resp ensureAppUserResp
    if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
        t.Fatalf("unmarshal: %v", err)
    }
    if !resp.Created || resp.UserID == "" {
        t.Errorf("first call: want created=true userId!=\"\", got %+v", resp)
    }
    firstID := resp.UserID

    // Second call with same discordId: created=false, same userId.
    rr2 := httptest.NewRecorder()
    req2 := httptest.NewRequest("POST", "/api/internal/bot/setup/users/ensure", bytes.NewReader(body))
    req2.Header.Set("Content-Type", "application/json")
    h.EnsureAppUser(rr2, req2)
    if rr2.Code != 200 {
        t.Fatalf("second call: status=%d body=%s", rr2.Code, rr2.Body.String())
    }
    var resp2 ensureAppUserResp
    if err := json.Unmarshal(rr2.Body.Bytes(), &resp2); err != nil {
        t.Fatalf("unmarshal: %v", err)
    }
    if resp2.Created {
        t.Errorf("second call: want created=false, got true")
    }
    if resp2.UserID != firstID {
        t.Errorf("second call: userId changed: first=%s second=%s", firstID, resp2.UserID)
    }
}

func TestEnsureAppUser_RejectsInvalidSnowflake(t *testing.T) {
    h := newEnsureTestHandler(t)

    body, _ := json.Marshal(map[string]any{
        "discordId":  "not-a-snowflake",
        "username":   "alice",
        "globalName": "",
        "avatarHash": "",
    })
    rr := httptest.NewRecorder()
    req := httptest.NewRequest("POST", "/api/internal/bot/setup/users/ensure", bytes.NewReader(body))
    req.Header.Set("Content-Type", "application/json")
    h.EnsureAppUser(rr, req)
    if rr.Code != http.StatusBadRequest {
        t.Errorf("want 400, got %d body=%s", rr.Code, rr.Body.String())
    }
}
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
cd apps/backend && go test ./internal/http/ -run 'TestEnsureAppUser_' -count=1 -v
```

Expected: FAIL with `h.EnsureAppUser undefined`. (If `TEST_POSTGRES_URL` is unset, the test will SKIP — set it locally to a throwaway test DB to actually exercise this. CI will exercise it.)

- [ ] **Step 3: Implement the handler**

Create `apps/backend/internal/http/bot_users_handlers.go`:

```go
package httpapi

import (
    "encoding/json"
    "log"
    "net/http"

    "rustlens/backend/internal/appauth"
)

type ensureAppUserRequest struct {
    DiscordID  string `json:"discordId"`
    Username   string `json:"username"`
    GlobalName string `json:"globalName"`
    AvatarHash string `json:"avatarHash"`
}

// EnsureAppUser: POST /api/internal/bot/setup/users/ensure
//
// Idempotent. Inserts an app_users row keyed on Discord ID if one does not
// exist, otherwise refreshes cached profile fields and returns the existing
// row. Called by the bot at the top of every interaction so that every
// Discord user who touches the platform has a stable RustLens user id.
func (h *Handler) EnsureAppUser(w http.ResponseWriter, r *http.Request) {
    r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
    var req ensureAppUserRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        writeError(w, http.StatusBadRequest, "invalid request body")
        return
    }
    if !validateSnowflakeValue(req.DiscordID) {
        writeError(w, http.StatusBadRequest, "discordId must be a valid Discord snowflake")
        return
    }
    if h.appauth == nil {
        writeError(w, http.StatusInternalServerError, "appauth repo not configured")
        return
    }
    userID, created, err := h.appauth.FindOrCreateByDiscordID(r.Context(), appauth.DiscordProfile{
        ID:         req.DiscordID,
        Username:   req.Username,
        GlobalName: req.GlobalName,
        AvatarHash: req.AvatarHash,
    })
    if err != nil {
        log.Printf("[backend] EnsureAppUser FindOrCreateByDiscordID error for %s: %v", req.DiscordID, err)
        writeError(w, http.StatusInternalServerError, "failed to ensure user")
        return
    }
    writeJSON(w, http.StatusOK, map[string]any{
        "userId":  userID,
        "created": created,
    })
}
```

- [ ] **Step 4: Run the test to verify it passes**

```bash
cd apps/backend && go test ./internal/http/ -run 'TestEnsureAppUser_' -count=1 -v
```

Expected: PASS (or SKIP if no `TEST_POSTGRES_URL`).

- [ ] **Step 5: Mount the route in `router.go`**

Edit `apps/backend/internal/http/router.go`. Inside the `r.Route("/api/internal/bot/setup", func(r chi.Router) { … })` block, near the existing `r.Post("/bot-user-installs/seen", handler.TouchBotUserInstallSeen)` line (around line 184), add:

```go
r.Post("/users/ensure", handler.EnsureAppUser)
```

- [ ] **Step 6: Re-run all http package tests + build**

```bash
cd apps/backend && go test ./internal/http/... -count=1 && go build ./...
```

Expected: all tests PASS or SKIP, build succeeds.

- [ ] **Step 7: Commit**

```bash
git add apps/backend/internal/http/bot_users_handlers.go apps/backend/internal/http/bot_users_handlers_test.go apps/backend/internal/http/router.go
git commit -m "feat(backend): add POST /api/internal/bot/setup/users/ensure"
```

---

## Task 3: Add `ListAdminMembers` HTTP handler (TDD)

**Goal:** A new admin endpoint `GET /api/app/admin/members` that returns the same `AdminUsersPage` shape but filtered to users with zero admin role grants.

**Files:**
- Modify: `apps/backend/internal/http/admin_users_query.go`
- Modify: `apps/backend/internal/http/admin_read_handlers.go`
- Create: `apps/backend/internal/http/admin_members_test.go`
- Modify: `apps/backend/internal/http/router.go`

- [ ] **Step 1: Write the failing handler test**

Create `apps/backend/internal/http/admin_members_test.go`:

```go
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

func (ts *testHandlerServer) getMembers(t *testing.T, path string) listAdminUsersResp {
    t.Helper()
    rr := httptest.NewRecorder()
    req := httptest.NewRequest("GET", path, nil)
    ts.h.ListAdminMembers(rr, req)
    if rr.Code != 200 {
        t.Fatalf("GET %s: status=%d body=%s", path, rr.Code, rr.Body.String())
    }
    var resp listAdminUsersResp
    if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
        t.Fatalf("unmarshal: %v; body=%s", err, rr.Body.String())
    }
    return resp
}

func TestListAdminMembers_ExcludesAdmins(t *testing.T) {
    ts := newTestHandlerServer(t)

    seedUser(t, ts.pool, "usr_admin1", "Andy Admin", seedOpts{Discord: "andy"})
    seedUser(t, ts.pool, "usr_mem1", "Mona Member", seedOpts{Discord: "mona"})
    seedUser(t, ts.pool, "usr_mem2", "Mike Member", seedOpts{Discord: "mike"})

    // Fixed role key; TRUNCATE in newTestHandlerServer doesn't touch admin_roles,
    // but `ON CONFLICT (key) DO NOTHING` in grantAdminRole keeps this idempotent
    // across re-runs.
    grantAdminRole(t, ts.pool, "usr_admin1", "members_test_admin")

    resp := ts.getMembers(t, "/api/app/admin/members")
    if resp.Total != 2 {
        t.Errorf("members total: want 2, got %d", resp.Total)
    }
    for _, item := range resp.Items {
        if item.User.ID == "usr_admin1" {
            t.Errorf("members listing leaked admin user: %s", item.User.ID)
        }
    }
}

func TestListAdminMembers_SearchAndPagination(t *testing.T) {
    ts := newTestHandlerServer(t)

    seedUser(t, ts.pool, "usr_a", "Alice", seedOpts{Discord: "alice"})
    seedUser(t, ts.pool, "usr_b", "Bob", seedOpts{Discord: "bob"})
    seedUser(t, ts.pool, "usr_c", "Carol", seedOpts{Discord: "carol"})

    if r := ts.getMembers(t, "/api/app/admin/members?q=alice"); r.Total != 1 {
        t.Errorf("q=alice: want 1 got %d", r.Total)
    }

    r := ts.getMembers(t, "/api/app/admin/members?pageSize=2&page=1")
    if len(r.Items) != 2 || r.Total != 3 {
        t.Errorf("page1: want len=2 total=3, got len=%d total=%d", len(r.Items), r.Total)
    }
}
```

Note: `grantAdminRole` is idempotent via `ON CONFLICT DO NOTHING` on both `admin_roles(key)` and `admin_role_permissions(role_id, permission_key)`, so a fixed role key like `"members_test_admin"` is safe across re-runs even though `newTestHandlerServer` does not truncate `admin_roles`.

- [ ] **Step 2: Run the test to verify it fails**

```bash
cd apps/backend && go test ./internal/http/ -run 'TestListAdminMembers_' -count=1 -v
```

Expected: FAIL with `ts.h.ListAdminMembers undefined`.

- [ ] **Step 3: Add the members-only WHERE clause helper to `admin_users_query.go`**

Edit `apps/backend/internal/http/admin_users_query.go`. Add this exported helper at the bottom of the file:

```go
// membersOnlyClause is an extra SQL fragment that, when appended via AND to
// the WHERE clause built by buildUsersQuery, restricts results to users who
// hold no admin role (i.e. no role that has any row in admin_role_permissions).
// It takes no positional arguments — safe to concatenate without shifting
// argument indices.
const membersOnlyClause = `NOT EXISTS (
    SELECT 1
    FROM admin_user_roles aur
    JOIN admin_role_permissions arp ON arp.role_id = aur.role_id
    WHERE aur.user_id = u.id
)`
```

- [ ] **Step 4: Implement `ListAdminMembers` in `admin_read_handlers.go`**

Edit `apps/backend/internal/http/admin_read_handlers.go`. Add the new handler immediately after the closing brace of `ListAdminUsers` (around line 245):

```go
// ListAdminMembers: GET /api/app/admin/members
//
// Inverse of ListAdminUsers — returns app_users rows that hold zero admin
// roles (i.e. no role with any permission grant). Query params mirror
// ListAdminUsers exactly except that the `role` param is ignored: members
// have no admin roles by definition, so role-key filtering is meaningless.
func (h *Handler) ListAdminMembers(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()
    pool := h.postgres.Pool()
    q := r.URL.Query()

    page := 1
    pageSize := 50
    if v := q.Get("page"); v != "" {
        parseLimit(v, &page)
        if page < 1 {
            page = 1
        }
    }
    if v := q.Get("pageSize"); v != "" {
        parseLimit(v, &pageSize)
    }
    if pageSize < 1 || pageSize > 200 {
        pageSize = 50
    }
    offset := (page - 1) * pageSize

    whereSQL, args, orderBy, err := buildUsersQuery(q)
    if err != nil {
        writeError(w, http.StatusBadRequest, err.Error())
        return
    }
    // Append the members-only restriction.
    if whereSQL == "" {
        whereSQL = "WHERE " + membersOnlyClause
    } else {
        whereSQL = whereSQL + " AND " + membersOnlyClause
    }

    var total int
    countSQL := fmt.Sprintf(`SELECT COUNT(*) FROM app_users u %s`, whereSQL)
    if err := pool.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
        writeError(w, http.StatusInternalServerError, err.Error())
        return
    }

    args = append(args, pageSize, offset)
    listSQL := fmt.Sprintf(`
        SELECT u.id,
               COALESCE(
                   NULLIF(u.display_name, ''),
                   NULLIF(u.discord_global_name, ''),
                   NULLIF(u.discord_username, ''),
                   ''
               ) AS display_name,
               COALESCE(
                   NULLIF(u.avatar_url, ''),
                   CASE
                       WHEN NULLIF(u.discord_avatar, '') IS NOT NULL
                            AND NULLIF(u.discord_id, '') IS NOT NULL
                       THEN 'https://cdn.discordapp.com/avatars/'
                            || u.discord_id || '/' || u.discord_avatar || '.png'
                       ELSE ''
                   END
               ) AS avatar_url,
               u.roles::text,
               u.created_at, u.updated_at, u.last_login_at,
               u.suspended_at, u.deleted_at,
               COALESCE(u.discord_id, ''),
               COALESCE(u.discord_username, ''),
               COALESCE(u.discord_global_name, ''),
               COALESCE(u.discord_avatar, ''),
               COALESCE((SELECT COUNT(*) FROM user_guilds g WHERE g.discord_id = u.discord_id AND g.left_at IS NULL), 0),
               0 AS steam_count,
               ARRAY[]::text[] AS admin_role_keys
        FROM app_users u
        %s
        ORDER BY %s
        LIMIT $%d OFFSET $%d
    `, whereSQL, orderBy, len(args)-1, len(args))

    rows, err := pool.Query(ctx, listSQL, args...)
    if err != nil {
        writeError(w, http.StatusInternalServerError, err.Error())
        return
    }
    defer rows.Close()

    items := []adminUserItem{}
    for rows.Next() {
        var it adminUserItem
        var rolesLegacyJSON string
        var suspendedAt, deletedAt *time.Time
        var guildCount, steamCount int64
        var adminRoleKeys []string
        if err := rows.Scan(
            &it.User.ID, &it.User.DisplayName, &it.User.AvatarURL, &rolesLegacyJSON,
            &it.User.CreatedAt, &it.User.UpdatedAt, &it.User.LastLoginAt,
            &suspendedAt, &deletedAt,
            &it.DiscordID, &it.DiscordUsername, &it.DiscordGlobalName, &it.DiscordAvatar,
            &guildCount, &steamCount, &adminRoleKeys,
        ); err != nil {
            writeError(w, http.StatusInternalServerError, err.Error())
            return
        }
        if rolesLegacyJSON != "" {
            _ = json.Unmarshal([]byte(rolesLegacyJSON), &it.User.Roles)
        }
        if it.User.Roles == nil {
            it.User.Roles = []string{}
        }
        it.GuildCount = int(guildCount)
        it.SteamConnectionCount = int(steamCount)
        it.AdminRoles = adminRoleKeys
        if it.AdminRoles == nil {
            it.AdminRoles = []string{}
        }
        if suspendedAt != nil {
            t := suspendedAt.Format(time.RFC3339)
            it.SuspendedAt = &t
        }
        if deletedAt != nil {
            t := deletedAt.Format(time.RFC3339)
            it.DeletedAt = &t
        }
        items = append(items, it)
    }
    writeJSON(w, http.StatusOK, map[string]any{
        "items": items, "total": total, "page": page, "pageSize": pageSize,
    })
}
```

The SELECT mirrors `ListAdminUsers` but skips the `LEFT JOIN admin_user_roles` since members have none — we hardcode an empty array for `admin_role_keys`. This avoids the GROUP BY entirely.

- [ ] **Step 5: Run the new tests**

```bash
cd apps/backend && go test ./internal/http/ -run 'TestListAdminMembers_' -count=1 -v
```

Expected: PASS (or SKIP without `TEST_POSTGRES_URL`).

- [ ] **Step 6: Mount the admin route in `router.go`**

Edit `apps/backend/internal/http/router.go`. Find the line `r.Get("/users", handler.ListAdminUsers)` (around line 309) and add directly below it:

```go
r.Get("/members", handler.ListAdminMembers)
```

- [ ] **Step 7: Re-run all http package tests + build**

```bash
cd apps/backend && go test ./internal/http/... -count=1 && go build ./...
```

Expected: all PASS or SKIP, build succeeds.

- [ ] **Step 8: Commit**

```bash
git add apps/backend/internal/http/admin_users_query.go apps/backend/internal/http/admin_read_handlers.go apps/backend/internal/http/admin_members_test.go apps/backend/internal/http/router.go
git commit -m "feat(backend): add GET /api/app/admin/members for non-admin users"
```

---

## Task 4: Add `ensureAppUser` to the bot's `BackendClient` (TDD)

**Goal:** Typed method on the bot's `BackendClient` for the new endpoint.

**Files:**
- Modify: `apps/bot/src/backend-client.ts`
- Modify: `apps/bot/src/backend-client.test.ts`

- [ ] **Step 1: Write the failing test**

Edit `apps/bot/src/backend-client.test.ts`. Add this test inside the existing `describe("BackendClient", () => { … })` block (just after the `touchBotUserInstallSeen` test if one exists, otherwise at the end):

```ts
it("ensures an app_users row exists for a discord user", async () => {
  const mockResponse = {
    ok: true,
    json: async () => ({ userId: "usr_abcdef0123456789abcdef01", created: true }),
  } as Response;

  const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(mockResponse);

  const client = new BackendClient(
    "http://localhost:8080",
    "secret",
    "rustlens-bot",
    "rustlens-backend",
  );
  const result = await client.ensureAppUser("worker-a", {
    discordId:  "111222333444555666",
    username:   "alice",
    globalName: "Alice",
    avatarHash: "deadbeef",
  });

  expect(result).toEqual({ userId: "usr_abcdef0123456789abcdef01", created: true });
  expect(fetchSpy).toHaveBeenCalledOnce();
  const [url, init] = fetchSpy.mock.calls[0];
  expect(url).toBe("http://localhost:8080/api/internal/bot/setup/users/ensure");
  expect(init?.method).toBe("POST");
  expect(JSON.parse(init?.body as string)).toEqual({
    discordId:  "111222333444555666",
    username:   "alice",
    globalName: "Alice",
    avatarHash: "deadbeef",
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
cd apps/bot && npx vitest run backend-client.test.ts -t "ensures an app_users row"
```

Expected: FAIL with `client.ensureAppUser is not a function`.

- [ ] **Step 3: Add the method to `BackendClient`**

Edit `apps/bot/src/backend-client.ts`. Find the `touchBotUserInstallSeen` method (around line 1056) and add this method directly after it:

```ts
ensureAppUser(
  workerId: string,
  payload: {
    discordId:  string;
    username:   string;
    globalName: string | null;
    avatarHash: string | null;
  },
): Promise<{ userId: string; created: boolean }> {
  return this.call<{ userId: string; created: boolean }>(
    workerId,
    `/api/internal/bot/setup/users/ensure`,
    "POST",
    {
      discordId:  payload.discordId,
      username:   payload.username,
      globalName: payload.globalName,
      avatarHash: payload.avatarHash,
    },
  );
}
```

- [ ] **Step 4: Run the test to verify it passes**

```bash
cd apps/bot && npx vitest run backend-client.test.ts -t "ensures an app_users row"
```

Expected: PASS.

- [ ] **Step 5: Run the full bot test suite + lint to confirm no regression**

```bash
cd apps/bot && npm test && npm run lint
```

Expected: all tests PASS, no TS errors.

- [ ] **Step 6: Commit**

```bash
git add apps/bot/src/backend-client.ts apps/bot/src/backend-client.test.ts
git commit -m "feat(bot): add ensureAppUser to BackendClient"
```

---

## Task 5: Add `ensureAppUser` helper module with dedupe (TDD)

**Goal:** A fire-and-forget helper that the shard worker can call once per `interactionCreate`. Wraps a TTL cache so autocomplete keystroke storms don't hammer the backend.

**Files:**
- Create: `apps/bot/src/ensure-app-user.ts`
- Create: `apps/bot/src/ensure-app-user.test.ts`

- [ ] **Step 1: Write the failing test**

Create `apps/bot/src/ensure-app-user.test.ts`:

```ts
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import {
  __resetEnsureAppUserCacheForTests,
  ensureAppUser,
  __setBackendForTests,
} from "./ensure-app-user.js";

type FakeInteraction = {
  user: { id: string; username: string; globalName: string | null; avatar: string | null };
};

function makeInteraction(id = "111222333444555666"): FakeInteraction {
  return {
    user: {
      id,
      username:   "alice",
      globalName: "Alice",
      avatar:     "deadbeef",
    },
  };
}

describe("ensureAppUser", () => {
  let backendSpy: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    backendSpy = vi.fn().mockResolvedValue({ userId: "usr_x", created: true });
    __setBackendForTests({
      // Only the method we exercise needs a real impl.
      ensureAppUser: backendSpy,
    } as unknown as Parameters<typeof __setBackendForTests>[0]);
    __resetEnsureAppUserCacheForTests();
  });

  afterEach(() => {
    __setBackendForTests(null);
  });

  it("posts to the backend on first call for a user", async () => {
    ensureAppUser(makeInteraction() as never);
    // Allow microtask queue to flush so the .catch chain runs.
    await Promise.resolve();
    expect(backendSpy).toHaveBeenCalledOnce();
    const [, payload] = backendSpy.mock.calls[0];
    expect(payload).toEqual({
      discordId:  "111222333444555666",
      username:   "alice",
      globalName: "Alice",
      avatarHash: "deadbeef",
    });
  });

  it("dedupes a second call within the TTL window", async () => {
    ensureAppUser(makeInteraction() as never);
    ensureAppUser(makeInteraction() as never);
    await Promise.resolve();
    expect(backendSpy).toHaveBeenCalledOnce();
  });

  it("fires again for a different user", async () => {
    ensureAppUser(makeInteraction("111") as never);
    ensureAppUser(makeInteraction("222") as never);
    await Promise.resolve();
    expect(backendSpy).toHaveBeenCalledTimes(2);
  });

  it("swallows backend errors without throwing", async () => {
    backendSpy.mockRejectedValueOnce(new Error("boom"));
    // Must not throw.
    expect(() => ensureAppUser(makeInteraction() as never)).not.toThrow();
    await Promise.resolve();
    await Promise.resolve();
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
cd apps/bot && npx vitest run ensure-app-user.test.ts
```

Expected: FAIL with `Cannot find module './ensure-app-user.js'` or similar.

- [ ] **Step 3: Implement the helper module**

Create `apps/bot/src/ensure-app-user.ts`:

```ts
import type { Interaction } from "discord.js";

import { BackendClient } from "./backend-client.js";
import { loadConfig } from "./config.js";

const shardID = process.env.SHARD_ID ?? "0";

let _backend: BackendClient | undefined;
let _workerID: string | undefined;
// Test injection point. When non-null, used instead of the real lazy-init path.
let _backendOverride: BackendClient | null = null;

function getBackend(): BackendClient {
  if (_backendOverride) return _backendOverride;
  if (!_backend) {
    const config = loadConfig();
    _backend = new BackendClient(
      config.backendUrl,
      config.jwtSecret,
      config.jwtIssuer,
      config.jwtAudience,
    );
    _workerID = `${config.workerId}-ensure-app-user-${shardID}`;
  }
  return _backend;
}

function getWorkerID(): string {
  if (_backendOverride) return "test-worker";
  getBackend();
  return _workerID!;
}

const ensureAppUserCache = new Map<string, number>();
const ENSURE_APP_USER_TTL_MS = 5 * 60_000;

/**
 * Fire-and-forget: on the first call for a given Discord user (within the
 * 5-minute TTL), POST to the backend so an app_users row exists for them.
 * Subsequent calls within the window are no-ops. Errors are logged but never
 * thrown — this is on the interaction hot path.
 */
export function ensureAppUser(interaction: Interaction): void {
  const u = interaction.user;
  if (!u) return;

  const now = Date.now();
  const last = ensureAppUserCache.get(u.id);
  if (last !== undefined && now - last < ENSURE_APP_USER_TTL_MS) return;
  ensureAppUserCache.set(u.id, now);

  getBackend()
    .ensureAppUser(getWorkerID(), {
      discordId:  u.id,
      username:   u.username,
      globalName: u.globalName ?? null,
      avatarHash: u.avatar ?? null,
    })
    .catch((error) => {
      console.error(JSON.stringify({
        level:   "warn",
        message: "[bot] ensureAppUser failed",
        userId:  u.id,
        error:   String(error),
      }));
    });
}

// --- Test-only helpers ---

export function __resetEnsureAppUserCacheForTests(): void {
  ensureAppUserCache.clear();
}

export function __setBackendForTests(backend: BackendClient | null): void {
  _backendOverride = backend;
}
```

- [ ] **Step 4: Run the test to verify it passes**

```bash
cd apps/bot && npx vitest run ensure-app-user.test.ts
```

Expected: all four cases PASS.

- [ ] **Step 5: Run the full bot test suite + lint**

```bash
cd apps/bot && npm test && npm run lint
```

Expected: all PASS, no TS errors.

- [ ] **Step 6: Commit**

```bash
git add apps/bot/src/ensure-app-user.ts apps/bot/src/ensure-app-user.test.ts
git commit -m "feat(bot): add ensure-app-user helper with TTL dedupe"
```

---

## Task 6: Wire `ensureAppUser` into the `interactionCreate` listener

**Goal:** One-line plug-in at the top of the existing listener.

**Files:**
- Modify: `apps/bot/src/shard-worker.ts`

- [ ] **Step 1: Add the import and call**

Edit `apps/bot/src/shard-worker.ts`. Add to the import block near the top:

```ts
import { ensureAppUser } from "./ensure-app-user.js";
```

Find the `client.on("interactionCreate", async (interaction) => {` line (around line 333). Add `ensureAppUser(interaction);` immediately after the existing `reportUserInstallSeen(interaction);` call:

```ts
client.on("interactionCreate", async (interaction) => {
  reportUserInstallSeen(interaction);
  ensureAppUser(interaction);
  try {
    // … existing dispatch unchanged
```

- [ ] **Step 2: Type-check + run bot tests**

```bash
cd apps/bot && npm run lint && npm test
```

Expected: no TS errors, all tests PASS.

- [ ] **Step 3: Commit**

```bash
git add apps/bot/src/shard-worker.ts
git commit -m "feat(bot): ensure app_users row on every interaction"
```

---

## Task 7: Add `getAdminMembers` to the frontend API client

**Goal:** Typed client wrapper for `GET /api/app/admin/members`. Reuses existing `AdminUsersQuery` / `AdminUsersPage` types.

**Files:**
- Modify: `apps/frontend/lib/rustlens-api.ts`

- [ ] **Step 1: Add the function**

Edit `apps/frontend/lib/rustlens-api.ts`. Find `getAdminUsers` (around line 390) and add this function immediately after its closing brace:

```ts
export async function getAdminMembers(params: AdminUsersQuery = {}): Promise<AdminUsersPage> {
    const sp = new URLSearchParams();
    if (params.q) sp.set("q", params.q);
    if (params.status) sp.set("status", params.status);
    if (params.discord) sp.set("discord", params.discord);
    if (params.steam) sp.set("steam", params.steam);
    if (params.sort) sp.set("sort", params.sort);
    if (params.page) sp.set("page", String(params.page));
    if (params.pageSize) sp.set("pageSize", String(params.pageSize));
    // Backend ignores `role` for /admin/members — members have no admin roles
    // by definition. Drop it from the querystring entirely.
    const qs = sp.toString();
    return backendFetch<AdminUsersPage>(
        `/api/app/admin/members${qs ? `?${qs}` : ""}`,
        undefined,
        { requireSession: true },
    );
}
```

- [ ] **Step 2: Type-check**

```bash
cd apps/frontend && npm run lint
```

Expected: no TS errors.

- [ ] **Step 3: Commit**

```bash
git add apps/frontend/lib/rustlens-api.ts
git commit -m "feat(frontend): add getAdminMembers API client"
```

---

## Task 8: Build the `MembersTable` client component (TDD)

**Goal:** Read-only table mirroring `user-installs-table.tsx` with the minimal column set (avatar, name + @username, Discord ID click-to-copy, joined).

**Files:**
- Create: `apps/frontend/app/admin/members/_components/members-table.tsx`
- Create: `apps/frontend/app/admin/members/_components/__tests__/members-table.test.tsx`

- [ ] **Step 1: Write the failing component test**

Create `apps/frontend/app/admin/members/_components/__tests__/members-table.test.tsx`:

```tsx
import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";

import { MembersTable } from "../members-table";

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn() }),
  useSearchParams: () => new URLSearchParams(),
}));

function fixturePage(items: number) {
  return {
    items: Array.from({ length: items }, (_, i) => ({
      user: {
        id:                `usr_${i}`,
        discordId:         `100000000000000${i}`,
        discordUsername:   `user${i}`,
        discordGlobalName: `User ${i}`,
        discordAvatar:     "",
        displayName:       `User ${i}`,
        avatarUrl:         "",
        roles:             ["user"],
        createdAt:         "2026-05-01T00:00:00Z",
        updatedAt:         "2026-05-01T00:00:00Z",
        lastLoginAt:       "2026-05-01T00:00:00Z",
      },
      discordId:           `100000000000000${i}`,
      discordUsername:     `user${i}`,
      discordGlobalName:   `User ${i}`,
      discordAvatar:       "",
      guildCount:          0,
      steamConnectionCount: 0,
      adminRoles:          [],
    })),
    total:    items,
    page:     1,
    pageSize: 20,
  };
}

describe("MembersTable", () => {
  it("renders one row per member with name and discord id", () => {
    render(
      <MembersTable
        initialQuery=""
        pageSizeOptions={[10, 20, 30, 40, 50]}
        data={fixturePage(2)}
      />,
    );

    expect(screen.getByText("User 0")).toBeInTheDocument();
    expect(screen.getByText("@user0")).toBeInTheDocument();
    expect(screen.getByText("1000000000000000")).toBeInTheDocument();
    expect(screen.getByText("User 1")).toBeInTheDocument();
  });

  it("renders an empty-state row when there are no members", () => {
    render(
      <MembersTable
        initialQuery=""
        pageSizeOptions={[10, 20, 30, 40, 50]}
        data={fixturePage(0)}
      />,
    );

    expect(screen.getByText("No members found.")).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
cd apps/frontend && npx vitest run app/admin/members
```

Expected: FAIL with module-not-found.

- [ ] **Step 3: Implement `MembersTable`**

Create `apps/frontend/app/admin/members/_components/members-table.tsx`:

```tsx
"use client";

import * as React from "react";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import {
  ChevronLeftIcon,
  ChevronRightIcon,
  ChevronsLeftIcon,
  ChevronsRightIcon,
  FileIcon,
} from "lucide-react";

import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
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
import { userAvatarUrl } from "@/lib/discord-cdn";
import type { AdminUserItem, AdminUsersPage } from "@/lib/rustlens-api";

type MembersTableProps = {
  initialQuery: string;
  pageSizeOptions: number[];
  data: AdminUsersPage;
};

const SEARCH_DEBOUNCE_MS = 300;

function formatRelative(iso: string | null | undefined): string {
  if (!iso) return "—";
  const ts = new Date(iso).getTime();
  if (!Number.isFinite(ts)) return "—";
  const diff = Date.now() - ts;
  const minutes = Math.floor(diff / 60_000);
  if (minutes < 1) return "just now";
  if (minutes < 60) return `${minutes} min ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.floor(hours / 24);
  if (days < 1) return "today";
  if (days === 1) return "1 day ago";
  if (days < 30) return `${days} days ago`;
  const months = Math.floor(days / 30);
  if (months === 1) return "1 month ago";
  if (months < 12) return `${months} months ago`;
  const years = Math.floor(days / 365);
  return years === 1 ? "1 year ago" : `${years} years ago`;
}

function memberLabel(u: AdminUserItem): string {
  return u.discordGlobalName || u.discordUsername || u.user.displayName || "—";
}

function memberInitial(u: AdminUserItem): string {
  const label = memberLabel(u);
  return label === "—" ? "?" : label.charAt(0).toUpperCase();
}

export function MembersTable({
  initialQuery,
  pageSizeOptions,
  data,
}: MembersTableProps) {
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
          placeholder="Search by username, global name, or Discord ID…"
          value={searchValue}
          onChange={(e) => setSearchValue(e.target.value)}
          className="max-w-md"
          aria-label="Search members"
        />
      </div>

      <div className="overflow-hidden rounded-lg border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="w-12"></TableHead>
              <TableHead>User</TableHead>
              <TableHead>Discord ID</TableHead>
              <TableHead>Joined</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {data.items.length === 0 ? (
              <TableRow>
                <TableCell colSpan={4} className="h-24 text-center">
                  No members found.
                </TableCell>
              </TableRow>
            ) : (
              data.items.map((u) => {
                const avatarUrl = userAvatarUrl(u.discordId, u.discordAvatar, 64);
                return (
                  <TableRow
                    key={u.user.id}
                    className="cursor-pointer hover:bg-muted/50"
                  >
                    <TableCell>
                      <Link href={`/admin/users/${u.user.id}`} className="block">
                        <Avatar className="size-8">
                          {avatarUrl && <AvatarImage src={avatarUrl} alt={memberLabel(u)} />}
                          <AvatarFallback>{memberInitial(u)}</AvatarFallback>
                        </Avatar>
                      </Link>
                    </TableCell>
                    <TableCell className="font-medium">
                      <Link href={`/admin/users/${u.user.id}`} className="block">
                        <div className="flex flex-col">
                          <span>{memberLabel(u)}</span>
                          {u.discordUsername ? (
                            <span className="text-xs text-muted-foreground">
                              @{u.discordUsername}
                            </span>
                          ) : null}
                        </div>
                      </Link>
                    </TableCell>
                    <TableCell>
                      <button
                        type="button"
                        onClick={(e) => {
                          e.stopPropagation();
                          navigator.clipboard?.writeText(u.discordId);
                        }}
                        className="font-mono text-xs text-muted-foreground hover:text-foreground"
                        title="Click to copy"
                      >
                        {u.discordId}
                      </button>
                    </TableCell>
                    <TableCell
                      className="text-sm text-muted-foreground"
                      title={u.user.createdAt}
                    >
                      <Link href={`/admin/users/${u.user.id}`} className="block">
                        {formatRelative(u.user.createdAt)}
                      </Link>
                    </TableCell>
                  </TableRow>
                );
              })
            )}
          </TableBody>
        </Table>
      </div>

      <div className="flex items-center gap-4">
        <div className="flex flex-1 items-center">
          <Select
            value={String(data.pageSize)}
            onValueChange={(value) => setPageSize(Number(value))}
          >
            <SelectTrigger
              size="sm"
              className="w-20"
              id="rows-per-page"
              aria-label="Rows per page"
            >
              <FileIcon className="size-4 text-muted-foreground" />
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
        <div className="flex items-center gap-2">
          <Button
            variant="outline"
            className="hidden size-8 lg:flex"
            size="icon"
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
          <div className="min-w-[7rem] text-center text-sm font-medium">
            Page {data.page} of {totalPages}
          </div>
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
        <div className="flex flex-1 justify-end text-sm text-muted-foreground">
          {data.total} {data.total === 1 ? "member" : "members"}
        </div>
      </div>
    </div>
  );
}
```

- [ ] **Step 4: Run the test to verify it passes**

```bash
cd apps/frontend && npx vitest run app/admin/members
```

Expected: both test cases PASS.

- [ ] **Step 5: Type-check the frontend**

```bash
cd apps/frontend && npm run lint
```

Expected: no TS errors.

- [ ] **Step 6: Commit**

```bash
git add apps/frontend/app/admin/members/_components/members-table.tsx apps/frontend/app/admin/members/_components/__tests__/members-table.test.tsx
git commit -m "feat(frontend): add MembersTable component"
```

---

## Task 9: Add the `/admin/members` route

**Goal:** Server-component page that fetches the first page of members and renders `MembersTable` inside the standard admin shell.

**Files:**
- Create: `apps/frontend/app/admin/members/page.tsx`

- [ ] **Step 1: Implement the page**

Create `apps/frontend/app/admin/members/page.tsx`:

```tsx
import { AppSidebar } from "../_components/app-sidebar";
import { MembersTable } from "./_components/members-table";
import { SiteHeader } from "@/components/site-header";
import { SidebarInset, SidebarProvider } from "@/components/ui/sidebar";
import { getCsrfToken } from "@/lib/csrf";
import { getAdminMembers, getCurrentUserSafe } from "@/lib/rustlens-api";
import { requirePermission } from "@/lib/session";

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

export default async function AdminMembersPage({
  searchParams,
}: {
  searchParams: Promise<Record<string, string | string[] | undefined>>;
}) {
  const session = await requirePermission("users.read");
  const sp = await searchParams;

  const qRaw = Array.isArray(sp.q) ? sp.q[0] : sp.q;
  const q = qRaw ?? "";
  const page = parsePositiveInt(sp.page, 1);
  const pageSize = clampPageSize(parsePositiveInt(sp.pageSize, DEFAULT_PAGE_SIZE));

  const [csrfToken, currentUser, members] = await Promise.all([
    getCsrfToken(),
    getCurrentUserSafe(),
    getAdminMembers({ q: q || undefined, page, pageSize }),
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
      <AppSidebar
        csrfToken={csrfToken}
        user={user}
        permissions={session.permissions ?? []}
        variant="inset"
      />
      <SidebarInset>
        <SiteHeader />
        <div className="flex flex-1 flex-col">
          <div className="@container/main flex flex-1 flex-col gap-2">
            <div className="flex flex-col gap-4 py-4 md:gap-6 md:py-6 px-4 lg:px-6">
              <div>
                <h1 className="text-2xl font-semibold">Members</h1>
                <p className="text-sm text-muted-foreground">
                  Discord users who have used RustLens.
                </p>
              </div>
              <MembersTable
                initialQuery={q}
                pageSizeOptions={PAGE_SIZE_OPTIONS}
                data={members}
              />
            </div>
          </div>
        </div>
      </SidebarInset>
    </SidebarProvider>
  );
}
```

- [ ] **Step 2: Type-check**

```bash
cd apps/frontend && npm run lint
```

Expected: no TS errors.

- [ ] **Step 3: Run frontend tests (sanity)**

```bash
cd apps/frontend && npm test
```

Expected: all PASS.

- [ ] **Step 4: Commit**

```bash
git add apps/frontend/app/admin/members/page.tsx
git commit -m "feat(frontend): add /admin/members route"
```

---

## Task 10: Add the `Members` sidebar nav item

**Goal:** New entry in the Discord sidebar group under the existing `User Apps` item.

**Files:**
- Modify: `apps/frontend/components/dashboard-app-sidebar.tsx`

- [ ] **Step 1: Add the `UserIcon` import**

Edit `apps/frontend/components/dashboard-app-sidebar.tsx`. Find the existing lucide-react import block (around lines 25–39). Add `UserIcon` to it (keep the list alphabetical):

```tsx
import {
    ActivityIcon,
    CameraIcon,
    CircleHelpIcon,
    FileTextIcon,
    KeyIcon,
    KeyRoundIcon,
    LayoutDashboardIcon,
    NetworkIcon,
    SearchIcon,
    ServerIcon,
    Settings2Icon,
    UserIcon,
    UsersIcon,
    UsersRoundIcon,
} from "lucide-react";
```

- [ ] **Step 2: Add the nav item to the Discord group**

In the same file, locate the existing Discord `NavGroup` block (around lines 176–196). Inside its `items={[...]}` array, after the `User Apps` spread block, add a third spread:

```tsx
<NavGroup
    label="Discord"
    items={[
        {
            title: "Guilds",
            path: "/admin/guilds",
            icon: UsersRoundIcon,
            isActive: pathname === "/admin/guilds" || pathname.startsWith("/admin/guilds/"),
        },
        ...(permissions.includes("users.read")
            ? [{
                    title: "User Apps",
                    path: "/admin/user-installs",
                    icon: UsersIcon,
                    isActive:
                        pathname === "/admin/user-installs" ||
                        pathname.startsWith("/admin/user-installs/"),
                }]
            : []),
        ...(permissions.includes("users.read")
            ? [{
                    title: "Members",
                    path: "/admin/members",
                    icon: UserIcon,
                    isActive:
                        pathname === "/admin/members" ||
                        pathname.startsWith("/admin/members/"),
                }]
            : []),
    ]}
/>
```

- [ ] **Step 3: Type-check + run frontend tests**

```bash
cd apps/frontend && npm run lint && npm test
```

Expected: no TS errors, all PASS.

- [ ] **Step 4: Smoke-test the page locally (manual)**

Run the dev server (if a fresh window is available — otherwise note in the commit body that the maintainer should verify):

```bash
cd apps/frontend && npm run dev
```

Open `http://localhost:3000/admin/members`, log in as an admin with `users.read`, and confirm:
- The Discord sidebar group shows `Guilds`, `User Apps`, `Members`.
- The Members page loads, shows the table headers (User / Discord ID / Joined), and either rows or "No members found."
- Clicking a row navigates to `/admin/users/<userId>` and that page renders (it's the existing user detail page).

If anything renders broken, note the failure in the commit body rather than fixing inline — the next iteration will tighten visuals.

- [ ] **Step 5: Commit**

```bash
git add apps/frontend/components/dashboard-app-sidebar.tsx
git commit -m "feat(frontend): add Members sidebar entry under Discord group"
```

---

## Post-implementation verification

After all tasks land, run one final sanity sweep:

- [ ] **Backend:** `cd apps/backend && go build ./... && go test ./... -count=1`
- [ ] **Bot:** `cd apps/bot && npm run lint && npm test`
- [ ] **Frontend:** `cd apps/frontend && npm run lint && npm test`
- [ ] **Manual:** invoke any slash command from a Discord account that has never logged into RustLens. Confirm a new `app_users` row appears (query: `SELECT id, discord_id, created_at FROM app_users WHERE discord_id = '<that-discord-id>'`). Reload `/admin/members` and confirm the new user appears there.
