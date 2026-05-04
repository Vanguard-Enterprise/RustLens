# Admin RBAC management — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an `/admin/roles` page that lets superadmins create, rename, delete, and edit the permission set of custom admin roles. System roles stay locked.

**Architecture:** Mutations route through the existing admin action dispatcher (`POST /api/app/admin/actions/{type}`) with four new actions. One new read endpoint (`GET /api/app/admin/permissions`) and one additive change to the existing `GET /api/app/admin/roles` (adds `memberCount`). Frontend is a Next.js 16 server component that calls `requirePermission("admins.grant")`, with a client-side table + Sheet drawer for create/edit and a confirmation dialog for delete. Mutations from the client cross over to backend via Next.js Server Actions that wrap the existing server-only `backendFetch` helper.

**Tech Stack:** Go 1.24 (chi, pgx), Next.js 16 (React 19, App Router), shadcn/ui (Sheet, Dialog, Table, Checkbox), Tailwind v4.

**Spec:** `docs/superpowers/specs/2026-05-04-admin-rbac-management-design.md`.

---

## File map

### Backend — create

- `apps/backend/internal/admin/actions/roles_create.go`
- `apps/backend/internal/admin/actions/roles_create_test.go`
- `apps/backend/internal/admin/actions/roles_update.go`
- `apps/backend/internal/admin/actions/roles_update_test.go`
- `apps/backend/internal/admin/actions/roles_delete.go`
- `apps/backend/internal/admin/actions/roles_delete_test.go`
- `apps/backend/internal/admin/actions/roles_permissions_set.go`
- `apps/backend/internal/admin/actions/roles_permissions_set_test.go`

### Backend — modify

- `apps/backend/internal/admin/actions/guards.go` — add `AssertNotSystemRole(ctx, pool, roleID)` helper.
- `apps/backend/internal/admin/rbac/store.go` — add `ListRolesWithMemberCount(ctx)`.
- `apps/backend/internal/http/handlers.go` — modify `ListAdminRoles` to use the new store method and emit `memberCount`; add `ListAdminPermissions` handler.
- `apps/backend/internal/http/router.go` — register `GET /api/app/admin/permissions`.
- `apps/backend/internal/http/admin_actions_register.go` — register the four new actions.
- `apps/backend/internal/http/openapi.go` — document the new endpoint and four new action types.
- `apps/backend/internal/http/admin_read_handlers_test.go` — additive assertions for `memberCount`.
- `AGENTS.md` (root) and `apps/backend/AGENTS.md`.

### Frontend — create

- `apps/frontend/app/admin/roles/page.tsx`
- `apps/frontend/app/admin/roles/actions.ts`
- `apps/frontend/app/admin/roles/_components/roles-page.tsx`
- `apps/frontend/app/admin/roles/_components/roles-table.tsx`
- `apps/frontend/app/admin/roles/_components/role-drawer.tsx`
- `apps/frontend/app/admin/roles/_components/role-permissions-grid.tsx`
- `apps/frontend/app/admin/roles/_components/delete-role-dialog.tsx`

### Frontend — modify

- `apps/frontend/lib/rustlens-api.ts` — extend `AdminRoleItem` with `memberCount`, add `AdminPermissionItem` type, add `getAdminPermissions()`, add `postAdminAction<T>()` helper.
- `apps/frontend/components/dashboard-app-sidebar.tsx` — accept `permissions?: string[]`, render permission-gated "Access" group with a Roles entry.
- `apps/frontend/app/admin/_components/app-sidebar.tsx` — accept and forward `permissions`.
- `apps/frontend/app/admin/page.tsx` — pass `permissions={session.permissions}` into `<AppSidebar>`.
- `apps/frontend/app/admin/guilds/page.tsx` — pass `permissions={session.permissions}` into `<AppSidebar>`.

---

## Conventions used by every backend action

When tasks below say "follow the existing action pattern", the engineer should match `apps/backend/internal/admin/actions/users_edit.go`. Required interface (defined in `action.go`):

```go
type AdminAction interface {
    Type() string
    Permission() string
    RequiresReason() bool
    Dangerous() bool
    SensitiveParamKeys() []string
    ResourceType() string
    Validate(params json.RawMessage) (ValidatedParams, error)
    Execute(ctx context.Context, in ActionInput) (ActionResult, error)
}
```

Error sentinels (in `errors.go`):

- `ErrNotFound` → 404
- `ErrConflict` → 409
- `ErrPermissionDenied` → 403
- `ErrValidation` → 400

Wrap with `fmt.Errorf("%w: …", ErrXxx, …)`.

Test harness pattern: copy the structure of `users_roles_test.go` (it's the closest match — tests against a real Postgres + real `rbac.Resolver`). Each test file uses `os.Getenv("TEST_POSTGRES_URL")` and skips if unset; tests share a DB so use unique IDs (e.g. `usr_roles_create_actor_001`).

---

## Task 1: `AssertNotSystemRole` guard

**Files:**
- Modify: `apps/backend/internal/admin/actions/guards.go`
- Test: `apps/backend/internal/admin/actions/guards_test.go` (create if absent)

- [ ] **Step 1: Write the failing test**

If `guards_test.go` does not exist, create it with package `actions_test`. Add:

```go
package actions_test

import (
    "context"
    "errors"
    "os"
    "testing"

    "rustlens/backend/internal/admin/actions"
    "rustlens/backend/internal/admin/rbac"
    "rustlens/backend/internal/store"
)

func TestAssertNotSystemRole(t *testing.T) {
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
        t.Fatalf("schema: %v", err)
    }
    rb := rbac.NewStore(s.Pool())
    if err := rb.SeedPermissionsAndRoles(ctx); err != nil {
        t.Fatalf("seed: %v", err)
    }

    sysID, err := rb.RoleIDByKey(ctx, rbac.RoleModerator)
    if err != nil {
        t.Fatalf("system role id: %v", err)
    }
    if err := actions.AssertNotSystemRole(ctx, s.Pool(), sysID.String()); !errors.Is(err, actions.ErrPermissionDenied) {
        t.Errorf("system role should be denied; got %v", err)
    }

    // Insert a custom role
    var customID string
    if err := s.Pool().QueryRow(ctx,
        `INSERT INTO admin_roles (key, name, description, is_system) VALUES ($1,$2,$3,FALSE) RETURNING id::text`,
        "custom_assert_not_system", "Custom", "",
    ).Scan(&customID); err != nil {
        t.Fatalf("insert custom: %v", err)
    }
    t.Cleanup(func() {
        _, _ = s.Pool().Exec(context.Background(), `DELETE FROM admin_roles WHERE id::text=$1`, customID)
    })
    if err := actions.AssertNotSystemRole(ctx, s.Pool(), customID); err != nil {
        t.Errorf("custom role should be allowed; got %v", err)
    }

    // Missing role
    if err := actions.AssertNotSystemRole(ctx, s.Pool(), "00000000-0000-0000-0000-000000000000"); !errors.Is(err, actions.ErrNotFound) {
        t.Errorf("missing role should be ErrNotFound; got %v", err)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd apps/backend && go test ./internal/admin/actions/ -run TestAssertNotSystemRole -v`
Expected: compile error (`undefined: actions.AssertNotSystemRole`).

- [ ] **Step 3: Implement the helper**

Append to `apps/backend/internal/admin/actions/guards.go`:

```go
// AssertNotSystemRole returns ErrPermissionDenied if the role at roleID has
// is_system=true, ErrNotFound if no row exists, or nil otherwise. roleID may
// be either a UUID string or a bare key — the query handles both.
func AssertNotSystemRole(ctx context.Context, pool *pgxpool.Pool, roleID string) error {
    var isSystem bool
    err := pool.QueryRow(ctx,
        `SELECT is_system FROM admin_roles WHERE id::text = $1`, roleID,
    ).Scan(&isSystem)
    if err != nil {
        return fmt.Errorf("%w: role %s", ErrNotFound, roleID)
    }
    if isSystem {
        return fmt.Errorf("%w: cannot modify system role", ErrPermissionDenied)
    }
    return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd apps/backend && TEST_POSTGRES_URL=postgres://rustlens:rustlens@localhost:5432/rustlens?sslmode=disable go test ./internal/admin/actions/ -run TestAssertNotSystemRole -v`
Expected: PASS (or SKIP if Postgres is unavailable; SKIP is acceptable for now and the action tests will exercise it transitively).

- [ ] **Step 5: Commit**

```bash
git add apps/backend/internal/admin/actions/guards.go apps/backend/internal/admin/actions/guards_test.go
git commit -m "feat(backend): add AssertNotSystemRole admin action guard"
```

---

## Task 2: `roles.create` action

**Files:**
- Create: `apps/backend/internal/admin/actions/roles_create.go`
- Test: `apps/backend/internal/admin/actions/roles_create_test.go`
- Modify: `apps/backend/internal/http/admin_actions_register.go`

- [ ] **Step 1: Write the failing test**

Create `apps/backend/internal/admin/actions/roles_create_test.go`. Use the test-harness pattern from `users_roles_test.go`:

```go
package actions_test

import (
    "context"
    "net/http"
    "os"
    "testing"

    "github.com/go-chi/chi/v5"

    "rustlens/backend/internal/admin/actions"
    "rustlens/backend/internal/admin/audit"
    "rustlens/backend/internal/admin/rbac"
    "rustlens/backend/internal/store"
)

func newRolesCRUDHarness(t *testing.T) (*httptest.Server, *store.PostgresStore, *rbac.Store, func(map[string]bool)) {
    t.Helper()
    dsn := os.Getenv("TEST_POSTGRES_URL")
    if dsn == "" {
        t.Skip("TEST_POSTGRES_URL not set")
    }
    ctx := context.Background()
    s, err := store.NewPostgresStore(ctx, dsn)
    if err != nil { t.Fatalf("connect: %v", err) }
    t.Cleanup(s.Close)
    if err := s.EnsureSchema(ctx); err != nil { t.Fatalf("schema: %v", err) }
    rb := rbac.NewStore(s.Pool())
    if err := rb.SeedPermissionsAndRoles(ctx); err != nil { t.Fatalf("seed: %v", err) }
    au := audit.NewStore(s.Pool())
    perms := &fakePermResolver{allow: map[string]bool{}}
    reg := actions.NewRegistry(rbac.IsValidPermission)
    if err := reg.Register(actions.NewRolesCreateAction(s.Pool())); err != nil { t.Fatalf("reg create: %v", err) }
    if err := reg.Register(actions.NewRolesUpdateAction(s.Pool())); err != nil { t.Fatalf("reg update: %v", err) }
    if err := reg.Register(actions.NewRolesDeleteAction(s.Pool())); err != nil { t.Fatalf("reg delete: %v", err) }
    if err := reg.Register(actions.NewRolesPermissionsSetAction(s.Pool())); err != nil { t.Fatalf("reg pset: %v", err) }
    disp := actions.NewDispatcher(reg, perms, auditSinkBridge{au}, fixedActor)
    r := chi.NewRouter()
    r.Post("/api/app/admin/actions/{type}", disp.ServeHTTP)
    srv := httptest.NewServer(r)
    t.Cleanup(srv.Close)
    return srv, s, rb, func(a map[string]bool) { perms.allow = a }
}

func deleteRoleByKey(t *testing.T, s *store.PostgresStore, key string) {
    t.Helper()
    _, _ = s.Pool().Exec(context.Background(), `DELETE FROM admin_roles WHERE key=$1`, key)
}

func TestRolesCreate_HappyPath(t *testing.T) {
    srv, s, _, setPerms := newRolesCRUDHarness(t)
    setPerms(map[string]bool{rbac.PermAdminsGrant: true})
    seedAppUser(t, s, "usr_test_actor", "Actor", false)
    t.Cleanup(func() { deleteRoleByKey(t, s, "rcreate_happy") })

    body := mustJSON(t, map[string]any{
        "params": map[string]any{
            "key":         "rcreate_happy",
            "name":        "Created",
            "description": "for tests",
            "permissions": []string{rbac.PermUsersRead, rbac.PermUsersExport},
        },
        "reason": "test",
    })
    resp := mustPOST(t, srv.URL+"/api/app/admin/actions/roles.create", body)
    if resp.StatusCode != 200 {
        t.Fatalf("status=%d body=%s", resp.StatusCode, readBody(resp))
    }

    var isSystem bool
    if err := s.Pool().QueryRow(context.Background(),
        `SELECT is_system FROM admin_roles WHERE key=$1`, "rcreate_happy",
    ).Scan(&isSystem); err != nil {
        t.Fatalf("created row missing: %v", err)
    }
    if isSystem {
        t.Errorf("created role should not be system")
    }

    var permCount int
    if err := s.Pool().QueryRow(context.Background(), `
        SELECT COUNT(*) FROM admin_role_permissions rp
        JOIN admin_roles r ON r.id=rp.role_id WHERE r.key=$1`, "rcreate_happy").Scan(&permCount); err != nil {
        t.Fatal(err)
    }
    if permCount != 2 { t.Errorf("perm count = %d, want 2", permCount) }
}

func TestRolesCreate_DuplicateKeyRejected(t *testing.T) {
    srv, s, _, setPerms := newRolesCRUDHarness(t)
    setPerms(map[string]bool{rbac.PermAdminsGrant: true})
    seedAppUser(t, s, "usr_test_actor", "Actor", false)

    body := mustJSON(t, map[string]any{
        "params": map[string]any{"key": rbac.RoleModerator, "name": "X", "description": "", "permissions": []string{}},
        "reason": "t",
    })
    resp := mustPOST(t, srv.URL+"/api/app/admin/actions/roles.create", body)
    if resp.StatusCode != http.StatusConflict {
        t.Errorf("system-key collision should be 409; got %d body=%s", resp.StatusCode, readBody(resp))
    }
}

func TestRolesCreate_InvalidKeyFormat(t *testing.T) {
    srv, s, _, setPerms := newRolesCRUDHarness(t)
    setPerms(map[string]bool{rbac.PermAdminsGrant: true})
    seedAppUser(t, s, "usr_test_actor", "Actor", false)

    body := mustJSON(t, map[string]any{
        "params": map[string]any{"key": "Bad-Key!", "name": "X", "description": "", "permissions": []string{}},
        "reason": "t",
    })
    resp := mustPOST(t, srv.URL+"/api/app/admin/actions/roles.create", body)
    if resp.StatusCode != http.StatusBadRequest {
        t.Errorf("bad key format should be 400; got %d body=%s", resp.StatusCode, readBody(resp))
    }
}

func TestRolesCreate_BadPermissionRejected(t *testing.T) {
    srv, s, _, setPerms := newRolesCRUDHarness(t)
    setPerms(map[string]bool{rbac.PermAdminsGrant: true})
    seedAppUser(t, s, "usr_test_actor", "Actor", false)
    t.Cleanup(func() { deleteRoleByKey(t, s, "rcreate_badperm") })

    body := mustJSON(t, map[string]any{
        "params": map[string]any{"key": "rcreate_badperm", "name": "X", "description": "", "permissions": []string{"not.a.permission"}},
        "reason": "t",
    })
    resp := mustPOST(t, srv.URL+"/api/app/admin/actions/roles.create", body)
    if resp.StatusCode != http.StatusBadRequest {
        t.Errorf("bad permission should be 400; got %d body=%s", resp.StatusCode, readBody(resp))
    }
}

func TestRolesCreate_ReasonRequired(t *testing.T) {
    srv, s, _, setPerms := newRolesCRUDHarness(t)
    setPerms(map[string]bool{rbac.PermAdminsGrant: true})
    seedAppUser(t, s, "usr_test_actor", "Actor", false)

    body := mustJSON(t, map[string]any{
        "params": map[string]any{"key": "rcreate_noreason", "name": "X", "description": "", "permissions": []string{}},
    })
    resp := mustPOST(t, srv.URL+"/api/app/admin/actions/roles.create", body)
    if resp.StatusCode != http.StatusBadRequest {
        t.Errorf("missing reason should be 400; got %d", resp.StatusCode)
    }
}

func TestRolesCreate_PermissionDenied(t *testing.T) {
    srv, s, _, setPerms := newRolesCRUDHarness(t)
    setPerms(map[string]bool{}) // no admins.grant
    seedAppUser(t, s, "usr_test_actor", "Actor", false)

    body := mustJSON(t, map[string]any{
        "params": map[string]any{"key": "rcreate_no_perm", "name": "X", "description": "", "permissions": []string{}},
        "reason": "t",
    })
    resp := mustPOST(t, srv.URL+"/api/app/admin/actions/roles.create", body)
    if resp.StatusCode != http.StatusForbidden {
        t.Errorf("missing admins.grant should be 403; got %d", resp.StatusCode)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd apps/backend && go test ./internal/admin/actions/ -run TestRolesCreate -v`
Expected: compile error (`undefined: actions.NewRolesCreateAction` etc.).

- [ ] **Step 3: Implement `roles.create`**

Create `apps/backend/internal/admin/actions/roles_create.go`:

```go
package actions

import (
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "regexp"

    "github.com/jackc/pgx/v5/pgxpool"

    "rustlens/backend/internal/admin/rbac"
)

type RolesCreateParams struct {
    Key         string   `json:"key"`
    Name        string   `json:"name"`
    Description string   `json:"description"`
    Permissions []string `json:"permissions"`
}

type rolesCreateAction struct{ pool *pgxpool.Pool }

// NewRolesCreateAction returns the roles.create admin action. It inserts a new
// non-system role and its permission assignments in one transaction. Refuses
// keys that collide with seeded system roles (case-insensitive on key).
func NewRolesCreateAction(pool *pgxpool.Pool) AdminAction {
    return &rolesCreateAction{pool: pool}
}

func (a *rolesCreateAction) Type() string                 { return "roles.create" }
func (a *rolesCreateAction) Permission() string           { return rbac.PermAdminsGrant }
func (a *rolesCreateAction) RequiresReason() bool         { return true }
func (a *rolesCreateAction) Dangerous() bool              { return false }
func (a *rolesCreateAction) SensitiveParamKeys() []string { return nil }
func (a *rolesCreateAction) ResourceType() string         { return "role" }

var roleKeyRe = regexp.MustCompile(`^[a-z][a-z0-9_]{1,63}$`)

func (a *rolesCreateAction) Validate(raw json.RawMessage) (ValidatedParams, error) {
    var p RolesCreateParams
    if err := json.Unmarshal(raw, &p); err != nil {
        return ValidatedParams{}, fmt.Errorf("validation: %w", err)
    }
    if !roleKeyRe.MatchString(p.Key) {
        return ValidatedParams{}, errors.New("key must match ^[a-z][a-z0-9_]{1,63}$")
    }
    if p.Name == "" || len(p.Name) > 80 {
        return ValidatedParams{}, errors.New("name required, max 80 chars")
    }
    if len(p.Description) > 500 {
        return ValidatedParams{}, errors.New("description too long")
    }
    seen := map[string]bool{}
    for _, k := range p.Permissions {
        if !rbac.IsValidPermission(k) {
            return ValidatedParams{}, fmt.Errorf("unknown permission: %s", k)
        }
        if seen[k] {
            return ValidatedParams{}, fmt.Errorf("duplicate permission: %s", k)
        }
        seen[k] = true
    }
    return ValidatedParams{ResourceID: p.Key}, nil
}

func (a *rolesCreateAction) Execute(ctx context.Context, in ActionInput) (ActionResult, error) {
    var p RolesCreateParams
    if err := json.Unmarshal(in.Params, &p); err != nil {
        return ActionResult{}, fmt.Errorf("%w: %s", ErrValidation, err.Error())
    }

    // System-key collision (covers both pre-existing system rows and any
    // future seeded ones).
    for _, sys := range rbac.SystemRoles() {
        if sys.Key == p.Key {
            return ActionResult{}, fmt.Errorf("%w: key %s reserved for system role", ErrConflict, p.Key)
        }
    }

    tx, err := a.pool.Begin(ctx)
    if err != nil {
        return ActionResult{}, err
    }
    defer tx.Rollback(ctx)

    var newID string
    err = tx.QueryRow(ctx, `
        INSERT INTO admin_roles (key, name, description, is_system)
        VALUES ($1,$2,$3,FALSE) RETURNING id::text
    `, p.Key, p.Name, p.Description).Scan(&newID)
    if err != nil {
        // 23505 = unique_violation
        if pgErr, ok := pgxErrCode(err); ok && pgErr == "23505" {
            return ActionResult{}, fmt.Errorf("%w: key %s already exists", ErrConflict, p.Key)
        }
        return ActionResult{}, err
    }
    for _, k := range p.Permissions {
        if _, err := tx.Exec(ctx,
            `INSERT INTO admin_role_permissions (role_id, permission_key) VALUES ($1::uuid, $2)`,
            newID, k,
        ); err != nil {
            return ActionResult{}, err
        }
    }
    if err := tx.Commit(ctx); err != nil {
        return ActionResult{}, err
    }

    return ActionResult{
        ResourceID: newID,
        Payload:    map[string]any{"id": newID, "key": p.Key},
    }, nil
}

// pgxErrCode extracts the SQLSTATE from a pgx driver error if present.
// pgx v5's *pgconn.PgError implements SQLState() so this catches it without
// importing pgconn explicitly.
func pgxErrCode(err error) (string, bool) {
    var pgErr interface{ SQLState() string }
    if errors.As(err, &pgErr) {
        return pgErr.SQLState(), true
    }
    return "", false
}
```

- [ ] **Step 4: Register the action**

Modify `apps/backend/internal/http/admin_actions_register.go`. Add inside `registerAdminActions` after the `NewUsersPurgeAction` block:

```go
if err := reg.Register(actions.NewRolesCreateAction(pool)); err != nil {
    return err
}
if err := reg.Register(actions.NewRolesUpdateAction(pool)); err != nil {
    return err
}
if err := reg.Register(actions.NewRolesDeleteAction(pool)); err != nil {
    return err
}
if err := reg.Register(actions.NewRolesPermissionsSetAction(pool)); err != nil {
    return err
}
```

(All four are added now even though only one exists yet — Tasks 3-5 fill in the others. The build will fail until Tasks 3-5 land. To keep the build green between commits, only register `NewRolesCreateAction` here in this commit.)

For this commit, register only:

```go
if err := reg.Register(actions.NewRolesCreateAction(pool)); err != nil {
    return err
}
```

The remaining three lines are added in Tasks 3, 4, and 5 respectively.

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd apps/backend && TEST_POSTGRES_URL=postgres://rustlens:rustlens@localhost:5432/rustlens?sslmode=disable go test ./internal/admin/actions/ -run TestRolesCreate -v`
Expected: All five `TestRolesCreate_*` PASS (or SKIP if Postgres unreachable).

Also run: `cd apps/backend && go build ./...`
Expected: build succeeds.

- [ ] **Step 6: Commit**

```bash
git add apps/backend/internal/admin/actions/roles_create.go apps/backend/internal/admin/actions/roles_create_test.go apps/backend/internal/http/admin_actions_register.go
git commit -m "feat(backend): add roles.create admin action"
```

---

## Task 3: `roles.update` action

**Files:**
- Create: `apps/backend/internal/admin/actions/roles_update.go`
- Test: `apps/backend/internal/admin/actions/roles_update_test.go`
- Modify: `apps/backend/internal/http/admin_actions_register.go`

- [ ] **Step 1: Write the failing test**

Create `apps/backend/internal/admin/actions/roles_update_test.go`:

```go
package actions_test

import (
    "context"
    "net/http"
    "testing"

    "rustlens/backend/internal/admin/rbac"
)

func insertCustomRole(t *testing.T, s *store.PostgresStore, key, name string) string {
    t.Helper()
    var id string
    if err := s.Pool().QueryRow(context.Background(),
        `INSERT INTO admin_roles (key, name, description, is_system) VALUES ($1,$2,'',FALSE) RETURNING id::text`,
        key, name,
    ).Scan(&id); err != nil {
        t.Fatalf("insert custom role: %v", err)
    }
    t.Cleanup(func() { _, _ = s.Pool().Exec(context.Background(), `DELETE FROM admin_roles WHERE id::text=$1`, id) })
    return id
}

func TestRolesUpdate_HappyPath(t *testing.T) {
    srv, s, _, setPerms := newRolesCRUDHarness(t)
    setPerms(map[string]bool{rbac.PermAdminsGrant: true})
    seedAppUser(t, s, "usr_test_actor", "Actor", false)
    id := insertCustomRole(t, s, "rupd_happy", "Original")

    body := mustJSON(t, map[string]any{
        "params": map[string]any{"id": id, "name": "Updated", "description": "new desc"},
        "reason": "t",
    })
    resp := mustPOST(t, srv.URL+"/api/app/admin/actions/roles.update", body)
    if resp.StatusCode != 200 {
        t.Fatalf("status=%d body=%s", resp.StatusCode, readBody(resp))
    }
    var name, desc string
    if err := s.Pool().QueryRow(context.Background(),
        `SELECT name, COALESCE(description,'') FROM admin_roles WHERE id::text=$1`, id,
    ).Scan(&name, &desc); err != nil {
        t.Fatal(err)
    }
    if name != "Updated" || desc != "new desc" {
        t.Errorf("name=%q desc=%q", name, desc)
    }
}

func TestRolesUpdate_SystemRoleRejected(t *testing.T) {
    srv, s, rb, setPerms := newRolesCRUDHarness(t)
    setPerms(map[string]bool{rbac.PermAdminsGrant: true})
    seedAppUser(t, s, "usr_test_actor", "Actor", false)
    sysID, err := rb.RoleIDByKey(context.Background(), rbac.RoleModerator)
    if err != nil { t.Fatal(err) }

    body := mustJSON(t, map[string]any{
        "params": map[string]any{"id": sysID.String(), "name": "X", "description": ""},
        "reason": "t",
    })
    resp := mustPOST(t, srv.URL+"/api/app/admin/actions/roles.update", body)
    if resp.StatusCode != http.StatusForbidden {
        t.Errorf("status=%d; want 403 body=%s", resp.StatusCode, readBody(resp))
    }
}

func TestRolesUpdate_NonExistentID(t *testing.T) {
    srv, s, _, setPerms := newRolesCRUDHarness(t)
    setPerms(map[string]bool{rbac.PermAdminsGrant: true})
    seedAppUser(t, s, "usr_test_actor", "Actor", false)

    body := mustJSON(t, map[string]any{
        "params": map[string]any{"id": "00000000-0000-0000-0000-000000000000", "name": "X", "description": ""},
        "reason": "t",
    })
    resp := mustPOST(t, srv.URL+"/api/app/admin/actions/roles.update", body)
    if resp.StatusCode != http.StatusNotFound {
        t.Errorf("status=%d; want 404 body=%s", resp.StatusCode, readBody(resp))
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd apps/backend && go test ./internal/admin/actions/ -run TestRolesUpdate -v`
Expected: compile error (`undefined: NewRolesUpdateAction`).

- [ ] **Step 3: Implement `roles.update`**

Create `apps/backend/internal/admin/actions/roles_update.go`:

```go
package actions

import (
    "context"
    "encoding/json"
    "errors"
    "fmt"

    "github.com/jackc/pgx/v5/pgxpool"

    "rustlens/backend/internal/admin/rbac"
)

type RolesUpdateParams struct {
    ID          string `json:"id"`
    Name        string `json:"name"`
    Description string `json:"description"`
}

type rolesUpdateAction struct{ pool *pgxpool.Pool }

// NewRolesUpdateAction updates a non-system role's name and description.
// Key is intentionally immutable post-create.
func NewRolesUpdateAction(pool *pgxpool.Pool) AdminAction {
    return &rolesUpdateAction{pool: pool}
}

func (a *rolesUpdateAction) Type() string                 { return "roles.update" }
func (a *rolesUpdateAction) Permission() string           { return rbac.PermAdminsGrant }
func (a *rolesUpdateAction) RequiresReason() bool         { return true }
func (a *rolesUpdateAction) Dangerous() bool              { return false }
func (a *rolesUpdateAction) SensitiveParamKeys() []string { return nil }
func (a *rolesUpdateAction) ResourceType() string         { return "role" }

func (a *rolesUpdateAction) Validate(raw json.RawMessage) (ValidatedParams, error) {
    var p RolesUpdateParams
    if err := json.Unmarshal(raw, &p); err != nil {
        return ValidatedParams{}, fmt.Errorf("validation: %w", err)
    }
    if p.ID == "" {
        return ValidatedParams{}, errors.New("id is required")
    }
    if p.Name == "" || len(p.Name) > 80 {
        return ValidatedParams{}, errors.New("name required, max 80 chars")
    }
    if len(p.Description) > 500 {
        return ValidatedParams{}, errors.New("description too long")
    }
    return ValidatedParams{ResourceID: p.ID}, nil
}

func (a *rolesUpdateAction) Execute(ctx context.Context, in ActionInput) (ActionResult, error) {
    var p RolesUpdateParams
    if err := json.Unmarshal(in.Params, &p); err != nil {
        return ActionResult{}, fmt.Errorf("%w: %s", ErrValidation, err.Error())
    }

    if err := AssertNotSystemRole(ctx, a.pool, p.ID); err != nil {
        return ActionResult{}, err
    }

    tag, err := a.pool.Exec(ctx,
        `UPDATE admin_roles SET name=$2, description=$3, updated_at=NOW() WHERE id::text=$1 AND is_system=FALSE`,
        p.ID, p.Name, p.Description,
    )
    if err != nil {
        return ActionResult{}, err
    }
    if tag.RowsAffected() == 0 {
        return ActionResult{}, fmt.Errorf("%w: role %s", ErrNotFound, p.ID)
    }

    return ActionResult{
        ResourceID: p.ID,
        Payload:    map[string]any{"id": p.ID},
    }, nil
}
```

- [ ] **Step 4: Register the action**

In `apps/backend/internal/http/admin_actions_register.go`, add directly under the existing `NewRolesCreateAction` registration:

```go
if err := reg.Register(actions.NewRolesUpdateAction(pool)); err != nil {
    return err
}
```

- [ ] **Step 5: Run tests + build**

```
cd apps/backend && TEST_POSTGRES_URL=postgres://rustlens:rustlens@localhost:5432/rustlens?sslmode=disable go test ./internal/admin/actions/ -run TestRolesUpdate -v
cd apps/backend && go build ./...
```
Expected: PASS or SKIP; build succeeds.

- [ ] **Step 6: Commit**

```bash
git add apps/backend/internal/admin/actions/roles_update.go apps/backend/internal/admin/actions/roles_update_test.go apps/backend/internal/http/admin_actions_register.go
git commit -m "feat(backend): add roles.update admin action"
```

---

## Task 4: `roles.delete` action

**Files:**
- Create: `apps/backend/internal/admin/actions/roles_delete.go`
- Test: `apps/backend/internal/admin/actions/roles_delete_test.go`
- Modify: `apps/backend/internal/http/admin_actions_register.go`

- [ ] **Step 1: Write the failing test**

Create `apps/backend/internal/admin/actions/roles_delete_test.go`:

```go
package actions_test

import (
    "context"
    "net/http"
    "testing"

    "rustlens/backend/internal/admin/rbac"
)

func TestRolesDelete_HappyPath_CascadesAssignments(t *testing.T) {
    srv, s, rb, setPerms := newRolesCRUDHarness(t)
    setPerms(map[string]bool{rbac.PermAdminsGrant: true})
    seedAppUser(t, s, "usr_test_actor", "Actor", false)
    seedAppUser(t, s, "usr_rdel_target", "Target", false)
    id := insertCustomRole(t, s, "rdel_happy", "Custom")

    // Assign the role to a user and a permission so cascade can be observed.
    if _, err := s.Pool().Exec(context.Background(),
        `INSERT INTO admin_user_roles (user_id, role_id) VALUES ($1, $2::uuid)`,
        "usr_rdel_target", id,
    ); err != nil { t.Fatal(err) }
    if _, err := s.Pool().Exec(context.Background(),
        `INSERT INTO admin_role_permissions (role_id, permission_key) VALUES ($1::uuid, $2)`,
        id, rbac.PermUsersRead,
    ); err != nil { t.Fatal(err) }

    body := mustJSON(t, map[string]any{
        "params": map[string]any{"id": id},
        "reason": "t",
    })
    resp := mustPOST(t, srv.URL+"/api/app/admin/actions/roles.delete", body)
    if resp.StatusCode != 200 {
        t.Fatalf("status=%d body=%s", resp.StatusCode, readBody(resp))
    }

    var n int
    if err := s.Pool().QueryRow(context.Background(),
        `SELECT COUNT(*) FROM admin_roles WHERE id::text=$1`, id,
    ).Scan(&n); err != nil { t.Fatal(err) }
    if n != 0 { t.Errorf("role still exists; count=%d", n) }

    if err := s.Pool().QueryRow(context.Background(),
        `SELECT COUNT(*) FROM admin_user_roles WHERE role_id::text=$1`, id,
    ).Scan(&n); err != nil { t.Fatal(err) }
    if n != 0 { t.Errorf("assignments not cascaded; count=%d", n) }

    if err := s.Pool().QueryRow(context.Background(),
        `SELECT COUNT(*) FROM admin_role_permissions WHERE role_id::text=$1`, id,
    ).Scan(&n); err != nil { t.Fatal(err) }
    if n != 0 { t.Errorf("permissions not cascaded; count=%d", n) }

    _ = rb // unused
}

func TestRolesDelete_SystemRoleRejected(t *testing.T) {
    srv, s, rb, setPerms := newRolesCRUDHarness(t)
    setPerms(map[string]bool{rbac.PermAdminsGrant: true})
    seedAppUser(t, s, "usr_test_actor", "Actor", false)
    sysID, err := rb.RoleIDByKey(context.Background(), rbac.RoleModerator)
    if err != nil { t.Fatal(err) }

    body := mustJSON(t, map[string]any{
        "params": map[string]any{"id": sysID.String()},
        "reason": "t",
    })
    resp := mustPOST(t, srv.URL+"/api/app/admin/actions/roles.delete", body)
    if resp.StatusCode != http.StatusForbidden {
        t.Errorf("status=%d; want 403 body=%s", resp.StatusCode, readBody(resp))
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd apps/backend && go test ./internal/admin/actions/ -run TestRolesDelete -v`
Expected: compile error.

- [ ] **Step 3: Implement `roles.delete`**

Create `apps/backend/internal/admin/actions/roles_delete.go`:

```go
package actions

import (
    "context"
    "encoding/json"
    "errors"
    "fmt"

    "github.com/jackc/pgx/v5/pgxpool"

    "rustlens/backend/internal/admin/rbac"
)

type RolesDeleteParams struct {
    ID string `json:"id"`
}

type rolesDeleteAction struct{ pool *pgxpool.Pool }

// NewRolesDeleteAction permanently deletes a non-system role. FK ON DELETE
// CASCADE removes admin_role_permissions and admin_user_roles rows.
func NewRolesDeleteAction(pool *pgxpool.Pool) AdminAction {
    return &rolesDeleteAction{pool: pool}
}

func (a *rolesDeleteAction) Type() string                 { return "roles.delete" }
func (a *rolesDeleteAction) Permission() string           { return rbac.PermAdminsGrant }
func (a *rolesDeleteAction) RequiresReason() bool         { return true }
func (a *rolesDeleteAction) Dangerous() bool              { return false }
func (a *rolesDeleteAction) SensitiveParamKeys() []string { return nil }
func (a *rolesDeleteAction) ResourceType() string         { return "role" }

func (a *rolesDeleteAction) Validate(raw json.RawMessage) (ValidatedParams, error) {
    var p RolesDeleteParams
    if err := json.Unmarshal(raw, &p); err != nil {
        return ValidatedParams{}, fmt.Errorf("validation: %w", err)
    }
    if p.ID == "" {
        return ValidatedParams{}, errors.New("id is required")
    }
    return ValidatedParams{ResourceID: p.ID}, nil
}

func (a *rolesDeleteAction) Execute(ctx context.Context, in ActionInput) (ActionResult, error) {
    var p RolesDeleteParams
    if err := json.Unmarshal(in.Params, &p); err != nil {
        return ActionResult{}, fmt.Errorf("%w: %s", ErrValidation, err.Error())
    }

    if err := AssertNotSystemRole(ctx, a.pool, p.ID); err != nil {
        return ActionResult{}, err
    }

    tag, err := a.pool.Exec(ctx,
        `DELETE FROM admin_roles WHERE id::text=$1 AND is_system=FALSE`, p.ID,
    )
    if err != nil {
        return ActionResult{}, err
    }
    if tag.RowsAffected() == 0 {
        return ActionResult{}, fmt.Errorf("%w: role %s", ErrNotFound, p.ID)
    }

    return ActionResult{
        ResourceID: p.ID,
        Payload:    map[string]any{"id": p.ID},
    }, nil
}
```

- [ ] **Step 4: Register the action**

In `apps/backend/internal/http/admin_actions_register.go`, append after the `roles.update` registration:

```go
if err := reg.Register(actions.NewRolesDeleteAction(pool)); err != nil {
    return err
}
```

- [ ] **Step 5: Run tests + build**

```
cd apps/backend && TEST_POSTGRES_URL=postgres://rustlens:rustlens@localhost:5432/rustlens?sslmode=disable go test ./internal/admin/actions/ -run TestRolesDelete -v
cd apps/backend && go build ./...
```
Expected: PASS or SKIP; build succeeds.

- [ ] **Step 6: Commit**

```bash
git add apps/backend/internal/admin/actions/roles_delete.go apps/backend/internal/admin/actions/roles_delete_test.go apps/backend/internal/http/admin_actions_register.go
git commit -m "feat(backend): add roles.delete admin action with cascade cleanup"
```

---

## Task 5: `roles.permissions.set` action

**Files:**
- Create: `apps/backend/internal/admin/actions/roles_permissions_set.go`
- Test: `apps/backend/internal/admin/actions/roles_permissions_set_test.go`
- Modify: `apps/backend/internal/http/admin_actions_register.go`

- [ ] **Step 1: Write the failing test**

Create `apps/backend/internal/admin/actions/roles_permissions_set_test.go`:

```go
package actions_test

import (
    "context"
    "net/http"
    "sort"
    "testing"

    "rustlens/backend/internal/admin/rbac"
)

func currentPerms(t *testing.T, s *store.PostgresStore, roleID string) []string {
    t.Helper()
    rows, err := s.Pool().Query(context.Background(),
        `SELECT permission_key FROM admin_role_permissions WHERE role_id::text=$1 ORDER BY permission_key`, roleID,
    )
    if err != nil { t.Fatal(err) }
    defer rows.Close()
    var out []string
    for rows.Next() {
        var k string
        if err := rows.Scan(&k); err != nil { t.Fatal(err) }
        out = append(out, k)
    }
    return out
}

func TestRolesPermissionsSet_HappyPath(t *testing.T) {
    srv, s, _, setPerms := newRolesCRUDHarness(t)
    setPerms(map[string]bool{rbac.PermAdminsGrant: true})
    seedAppUser(t, s, "usr_test_actor", "Actor", false)
    id := insertCustomRole(t, s, "rpset_happy", "Custom")
    // Pre-seed one permission so we can observe diff (delete + insert).
    if _, err := s.Pool().Exec(context.Background(),
        `INSERT INTO admin_role_permissions (role_id, permission_key) VALUES ($1::uuid, $2)`,
        id, rbac.PermUsersRead,
    ); err != nil { t.Fatal(err) }

    body := mustJSON(t, map[string]any{
        "params": map[string]any{
            "id":          id,
            "permissions": []string{rbac.PermUsersExport, rbac.PermAuditRead},
        },
        "reason": "t",
    })
    resp := mustPOST(t, srv.URL+"/api/app/admin/actions/roles.permissions.set", body)
    if resp.StatusCode != 200 {
        t.Fatalf("status=%d body=%s", resp.StatusCode, readBody(resp))
    }

    got := currentPerms(t, s, id)
    want := []string{rbac.PermAuditRead, rbac.PermUsersExport}
    sort.Strings(got); sort.Strings(want)
    if len(got) != len(want) {
        t.Fatalf("perms=%v want=%v", got, want)
    }
    for i := range got {
        if got[i] != want[i] { t.Errorf("perms=%v want=%v", got, want) }
    }
}

func TestRolesPermissionsSet_IdempotentReset(t *testing.T) {
    srv, s, _, setPerms := newRolesCRUDHarness(t)
    setPerms(map[string]bool{rbac.PermAdminsGrant: true})
    seedAppUser(t, s, "usr_test_actor", "Actor", false)
    id := insertCustomRole(t, s, "rpset_idem", "Custom")

    body := mustJSON(t, map[string]any{
        "params": map[string]any{"id": id, "permissions": []string{rbac.PermUsersRead}},
        "reason": "t",
    })
    if r := mustPOST(t, srv.URL+"/api/app/admin/actions/roles.permissions.set", body); r.StatusCode != 200 {
        t.Fatalf("first call status=%d body=%s", r.StatusCode, readBody(r))
    }
    if r := mustPOST(t, srv.URL+"/api/app/admin/actions/roles.permissions.set", body); r.StatusCode != 200 {
        t.Fatalf("second call status=%d body=%s", r.StatusCode, readBody(r))
    }
    got := currentPerms(t, s, id)
    if len(got) != 1 || got[0] != rbac.PermUsersRead {
        t.Errorf("after idempotent reset, perms=%v", got)
    }
}

func TestRolesPermissionsSet_BadPermissionRejected(t *testing.T) {
    srv, s, _, setPerms := newRolesCRUDHarness(t)
    setPerms(map[string]bool{rbac.PermAdminsGrant: true})
    seedAppUser(t, s, "usr_test_actor", "Actor", false)
    id := insertCustomRole(t, s, "rpset_badperm", "Custom")

    body := mustJSON(t, map[string]any{
        "params": map[string]any{"id": id, "permissions": []string{"not.a.permission"}},
        "reason": "t",
    })
    resp := mustPOST(t, srv.URL+"/api/app/admin/actions/roles.permissions.set", body)
    if resp.StatusCode != http.StatusBadRequest {
        t.Errorf("status=%d; want 400", resp.StatusCode)
    }
}

func TestRolesPermissionsSet_SystemRoleRejected(t *testing.T) {
    srv, s, rb, setPerms := newRolesCRUDHarness(t)
    setPerms(map[string]bool{rbac.PermAdminsGrant: true})
    seedAppUser(t, s, "usr_test_actor", "Actor", false)
    sysID, err := rb.RoleIDByKey(context.Background(), rbac.RoleModerator)
    if err != nil { t.Fatal(err) }

    body := mustJSON(t, map[string]any{
        "params": map[string]any{"id": sysID.String(), "permissions": []string{rbac.PermUsersRead}},
        "reason": "t",
    })
    resp := mustPOST(t, srv.URL+"/api/app/admin/actions/roles.permissions.set", body)
    if resp.StatusCode != http.StatusForbidden {
        t.Errorf("status=%d; want 403", resp.StatusCode)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd apps/backend && go test ./internal/admin/actions/ -run TestRolesPermissionsSet -v`
Expected: compile error.

- [ ] **Step 3: Implement `roles.permissions.set`**

Create `apps/backend/internal/admin/actions/roles_permissions_set.go`:

```go
package actions

import (
    "context"
    "encoding/json"
    "errors"
    "fmt"

    "github.com/jackc/pgx/v5/pgxpool"

    "rustlens/backend/internal/admin/rbac"
)

type RolesPermissionsSetParams struct {
    ID          string   `json:"id"`
    Permissions []string `json:"permissions"`
}

type rolesPermissionsSetAction struct{ pool *pgxpool.Pool }

// NewRolesPermissionsSetAction replaces the entire permission set on a
// non-system role. Operates as DELETE-all + bulk INSERT in one transaction.
func NewRolesPermissionsSetAction(pool *pgxpool.Pool) AdminAction {
    return &rolesPermissionsSetAction{pool: pool}
}

func (a *rolesPermissionsSetAction) Type() string                 { return "roles.permissions.set" }
func (a *rolesPermissionsSetAction) Permission() string           { return rbac.PermAdminsGrant }
func (a *rolesPermissionsSetAction) RequiresReason() bool         { return true }
func (a *rolesPermissionsSetAction) Dangerous() bool              { return false }
func (a *rolesPermissionsSetAction) SensitiveParamKeys() []string { return nil }
func (a *rolesPermissionsSetAction) ResourceType() string         { return "role" }

func (a *rolesPermissionsSetAction) Validate(raw json.RawMessage) (ValidatedParams, error) {
    var p RolesPermissionsSetParams
    if err := json.Unmarshal(raw, &p); err != nil {
        return ValidatedParams{}, fmt.Errorf("validation: %w", err)
    }
    if p.ID == "" {
        return ValidatedParams{}, errors.New("id is required")
    }
    seen := map[string]bool{}
    for _, k := range p.Permissions {
        if !rbac.IsValidPermission(k) {
            return ValidatedParams{}, fmt.Errorf("unknown permission: %s", k)
        }
        if seen[k] {
            return ValidatedParams{}, fmt.Errorf("duplicate permission: %s", k)
        }
        seen[k] = true
    }
    return ValidatedParams{ResourceID: p.ID}, nil
}

func (a *rolesPermissionsSetAction) Execute(ctx context.Context, in ActionInput) (ActionResult, error) {
    var p RolesPermissionsSetParams
    if err := json.Unmarshal(in.Params, &p); err != nil {
        return ActionResult{}, fmt.Errorf("%w: %s", ErrValidation, err.Error())
    }

    if err := AssertNotSystemRole(ctx, a.pool, p.ID); err != nil {
        return ActionResult{}, err
    }

    tx, err := a.pool.Begin(ctx)
    if err != nil {
        return ActionResult{}, err
    }
    defer tx.Rollback(ctx)

    if _, err := tx.Exec(ctx,
        `DELETE FROM admin_role_permissions WHERE role_id::text=$1`, p.ID,
    ); err != nil {
        return ActionResult{}, err
    }
    for _, k := range p.Permissions {
        if _, err := tx.Exec(ctx,
            `INSERT INTO admin_role_permissions (role_id, permission_key) VALUES ($1::uuid, $2)`,
            p.ID, k,
        ); err != nil {
            return ActionResult{}, err
        }
    }
    if err := tx.Commit(ctx); err != nil {
        return ActionResult{}, err
    }

    return ActionResult{
        ResourceID: p.ID,
        Payload:    map[string]any{"id": p.ID, "count": len(p.Permissions)},
    }, nil
}
```

- [ ] **Step 4: Register the action**

In `apps/backend/internal/http/admin_actions_register.go`, append after the `roles.delete` registration:

```go
if err := reg.Register(actions.NewRolesPermissionsSetAction(pool)); err != nil {
    return err
}
```

- [ ] **Step 5: Run tests + build**

```
cd apps/backend && TEST_POSTGRES_URL=postgres://rustlens:rustlens@localhost:5432/rustlens?sslmode=disable go test ./internal/admin/actions/ -run TestRolesPermissionsSet -v
cd apps/backend && go build ./...
```
Expected: PASS or SKIP; build succeeds.

- [ ] **Step 6: Commit**

```bash
git add apps/backend/internal/admin/actions/roles_permissions_set.go apps/backend/internal/admin/actions/roles_permissions_set_test.go apps/backend/internal/http/admin_actions_register.go
git commit -m "feat(backend): add roles.permissions.set admin action"
```

---

## Task 6: `GET /api/app/admin/permissions` endpoint

**Files:**
- Modify: `apps/backend/internal/http/handlers.go`
- Modify: `apps/backend/internal/http/router.go`
- Test: `apps/backend/internal/http/admin_permissions_handler_test.go`

- [ ] **Step 1: Write the failing test**

Create `apps/backend/internal/http/admin_permissions_handler_test.go`:

```go
package httpapi_test

import (
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"

    httpapi "rustlens/backend/internal/http"
)

// TestListAdminPermissions verifies the endpoint returns the static catalog
// from rbac.AllPermissions(). Uses the same test-server harness as the rest
// of the admin_*_handlers tests (see admin_read_handlers_test.go for setup).
func TestListAdminPermissions_ReturnsCatalog(t *testing.T) {
    ts := newAdminTestServer(t) // existing helper in admin_read_handlers_test.go

    req := httptest.NewRequest("GET", "/api/app/admin/permissions", nil)
    rec := httptest.NewRecorder()
    ts.handler.ServeHTTP(rec, req)

    if rec.Code != http.StatusOK {
        t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
    }
    var out struct {
        Items []struct {
            Key         string `json:"key"`
            Description string `json:"description"`
        } `json:"items"`
    }
    if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
        t.Fatalf("decode: %v", err)
    }
    if len(out.Items) < 10 {
        t.Errorf("expected at least 10 permissions, got %d", len(out.Items))
    }
    found := false
    for _, p := range out.Items {
        if p.Key == "users.read" && strings.Contains(p.Description, "View user profiles") {
            found = true
            break
        }
    }
    if !found {
        t.Errorf("users.read not present with expected description")
    }
    _ = httpapi.NewHandler // anchor import
}
```

> Note: the `newAdminTestServer` helper already exists in `admin_read_handlers_test.go`. If its signature differs from the assumption above, follow the patterns used by the other tests in that file when wiring the route through.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd apps/backend && go test ./internal/http/ -run TestListAdminPermissions -v`
Expected: FAIL — route returns 404.

- [ ] **Step 3: Add the handler**

Append to `apps/backend/internal/http/handlers.go` (just below `ListAdminRoles`):

```go
// adminPermissionItem is the JSON shape for a single permission in the
// ListAdminPermissions response.
type adminPermissionItem struct {
    Key         string `json:"key"`
    Description string `json:"description"`
}

// ListAdminPermissions handles GET /api/app/admin/permissions. Returns the
// canonical permission catalog from rbac.AllPermissions(). Static; no DB read.
func (h *Handler) ListAdminPermissions(w http.ResponseWriter, r *http.Request) {
    perms := rbac.AllPermissions()
    items := make([]adminPermissionItem, len(perms))
    for i, p := range perms {
        items[i] = adminPermissionItem{Key: p.Key, Description: p.Description}
    }
    writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
```

- [ ] **Step 4: Register the route**

In `apps/backend/internal/http/router.go`, inside the `r.Route("/admin", func(r chi.Router) {` block, add directly after the `r.Get("/roles", handler.ListAdminRoles)` line:

```go
r.Get("/permissions", handler.ListAdminPermissions)
```

- [ ] **Step 5: Run tests + build**

```
cd apps/backend && go test ./internal/http/ -run TestListAdminPermissions -v
cd apps/backend && go build ./...
```
Expected: PASS; build succeeds.

- [ ] **Step 6: Commit**

```bash
git add apps/backend/internal/http/handlers.go apps/backend/internal/http/router.go apps/backend/internal/http/admin_permissions_handler_test.go
git commit -m "feat(backend): expose GET /api/app/admin/permissions"
```

---

## Task 7: Add `memberCount` to `GET /api/app/admin/roles`

**Files:**
- Modify: `apps/backend/internal/admin/rbac/store.go`
- Test: `apps/backend/internal/admin/rbac/store_test.go`
- Modify: `apps/backend/internal/http/handlers.go` (`ListAdminRoles`)
- Modify: `apps/backend/internal/http/admin_read_handlers_test.go` if it asserts the roles shape, OR add a new test in the existing roles handler test file.

- [ ] **Step 1: Write the failing test (store layer)**

Append to `apps/backend/internal/admin/rbac/store_test.go`:

```go
func TestListRolesWithMemberCount(t *testing.T) {
    s, rb := newStoreHarness(t) // existing helper in store_test.go
    ctx := context.Background()
    if err := rb.SeedPermissionsAndRoles(ctx); err != nil { t.Fatalf("seed: %v", err) }

    // Find the moderator role and assign two distinct app_users to it.
    modID, err := rb.RoleIDByKey(ctx, rbac.RoleModerator)
    if err != nil { t.Fatal(err) }
    seedTestUser(t, s, "usr_lrwmc_a") // existing helper
    seedTestUser(t, s, "usr_lrwmc_b")
    if err := rb.AssignRole(ctx, "usr_lrwmc_a", modID, ""); err != nil { t.Fatal(err) }
    if err := rb.AssignRole(ctx, "usr_lrwmc_b", modID, ""); err != nil { t.Fatal(err) }
    t.Cleanup(func() {
        _, _ = s.Pool().Exec(context.Background(),
            `DELETE FROM admin_user_roles WHERE user_id IN ('usr_lrwmc_a','usr_lrwmc_b')`)
    })

    rolesWC, err := rb.ListRolesWithMemberCount(ctx)
    if err != nil { t.Fatalf("list: %v", err) }
    foundMod := false
    for _, r := range rolesWC {
        if r.Key == rbac.RoleModerator {
            foundMod = true
            if r.MemberCount < 2 {
                t.Errorf("moderator memberCount=%d, want >=2", r.MemberCount)
            }
        }
    }
    if !foundMod { t.Errorf("moderator role missing from result") }
}
```

> If `newStoreHarness` / `seedTestUser` don't exist with those names in the existing `store_test.go`, mirror whatever bootstrap pattern the existing tests in that file use. The point of the test is: assign N distinct users to a role, call the new method, observe `MemberCount >= N`.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd apps/backend && go test ./internal/admin/rbac/ -run TestListRolesWithMemberCount -v`
Expected: compile error (`undefined: ListRolesWithMemberCount`).

- [ ] **Step 3: Add the store method**

In `apps/backend/internal/admin/rbac/store.go`, add a new struct and method:

```go
// RoleWithMemberCount extends Role with the count of distinct app_users
// currently holding this role (admin_user_roles rows).
type RoleWithMemberCount struct {
    Role
    MemberCount int
}

func (s *Store) ListRolesWithMemberCount(ctx context.Context) ([]RoleWithMemberCount, error) {
    rows, err := s.pool.Query(ctx, `
        SELECT r.id, r.key, r.name, COALESCE(r.description, ''), r.is_system,
               COUNT(ur.user_id)::int AS member_count
        FROM admin_roles r
        LEFT JOIN admin_user_roles ur ON ur.role_id = r.id
        GROUP BY r.id, r.key, r.name, r.description, r.is_system
        ORDER BY r.key
    `)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    var out []RoleWithMemberCount
    for rows.Next() {
        var r RoleWithMemberCount
        if err := rows.Scan(&r.ID, &r.Key, &r.Name, &r.Description, &r.IsSystem, &r.MemberCount); err != nil {
            return nil, err
        }
        out = append(out, r)
    }
    return out, rows.Err()
}
```

- [ ] **Step 4: Modify `ListAdminRoles` handler**

In `apps/backend/internal/http/handlers.go`, modify the `adminRoleItem` struct and `ListAdminRoles`:

```go
type adminRoleItem struct {
    ID          string   `json:"id"`
    Key         string   `json:"key"`
    Name        string   `json:"name"`
    Description string   `json:"description"`
    IsSystem    bool     `json:"isSystem"`
    Permissions []string `json:"permissions"`
    MemberCount int      `json:"memberCount"`
}

func (h *Handler) ListAdminRoles(w http.ResponseWriter, r *http.Request) {
    roles, err := h.rbacStore.ListRolesWithMemberCount(r.Context())
    if err != nil {
        log.Printf("[backend] ListAdminRoles: ListRolesWithMemberCount error: %v", err)
        writeError(w, http.StatusInternalServerError, "failed to list roles")
        return
    }

    items := make([]adminRoleItem, 0, len(roles))
    for _, rl := range roles {
        var perms []string
        if rl.Key == rbac.RoleSuperadmin {
            all := rbac.AllPermissions()
            perms = make([]string, len(all))
            for i, p := range all {
                perms[i] = p.Key
            }
        } else {
            perms, err = h.rbacStore.PermissionsForRole(r.Context(), rl.ID)
            if err != nil {
                log.Printf("[backend] ListAdminRoles: PermissionsForRole(%s) error: %v", rl.Key, err)
                writeError(w, http.StatusInternalServerError, "failed to load role permissions")
                return
            }
        }
        if perms == nil {
            perms = []string{}
        }
        items = append(items, adminRoleItem{
            ID:          rl.ID.String(),
            Key:         rl.Key,
            Name:        rl.Name,
            Description: rl.Description,
            IsSystem:    rl.IsSystem,
            Permissions: perms,
            MemberCount: rl.MemberCount,
        })
    }

    writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
```

- [ ] **Step 5: Add a handler-level test for `memberCount`**

Append to whichever existing test file already covers `ListAdminRoles` (search with `grep -l "ListAdminRoles" apps/backend/internal/http/*_test.go`). If no test currently covers the roles endpoint, add a small one to `admin_permissions_handler_test.go` from Task 6:

```go
func TestListAdminRoles_IncludesMemberCount(t *testing.T) {
    ts := newAdminTestServer(t)
    // Setup: seed a user and assign moderator. (Use whatever helper the
    // surrounding tests already use; mirror its pattern.)
    // ...assignment setup omitted — copy from existing tests in the file...

    req := httptest.NewRequest("GET", "/api/app/admin/roles", nil)
    rec := httptest.NewRecorder()
    ts.handler.ServeHTTP(rec, req)

    if rec.Code != http.StatusOK {
        t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
    }
    var out struct {
        Items []struct {
            Key         string `json:"key"`
            MemberCount int    `json:"memberCount"`
        } `json:"items"`
    }
    if err := json.NewDecoder(rec.Body).Decode(&out); err != nil { t.Fatal(err) }
    sawField := false
    for _, r := range out.Items {
        // memberCount is always present (omitempty NOT set), so any non-error
        // decode + non-empty list confirms the shape.
        _ = r.MemberCount
        if r.Key != "" { sawField = true }
    }
    if !sawField {
        t.Errorf("expected at least one role in response")
    }
}
```

- [ ] **Step 6: Run tests + build**

```
cd apps/backend && TEST_POSTGRES_URL=postgres://rustlens:rustlens@localhost:5432/rustlens?sslmode=disable go test ./internal/admin/rbac/ -run TestListRolesWithMemberCount -v
cd apps/backend && go test ./internal/http/ -run "TestListAdmin" -v
cd apps/backend && go build ./...
```
Expected: PASS or SKIP; build succeeds.

- [ ] **Step 7: Commit**

```bash
git add apps/backend/internal/admin/rbac/store.go apps/backend/internal/admin/rbac/store_test.go apps/backend/internal/http/handlers.go apps/backend/internal/http/admin_permissions_handler_test.go
git commit -m "feat(backend): include memberCount in admin roles list"
```

---

## Task 8: OpenAPI documentation

**Files:**
- Modify: `apps/backend/internal/http/openapi.go`

- [ ] **Step 1: Add `/api/app/admin/permissions` path**

Open `apps/backend/internal/http/openapi.go`. Find the existing block that documents `"/api/app/admin/audit"` (around line 2142 per the earlier grep). Immediately after the closing of the `/api/app/admin/guilds` block, insert:

```go
"/api/app/admin/permissions": map[string]any{
    "get": map[string]any{
        "summary":     "List the canonical admin permission catalog",
        "description": "Returns every permission key registered in rbac.AllPermissions(), with its human-readable description. Static catalog; no DB read.",
        "tags":        []string{"admin"},
        "responses": map[string]any{
            "200": map[string]any{
                "description": "Permission catalog",
                "content": map[string]any{
                    "application/json": map[string]any{
                        "schema": map[string]any{
                            "type": "object",
                            "properties": map[string]any{
                                "items": map[string]any{
                                    "type": "array",
                                    "items": map[string]any{
                                        "type": "object",
                                        "required": []string{"key", "description"},
                                        "properties": map[string]any{
                                            "key":         map[string]any{"type": "string"},
                                            "description": map[string]any{"type": "string"},
                                        },
                                    },
                                },
                            },
                        },
                    },
                },
            },
        },
    },
},
```

- [ ] **Step 2: Update the existing `/api/app/admin/roles` response shape**

Search the same file for the `"/api/app/admin/roles"` block (around line 2062-area; if not present, add a new block alongside `/admin/permissions`). Locate the response item schema and ensure it includes `memberCount: integer` alongside the other fields. If the file does not currently document `/admin/roles`, add a stub like:

```go
"/api/app/admin/roles": map[string]any{
    "get": map[string]any{
        "summary": "List admin roles with permission and member counts",
        "tags":    []string{"admin"},
        "responses": map[string]any{
            "200": map[string]any{
                "description": "Roles",
                "content": map[string]any{
                    "application/json": map[string]any{
                        "schema": map[string]any{
                            "type": "object",
                            "properties": map[string]any{
                                "items": map[string]any{
                                    "type": "array",
                                    "items": map[string]any{
                                        "type": "object",
                                        "required": []string{"id", "key", "name", "isSystem", "permissions", "memberCount"},
                                        "properties": map[string]any{
                                            "id":          map[string]any{"type": "string"},
                                            "key":         map[string]any{"type": "string"},
                                            "name":        map[string]any{"type": "string"},
                                            "description": map[string]any{"type": "string"},
                                            "isSystem":    map[string]any{"type": "boolean"},
                                            "permissions": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
                                            "memberCount": map[string]any{"type": "integer"},
                                        },
                                    },
                                },
                            },
                        },
                    },
                },
            },
        },
    },
},
```

- [ ] **Step 3: Run build to verify the file still compiles**

Run: `cd apps/backend && go build ./...`
Expected: build succeeds.

- [ ] **Step 4: Commit**

```bash
git add apps/backend/internal/http/openapi.go
git commit -m "docs(backend): document admin permissions endpoint and roles memberCount"
```

---

## Task 9: Frontend lib helpers

**Files:**
- Modify: `apps/frontend/lib/rustlens-api.ts`

- [ ] **Step 1: Extend types**

In `apps/frontend/lib/rustlens-api.ts`, modify the `AdminRoleItem` type (around line 151) to add `memberCount: number;` and add a new type below it:

```ts
export type AdminRoleItem = {
    id: string;
    key: string;
    name: string;
    description: string | null;
    isSystem: boolean;
    permissions: string[];
    memberCount: number;
};

export type AdminPermissionItem = {
    key: string;
    description: string;
};
```

- [ ] **Step 2: Add `getAdminPermissions`**

Directly below the existing `getAdminRoles` function, add:

```ts
export async function getAdminPermissions() {
    const payload = await backendFetch<{ items: AdminPermissionItem[] }>(
        "/api/app/admin/permissions",
        undefined,
        { requireSession: true },
    );
    return payload.items;
}
```

- [ ] **Step 3: Add `postAdminAction` helper**

Anywhere after `backendFetch` (alphabetical placement is fine), add:

```ts
type AdminActionEnvelope = {
    params: Record<string, unknown>;
    reason: string;
};

type AdminActionResponse<T> = { ok: true; result: T };

/**
 * Posts to the admin action dispatcher. Returns the action's result payload
 * (the inner `result` field from the standard `{ok, result}` envelope).
 *
 * Server-only helper; consumed by Server Actions in app/admin/.../actions.ts.
 */
export async function postAdminAction<T = unknown>(
    type: string,
    params: Record<string, unknown>,
    reason: string,
): Promise<T> {
    const envelope: AdminActionEnvelope = { params, reason };
    const payload = await backendFetch<AdminActionResponse<T>>(
        `/api/app/admin/actions/${encodeURIComponent(type)}`,
        { method: "POST", body: JSON.stringify(envelope) },
        { requireSession: true },
    );
    return payload.result;
}
```

- [ ] **Step 4: Add typed mutation wrappers**

At the bottom of the admin section in the same file, add:

```ts
export type CreateAdminRoleInput = {
    key: string;
    name: string;
    description: string;
    permissions: string[];
};

export async function createAdminRole(input: CreateAdminRoleInput, reason: string) {
    return postAdminAction<{ id: string; key: string }>("roles.create", input, reason);
}

export async function updateAdminRole(
    id: string,
    patch: { name: string; description: string },
    reason: string,
) {
    return postAdminAction<{ id: string }>("roles.update", { id, ...patch }, reason);
}

export async function setAdminRolePermissions(id: string, permissions: string[], reason: string) {
    return postAdminAction<{ id: string; count: number }>(
        "roles.permissions.set",
        { id, permissions },
        reason,
    );
}

export async function deleteAdminRole(id: string, reason: string) {
    return postAdminAction<{ id: string }>("roles.delete", { id }, reason);
}
```

- [ ] **Step 5: Type-check**

Run: `cd apps/frontend && pnpm exec tsc --noEmit`
Expected: no type errors.

- [ ] **Step 6: Commit**

```bash
git add apps/frontend/lib/rustlens-api.ts
git commit -m "feat(frontend): add admin role/permission API helpers"
```

---

## Task 10: Server Actions for the page

**Files:**
- Create: `apps/frontend/app/admin/roles/actions.ts`

- [ ] **Step 1: Create the Server Actions file**

Create `apps/frontend/app/admin/roles/actions.ts`:

```ts
"use server";

import { revalidatePath } from "next/cache";

import {
    createAdminRole,
    deleteAdminRole,
    setAdminRolePermissions,
    updateAdminRole,
    type CreateAdminRoleInput,
} from "@/lib/rustlens-api";
import { requirePermission } from "@/lib/session";

const ROLES_PATH = "/admin/roles";

export type SaveRoleResult =
    | { ok: true }
    | { ok: false; error: string };

export async function createRoleAction(
    input: CreateAdminRoleInput,
    reason: string,
): Promise<SaveRoleResult> {
    await requirePermission("admins.grant");
    if (!reason.trim()) return { ok: false, error: "reason_required" };
    try {
        await createAdminRole(input, reason.trim());
        revalidatePath(ROLES_PATH);
        return { ok: true };
    } catch (err) {
        return { ok: false, error: (err as Error).message };
    }
}

export async function updateRoleAction(
    id: string,
    patch: { name: string; description: string },
    reason: string,
): Promise<SaveRoleResult> {
    await requirePermission("admins.grant");
    if (!reason.trim()) return { ok: false, error: "reason_required" };
    try {
        await updateAdminRole(id, patch, reason.trim());
        revalidatePath(ROLES_PATH);
        return { ok: true };
    } catch (err) {
        return { ok: false, error: (err as Error).message };
    }
}

export async function setRolePermissionsAction(
    id: string,
    permissions: string[],
    reason: string,
): Promise<SaveRoleResult> {
    await requirePermission("admins.grant");
    if (!reason.trim()) return { ok: false, error: "reason_required" };
    try {
        await setAdminRolePermissions(id, permissions, reason.trim());
        revalidatePath(ROLES_PATH);
        return { ok: true };
    } catch (err) {
        return { ok: false, error: (err as Error).message };
    }
}

export async function deleteRoleAction(id: string, reason: string): Promise<SaveRoleResult> {
    await requirePermission("admins.grant");
    if (!reason.trim()) return { ok: false, error: "reason_required" };
    try {
        await deleteAdminRole(id, reason.trim());
        revalidatePath(ROLES_PATH);
        return { ok: true };
    } catch (err) {
        return { ok: false, error: (err as Error).message };
    }
}
```

- [ ] **Step 2: Type-check**

Run: `cd apps/frontend && pnpm exec tsc --noEmit`
Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add apps/frontend/app/admin/roles/actions.ts
git commit -m "feat(frontend): add server actions for admin role mutations"
```

---

## Task 11: Permissions grid component

**Files:**
- Create: `apps/frontend/app/admin/roles/_components/role-permissions-grid.tsx`

- [ ] **Step 1: Implement the component**

Create the file:

```tsx
"use client";

import { Checkbox } from "@/components/ui/checkbox";
import { Label } from "@/components/ui/label";
import type { AdminPermissionItem } from "@/lib/rustlens-api";

type Props = {
    catalog: AdminPermissionItem[];
    selected: Set<string>;
    onToggle: (key: string, on: boolean) => void;
    disabled?: boolean;
};

function groupByNamespace(catalog: AdminPermissionItem[]) {
    const groups = new Map<string, AdminPermissionItem[]>();
    for (const p of catalog) {
        const ns = p.key.split(".")[0] ?? "other";
        const arr = groups.get(ns);
        if (arr) arr.push(p);
        else groups.set(ns, [p]);
    }
    // Stable order: alphabetical group, then alphabetical within group.
    return [...groups.entries()]
        .sort((a, b) => a[0].localeCompare(b[0]))
        .map(([ns, items]) => ({
            ns,
            items: [...items].sort((a, b) => a.key.localeCompare(b.key)),
        }));
}

export function RolePermissionsGrid({ catalog, selected, onToggle, disabled = false }: Props) {
    const groups = groupByNamespace(catalog);
    return (
        <div className="flex flex-col gap-6">
            {groups.map((g) => (
                <section key={g.ns} className="flex flex-col gap-2">
                    <h3 className="text-sm font-semibold uppercase tracking-wide text-muted-foreground">
                        {g.ns}
                    </h3>
                    <ul className="flex flex-col gap-2">
                        {g.items.map((p) => {
                            const id = `perm-${p.key}`;
                            return (
                                <li key={p.key} className="flex items-start gap-3">
                                    <Checkbox
                                        id={id}
                                        checked={selected.has(p.key)}
                                        disabled={disabled}
                                        onCheckedChange={(v) => onToggle(p.key, v === true)}
                                    />
                                    <Label htmlFor={id} className="flex flex-col gap-0.5 leading-tight">
                                        <code className="text-xs font-mono">{p.key}</code>
                                        <span className="text-xs text-muted-foreground">{p.description}</span>
                                    </Label>
                                </li>
                            );
                        })}
                    </ul>
                </section>
            ))}
        </div>
    );
}
```

- [ ] **Step 2: Type-check**

Run: `cd apps/frontend && pnpm exec tsc --noEmit`
Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add apps/frontend/app/admin/roles/_components/role-permissions-grid.tsx
git commit -m "feat(frontend): add permission checkbox grid for role drawer"
```

---

## Task 12: Delete confirmation dialog

**Files:**
- Create: `apps/frontend/app/admin/roles/_components/delete-role-dialog.tsx`

- [ ] **Step 1: Implement**

```tsx
"use client";

import * as React from "react";

import { Button } from "@/components/ui/button";
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";

type Role = { id: string; name: string; memberCount: number };

type Props = {
    role: Role | null;
    open: boolean;
    onOpenChange: (open: boolean) => void;
    onConfirm: (reason: string) => Promise<void>;
};

export function DeleteRoleDialog({ role, open, onOpenChange, onConfirm }: Props) {
    const [reason, setReason] = React.useState("");
    const [submitting, setSubmitting] = React.useState(false);

    React.useEffect(() => {
        if (open) setReason("");
    }, [open]);

    if (!role) return null;

    const submit = async () => {
        if (!reason.trim()) return;
        setSubmitting(true);
        try {
            await onConfirm(reason.trim());
            onOpenChange(false);
        } finally {
            setSubmitting(false);
        }
    };

    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent>
                <DialogHeader>
                    <DialogTitle>Delete role &ldquo;{role.name}&rdquo;?</DialogTitle>
                    <DialogDescription>
                        {role.memberCount > 0
                            ? `This will also remove the role from ${role.memberCount} user${role.memberCount === 1 ? "" : "s"}.`
                            : "This role is not currently assigned to any user."}
                    </DialogDescription>
                </DialogHeader>
                <div className="flex flex-col gap-2">
                    <Label htmlFor="delete-role-reason">Reason</Label>
                    <Textarea
                        id="delete-role-reason"
                        placeholder="Required. Recorded in the admin audit log."
                        value={reason}
                        onChange={(e) => setReason(e.target.value)}
                    />
                </div>
                <DialogFooter>
                    <Button variant="outline" onClick={() => onOpenChange(false)}>
                        Cancel
                    </Button>
                    <Button
                        variant="destructive"
                        disabled={!reason.trim() || submitting}
                        onClick={submit}
                    >
                        {submitting ? "Deleting…" : "Delete"}
                    </Button>
                </DialogFooter>
            </DialogContent>
        </Dialog>
    );
}
```

- [ ] **Step 2: Type-check**

Run: `cd apps/frontend && pnpm exec tsc --noEmit`
Expected: no errors. (If `Textarea` is not a registered shadcn primitive, run `pnpm dlx shadcn@latest add textarea` from the frontend dir first.)

- [ ] **Step 3: Commit**

```bash
git add apps/frontend/app/admin/roles/_components/delete-role-dialog.tsx
git commit -m "feat(frontend): add delete-role confirmation dialog"
```

---

## Task 13: Role drawer (create + edit)

**Files:**
- Create: `apps/frontend/app/admin/roles/_components/role-drawer.tsx`

- [ ] **Step 1: Implement**

```tsx
"use client";

import * as React from "react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
    Sheet,
    SheetContent,
    SheetDescription,
    SheetFooter,
    SheetHeader,
    SheetTitle,
} from "@/components/ui/sheet";
import { Textarea } from "@/components/ui/textarea";
import type { AdminPermissionItem, AdminRoleItem } from "@/lib/rustlens-api";

import { RolePermissionsGrid } from "./role-permissions-grid";

type Mode = "create" | "edit";

type SubmitInput = {
    mode: Mode;
    role: AdminRoleItem | null;
    next: { key: string; name: string; description: string; permissions: string[] };
    reason: string;
};

type Props = {
    open: boolean;
    onOpenChange: (open: boolean) => void;
    mode: Mode;
    role: AdminRoleItem | null;
    catalog: AdminPermissionItem[];
    onSubmit: (input: SubmitInput) => Promise<void>;
};

const KEY_RE = /^[a-z][a-z0-9_]{1,63}$/;

export function RoleDrawer({ open, onOpenChange, mode, role, catalog, onSubmit }: Props) {
    const isSystem = mode === "edit" && role?.isSystem === true;
    const readOnly = isSystem;

    const [key, setKey] = React.useState("");
    const [name, setName] = React.useState("");
    const [description, setDescription] = React.useState("");
    const [perms, setPerms] = React.useState<Set<string>>(new Set());
    const [reason, setReason] = React.useState("");
    const [submitting, setSubmitting] = React.useState(false);
    const [error, setError] = React.useState<string | null>(null);

    React.useEffect(() => {
        if (!open) return;
        setError(null);
        setReason("");
        if (mode === "create") {
            setKey("");
            setName("");
            setDescription("");
            setPerms(new Set());
        } else if (role) {
            setKey(role.key);
            setName(role.name);
            setDescription(role.description ?? "");
            setPerms(new Set(role.permissions));
        }
    }, [open, mode, role]);

    const togglePerm = (k: string, on: boolean) => {
        setPerms((cur) => {
            const next = new Set(cur);
            if (on) next.add(k);
            else next.delete(k);
            return next;
        });
    };

    const validate = (): string | null => {
        if (mode === "create" && !KEY_RE.test(key)) {
            return "Key must match ^[a-z][a-z0-9_]{1,63}$";
        }
        if (!name.trim() || name.length > 80) return "Name is required (max 80 chars)";
        if (description.length > 500) return "Description too long (max 500 chars)";
        if (!reason.trim()) return "Reason is required";
        return null;
    };

    const submit = async () => {
        const v = validate();
        if (v) {
            setError(v);
            return;
        }
        setError(null);
        setSubmitting(true);
        try {
            await onSubmit({
                mode,
                role,
                next: {
                    key: key.trim(),
                    name: name.trim(),
                    description: description.trim(),
                    permissions: [...perms],
                },
                reason: reason.trim(),
            });
            onOpenChange(false);
        } catch (err) {
            setError((err as Error).message);
        } finally {
            setSubmitting(false);
        }
    };

    return (
        <Sheet open={open} onOpenChange={onOpenChange}>
            <SheetContent className="flex w-full flex-col gap-4 sm:max-w-2xl">
                <SheetHeader>
                    <SheetTitle>
                        {mode === "create" ? "New role" : isSystem ? `${role?.name} (system)` : `Edit ${role?.name}`}
                    </SheetTitle>
                    {isSystem ? (
                        <SheetDescription>
                            System role. Read-only — system roles are part of the application&apos;s
                            built-in trust model and cannot be modified.
                        </SheetDescription>
                    ) : null}
                </SheetHeader>

                <div className="flex flex-col gap-4 overflow-y-auto pr-2">
                    <div className="grid gap-2">
                        <Label htmlFor="role-key">Key</Label>
                        <Input
                            id="role-key"
                            value={key}
                            disabled={readOnly || mode === "edit"}
                            onChange={(e) => setKey(e.target.value)}
                            placeholder="snake_case_only"
                        />
                        {mode === "edit" ? (
                            <p className="text-xs text-muted-foreground">Key is immutable post-create.</p>
                        ) : null}
                    </div>
                    <div className="grid gap-2">
                        <Label htmlFor="role-name">Name</Label>
                        <Input
                            id="role-name"
                            value={name}
                            disabled={readOnly}
                            onChange={(e) => setName(e.target.value)}
                        />
                    </div>
                    <div className="grid gap-2">
                        <Label htmlFor="role-description">Description</Label>
                        <Textarea
                            id="role-description"
                            value={description}
                            disabled={readOnly}
                            onChange={(e) => setDescription(e.target.value)}
                        />
                    </div>

                    <div className="grid gap-2">
                        <Label>Permissions</Label>
                        <RolePermissionsGrid
                            catalog={catalog}
                            selected={perms}
                            onToggle={togglePerm}
                            disabled={readOnly}
                        />
                    </div>

                    {!readOnly ? (
                        <div className="grid gap-2 border-t pt-4">
                            <Label htmlFor="role-reason">Reason</Label>
                            <Textarea
                                id="role-reason"
                                placeholder="Required. Recorded in the admin audit log."
                                value={reason}
                                onChange={(e) => setReason(e.target.value)}
                            />
                        </div>
                    ) : null}

                    {error ? <p className="text-sm text-destructive">{error}</p> : null}
                </div>

                <SheetFooter>
                    <Button variant="outline" onClick={() => onOpenChange(false)}>
                        Close
                    </Button>
                    {!readOnly ? (
                        <Button onClick={submit} disabled={submitting}>
                            {submitting ? "Saving…" : "Save"}
                        </Button>
                    ) : null}
                </SheetFooter>
            </SheetContent>
        </Sheet>
    );
}
```

- [ ] **Step 2: Type-check**

Run: `cd apps/frontend && pnpm exec tsc --noEmit`
Expected: no errors. (If `Sheet` is not yet a registered shadcn primitive, run `pnpm dlx shadcn@latest add sheet` from the frontend dir first.)

- [ ] **Step 3: Commit**

```bash
git add apps/frontend/app/admin/roles/_components/role-drawer.tsx
git commit -m "feat(frontend): add role drawer for create + edit"
```

---

## Task 14: Roles table + page-level wiring

**Files:**
- Create: `apps/frontend/app/admin/roles/_components/roles-table.tsx`
- Create: `apps/frontend/app/admin/roles/_components/roles-page.tsx`

- [ ] **Step 1: Implement the table**

Create `apps/frontend/app/admin/roles/_components/roles-table.tsx`:

```tsx
"use client";

import * as React from "react";
import { MoreHorizontalIcon, PencilIcon, EyeIcon, Trash2Icon } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
    DropdownMenu,
    DropdownMenuContent,
    DropdownMenuItem,
    DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
    Table,
    TableBody,
    TableCell,
    TableHead,
    TableHeader,
    TableRow,
} from "@/components/ui/table";
import type { AdminRoleItem } from "@/lib/rustlens-api";

type Props = {
    roles: AdminRoleItem[];
    onEdit: (role: AdminRoleItem) => void;
    onDelete: (role: AdminRoleItem) => void;
};

export function RolesTable({ roles, onEdit, onDelete }: Props) {
    return (
        <div className="rounded-md border">
            <Table>
                <TableHeader>
                    <TableRow>
                        <TableHead>Name</TableHead>
                        <TableHead>Key</TableHead>
                        <TableHead>Description</TableHead>
                        <TableHead>System</TableHead>
                        <TableHead className="text-right">Permissions</TableHead>
                        <TableHead className="text-right">Members</TableHead>
                        <TableHead className="w-12" aria-label="Actions" />
                    </TableRow>
                </TableHeader>
                <TableBody>
                    {roles.map((r) => (
                        <TableRow key={r.id}>
                            <TableCell className="font-medium">{r.name}</TableCell>
                            <TableCell>
                                <code className="text-xs font-mono">{r.key}</code>
                            </TableCell>
                            <TableCell className="max-w-[36ch] truncate" title={r.description ?? ""}>
                                {r.description}
                            </TableCell>
                            <TableCell>{r.isSystem ? <Badge variant="secondary">System</Badge> : null}</TableCell>
                            <TableCell className="text-right tabular-nums">{r.permissions.length}</TableCell>
                            <TableCell className="text-right tabular-nums">{r.memberCount}</TableCell>
                            <TableCell>
                                <DropdownMenu>
                                    <DropdownMenuTrigger asChild>
                                        <Button variant="ghost" size="icon" aria-label={`Actions for ${r.name}`}>
                                            <MoreHorizontalIcon />
                                        </Button>
                                    </DropdownMenuTrigger>
                                    <DropdownMenuContent align="end">
                                        <DropdownMenuItem onClick={() => onEdit(r)}>
                                            {r.isSystem ? (
                                                <><EyeIcon className="mr-2 size-4" /> View</>
                                            ) : (
                                                <><PencilIcon className="mr-2 size-4" /> Edit</>
                                            )}
                                        </DropdownMenuItem>
                                        {r.isSystem ? null : (
                                            <DropdownMenuItem
                                                onClick={() => onDelete(r)}
                                                className="text-destructive"
                                            >
                                                <Trash2Icon className="mr-2 size-4" /> Delete
                                            </DropdownMenuItem>
                                        )}
                                    </DropdownMenuContent>
                                </DropdownMenu>
                            </TableCell>
                        </TableRow>
                    ))}
                </TableBody>
            </Table>
        </div>
    );
}
```

- [ ] **Step 2: Implement the page-level client component**

Create `apps/frontend/app/admin/roles/_components/roles-page.tsx`:

```tsx
"use client";

import * as React from "react";
import { useRouter } from "next/navigation";
import { PlusIcon } from "lucide-react";

import { Button } from "@/components/ui/button";
import type { AdminPermissionItem, AdminRoleItem } from "@/lib/rustlens-api";

import {
    createRoleAction,
    deleteRoleAction,
    setRolePermissionsAction,
    updateRoleAction,
} from "../actions";
import { DeleteRoleDialog } from "./delete-role-dialog";
import { RoleDrawer } from "./role-drawer";
import { RolesTable } from "./roles-table";

type Props = {
    initialRoles: AdminRoleItem[];
    permissionsCatalog: AdminPermissionItem[];
};

type DrawerState =
    | { open: false }
    | { open: true; mode: "create"; role: null }
    | { open: true; mode: "edit"; role: AdminRoleItem };

export function RolesPage({ initialRoles, permissionsCatalog }: Props) {
    const router = useRouter();
    const [drawer, setDrawer] = React.useState<DrawerState>({ open: false });
    const [deleting, setDeleting] = React.useState<AdminRoleItem | null>(null);
    const [error, setError] = React.useState<string | null>(null);

    const onSubmit = async (input: Parameters<React.ComponentProps<typeof RoleDrawer>["onSubmit"]>[0]) => {
        setError(null);
        if (input.mode === "create") {
            const res = await createRoleAction(
                {
                    key: input.next.key,
                    name: input.next.name,
                    description: input.next.description,
                    permissions: input.next.permissions,
                },
                input.reason,
            );
            if (!res.ok) throw new Error(res.error);
            router.refresh();
            return;
        }
        // Edit: post update + permissions.set sequentially. Reuse one reason.
        if (!input.role) return;
        const role = input.role;
        const nameChanged = role.name !== input.next.name;
        const descChanged = (role.description ?? "") !== input.next.description;
        const oldPerms = [...role.permissions].sort().join(",");
        const newPerms = [...input.next.permissions].sort().join(",");
        const permsChanged = oldPerms !== newPerms;

        if (nameChanged || descChanged) {
            const r = await updateRoleAction(
                role.id,
                { name: input.next.name, description: input.next.description },
                input.reason,
            );
            if (!r.ok) throw new Error(r.error);
        }
        if (permsChanged) {
            const r = await setRolePermissionsAction(role.id, input.next.permissions, input.reason);
            if (!r.ok) throw new Error(r.error);
        }
        router.refresh();
    };

    const onDeleteConfirm = async (reason: string) => {
        if (!deleting) return;
        const res = await deleteRoleAction(deleting.id, reason);
        if (!res.ok) {
            setError(res.error);
            return;
        }
        router.refresh();
    };

    return (
        <div className="flex flex-col gap-4">
            <div className="flex items-center justify-between">
                <div />
                <Button onClick={() => setDrawer({ open: true, mode: "create", role: null })}>
                    <PlusIcon className="mr-2 size-4" />
                    New role
                </Button>
            </div>

            {error ? <p className="text-sm text-destructive">{error}</p> : null}

            <RolesTable
                roles={initialRoles}
                onEdit={(role) => setDrawer({ open: true, mode: "edit", role })}
                onDelete={(role) => setDeleting(role)}
            />

            <RoleDrawer
                open={drawer.open}
                mode={drawer.open ? drawer.mode : "create"}
                role={drawer.open && drawer.mode === "edit" ? drawer.role : null}
                catalog={permissionsCatalog}
                onOpenChange={(open) => setDrawer(open ? drawer : { open: false })}
                onSubmit={onSubmit}
            />

            <DeleteRoleDialog
                role={deleting}
                open={deleting !== null}
                onOpenChange={(open) => {
                    if (!open) setDeleting(null);
                }}
                onConfirm={onDeleteConfirm}
            />
        </div>
    );
}
```

- [ ] **Step 3: Type-check**

Run: `cd apps/frontend && pnpm exec tsc --noEmit`
Expected: no errors. (If `DropdownMenu` isn't yet in the registry, run `pnpm dlx shadcn@latest add dropdown-menu`.)

- [ ] **Step 4: Commit**

```bash
git add apps/frontend/app/admin/roles/_components/roles-table.tsx apps/frontend/app/admin/roles/_components/roles-page.tsx
git commit -m "feat(frontend): add admin roles table and page wiring"
```

---

## Task 15: Page route

**Files:**
- Create: `apps/frontend/app/admin/roles/page.tsx`

- [ ] **Step 1: Implement the page**

Create `apps/frontend/app/admin/roles/page.tsx`:

```tsx
import { AppSidebar } from "../_components/app-sidebar";
import { RolesPage } from "./_components/roles-page";
import { SiteHeader } from "@/components/site-header";
import { SidebarInset, SidebarProvider } from "@/components/ui/sidebar";
import { getCsrfToken } from "@/lib/csrf";
import {
    getAdminPermissions,
    getAdminRoles,
    getCurrentUserSafe,
} from "@/lib/rustlens-api";
import { requirePermission } from "@/lib/session";

export default async function AdminRolesPage() {
    const session = await requirePermission("admins.grant");

    const [csrfToken, currentUser, roles, permissionsCatalog] = await Promise.all([
        getCsrfToken(),
        getCurrentUserSafe(),
        getAdminRoles(),
        getAdminPermissions(),
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
                                <h1 className="text-2xl font-semibold">Roles</h1>
                                <p className="text-sm text-muted-foreground">
                                    Create and manage admin roles. System roles are read-only.
                                </p>
                            </div>
                            <RolesPage initialRoles={roles} permissionsCatalog={permissionsCatalog} />
                        </div>
                    </div>
                </div>
            </SidebarInset>
        </SidebarProvider>
    );
}
```

- [ ] **Step 2: Type-check**

Run: `cd apps/frontend && pnpm exec tsc --noEmit`
Expected: errors only on the `permissions` prop to `AppSidebar` (because we haven't wired the prop yet — fixed in Task 16).

- [ ] **Step 3: Commit**

```bash
git add apps/frontend/app/admin/roles/page.tsx
git commit -m "feat(frontend): add /admin/roles page route"
```

---

## Task 16: Sidebar permission threading + Roles entry

**Files:**
- Modify: `apps/frontend/components/dashboard-app-sidebar.tsx`
- Modify: `apps/frontend/app/admin/_components/app-sidebar.tsx`
- Modify: `apps/frontend/app/admin/page.tsx`
- Modify: `apps/frontend/app/admin/guilds/page.tsx`

- [ ] **Step 1: Add `permissions` prop to `DashboardAppSidebar` and render the Access group**

In `apps/frontend/components/dashboard-app-sidebar.tsx`:

1. Add `KeyRoundIcon` to the lucide import block.
2. Extend `DashboardAppSidebarProps`:

```ts
type DashboardAppSidebarProps = Omit<
    React.ComponentProps<typeof Sidebar>,
    "variant"
> & {
    area: DashboardSidebarArea;
    user: SidebarNavUserData;
    csrfToken: string;
    permissions?: string[];
    sidebarVariant?: React.ComponentProps<typeof Sidebar>["variant"];
};
```

3. Destructure `permissions = []` in the function signature, alongside `area`, etc.
4. Replace the current admin branch:

```tsx
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
) : ( ... )}
```

with:

```tsx
{area === "admin" ? (
    <>
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
        {permissions.includes("admins.grant") ? (
            <NavGroup
                label="Access"
                items={[
                    {
                        title: "Roles",
                        path: "/admin/roles",
                        icon: KeyRoundIcon,
                        isActive: pathname === "/admin/roles" || pathname.startsWith("/admin/roles/"),
                    },
                ]}
            />
        ) : null}
    </>
) : ( ... )}
```

- [ ] **Step 2: Forward `permissions` through `app/admin/_components/app-sidebar.tsx`**

Replace the file's content with:

```tsx
"use client";

import * as React from "react";

import {
    DashboardAppSidebar,
} from "@/components/dashboard-app-sidebar";
import type { SidebarNavUser } from "@/components/sidebar-nav-user";
import type { Sidebar } from "@/components/ui/sidebar";

type AppSidebarProps = Omit<React.ComponentProps<typeof Sidebar>, "variant"> & {
    user: SidebarNavUser;
    csrfToken: string;
    permissions?: string[];
    variant?: React.ComponentProps<typeof Sidebar>["variant"];
};

export function AppSidebar({ variant, ...props }: AppSidebarProps) {
    return <DashboardAppSidebar area="admin" sidebarVariant={variant} {...props} />;
}
```

- [ ] **Step 3: Pass `permissions` from existing admin pages**

In `apps/frontend/app/admin/page.tsx`, change the `<AppSidebar>` line:

```tsx
<AppSidebar
    csrfToken={csrfToken}
    user={user}
    permissions={session.permissions ?? []}
    variant="inset"
/>
```

Apply the same change to `apps/frontend/app/admin/guilds/page.tsx`.

(The `/admin/roles` page already passes `permissions` in Task 15.)

- [ ] **Step 4: Type-check + boot the dev server**

```
cd apps/frontend && pnpm exec tsc --noEmit
make dev
```

Open `http://localhost/admin/roles` while logged in as a superadmin and verify:
- The Roles entry appears under the Access group in the sidebar.
- The page loads, lists all 7+ roles, system roles show the badge, action menu Edit on a system role opens the drawer in read-only mode, action menu Edit on a custom role opens it editable.
- Logging in as a non-`admins.grant` user causes the Access group not to render and direct visit to `/admin/roles` redirects to `/admin`.

- [ ] **Step 5: Commit**

```bash
git add apps/frontend/components/dashboard-app-sidebar.tsx apps/frontend/app/admin/_components/app-sidebar.tsx apps/frontend/app/admin/page.tsx apps/frontend/app/admin/guilds/page.tsx
git commit -m "feat(frontend): add Access > Roles sidebar entry gated by admins.grant"
```

---

## Task 17: Documentation updates

**Files:**
- Modify: `AGENTS.md` (root)
- Modify: `apps/backend/AGENTS.md`

- [ ] **Step 1: Root AGENTS.md**

Find the line that documents `/admin/guilds` in the frontend "Routing/Pages" section (around the listing under `apps/frontend/`):

```
admin/guilds/page.tsx               # Admin Discord/Guilds list (paginated bot_guilds with name+ID+owner search)
```

Insert immediately after it:

```
admin/roles/page.tsx                # Admin RBAC management — create/edit/delete custom roles, view system roles read-only (admins.grant only)
```

In the "Key patterns" list under the backend section, find the bullet describing `GET /api/app/admin/guilds` and add a new bullet directly after it:

```
- The admin `GET /api/app/admin/permissions` route returns the canonical permission catalog from `rbac.AllPermissions()` (key + description). Static; no DB read. Used by the `/admin/roles` page to render the permission checkbox grid.
- The admin `GET /api/app/admin/roles` response now includes `memberCount` per role (count of distinct `admin_user_roles` rows). Permissions array still resolves to all-permissions for `superadmin` and to `admin_role_permissions` rows for everyone else.
- Admin role mutations go through the action dispatcher: `roles.create`, `roles.update`, `roles.delete`, `roles.permissions.set`. All four require `admins.grant` and a `reason`. The three mutating actions (`update`, `delete`, `permissions.set`) reject roles with `is_system=true`. Deletion cascades to `admin_role_permissions` and `admin_user_roles` via FK.
```

- [ ] **Step 2: apps/backend/AGENTS.md**

This file is currently a one-line `AGENTS.md` reference. If it has additional content, mirror the same backend bullets from Step 1; otherwise leave as-is. Inspect with `cat apps/backend/AGENTS.md` first.

- [ ] **Step 3: Commit**

```bash
git add AGENTS.md apps/backend/AGENTS.md
git commit -m "docs: document /admin/roles page and admin role action types"
```

---

## Self-review checklist (run before handing off)

After all 17 tasks land, do a final verification pass:

- [ ] Backend builds: `cd apps/backend && go build ./...`
- [ ] Backend vet clean: `cd apps/backend && go vet ./...`
- [ ] Backend tests (with Postgres available): `cd apps/backend && TEST_POSTGRES_URL=… go test ./internal/admin/... ./internal/http/...`
- [ ] Frontend type-check: `cd apps/frontend && pnpm exec tsc --noEmit`
- [ ] Frontend lint: `cd apps/frontend && pnpm lint`
- [ ] Manual smoke: `make dev`, log in as superadmin, create a custom role with 3 permissions, edit name + permissions, delete it. Verify each operation appears in `audit_events` (`SELECT action_type, resource_id, reason FROM audit_events WHERE action_type LIKE 'roles.%' ORDER BY created_at DESC LIMIT 10;`).
- [ ] Manual smoke: as a non-admin, hit `/admin/roles` directly → should redirect; sidebar should not show the Access group.
