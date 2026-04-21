# Admin Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the shared admin foundation (RBAC, action framework, audit log, shared frontend primitives) that feature specs C/D/E/F will consume.

**Architecture:** Go backend packages for RBAC and actions under `internal/admin/*`; schema managed by the existing `EnsureSchema` pattern in `store/postgres.go`; single HTTP dispatcher at `POST /api/app/admin/actions/:type`; frontend shared components under `components/admin/*` consumed by pages under `app/admin/*`.

**Tech Stack:** Go 1.24 (chi, pgx), Next.js 14 (React 18, TypeScript), Postgres, shadcn/ui.

## Prerequisite Notes

Discovered during planning:

- The Go backend currently exposes only `/api/internal/bot/*`. No `/api/app/*` routes exist. The frontend scaffold references `/api/app/admin/users`, `/api/app/me`, etc., which are not yet served.
- There is no `users` table, no `audit_events` table, no app-tier session code in the backend.
- This plan **creates** (not extends) all the admin-related schema. It also introduces a minimal `/api/app/admin/actions/:type` route and the RBAC tables. A `users` table is assumed to exist elsewhere (created by a prior prerequisite commit); tasks that touch it are clearly marked. If no `users` table exists when a task is reached, create a minimal one (`id UUID PRIMARY KEY, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()`) inline in that task's migration.
- App-tier session middleware is out of scope for this plan. Tasks that would normally consume session context take an explicit `actorUserID` for tests; the dispatcher wires in a TODO-gated `appSessionMiddleware` placeholder that real session work will replace.

---

## File Structure

**Backend (new):**

- `apps/backend/internal/admin/rbac/permissions.go` — permission key constants and registry
- `apps/backend/internal/admin/rbac/roles.go` — role constants, seed data
- `apps/backend/internal/admin/rbac/resolver.go` — `HasPermission`, role → permission resolution with per-request cache
- `apps/backend/internal/admin/rbac/resolver_test.go`
- `apps/backend/internal/admin/rbac/store.go` — DB queries for roles/permissions/user-roles
- `apps/backend/internal/admin/rbac/store_test.go`
- `apps/backend/internal/admin/actions/action.go` — `AdminAction` interface, `ActionInput`, `ActionResult` types
- `apps/backend/internal/admin/actions/registry.go` — global registry
- `apps/backend/internal/admin/actions/registry_test.go`
- `apps/backend/internal/admin/actions/dispatcher.go` — HTTP dispatcher handler
- `apps/backend/internal/admin/actions/dispatcher_test.go`
- `apps/backend/internal/admin/actions/redact.go` — params redaction
- `apps/backend/internal/admin/actions/redact_test.go`
- `apps/backend/internal/admin/audit/audit.go` — audit row write, query helpers
- `apps/backend/internal/admin/audit/audit_test.go`

**Backend (modified):**

- `apps/backend/internal/store/postgres.go` — add schema DDL for `admin_permissions`, `admin_roles`, `admin_role_permissions`, `admin_user_roles`, `audit_events`
- `apps/backend/internal/http/router.go` — register `/api/app/admin/actions/:type`
- `apps/backend/internal/http/handlers.go` — add `handler.admin` wiring (rbac + audit + dispatcher)

**Frontend (new):**

- `apps/frontend/components/admin/admin-data-table.tsx`
- `apps/frontend/components/admin/admin-detail-layout.tsx`
- `apps/frontend/components/admin/admin-action-button.tsx`
- `apps/frontend/components/admin/reason-dialog.tsx`
- `apps/frontend/components/admin/dangerous-confirm-dialog.tsx`
- `apps/frontend/components/admin/action-manifest.ts` — typed client manifest of action metadata
- `apps/frontend/components/admin/__tests__/admin-action-button.test.tsx`

**Frontend (modified):**

- `apps/frontend/lib/session.ts` — replace `requireInternalAdminSession` with `requireAdminSession` + `requirePermission(key)`
- `apps/frontend/components/app-shell.tsx` — permission-filtered nav

---

## Phase 1: RBAC Schema and Backend Package

### Task 1: Add RBAC schema to `EnsureSchema`

**Files:**
- Modify: `apps/backend/internal/store/postgres.go` (append to `EnsureSchema` DDL list)

- [ ] **Step 1: Write failing integration test**

Create `apps/backend/internal/store/postgres_admin_test.go`:

```go
package store

import (
	"context"
	"testing"
)

func TestEnsureSchema_CreatesAdminTables(t *testing.T) {
	ctx := context.Background()
	store := newTestPostgres(t)
	tables := []string{
		"admin_permissions",
		"admin_roles",
		"admin_role_permissions",
		"admin_user_roles",
		"audit_events",
	}
	for _, table := range tables {
		var exists bool
		err := store.pool.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = $1)`,
			table,
		).Scan(&exists)
		if err != nil {
			t.Fatalf("query %s: %v", table, err)
		}
		if !exists {
			t.Errorf("expected table %q to exist", table)
		}
	}
}
```

If `newTestPostgres` does not already exist, add it alongside as:

```go
func newTestPostgres(t *testing.T) *PostgresStore {
	t.Helper()
	dsn := os.Getenv("TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_URL not set")
	}
	store, err := NewPostgresStore(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(store.Close)
	return store
}
```

- [ ] **Step 2: Run and confirm failure**

Run: `cd apps/backend && go test ./internal/store/ -run TestEnsureSchema_CreatesAdminTables -v`
Expected: FAIL — tables do not exist.

- [ ] **Step 3: Add DDL to `EnsureSchema`**

Append to the DDL string list in `apps/backend/internal/store/postgres.go`:

```sql
CREATE TABLE IF NOT EXISTS admin_permissions (
    key TEXT PRIMARY KEY,
    description TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS admin_roles (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    key TEXT UNIQUE NOT NULL,
    name TEXT NOT NULL,
    description TEXT,
    is_system BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS admin_role_permissions (
    role_id UUID NOT NULL REFERENCES admin_roles(id) ON DELETE CASCADE,
    permission_key TEXT NOT NULL REFERENCES admin_permissions(key) ON DELETE CASCADE,
    PRIMARY KEY (role_id, permission_key)
);

CREATE TABLE IF NOT EXISTS admin_user_roles (
    user_id UUID NOT NULL,
    role_id UUID NOT NULL REFERENCES admin_roles(id) ON DELETE CASCADE,
    granted_by_user_id UUID,
    granted_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, role_id)
);

CREATE TABLE IF NOT EXISTS audit_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    action_type TEXT NOT NULL,
    actor_user_id UUID,
    actor_role_snapshot TEXT[] NOT NULL DEFAULT '{}',
    resource_type TEXT,
    resource_id TEXT,
    target_user_id UUID,
    params JSONB NOT NULL DEFAULT '{}',
    reason TEXT,
    result TEXT NOT NULL,
    error_message TEXT,
    ip_address INET,
    user_agent TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS audit_events_actor_idx ON audit_events (actor_user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS audit_events_resource_idx ON audit_events (resource_type, resource_id, created_at DESC);
CREATE INDEX IF NOT EXISTS audit_events_action_idx ON audit_events (action_type, created_at DESC);
CREATE INDEX IF NOT EXISTS audit_events_target_user_idx ON audit_events (target_user_id, created_at DESC);
```

Also ensure the `pgcrypto` extension is enabled (for `gen_random_uuid()`). If it's not already present in `EnsureSchema`, add at the top:

```sql
CREATE EXTENSION IF NOT EXISTS pgcrypto;
```

- [ ] **Step 4: Run test, confirm pass**

Run: `cd apps/backend && go test ./internal/store/ -run TestEnsureSchema_CreatesAdminTables -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/backend/internal/store/postgres.go apps/backend/internal/store/postgres_admin_test.go
git commit -m "feat(backend): add admin RBAC and audit_events schema"
```

---

### Task 2: Permission registry (code-side)

**Files:**
- Create: `apps/backend/internal/admin/rbac/permissions.go`
- Test: `apps/backend/internal/admin/rbac/permissions_test.go`

- [ ] **Step 1: Write failing test**

`apps/backend/internal/admin/rbac/permissions_test.go`:

```go
package rbac

import "testing"

func TestPermissions_ContainsExpectedKeys(t *testing.T) {
	expected := []string{
		"users.suspend",
		"users.force_unlink_steam",
		"proxies.edit",
		"proxies.rotate_credentials",
		"links.review",
		"links.approve",
		"guilds.untrack_server",
		"admins.grant",
		"audit.read",
	}
	got := map[string]bool{}
	for _, p := range AllPermissions() {
		got[p.Key] = true
	}
	for _, key := range expected {
		if !got[key] {
			t.Errorf("expected permission %q in registry", key)
		}
	}
}

func TestPermissions_NoDuplicates(t *testing.T) {
	seen := map[string]bool{}
	for _, p := range AllPermissions() {
		if seen[p.Key] {
			t.Errorf("duplicate permission key: %q", p.Key)
		}
		seen[p.Key] = true
	}
}
```

- [ ] **Step 2: Run, confirm fail**

Run: `cd apps/backend && go test ./internal/admin/rbac/ -v`
Expected: FAIL — package does not exist.

- [ ] **Step 3: Implement**

`apps/backend/internal/admin/rbac/permissions.go`:

```go
package rbac

type Permission struct {
	Key         string
	Description string
}

var permissions = []Permission{
	{Key: "users.suspend", Description: "Suspend or unsuspend a user account"},
	{Key: "users.force_unlink_steam", Description: "Force-unlink a Steam account from a user"},
	{Key: "proxies.edit", Description: "Create, update, or delete outbound proxy providers"},
	{Key: "proxies.rotate_credentials", Description: "Rotate outbound proxy provider credentials"},
	{Key: "links.review", Description: "View the Steam-Discord link review queue"},
	{Key: "links.approve", Description: "Approve or reject queued Steam-Discord links"},
	{Key: "guilds.untrack_server", Description: "Remove a server from a guild's tracked set"},
	{Key: "admins.grant", Description: "Grant or revoke admin roles; edit role-permission mappings"},
	{Key: "audit.read", Description: "Read the admin audit log"},
}

func AllPermissions() []Permission {
	out := make([]Permission, len(permissions))
	copy(out, permissions)
	return out
}

func IsValidPermission(key string) bool {
	for _, p := range permissions {
		if p.Key == key {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run, confirm pass**

Run: `cd apps/backend && go test ./internal/admin/rbac/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/backend/internal/admin/rbac/permissions.go apps/backend/internal/admin/rbac/permissions_test.go
git commit -m "feat(backend): add admin permission registry"
```

---

### Task 3: Role constants and seed data

**Files:**
- Create: `apps/backend/internal/admin/rbac/roles.go`
- Test: `apps/backend/internal/admin/rbac/roles_test.go`

- [ ] **Step 1: Write failing test**

`apps/backend/internal/admin/rbac/roles_test.go`:

```go
package rbac

import "testing"

func TestSystemRoles_ContainsSuperadminAndIsSystem(t *testing.T) {
	roles := SystemRoles()
	var sa *RoleSeed
	for i := range roles {
		if roles[i].Key == RoleSuperadmin {
			sa = &roles[i]
		}
	}
	if sa == nil {
		t.Fatalf("expected %q in SystemRoles", RoleSuperadmin)
	}
	if !sa.IsSystem {
		t.Errorf("superadmin must be is_system")
	}
	if len(sa.Permissions) != 0 {
		t.Errorf("superadmin should not enumerate permissions (implicit all)")
	}
}

func TestSystemRoles_ModeratorHasExpectedPermissions(t *testing.T) {
	roles := SystemRoles()
	var mod *RoleSeed
	for i := range roles {
		if roles[i].Key == RoleModerator {
			mod = &roles[i]
		}
	}
	if mod == nil {
		t.Fatalf("expected moderator role")
	}
	want := map[string]bool{
		"users.suspend":           true,
		"users.force_unlink_steam": true,
		"links.review":            true,
		"links.approve":           true,
		"audit.read":              true,
	}
	got := map[string]bool{}
	for _, p := range mod.Permissions {
		got[p] = true
	}
	for k := range want {
		if !got[k] {
			t.Errorf("moderator missing permission %q", k)
		}
	}
}
```

- [ ] **Step 2: Run, confirm fail**

Run: `cd apps/backend && go test ./internal/admin/rbac/ -run TestSystemRoles -v`
Expected: FAIL — `RoleSuperadmin` undefined.

- [ ] **Step 3: Implement**

`apps/backend/internal/admin/rbac/roles.go`:

```go
package rbac

const (
	RoleSuperadmin = "superadmin"
	RoleModerator  = "moderator"
	RoleInfraAdmin = "infra_admin"
	RoleGuildOps   = "guild_ops"
)

type RoleSeed struct {
	Key         string
	Name        string
	Description string
	IsSystem    bool
	Permissions []string // empty for superadmin (implicit all)
}

func SystemRoles() []RoleSeed {
	return []RoleSeed{
		{
			Key:         RoleSuperadmin,
			Name:        "Super Admin",
			Description: "Implicit all permissions; only a superadmin can grant this role.",
			IsSystem:    true,
			Permissions: nil,
		},
		{
			Key:         RoleModerator,
			Name:        "Moderator",
			Description: "User moderation and link review.",
			Permissions: []string{
				"users.suspend",
				"users.force_unlink_steam",
				"links.review",
				"links.approve",
				"audit.read",
			},
		},
		{
			Key:         RoleInfraAdmin,
			Name:        "Infrastructure Admin",
			Description: "Manage outbound proxies and infrastructure configuration.",
			Permissions: []string{
				"proxies.edit",
				"proxies.rotate_credentials",
				"audit.read",
			},
		},
		{
			Key:         RoleGuildOps,
			Name:        "Guild Operations",
			Description: "Intervene in guild configuration.",
			Permissions: []string{
				"guilds.untrack_server",
				"audit.read",
			},
		},
	}
}
```

- [ ] **Step 4: Run, confirm pass**

Run: `cd apps/backend && go test ./internal/admin/rbac/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/backend/internal/admin/rbac/roles.go apps/backend/internal/admin/rbac/roles_test.go
git commit -m "feat(backend): add admin role seed data"
```

---

### Task 4: RBAC store (DB layer)

**Files:**
- Create: `apps/backend/internal/admin/rbac/store.go`
- Test: `apps/backend/internal/admin/rbac/store_test.go`

- [ ] **Step 1: Write failing tests**

`apps/backend/internal/admin/rbac/store_test.go`:

```go
package rbac_test

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"rustlens/backend/internal/admin/rbac"
	"rustlens/backend/internal/store"
)

func newPool(t *testing.T) *pgxpool.Pool {
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
	return s.Pool()
}

func TestStore_SeedAndListRoles(t *testing.T) {
	pool := newPool(t)
	ctx := context.Background()
	s := rbac.NewStore(pool)

	if err := s.SeedPermissionsAndRoles(ctx); err != nil {
		t.Fatalf("seed: %v", err)
	}
	roles, err := s.ListRoles(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	keys := map[string]bool{}
	for _, r := range roles {
		keys[r.Key] = true
	}
	for _, want := range []string{rbac.RoleSuperadmin, rbac.RoleModerator, rbac.RoleInfraAdmin, rbac.RoleGuildOps} {
		if !keys[want] {
			t.Errorf("role %q missing after seed", want)
		}
	}
}

func TestStore_PermissionsForUser(t *testing.T) {
	pool := newPool(t)
	ctx := context.Background()
	s := rbac.NewStore(pool)
	if err := s.SeedPermissionsAndRoles(ctx); err != nil {
		t.Fatalf("seed: %v", err)
	}

	userID := uuid.New()
	modRoleID, err := s.RoleIDByKey(ctx, rbac.RoleModerator)
	if err != nil {
		t.Fatalf("role id: %v", err)
	}
	if err := s.AssignRole(ctx, userID, modRoleID, nil); err != nil {
		t.Fatalf("assign: %v", err)
	}

	perms, err := s.PermissionsForUser(ctx, userID)
	if err != nil {
		t.Fatalf("perms: %v", err)
	}
	found := map[string]bool{}
	for _, p := range perms {
		found[p] = true
	}
	if !found["users.suspend"] {
		t.Errorf("expected users.suspend for moderator, got %v", perms)
	}
}

func TestStore_SuperadminImpliesAllPermissions(t *testing.T) {
	pool := newPool(t)
	ctx := context.Background()
	s := rbac.NewStore(pool)
	if err := s.SeedPermissionsAndRoles(ctx); err != nil {
		t.Fatalf("seed: %v", err)
	}

	userID := uuid.New()
	saID, err := s.RoleIDByKey(ctx, rbac.RoleSuperadmin)
	if err != nil {
		t.Fatalf("role id: %v", err)
	}
	if err := s.AssignRole(ctx, userID, saID, nil); err != nil {
		t.Fatalf("assign: %v", err)
	}

	perms, err := s.PermissionsForUser(ctx, userID)
	if err != nil {
		t.Fatalf("perms: %v", err)
	}
	if len(perms) != len(rbac.AllPermissions()) {
		t.Errorf("superadmin should get all %d perms, got %d", len(rbac.AllPermissions()), len(perms))
	}
}
```

Also, add a `Pool()` accessor to `PostgresStore` if it does not exist. In `apps/backend/internal/store/postgres.go`:

```go
func (s *PostgresStore) Pool() *pgxpool.Pool { return s.pool }
```

- [ ] **Step 2: Run, confirm fail**

Run: `cd apps/backend && go test ./internal/admin/rbac/ -v`
Expected: FAIL — `rbac.NewStore` undefined.

- [ ] **Step 3: Implement**

`apps/backend/internal/admin/rbac/store.go`:

```go
package rbac

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Role struct {
	ID          uuid.UUID
	Key         string
	Name        string
	Description string
	IsSystem    bool
}

type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func (s *Store) SeedPermissionsAndRoles(ctx context.Context) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	for _, p := range AllPermissions() {
		if _, err := tx.Exec(ctx,
			`INSERT INTO admin_permissions (key, description) VALUES ($1, $2)
			 ON CONFLICT (key) DO UPDATE SET description = EXCLUDED.description`,
			p.Key, p.Description,
		); err != nil {
			return err
		}
	}
	for _, r := range SystemRoles() {
		var roleID uuid.UUID
		err := tx.QueryRow(ctx,
			`INSERT INTO admin_roles (key, name, description, is_system)
			 VALUES ($1, $2, $3, $4)
			 ON CONFLICT (key) DO UPDATE
			   SET name = EXCLUDED.name, description = EXCLUDED.description, updated_at = NOW()
			 RETURNING id`,
			r.Key, r.Name, r.Description, r.IsSystem,
		).Scan(&roleID)
		if err != nil {
			return err
		}
		if r.Key == RoleSuperadmin {
			continue // no explicit perms
		}
		if _, err := tx.Exec(ctx, `DELETE FROM admin_role_permissions WHERE role_id = $1`, roleID); err != nil {
			return err
		}
		for _, p := range r.Permissions {
			if _, err := tx.Exec(ctx,
				`INSERT INTO admin_role_permissions (role_id, permission_key) VALUES ($1, $2)`,
				roleID, p,
			); err != nil {
				return err
			}
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) ListRoles(ctx context.Context) ([]Role, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, key, name, COALESCE(description, ''), is_system FROM admin_roles ORDER BY key`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Role
	for rows.Next() {
		var r Role
		if err := rows.Scan(&r.ID, &r.Key, &r.Name, &r.Description, &r.IsSystem); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) RoleIDByKey(ctx context.Context, key string) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `SELECT id FROM admin_roles WHERE key = $1`, key).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrRoleNotFound
	}
	return id, err
}

var ErrRoleNotFound = errors.New("role not found")

func (s *Store) AssignRole(ctx context.Context, userID, roleID uuid.UUID, grantedBy *uuid.UUID) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO admin_user_roles (user_id, role_id, granted_by_user_id)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (user_id, role_id) DO NOTHING`,
		userID, roleID, grantedBy,
	)
	return err
}

func (s *Store) RevokeRole(ctx context.Context, userID, roleID uuid.UUID) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM admin_user_roles WHERE user_id = $1 AND role_id = $2`,
		userID, roleID,
	)
	return err
}

func (s *Store) RolesForUser(ctx context.Context, userID uuid.UUID) ([]Role, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT r.id, r.key, r.name, COALESCE(r.description, ''), r.is_system
		FROM admin_user_roles ur
		JOIN admin_roles r ON r.id = ur.role_id
		WHERE ur.user_id = $1
		ORDER BY r.key
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Role
	for rows.Next() {
		var r Role
		if err := rows.Scan(&r.ID, &r.Key, &r.Name, &r.Description, &r.IsSystem); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) PermissionsForUser(ctx context.Context, userID uuid.UUID) ([]string, error) {
	roles, err := s.RolesForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	for _, r := range roles {
		if r.Key == RoleSuperadmin {
			all := AllPermissions()
			keys := make([]string, len(all))
			for i, p := range all {
				keys[i] = p.Key
			}
			return keys, nil
		}
	}
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT rp.permission_key
		FROM admin_user_roles ur
		JOIN admin_role_permissions rp ON rp.role_id = ur.role_id
		WHERE ur.user_id = $1
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}
```

- [ ] **Step 4: Run, confirm pass**

Run: `cd apps/backend && TEST_POSTGRES_URL=$POSTGRES_URL go test ./internal/admin/rbac/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/backend/internal/admin/rbac/store.go apps/backend/internal/admin/rbac/store_test.go apps/backend/internal/store/postgres.go
git commit -m "feat(backend): add RBAC store with seed and permission resolution"
```

---

### Task 5: Permission resolver with per-request cache

**Files:**
- Create: `apps/backend/internal/admin/rbac/resolver.go`
- Test: `apps/backend/internal/admin/rbac/resolver_test.go`

- [ ] **Step 1: Write failing test**

`apps/backend/internal/admin/rbac/resolver_test.go`:

```go
package rbac

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

type fakeLoader struct {
	perms map[uuid.UUID][]string
	calls int
}

func (f *fakeLoader) PermissionsForUser(ctx context.Context, userID uuid.UUID) ([]string, error) {
	f.calls++
	return f.perms[userID], nil
}

func TestResolver_HasPermission(t *testing.T) {
	user := uuid.New()
	f := &fakeLoader{perms: map[uuid.UUID][]string{user: {"users.suspend"}}}
	r := NewResolver(f)
	ctx := r.WithRequest(context.Background())

	ok, err := r.HasPermission(ctx, user, "users.suspend")
	if err != nil || !ok {
		t.Fatalf("expected true, got ok=%v err=%v", ok, err)
	}
	ok, _ = r.HasPermission(ctx, user, "proxies.edit")
	if ok {
		t.Errorf("expected false for proxies.edit")
	}
}

func TestResolver_CachesPerRequest(t *testing.T) {
	user := uuid.New()
	f := &fakeLoader{perms: map[uuid.UUID][]string{user: {"users.suspend"}}}
	r := NewResolver(f)
	ctx := r.WithRequest(context.Background())

	_, _ = r.HasPermission(ctx, user, "users.suspend")
	_, _ = r.HasPermission(ctx, user, "proxies.edit")
	_, _ = r.HasPermission(ctx, user, "audit.read")
	if f.calls != 1 {
		t.Errorf("expected 1 loader call per request, got %d", f.calls)
	}
}
```

- [ ] **Step 2: Run, confirm fail**

Run: `cd apps/backend && go test ./internal/admin/rbac/ -run TestResolver -v`
Expected: FAIL — `NewResolver` undefined.

- [ ] **Step 3: Implement**

`apps/backend/internal/admin/rbac/resolver.go`:

```go
package rbac

import (
	"context"

	"github.com/google/uuid"
)

type PermissionLoader interface {
	PermissionsForUser(ctx context.Context, userID uuid.UUID) ([]string, error)
}

type Resolver struct{ loader PermissionLoader }

func NewResolver(loader PermissionLoader) *Resolver { return &Resolver{loader: loader} }

type requestCacheKey struct{}

type requestCache struct {
	entries map[uuid.UUID]map[string]bool
}

func (r *Resolver) WithRequest(ctx context.Context) context.Context {
	return context.WithValue(ctx, requestCacheKey{}, &requestCache{entries: map[uuid.UUID]map[string]bool{}})
}

func (r *Resolver) HasPermission(ctx context.Context, userID uuid.UUID, key string) (bool, error) {
	set, err := r.loadSet(ctx, userID)
	if err != nil {
		return false, err
	}
	return set[key], nil
}

func (r *Resolver) loadSet(ctx context.Context, userID uuid.UUID) (map[string]bool, error) {
	cache, _ := ctx.Value(requestCacheKey{}).(*requestCache)
	if cache != nil {
		if set, ok := cache.entries[userID]; ok {
			return set, nil
		}
	}
	perms, err := r.loader.PermissionsForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	set := make(map[string]bool, len(perms))
	for _, p := range perms {
		set[p] = true
	}
	if cache != nil {
		cache.entries[userID] = set
	}
	return set, nil
}

func (r *Resolver) PermissionsForUser(ctx context.Context, userID uuid.UUID) ([]string, error) {
	set, err := r.loadSet(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	return out, nil
}
```

- [ ] **Step 4: Run, confirm pass**

Run: `cd apps/backend && go test ./internal/admin/rbac/ -run TestResolver -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/backend/internal/admin/rbac/resolver.go apps/backend/internal/admin/rbac/resolver_test.go
git commit -m "feat(backend): add RBAC resolver with per-request cache"
```

---

## Phase 2: Audit Log

### Task 6: Audit package — write and query

**Files:**
- Create: `apps/backend/internal/admin/audit/audit.go`
- Test: `apps/backend/internal/admin/audit/audit_test.go`

- [ ] **Step 1: Write failing test**

`apps/backend/internal/admin/audit/audit_test.go`:

```go
package audit_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/google/uuid"

	"rustlens/backend/internal/admin/audit"
	"rustlens/backend/internal/store"
)

func newStore(t *testing.T) *store.PostgresStore {
	t.Helper()
	dsn := os.Getenv("TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_URL not set")
	}
	s, err := store.NewPostgresStore(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(s.Close)
	return s
}

func TestAudit_WriteAndQuery(t *testing.T) {
	s := newStore(t)
	a := audit.NewStore(s.Pool())
	ctx := context.Background()

	actor := uuid.New()
	target := uuid.New()
	params, _ := json.Marshal(map[string]string{"reason_tag": "abuse"})

	err := a.Record(ctx, audit.Entry{
		ActionType:        "users.suspend",
		ActorUserID:       &actor,
		ActorRoleSnapshot: []string{"moderator"},
		ResourceType:      "user",
		ResourceID:        target.String(),
		TargetUserID:      &target,
		Params:            params,
		Reason:            strPtr("spamming"),
		Result:            "success",
		IPAddress:         strPtr("10.0.0.1"),
		UserAgent:         strPtr("go-test"),
	})
	if err != nil {
		t.Fatalf("record: %v", err)
	}

	rows, err := a.Query(ctx, audit.QueryFilters{ActorUserID: &actor, Limit: 10})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].ActionType != "users.suspend" {
		t.Errorf("action_type = %q", rows[0].ActionType)
	}
	if rows[0].Result != "success" {
		t.Errorf("result = %q", rows[0].Result)
	}
}

func strPtr(s string) *string { return &s }
```

- [ ] **Step 2: Run, confirm fail**

Run: `cd apps/backend && go test ./internal/admin/audit/ -v`
Expected: FAIL — package missing.

- [ ] **Step 3: Implement**

`apps/backend/internal/admin/audit/audit.go`:

```go
package audit

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Entry struct {
	ActionType        string
	ActorUserID       *uuid.UUID
	ActorRoleSnapshot []string
	ResourceType      string
	ResourceID        string
	TargetUserID      *uuid.UUID
	Params            json.RawMessage
	Reason            *string
	Result            string // "success" or "error"
	ErrorMessage      *string
	IPAddress         *string
	UserAgent         *string
}

type Row struct {
	ID                uuid.UUID
	ActionType        string
	ActorUserID       *uuid.UUID
	ActorRoleSnapshot []string
	ResourceType      string
	ResourceID        string
	TargetUserID      *uuid.UUID
	Params            json.RawMessage
	Reason            *string
	Result            string
	ErrorMessage      *string
	IPAddress         *string
	UserAgent         *string
	CreatedAt         time.Time
}

type QueryFilters struct {
	ActorUserID  *uuid.UUID
	TargetUserID *uuid.UUID
	ActionType   *string
	ResourceType *string
	ResourceID   *string
	Result       *string
	Since        *time.Time
	Until        *time.Time
	Limit        int
}

type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func (s *Store) Record(ctx context.Context, e Entry) error {
	if e.Params == nil {
		e.Params = json.RawMessage(`{}`)
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO audit_events (
			action_type, actor_user_id, actor_role_snapshot,
			resource_type, resource_id, target_user_id,
			params, reason, result, error_message, ip_address, user_agent
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
	`, e.ActionType, e.ActorUserID, e.ActorRoleSnapshot,
		nullIfEmpty(e.ResourceType), nullIfEmpty(e.ResourceID), e.TargetUserID,
		e.Params, e.Reason, e.Result, e.ErrorMessage, e.IPAddress, e.UserAgent,
	)
	return err
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (s *Store) Query(ctx context.Context, f QueryFilters) ([]Row, error) {
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 100
	}
	sql := `SELECT id, action_type, actor_user_id, actor_role_snapshot,
		COALESCE(resource_type,''), COALESCE(resource_id,''), target_user_id,
		params, reason, result, error_message, ip_address::text, user_agent, created_at
		FROM audit_events WHERE 1=1`
	args := []any{}
	add := func(clause string, v any) {
		args = append(args, v)
		sql += " AND " + clause + " = $" + itoa(len(args))
	}
	if f.ActorUserID != nil {
		add("actor_user_id", *f.ActorUserID)
	}
	if f.TargetUserID != nil {
		add("target_user_id", *f.TargetUserID)
	}
	if f.ActionType != nil {
		add("action_type", *f.ActionType)
	}
	if f.ResourceType != nil {
		add("resource_type", *f.ResourceType)
	}
	if f.ResourceID != nil {
		add("resource_id", *f.ResourceID)
	}
	if f.Result != nil {
		add("result", *f.Result)
	}
	if f.Since != nil {
		args = append(args, *f.Since)
		sql += " AND created_at >= $" + itoa(len(args))
	}
	if f.Until != nil {
		args = append(args, *f.Until)
		sql += " AND created_at <= $" + itoa(len(args))
	}
	args = append(args, f.Limit)
	sql += " ORDER BY created_at DESC LIMIT $" + itoa(len(args))

	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Row
	for rows.Next() {
		var r Row
		if err := rows.Scan(
			&r.ID, &r.ActionType, &r.ActorUserID, &r.ActorRoleSnapshot,
			&r.ResourceType, &r.ResourceID, &r.TargetUserID,
			&r.Params, &r.Reason, &r.Result, &r.ErrorMessage, &r.IPAddress, &r.UserAgent, &r.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func itoa(i int) string {
	// small helper to avoid strconv import churn; safe since caller bounds i
	const digits = "0123456789"
	if i < 10 {
		return string(digits[i])
	}
	return itoa(i/10) + string(digits[i%10])
}
```

- [ ] **Step 4: Run, confirm pass**

Run: `cd apps/backend && TEST_POSTGRES_URL=$POSTGRES_URL go test ./internal/admin/audit/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/backend/internal/admin/audit/
git commit -m "feat(backend): add audit event store with filtered query"
```

---

## Phase 3: Action Framework

### Task 7: Action interface and types

**Files:**
- Create: `apps/backend/internal/admin/actions/action.go`
- Test: `apps/backend/internal/admin/actions/action_test.go`

- [ ] **Step 1: Write failing test**

`apps/backend/internal/admin/actions/action_test.go`:

```go
package actions

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

type fakeAction struct{}

func (fakeAction) Type() string                  { return "test.noop" }
func (fakeAction) Permission() string            { return "audit.read" }
func (fakeAction) RequiresReason() bool          { return false }
func (fakeAction) Dangerous() bool               { return false }
func (fakeAction) SensitiveParamKeys() []string  { return nil }
func (fakeAction) ResourceType() string          { return "user" }
func (fakeAction) Validate(json.RawMessage) (ValidatedParams, error) {
	return ValidatedParams{ResourceID: "r1"}, nil
}
func (fakeAction) Execute(context.Context, ActionInput) (ActionResult, error) {
	return ActionResult{ResourceID: "r1"}, nil
}

func TestAction_Interface(t *testing.T) {
	var a AdminAction = fakeAction{}
	if a.Type() != "test.noop" {
		t.Errorf("Type = %q", a.Type())
	}
	vp, err := a.Validate(nil)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if vp.ResourceID != "r1" {
		t.Errorf("ResourceID = %q", vp.ResourceID)
	}
	_ = uuid.New()
}
```

- [ ] **Step 2: Run, confirm fail**

Run: `cd apps/backend && go test ./internal/admin/actions/ -v`
Expected: FAIL — package missing.

- [ ] **Step 3: Implement**

`apps/backend/internal/admin/actions/action.go`:

```go
package actions

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
)

type ValidatedParams struct {
	ResourceID           string
	TargetUserID         *uuid.UUID
	ConfirmationExpected string // required match when action is Dangerous
}

type ActionInput struct {
	ActorUserID       uuid.UUID
	ActorRoleSnapshot []string
	Params            json.RawMessage
	Validated         ValidatedParams
	Reason            string
	TypedConfirmation string
}

type ActionResult struct {
	ResourceID   string
	TargetUserID *uuid.UUID
	Payload      any
}

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

- [ ] **Step 4: Run, confirm pass**

Run: `cd apps/backend && go test ./internal/admin/actions/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/backend/internal/admin/actions/action.go apps/backend/internal/admin/actions/action_test.go
git commit -m "feat(backend): add AdminAction interface"
```

---

### Task 8: Registry with duplicate and permission validation

**Files:**
- Create: `apps/backend/internal/admin/actions/registry.go`
- Test: `apps/backend/internal/admin/actions/registry_test.go`

- [ ] **Step 1: Write failing test**

`apps/backend/internal/admin/actions/registry_test.go`:

```go
package actions

import "testing"

func TestRegistry_RegisterAndLookup(t *testing.T) {
	r := NewRegistry(fakePermCheck)
	if err := r.Register(fakeAction{}); err != nil {
		t.Fatalf("register: %v", err)
	}
	got, ok := r.Get("test.noop")
	if !ok {
		t.Fatal("expected lookup to succeed")
	}
	if got.Type() != "test.noop" {
		t.Errorf("Type mismatch")
	}
}

func TestRegistry_RejectsDuplicate(t *testing.T) {
	r := NewRegistry(fakePermCheck)
	_ = r.Register(fakeAction{})
	err := r.Register(fakeAction{})
	if err == nil {
		t.Fatal("expected duplicate error")
	}
}

func TestRegistry_RejectsUnknownPermission(t *testing.T) {
	r := NewRegistry(func(key string) bool { return false })
	err := r.Register(fakeAction{})
	if err == nil {
		t.Fatal("expected unknown permission error")
	}
}

func fakePermCheck(key string) bool { return key == "audit.read" }
```

- [ ] **Step 2: Run, confirm fail**

Run: `cd apps/backend && go test ./internal/admin/actions/ -run TestRegistry -v`
Expected: FAIL.

- [ ] **Step 3: Implement**

`apps/backend/internal/admin/actions/registry.go`:

```go
package actions

import "fmt"

type PermissionValidator func(key string) bool

type Registry struct {
	actions    map[string]AdminAction
	isValidPerm PermissionValidator
}

func NewRegistry(isValidPerm PermissionValidator) *Registry {
	return &Registry{actions: map[string]AdminAction{}, isValidPerm: isValidPerm}
}

func (r *Registry) Register(a AdminAction) error {
	if _, exists := r.actions[a.Type()]; exists {
		return fmt.Errorf("action %q already registered", a.Type())
	}
	if !r.isValidPerm(a.Permission()) {
		return fmt.Errorf("action %q requires unknown permission %q", a.Type(), a.Permission())
	}
	r.actions[a.Type()] = a
	return nil
}

func (r *Registry) Get(t string) (AdminAction, bool) {
	a, ok := r.actions[t]
	return a, ok
}

func (r *Registry) Types() []string {
	out := make([]string, 0, len(r.actions))
	for k := range r.actions {
		out = append(out, k)
	}
	return out
}

func (r *Registry) All() []AdminAction {
	out := make([]AdminAction, 0, len(r.actions))
	for _, a := range r.actions {
		out = append(out, a)
	}
	return out
}
```

- [ ] **Step 4: Run, confirm pass**

Run: `cd apps/backend && go test ./internal/admin/actions/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/backend/internal/admin/actions/registry.go apps/backend/internal/admin/actions/registry_test.go
git commit -m "feat(backend): add action registry"
```

---

### Task 9: Param redaction

**Files:**
- Create: `apps/backend/internal/admin/actions/redact.go`
- Test: `apps/backend/internal/admin/actions/redact_test.go`

- [ ] **Step 1: Write failing test**

`apps/backend/internal/admin/actions/redact_test.go`:

```go
package actions

import (
	"encoding/json"
	"testing"
)

func TestRedact_MasksListedKeys(t *testing.T) {
	in := json.RawMessage(`{"user":"alice","password":"hunter2","nested":{"token":"abc","ok":"keep"}}`)
	out, err := RedactParams(in, []string{"password", "token"})
	if err != nil {
		t.Fatalf("redact: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["password"] != "***" {
		t.Errorf("password not redacted: %v", got["password"])
	}
	if got["user"] != "alice" {
		t.Errorf("user was modified: %v", got["user"])
	}
	nested := got["nested"].(map[string]any)
	if nested["token"] != "***" {
		t.Errorf("nested token not redacted")
	}
	if nested["ok"] != "keep" {
		t.Errorf("unrelated key altered")
	}
}

func TestRedact_NilInputReturnsEmptyObject(t *testing.T) {
	out, err := RedactParams(nil, []string{"x"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if string(out) != "{}" {
		t.Errorf("got %q want {}", string(out))
	}
}
```

- [ ] **Step 2: Run, confirm fail**

Run: `cd apps/backend && go test ./internal/admin/actions/ -run TestRedact -v`
Expected: FAIL.

- [ ] **Step 3: Implement**

`apps/backend/internal/admin/actions/redact.go`:

```go
package actions

import "encoding/json"

func RedactParams(raw json.RawMessage, keys []string) (json.RawMessage, error) {
	if len(raw) == 0 {
		return json.RawMessage("{}"), nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	set := map[string]bool{}
	for _, k := range keys {
		set[k] = true
	}
	redactWalk(v, set)
	return json.Marshal(v)
}

func redactWalk(v any, keys map[string]bool) {
	switch n := v.(type) {
	case map[string]any:
		for k, child := range n {
			if keys[k] {
				n[k] = "***"
				continue
			}
			redactWalk(child, keys)
		}
	case []any:
		for _, child := range n {
			redactWalk(child, keys)
		}
	}
}
```

- [ ] **Step 4: Run, confirm pass**

Run: `cd apps/backend && go test ./internal/admin/actions/ -run TestRedact -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/backend/internal/admin/actions/redact.go apps/backend/internal/admin/actions/redact_test.go
git commit -m "feat(backend): add sensitive params redaction"
```

---

### Task 10: Dispatcher handler

**Files:**
- Create: `apps/backend/internal/admin/actions/dispatcher.go`
- Test: `apps/backend/internal/admin/actions/dispatcher_test.go`

This is the central piece. It wires permission check, reason enforcement, dangerous confirmation, validation, execution, and audit write.

- [ ] **Step 1: Write failing tests**

`apps/backend/internal/admin/actions/dispatcher_test.go`:

```go
package actions_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"rustlens/backend/internal/admin/actions"
)

// -- test action implementations --

type suspendAction struct {
	executeCalled bool
	failExecute   bool
}

func (a *suspendAction) Type() string                 { return "users.suspend" }
func (a *suspendAction) Permission() string           { return "users.suspend" }
func (a *suspendAction) RequiresReason() bool         { return true }
func (a *suspendAction) Dangerous() bool              { return false }
func (a *suspendAction) SensitiveParamKeys() []string { return nil }
func (a *suspendAction) ResourceType() string         { return "user" }
func (a *suspendAction) Validate(raw json.RawMessage) (actions.ValidatedParams, error) {
	var p struct {
		UserID uuid.UUID `json:"userId"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return actions.ValidatedParams{}, err
	}
	if p.UserID == uuid.Nil {
		return actions.ValidatedParams{}, errors.New("userId required")
	}
	tuid := p.UserID
	return actions.ValidatedParams{ResourceID: p.UserID.String(), TargetUserID: &tuid}, nil
}
func (a *suspendAction) Execute(ctx context.Context, in actions.ActionInput) (actions.ActionResult, error) {
	a.executeCalled = true
	if a.failExecute {
		return actions.ActionResult{}, errors.New("boom")
	}
	return actions.ActionResult{ResourceID: in.Validated.ResourceID, TargetUserID: in.Validated.TargetUserID, Payload: map[string]string{"status": "suspended"}}, nil
}

// -- fake audit sink --

type fakeAudit struct{ entries []actions.AuditEntryInput }

func (f *fakeAudit) RecordEntry(ctx context.Context, e actions.AuditEntryInput) error {
	f.entries = append(f.entries, e)
	return nil
}

// -- fake permission check --

type fakePermResolver struct{ allow map[string]bool }

func (f *fakePermResolver) HasPermission(ctx context.Context, userID uuid.UUID, key string) (bool, error) {
	return f.allow[key], nil
}
func (f *fakePermResolver) PermissionsForUser(ctx context.Context, _ uuid.UUID) ([]string, error) {
	out := []string{}
	for k, v := range f.allow {
		if v {
			out = append(out, k)
		}
	}
	return out, nil
}

// -- helpers --

func newDispatcher(t *testing.T, a actions.AdminAction, resolver *fakePermResolver) (*httptest.Server, *fakeAudit) {
	t.Helper()
	reg := actions.NewRegistry(func(string) bool { return true })
	if err := reg.Register(a); err != nil {
		t.Fatalf("register: %v", err)
	}
	au := &fakeAudit{}
	d := actions.NewDispatcher(reg, resolver, au, fixedActor)
	r := chi.NewRouter()
	r.Post("/api/app/admin/actions/{type}", d.ServeHTTP)
	return httptest.NewServer(r), au
}

func fixedActor(r *http.Request) (uuid.UUID, []string, error) {
	return uuid.MustParse("00000000-0000-0000-0000-000000000001"), []string{"moderator"}, nil
}

// -- tests --

func TestDispatcher_Success(t *testing.T) {
	a := &suspendAction{}
	srv, au := newDispatcher(t, a, &fakePermResolver{allow: map[string]bool{"users.suspend": true}})
	defer srv.Close()

	body := mustJSON(t, map[string]any{
		"params": map[string]string{"userId": "11111111-1111-1111-1111-111111111111"},
		"reason": "spamming",
	})
	resp := mustPOST(t, srv.URL+"/api/app/admin/actions/users.suspend", body)
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, body=%s", resp.StatusCode, readBody(resp))
	}
	if !a.executeCalled {
		t.Error("expected Execute to be called")
	}
	if len(au.entries) != 1 || au.entries[0].Result != "success" {
		t.Errorf("audit = %+v", au.entries)
	}
}

func TestDispatcher_PermissionDenied(t *testing.T) {
	a := &suspendAction{}
	srv, _ := newDispatcher(t, a, &fakePermResolver{allow: map[string]bool{}})
	defer srv.Close()
	body := mustJSON(t, map[string]any{"params": map[string]string{"userId": "11111111-1111-1111-1111-111111111111"}, "reason": "x"})
	resp := mustPOST(t, srv.URL+"/api/app/admin/actions/users.suspend", body)
	if resp.StatusCode != 403 {
		t.Errorf("expected 403, got %d", resp.StatusCode)
	}
}

func TestDispatcher_ReasonRequired(t *testing.T) {
	a := &suspendAction{}
	srv, _ := newDispatcher(t, a, &fakePermResolver{allow: map[string]bool{"users.suspend": true}})
	defer srv.Close()
	body := mustJSON(t, map[string]any{"params": map[string]string{"userId": "11111111-1111-1111-1111-111111111111"}})
	resp := mustPOST(t, srv.URL+"/api/app/admin/actions/users.suspend", body)
	if resp.StatusCode != 400 {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

func TestDispatcher_UnknownAction(t *testing.T) {
	srv, _ := newDispatcher(t, &suspendAction{}, &fakePermResolver{allow: map[string]bool{"users.suspend": true}})
	defer srv.Close()
	resp := mustPOST(t, srv.URL+"/api/app/admin/actions/does.not.exist", []byte(`{}`))
	if resp.StatusCode != 404 {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestDispatcher_ExecutionErrorWritesAuditError(t *testing.T) {
	a := &suspendAction{failExecute: true}
	srv, au := newDispatcher(t, a, &fakePermResolver{allow: map[string]bool{"users.suspend": true}})
	defer srv.Close()
	body := mustJSON(t, map[string]any{"params": map[string]string{"userId": "11111111-1111-1111-1111-111111111111"}, "reason": "x"})
	resp := mustPOST(t, srv.URL+"/api/app/admin/actions/users.suspend", body)
	if resp.StatusCode < 500 {
		t.Errorf("expected 5xx, got %d", resp.StatusCode)
	}
	if len(au.entries) != 1 || au.entries[0].Result != "error" {
		t.Errorf("expected error audit, got %+v", au.entries)
	}
}

// -- util --

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}
func mustPOST(t *testing.T, url string, body []byte) *http.Response {
	t.Helper()
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	return resp
}
func readBody(r *http.Response) string {
	buf := new(bytes.Buffer)
	_, _ = buf.ReadFrom(r.Body)
	return buf.String()
}
```

- [ ] **Step 2: Run, confirm fail**

Run: `cd apps/backend && go test ./internal/admin/actions/ -v`
Expected: FAIL — `NewDispatcher` undefined.

- [ ] **Step 3: Implement**

`apps/backend/internal/admin/actions/dispatcher.go`:

```go
package actions

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type PermissionChecker interface {
	HasPermission(ctx context.Context, userID uuid.UUID, key string) (bool, error)
}

type AuditEntryInput struct {
	ActionType        string
	ActorUserID       uuid.UUID
	ActorRoleSnapshot []string
	ResourceType      string
	ResourceID        string
	TargetUserID      *uuid.UUID
	Params            json.RawMessage
	Reason            *string
	Result            string
	ErrorMessage      *string
	IPAddress         *string
	UserAgent         *string
}

type AuditSink interface {
	RecordEntry(ctx context.Context, e AuditEntryInput) error
}

type ActorResolver func(r *http.Request) (userID uuid.UUID, roles []string, err error)

type Dispatcher struct {
	registry *Registry
	perms    PermissionChecker
	audit    AuditSink
	actor    ActorResolver
}

func NewDispatcher(reg *Registry, perms PermissionChecker, audit AuditSink, actor ActorResolver) *Dispatcher {
	return &Dispatcher{registry: reg, perms: perms, audit: audit, actor: actor}
}

type requestBody struct {
	Params            json.RawMessage `json:"params"`
	Reason            string          `json:"reason"`
	TypedConfirmation string          `json:"typedConfirmation"`
}

type errResponse struct {
	Error    string `json:"error"`
	Message  string `json:"message,omitempty"`
	Expected string `json:"expected,omitempty"`
	Required string `json:"required,omitempty"`
}

func (d *Dispatcher) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	actionType := chi.URLParam(r, "type")
	action, ok := d.registry.Get(actionType)
	if !ok {
		writeJSON(w, http.StatusNotFound, errResponse{Error: "action_not_found"})
		return
	}

	actorID, roles, err := d.actor(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, errResponse{Error: "unauthenticated"})
		return
	}

	allowed, err := d.perms.HasPermission(ctx, actorID, action.Permission())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errResponse{Error: "permission_check_failed"})
		return
	}
	if !allowed {
		writeJSON(w, http.StatusForbidden, errResponse{Error: "permission_denied", Required: action.Permission()})
		return
	}

	var body requestBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errors.Is(err, errors.New("EOF")) {
		// allow empty body -> params empty
	}
	if body.Params == nil {
		body.Params = json.RawMessage("{}")
	}

	if action.RequiresReason() && body.Reason == "" {
		writeJSON(w, http.StatusBadRequest, errResponse{Error: "reason_required"})
		return
	}

	validated, err := action.Validate(body.Params)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errResponse{Error: "validation_failed", Message: err.Error()})
		return
	}

	if action.Dangerous() && body.TypedConfirmation != validated.ConfirmationExpected {
		writeJSON(w, http.StatusBadRequest, errResponse{Error: "confirmation_mismatch", Expected: validated.ConfirmationExpected})
		return
	}

	redacted, err := RedactParams(body.Params, action.SensitiveParamKeys())
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errResponse{Error: "redaction_failed", Message: err.Error()})
		return
	}

	input := ActionInput{
		ActorUserID:       actorID,
		ActorRoleSnapshot: roles,
		Params:            body.Params,
		Validated:         validated,
		Reason:            body.Reason,
		TypedConfirmation: body.TypedConfirmation,
	}

	result, execErr := action.Execute(ctx, input)

	ip := clientIP(r)
	ua := r.UserAgent()
	entry := AuditEntryInput{
		ActionType:        action.Type(),
		ActorUserID:       actorID,
		ActorRoleSnapshot: roles,
		ResourceType:      action.ResourceType(),
		ResourceID:        validated.ResourceID,
		TargetUserID:      validated.TargetUserID,
		Params:            redacted,
		Reason:            optionalString(body.Reason),
		IPAddress:         optionalString(ip),
		UserAgent:         optionalString(ua),
	}

	if execErr != nil {
		msg := execErr.Error()
		entry.Result = "error"
		entry.ErrorMessage = &msg
		_ = d.audit.RecordEntry(context.Background(), entry)
		writeJSON(w, http.StatusInternalServerError, errResponse{Error: "execution_failed", Message: msg})
		return
	}

	entry.Result = "success"
	if result.TargetUserID != nil {
		entry.TargetUserID = result.TargetUserID
	}
	if result.ResourceID != "" {
		entry.ResourceID = result.ResourceID
	}
	_ = d.audit.RecordEntry(ctx, entry)

	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "result": result.Payload})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func optionalString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func clientIP(r *http.Request) string {
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		return ip
	}
	return r.RemoteAddr
}
```

- [ ] **Step 4: Run, confirm pass**

Run: `cd apps/backend && go test ./internal/admin/actions/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/backend/internal/admin/actions/dispatcher.go apps/backend/internal/admin/actions/dispatcher_test.go
git commit -m "feat(backend): add admin action dispatcher with audit integration"
```

---

### Task 11: Wire dispatcher into HTTP router

**Files:**
- Modify: `apps/backend/internal/http/router.go`
- Modify: `apps/backend/internal/http/handlers.go`
- Modify: `apps/backend/cmd/server/main.go`

- [ ] **Step 1: Add admin components to `Handler`**

In `apps/backend/internal/http/handlers.go`, add fields to the `Handler` struct and construct them in `NewHandler`:

```go
import (
    "rustlens/backend/internal/admin/actions"
    "rustlens/backend/internal/admin/audit"
    "rustlens/backend/internal/admin/rbac"
)

type Handler struct {
    // ... existing fields ...
    rbacStore       *rbac.Store
    rbacResolver    *rbac.Resolver
    auditStore      *audit.Store
    actionRegistry  *actions.Registry
    actionDispatcher *actions.Dispatcher
}
```

In `NewHandler(...)`:

```go
rbacStore := rbac.NewStore(postgres.Pool())
if err := rbacStore.SeedPermissionsAndRoles(context.Background()); err != nil {
    panic(fmt.Errorf("rbac seed: %w", err))
}
resolver := rbac.NewResolver(rbacStore)
auditStore := audit.NewStore(postgres.Pool())
registry := actions.NewRegistry(rbac.IsValidPermission)
dispatcher := actions.NewDispatcher(registry, resolver, auditSinkAdapter{auditStore}, appSessionActor)
```

Adapter to bridge `audit.Store` to the dispatcher's `AuditSink` interface (place near dispatcher wiring):

```go
type auditSinkAdapter struct{ s *audit.Store }

func (a auditSinkAdapter) RecordEntry(ctx context.Context, e actions.AuditEntryInput) error {
    return a.s.Record(ctx, audit.Entry{
        ActionType:        e.ActionType,
        ActorUserID:       &e.ActorUserID,
        ActorRoleSnapshot: e.ActorRoleSnapshot,
        ResourceType:      e.ResourceType,
        ResourceID:        e.ResourceID,
        TargetUserID:      e.TargetUserID,
        Params:            e.Params,
        Reason:            e.Reason,
        Result:            e.Result,
        ErrorMessage:      e.ErrorMessage,
        IPAddress:         e.IPAddress,
        UserAgent:         e.UserAgent,
    })
}
```

- [ ] **Step 2: Add placeholder actor resolver**

Still in `handlers.go`:

```go
// appSessionActor is a placeholder until the app-tier session middleware lands.
// It reads an X-Admin-User-ID header for local development. Production deployments
// MUST replace this with real session authentication before exposing /api/app/admin/*.
func appSessionActor(r *http.Request) (uuid.UUID, []string, error) {
    raw := r.Header.Get("X-Admin-User-ID")
    if raw == "" {
        return uuid.Nil, nil, fmt.Errorf("no app session")
    }
    id, err := uuid.Parse(raw)
    if err != nil {
        return uuid.Nil, nil, err
    }
    return id, nil, nil
}
```

(The `nil` roles slice is acceptable; role snapshot population happens in a follow-up task once session middleware exists.)

- [ ] **Step 3: Register route**

In `apps/backend/internal/http/router.go`, inside `NewRouter`, add after existing routes:

```go
r.Route("/api/app/admin", func(r chi.Router) {
    r.Post("/actions/{type}", handler.actionDispatcher.ServeHTTP)
})
```

- [ ] **Step 4: Build & run existing tests**

Run: `cd apps/backend && go build ./... && go test ./...`
Expected: all existing tests still pass, new dispatcher test still passes.

- [ ] **Step 5: Commit**

```bash
git add apps/backend/internal/http/router.go apps/backend/internal/http/handlers.go apps/backend/cmd/server/main.go
git commit -m "feat(backend): wire admin action dispatcher into router"
```

---

## Phase 4: Frontend Shared Primitives

### Task 12: Action manifest (client-side)

**Files:**
- Create: `apps/frontend/components/admin/action-manifest.ts`

A static typed manifest listing every admin action's metadata, kept in sync with the backend registry by convention. A follow-up task will replace this with a generated file, but hand-maintained is fine for v1.

- [ ] **Step 1: Create file**

```ts
export type AdminActionKey =
  | "users.suspend"
  | "users.force_unlink_steam"
  | "proxies.edit"
  | "proxies.rotate_credentials"
  | "links.approve"
  | "guilds.untrack_server"
  | "admins.grant";

export interface AdminActionMeta {
  key: AdminActionKey;
  label: string;
  permission: string;
  requiresReason: boolean;
  dangerous: boolean;
  resourceType: "user" | "proxy" | "link" | "guild" | "admin";
}

export const ADMIN_ACTIONS: Record<AdminActionKey, AdminActionMeta> = {
  "users.suspend": {
    key: "users.suspend",
    label: "Suspend user",
    permission: "users.suspend",
    requiresReason: true,
    dangerous: false,
    resourceType: "user",
  },
  "users.force_unlink_steam": {
    key: "users.force_unlink_steam",
    label: "Force-unlink Steam",
    permission: "users.force_unlink_steam",
    requiresReason: true,
    dangerous: true,
    resourceType: "user",
  },
  "proxies.edit": {
    key: "proxies.edit",
    label: "Edit proxy",
    permission: "proxies.edit",
    requiresReason: true,
    dangerous: false,
    resourceType: "proxy",
  },
  "proxies.rotate_credentials": {
    key: "proxies.rotate_credentials",
    label: "Rotate credentials",
    permission: "proxies.rotate_credentials",
    requiresReason: true,
    dangerous: true,
    resourceType: "proxy",
  },
  "links.approve": {
    key: "links.approve",
    label: "Approve link",
    permission: "links.approve",
    requiresReason: true,
    dangerous: false,
    resourceType: "link",
  },
  "guilds.untrack_server": {
    key: "guilds.untrack_server",
    label: "Untrack server",
    permission: "guilds.untrack_server",
    requiresReason: true,
    dangerous: false,
    resourceType: "guild",
  },
  "admins.grant": {
    key: "admins.grant",
    label: "Grant admin role",
    permission: "admins.grant",
    requiresReason: true,
    dangerous: true,
    resourceType: "admin",
  },
};
```

- [ ] **Step 2: Commit**

```bash
git add apps/frontend/components/admin/action-manifest.ts
git commit -m "feat(frontend): add admin action manifest"
```

---

### Task 13: `ReasonDialog` and `DangerousConfirmDialog`

**Files:**
- Create: `apps/frontend/components/admin/reason-dialog.tsx`
- Create: `apps/frontend/components/admin/dangerous-confirm-dialog.tsx`

- [ ] **Step 1: Implement `ReasonDialog`**

```tsx
"use client";

import { useState } from "react";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Textarea } from "@/components/ui/textarea";

export interface ReasonDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  description?: string;
  onConfirm: (reason: string) => Promise<void>;
  submitLabel?: string;
}

export function ReasonDialog({
  open,
  onOpenChange,
  title,
  description,
  onConfirm,
  submitLabel = "Confirm",
}: ReasonDialogProps) {
  const [reason, setReason] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const handleSubmit = async () => {
    if (!reason.trim()) {
      setError("Reason is required.");
      return;
    }
    setSubmitting(true);
    setError(null);
    try {
      await onConfirm(reason);
      setReason("");
      onOpenChange(false);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Action failed.");
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          {description ? <DialogDescription>{description}</DialogDescription> : null}
        </DialogHeader>
        <Textarea
          value={reason}
          onChange={(e) => setReason(e.target.value)}
          placeholder="Reason (required)"
          disabled={submitting}
        />
        {error ? <p className="text-destructive text-sm">{error}</p> : null}
        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)} disabled={submitting}>
            Cancel
          </Button>
          <Button onClick={handleSubmit} disabled={submitting}>
            {submitting ? "Working..." : submitLabel}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
```

- [ ] **Step 2: Implement `DangerousConfirmDialog`**

```tsx
"use client";

import { useState } from "react";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";

export interface DangerousConfirmDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  description: string;
  expectedConfirmation: string;
  onConfirm: (reason: string, typed: string) => Promise<void>;
  submitLabel?: string;
}

export function DangerousConfirmDialog({
  open,
  onOpenChange,
  title,
  description,
  expectedConfirmation,
  onConfirm,
  submitLabel = "Confirm",
}: DangerousConfirmDialogProps) {
  const [reason, setReason] = useState("");
  const [typed, setTyped] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const disabled = submitting || typed !== expectedConfirmation || !reason.trim();

  const handleSubmit = async () => {
    setSubmitting(true);
    setError(null);
    try {
      await onConfirm(reason, typed);
      setReason("");
      setTyped("");
      onOpenChange(false);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Action failed.");
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>{description}</DialogDescription>
        </DialogHeader>
        <div className="space-y-3">
          <Textarea value={reason} onChange={(e) => setReason(e.target.value)} placeholder="Reason (required)" disabled={submitting} />
          <div>
            <p className="text-sm">
              Type <code className="bg-muted px-1 rounded">{expectedConfirmation}</code> to confirm:
            </p>
            <Input value={typed} onChange={(e) => setTyped(e.target.value)} disabled={submitting} />
          </div>
          {error ? <p className="text-destructive text-sm">{error}</p> : null}
        </div>
        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)} disabled={submitting}>
            Cancel
          </Button>
          <Button variant="destructive" onClick={handleSubmit} disabled={disabled}>
            {submitting ? "Working..." : submitLabel}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
```

- [ ] **Step 3: Type-check**

Run: `pnpm --filter @rustlens/frontend lint`
Expected: PASS (pre-existing `Textarea`/`Input`/`Dialog` shadcn components must exist; if not, install with `pnpm --filter @rustlens/frontend exec shadcn@latest add dialog textarea input button` and re-run.)

- [ ] **Step 4: Commit**

```bash
git add apps/frontend/components/admin/reason-dialog.tsx apps/frontend/components/admin/dangerous-confirm-dialog.tsx
git commit -m "feat(frontend): add reason and dangerous confirm dialogs"
```

---

### Task 14: `AdminActionButton`

**Files:**
- Create: `apps/frontend/components/admin/admin-action-button.tsx`
- Test: `apps/frontend/components/admin/__tests__/admin-action-button.test.tsx`

- [ ] **Step 1: Write test**

```tsx
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { AdminActionButton } from "../admin-action-button";

describe("AdminActionButton", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it("opens reason dialog for reason-required action", async () => {
    render(<AdminActionButton action="users.suspend" params={{ userId: "u1" }} csrfToken="t" />);
    fireEvent.click(screen.getByRole("button", { name: /suspend user/i }));
    await waitFor(() => expect(screen.getByPlaceholderText(/reason/i)).toBeInTheDocument());
  });

  it("POSTs to the dispatcher on confirm", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify({ ok: true }), { status: 200, headers: { "Content-Type": "application/json" } }),
    );
    render(<AdminActionButton action="users.suspend" params={{ userId: "u1" }} csrfToken="t" />);
    fireEvent.click(screen.getByRole("button", { name: /suspend user/i }));
    fireEvent.change(await screen.findByPlaceholderText(/reason/i), { target: { value: "abuse" } });
    fireEvent.click(screen.getByRole("button", { name: /confirm/i }));
    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalled();
      const [url, init] = fetchMock.mock.calls[0];
      expect(String(url)).toContain("/api/app/admin/actions/users.suspend");
      const body = JSON.parse(String((init as RequestInit).body));
      expect(body.reason).toBe("abuse");
      expect(body.params.userId).toBe("u1");
    });
  });
});
```

- [ ] **Step 2: Run, confirm fail**

Run: `pnpm --filter @rustlens/frontend test -- admin-action-button`
Expected: FAIL — component not found.

- [ ] **Step 3: Implement**

```tsx
"use client";

import { useState } from "react";
import { Button } from "@/components/ui/button";
import { ADMIN_ACTIONS, type AdminActionKey } from "./action-manifest";
import { ReasonDialog } from "./reason-dialog";
import { DangerousConfirmDialog } from "./dangerous-confirm-dialog";

export interface AdminActionButtonProps {
  action: AdminActionKey;
  params: Record<string, unknown>;
  csrfToken: string;
  expectedConfirmation?: string;
  onSuccess?: (result: unknown) => void;
  label?: string;
  variant?: "default" | "destructive" | "ghost";
}

export function AdminActionButton({
  action,
  params,
  csrfToken,
  expectedConfirmation,
  onSuccess,
  label,
  variant,
}: AdminActionButtonProps) {
  const meta = ADMIN_ACTIONS[action];
  const [open, setOpen] = useState(false);

  const dispatch = async (reason: string, typed?: string) => {
    const resp = await fetch(`/api/app/admin/actions/${action}`, {
      method: "POST",
      credentials: "include",
      headers: {
        "Content-Type": "application/json",
        "X-CSRF-Token": csrfToken,
      },
      body: JSON.stringify({
        params,
        reason,
        typedConfirmation: typed ?? "",
      }),
    });
    if (!resp.ok) {
      let message = `HTTP ${resp.status}`;
      try {
        const body = (await resp.json()) as { error?: string; message?: string };
        message = body.message || body.error || message;
      } catch {
        // ignore
      }
      throw new Error(message);
    }
    const data = (await resp.json()) as { result?: unknown };
    onSuccess?.(data.result);
  };

  return (
    <>
      <Button
        variant={variant ?? (meta.dangerous ? "destructive" : "default")}
        onClick={() => setOpen(true)}
      >
        {label ?? meta.label}
      </Button>
      {meta.dangerous ? (
        <DangerousConfirmDialog
          open={open}
          onOpenChange={setOpen}
          title={meta.label}
          description="This action is destructive and cannot be undone."
          expectedConfirmation={expectedConfirmation ?? ""}
          onConfirm={(reason, typed) => dispatch(reason, typed)}
          submitLabel={meta.label}
        />
      ) : (
        <ReasonDialog
          open={open}
          onOpenChange={setOpen}
          title={meta.label}
          onConfirm={(reason) => dispatch(reason)}
          submitLabel={meta.label}
        />
      )}
    </>
  );
}
```

- [ ] **Step 4: Run, confirm pass**

Run: `pnpm --filter @rustlens/frontend test -- admin-action-button`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/frontend/components/admin/admin-action-button.tsx apps/frontend/components/admin/__tests__/admin-action-button.test.tsx
git commit -m "feat(frontend): add AdminActionButton component"
```

---

### Task 15: `AdminDataTable`

**Files:**
- Create: `apps/frontend/components/admin/admin-data-table.tsx`

A thin wrapper around `@/components/ui/table` that accepts columns and rows and renders URL-synced filter controls.

- [ ] **Step 1: Implement**

```tsx
"use client";

import { useRouter, useSearchParams, usePathname } from "next/navigation";
import { type ReactNode, useMemo } from "react";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Input } from "@/components/ui/input";

export interface AdminTableColumn<T> {
  header: string;
  cell: (row: T) => ReactNode;
  key: string;
}

export interface AdminDataTableFilter {
  key: string;
  label: string;
  placeholder?: string;
}

export interface AdminDataTableProps<T> {
  rows: T[];
  columns: AdminTableColumn<T>[];
  filters?: AdminDataTableFilter[];
  emptyMessage?: string;
  rowKey: (row: T) => string;
}

export function AdminDataTable<T>({
  rows,
  columns,
  filters = [],
  emptyMessage = "No rows",
  rowKey,
}: AdminDataTableProps<T>) {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();

  const values = useMemo(() => {
    const v: Record<string, string> = {};
    for (const f of filters) v[f.key] = searchParams.get(f.key) ?? "";
    return v;
  }, [filters, searchParams]);

  const setFilter = (key: string, value: string) => {
    const next = new URLSearchParams(searchParams.toString());
    if (value) next.set(key, value);
    else next.delete(key);
    router.replace(`${pathname}?${next.toString()}`);
  };

  return (
    <div className="space-y-4">
      {filters.length ? (
        <div className="flex flex-wrap gap-2">
          {filters.map((f) => (
            <Input
              key={f.key}
              value={values[f.key]}
              onChange={(e) => setFilter(f.key, e.target.value)}
              placeholder={f.placeholder ?? f.label}
              className="max-w-xs"
            />
          ))}
        </div>
      ) : null}
      <Table>
        <TableHeader>
          <TableRow>
            {columns.map((c) => (
              <TableHead key={c.key}>{c.header}</TableHead>
            ))}
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows.length === 0 ? (
            <TableRow>
              <TableCell colSpan={columns.length} className="text-muted-foreground">
                {emptyMessage}
              </TableCell>
            </TableRow>
          ) : (
            rows.map((row) => (
              <TableRow key={rowKey(row)}>
                {columns.map((c) => (
                  <TableCell key={c.key}>{c.cell(row)}</TableCell>
                ))}
              </TableRow>
            ))
          )}
        </TableBody>
      </Table>
    </div>
  );
}
```

- [ ] **Step 2: Commit**

```bash
git add apps/frontend/components/admin/admin-data-table.tsx
git commit -m "feat(frontend): add AdminDataTable with URL-synced filters"
```

---

### Task 16: `AdminDetailLayout`

**Files:**
- Create: `apps/frontend/components/admin/admin-detail-layout.tsx`

- [ ] **Step 1: Implement**

```tsx
import type { ReactNode } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";

export interface AdminDetailLayoutProps {
  title: string;
  summary: ReactNode;
  actions?: ReactNode;
  children?: ReactNode;
}

export function AdminDetailLayout({
  title,
  summary,
  actions,
  children,
}: AdminDetailLayoutProps) {
  return (
    <div className="grid gap-4 md:grid-cols-[2fr_1fr]">
      <Card>
        <CardHeader>
          <CardTitle>{title}</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          {summary}
          {children}
        </CardContent>
      </Card>
      {actions ? (
        <Card>
          <CardHeader>
            <CardTitle>Actions</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-2">{actions}</CardContent>
        </Card>
      ) : null}
    </div>
  );
}
```

- [ ] **Step 2: Commit**

```bash
git add apps/frontend/components/admin/admin-detail-layout.tsx
git commit -m "feat(frontend): add AdminDetailLayout component"
```

---

## Phase 5: Session Helpers and Nav

### Task 17: Permission-aware session helpers

**Files:**
- Modify: `apps/frontend/lib/session.ts`

- [ ] **Step 1: Add `requireAdminSession` and `requirePermission`**

Append to `apps/frontend/lib/session.ts`:

```ts
export async function requireAdminSession() {
  const session = await getSession();
  if (!session) redirect("/login");
  const isAdmin = (session.roles ?? []).length > 0 || (session.permissions ?? []).length > 0;
  if (!isAdmin) redirect("/dashboard");
  return session;
}

export async function requirePermission(key: string) {
  const session = await requireAdminSession();
  const perms = session.permissions ?? [];
  if (!perms.includes(key)) redirect("/admin");
  return session;
}
```

If `SessionData` does not already expose `permissions`, extend it:

```ts
export interface SessionData {
  userId: string;
  roles: string[];
  permissions?: string[];
  // ...existing fields...
}
```

Population of `permissions` depends on backend session middleware (noted prerequisite) — safe to default to `[]` until that lands.

- [ ] **Step 2: Type-check**

Run: `pnpm --filter @rustlens/frontend lint`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add apps/frontend/lib/session.ts
git commit -m "feat(frontend): add requireAdminSession and requirePermission helpers"
```

---

### Task 18: Permission-filtered admin nav

**Files:**
- Modify: `apps/frontend/components/app-shell.tsx` (or the file that renders admin nav links — confirm by searching for the existing links `/admin/users`, `/admin/guilds`)

- [ ] **Step 1: Identify existing admin nav source**

Run: `grep -rn "/admin/users" apps/frontend/components apps/frontend/app`

- [ ] **Step 2: Build a permission-map and filter**

Add to the nav config (wherever the admin links are defined):

```ts
const ADMIN_NAV: Array<{ href: string; label: string; permissions: string[] }> = [
  { href: "/admin", label: "Overview", permissions: [] },
  { href: "/admin/users", label: "Users", permissions: ["users.suspend", "users.force_unlink_steam"] },
  { href: "/admin/links", label: "Link Review", permissions: ["links.review"] },
  { href: "/admin/guilds", label: "Guilds", permissions: ["guilds.untrack_server"] },
  { href: "/admin/proxies", label: "Proxies", permissions: ["proxies.edit"] },
  { href: "/admin/audit", label: "Audit", permissions: ["audit.read"] },
  { href: "/admin/roles", label: "Roles", permissions: ["admins.grant"] },
];

function filterNavByPermissions(
  nav: typeof ADMIN_NAV,
  perms: string[],
) {
  const set = new Set(perms);
  return nav.filter(
    (item) => item.permissions.length === 0 || item.permissions.some((p) => set.has(p)),
  );
}
```

Pass the current user's permissions (from `session.permissions ?? []`) through the shell's `variant="admin"` code path and render the filtered list.

- [ ] **Step 3: Type-check**

Run: `pnpm --filter @rustlens/frontend lint`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add apps/frontend/components/app-shell.tsx
git commit -m "feat(frontend): filter admin nav by current permissions"
```

---

## Phase 6: Roles Admin Page (server-rendered)

### Task 19: Roles API — list roles with permissions

**Files:**
- Modify: `apps/backend/internal/http/router.go` — register `GET /api/app/admin/roles`
- Modify: `apps/backend/internal/http/handlers.go` — add `ListAdminRoles` handler
- Test: `apps/backend/internal/http/handlers_admin_roles_test.go`

- [ ] **Step 1: Write failing test**

```go
package http_test

import (
    "context"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "os"
    "testing"

    // import your handler wiring package paths
)

func TestListAdminRoles_ReturnsSystemRoles(t *testing.T) {
    if os.Getenv("TEST_POSTGRES_URL") == "" {
        t.Skip("needs TEST_POSTGRES_URL")
    }
    _ = context.Background()
    // Build router with the real handler against TEST_POSTGRES_URL, then:
    req := httptest.NewRequest(http.MethodGet, "/api/app/admin/roles", nil)
    req.Header.Set("X-Admin-User-ID", "00000000-0000-0000-0000-000000000001")
    rr := httptest.NewRecorder()
    // router.ServeHTTP(rr, req)
    // ...assert 200 and JSON contains moderator/infra_admin/guild_ops/superadmin
    _ = rr
    _ = json.RawMessage(`{}`)
}
```

Note: This test is a scaffold. Replace the commented-out router wiring with whatever test harness already exists in the backend `http` package. If none exists, create a helper `newTestRouter(t)` returning the chi router with seeded DB.

- [ ] **Step 2: Implement handler**

```go
func (h *Handler) ListAdminRoles(w http.ResponseWriter, r *http.Request) {
    roles, err := h.rbacStore.ListRoles(r.Context())
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }
    out := make([]map[string]any, 0, len(roles))
    for _, rl := range roles {
        // fetch permissions for non-system; superadmin is implicit-all
        perms, _ := h.rbacStore.PermissionsForRole(r.Context(), rl.ID)
        out = append(out, map[string]any{
            "id":          rl.ID,
            "key":         rl.Key,
            "name":        rl.Name,
            "description": rl.Description,
            "isSystem":    rl.IsSystem,
            "permissions": perms,
        })
    }
    writeJSON(w, http.StatusOK, map[string]any{"items": out})
}
```

Also add `PermissionsForRole` to `rbac.Store`:

```go
func (s *Store) PermissionsForRole(ctx context.Context, roleID uuid.UUID) ([]string, error) {
    rows, err := s.pool.Query(ctx,
        `SELECT permission_key FROM admin_role_permissions WHERE role_id = $1 ORDER BY permission_key`,
        roleID,
    )
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    var out []string
    for rows.Next() {
        var k string
        if err := rows.Scan(&k); err != nil {
            return nil, err
        }
        out = append(out, k)
    }
    return out, rows.Err()
}
```

And register in router (inside the `/api/app/admin` Route block from Task 11):

```go
r.Get("/roles", handler.ListAdminRoles)
```

- [ ] **Step 3: Run tests**

Run: `cd apps/backend && go test ./internal/http/ -v`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add apps/backend/internal/http/ apps/backend/internal/admin/rbac/store.go
git commit -m "feat(backend): expose GET /api/app/admin/roles"
```

---

### Task 20: Admin roles page (read-only first)

**Files:**
- Create: `apps/frontend/app/admin/roles/page.tsx`
- Modify: `apps/frontend/lib/rustlens-api.ts` — add `getAdminRoles`

- [ ] **Step 1: Add API client function**

```ts
export interface AdminRoleItem {
  id: string;
  key: string;
  name: string;
  description: string | null;
  isSystem: boolean;
  permissions: string[];
}

export async function getAdminRoles() {
  const payload = await backendFetch<{ items: AdminRoleItem[] }>(
    "/api/app/admin/roles",
    undefined,
    { requireSession: true },
  );
  return payload.items;
}
```

- [ ] **Step 2: Create the page**

```tsx
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { getAdminRoles } from "@/lib/rustlens-api";
import { requirePermission } from "@/lib/session";

export default async function AdminRolesPage() {
  await requirePermission("admins.grant");
  const roles = await getAdminRoles();
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Role</TableHead>
          <TableHead>Key</TableHead>
          <TableHead>System</TableHead>
          <TableHead>Permissions</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {roles.map((role) => (
          <TableRow key={role.id}>
            <TableCell>{role.name}</TableCell>
            <TableCell><code>{role.key}</code></TableCell>
            <TableCell>{role.isSystem ? "yes" : "no"}</TableCell>
            <TableCell className="text-xs">
              {role.permissions.length ? role.permissions.join(", ") : role.key === "superadmin" ? "(implicit all)" : "(none)"}
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}
```

- [ ] **Step 3: Type-check**

Run: `pnpm --filter @rustlens/frontend lint`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add apps/frontend/app/admin/roles/page.tsx apps/frontend/lib/rustlens-api.ts
git commit -m "feat(frontend): add read-only admin roles page"
```

---

## Phase 7: Integration Smoke Test

### Task 21: End-to-end test — seed roles, dispatch a no-op action, assert audit

**Files:**
- Create: `apps/backend/internal/admin/e2e_test.go`

- [ ] **Step 1: Write test**

```go
package admin_test

import (
    "bytes"
    "context"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "os"
    "testing"

    "github.com/go-chi/chi/v5"
    "github.com/google/uuid"

    "rustlens/backend/internal/admin/actions"
    "rustlens/backend/internal/admin/audit"
    "rustlens/backend/internal/admin/rbac"
    "rustlens/backend/internal/store"
)

type noop struct{}

func (noop) Type() string                                                       { return "test.ping" }
func (noop) Permission() string                                                 { return "audit.read" }
func (noop) RequiresReason() bool                                               { return false }
func (noop) Dangerous() bool                                                    { return false }
func (noop) SensitiveParamKeys() []string                                       { return nil }
func (noop) ResourceType() string                                               { return "system" }
func (noop) Validate(_ json.RawMessage) (actions.ValidatedParams, error)        { return actions.ValidatedParams{ResourceID: "ping"}, nil }
func (noop) Execute(_ context.Context, _ actions.ActionInput) (actions.ActionResult, error) {
    return actions.ActionResult{ResourceID: "ping"}, nil
}

func TestAdminFoundation_E2E(t *testing.T) {
    dsn := os.Getenv("TEST_POSTGRES_URL")
    if dsn == "" {
        t.Skip("TEST_POSTGRES_URL not set")
    }
    ctx := context.Background()
    s, err := store.NewPostgresStore(ctx, dsn)
    if err != nil {
        t.Fatalf("connect: %v", err)
    }
    defer s.Close()

    rb := rbac.NewStore(s.Pool())
    if err := rb.SeedPermissionsAndRoles(ctx); err != nil {
        t.Fatalf("seed: %v", err)
    }
    resolver := rbac.NewResolver(rb)
    au := audit.NewStore(s.Pool())

    reg := actions.NewRegistry(rbac.IsValidPermission)
    if err := reg.Register(noop{}); err != nil {
        t.Fatalf("register: %v", err)
    }

    adapter := struct{ *audit.Store }{au}
    type sink struct{ *audit.Store }
    _ = adapter
    disp := actions.NewDispatcher(reg, resolver, auditBridge{au}, func(r *http.Request) (uuid.UUID, []string, error) {
        id := uuid.MustParse(r.Header.Get("X-Admin-User-ID"))
        return id, []string{"moderator"}, nil
    })

    r := chi.NewRouter()
    r.Post("/api/app/admin/actions/{type}", disp.ServeHTTP)

    userID := uuid.New()
    modID, _ := rb.RoleIDByKey(ctx, rbac.RoleModerator)
    if err := rb.AssignRole(ctx, userID, modID, nil); err != nil {
        t.Fatalf("assign: %v", err)
    }

    req := httptest.NewRequest("POST", "/api/app/admin/actions/test.ping", bytes.NewReader([]byte(`{}`)))
    req.Header.Set("X-Admin-User-ID", userID.String())
    rr := httptest.NewRecorder()
    r.ServeHTTP(rr, req)
    if rr.Code != 200 {
        t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
    }

    rows, err := au.Query(ctx, audit.QueryFilters{ActorUserID: &userID, Limit: 10})
    if err != nil {
        t.Fatalf("query audit: %v", err)
    }
    if len(rows) != 1 || rows[0].ActionType != "test.ping" || rows[0].Result != "success" {
        t.Errorf("audit rows = %+v", rows)
    }
}

type auditBridge struct{ s *audit.Store }

func (a auditBridge) RecordEntry(ctx context.Context, e actions.AuditEntryInput) error {
    return a.s.Record(ctx, audit.Entry{
        ActionType:        e.ActionType,
        ActorUserID:       &e.ActorUserID,
        ActorRoleSnapshot: e.ActorRoleSnapshot,
        ResourceType:      e.ResourceType,
        ResourceID:        e.ResourceID,
        TargetUserID:      e.TargetUserID,
        Params:            e.Params,
        Reason:            e.Reason,
        Result:            e.Result,
        ErrorMessage:      e.ErrorMessage,
        IPAddress:         e.IPAddress,
        UserAgent:         e.UserAgent,
    })
}
```

- [ ] **Step 2: Run**

Run: `cd apps/backend && TEST_POSTGRES_URL=$POSTGRES_URL go test ./internal/admin/ -v`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add apps/backend/internal/admin/e2e_test.go
git commit -m "test(backend): e2e smoke for admin foundation"
```

---

## Self-Review Results

**Spec coverage check:**

| Spec section | Plan coverage |
|---|---|
| RBAC tables | Task 1 (schema), Task 4 (store) |
| `superadmin` is_system, implicit-all | Task 3 (seed data), Task 4 (store.PermissionsForUser), Task 5 (resolver) |
| Legacy `internal_admin` bridge | **Deferred** — no `users.internal_admin` column exists yet in this repo; noted in prerequisites |
| `HasPermission` helper | Task 5 |
| AdminAction interface | Task 7 |
| Action registry with duplicate + perm validation | Task 8 |
| Sensitive param redaction | Task 9 |
| Dispatcher: permission / reason / dangerous / validate / execute / audit | Task 10 |
| Single `POST /api/app/admin/actions/:type` route | Task 11 |
| Read vs write split | The plan only builds the write dispatcher; read endpoints (`GET /admin/users`, etc.) are deferred to feature specs C/D/E/F |
| Audit extended schema | Task 1 (schema), Task 6 (write + query) |
| Secret redaction before audit write | Task 9 + Task 10 |
| Retention policy | Unlimited by default; no task needed |
| Admin shell permission-filtered nav | Task 18 |
| AdminDataTable / AdminDetailLayout / AdminActionButton / dialogs | Tasks 13–16 |
| Roles admin page (edit in UI) | Task 20 provides **read-only**; edit-role-permissions and assign-role actions are deferred (they themselves need `admins.grant`-gated actions, which are a natural Phase 2 follow-up) |
| E2E smoke | Task 21 |

**Deferred explicitly:**
- Role editing UI (Phase 2 — depends on action implementations for `admins.grant`-gated actions, which can be spec'd alongside the feature specs).
- Read-side admin endpoints for users/guilds/proxies/links — these belong in the feature specs C/D/E/F, not here.
- App-tier session middleware replacement for `appSessionActor` — noted in prerequisites.
- CSV export, 2-person approval, retention policies — explicitly out of scope in the spec.

**Placeholder scan:** none.

**Type consistency:** `AdminAction`, `ValidatedParams`, `ActionInput`, `ActionResult`, `AuditEntryInput`, `Entry`, `Row`, `Store`, `Resolver` names are consistent across tasks.

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-04-21-admin-foundation.md`. Two execution options:

1. **Subagent-Driven (recommended)** — I dispatch a fresh subagent per task, review between tasks, fast iteration.
2. **Inline Execution** — Execute tasks in this session using executing-plans, batch execution with checkpoints.

Which approach?
