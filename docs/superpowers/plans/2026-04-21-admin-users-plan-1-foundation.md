# Admin Users — Plan 1: Foundation

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship a searchable/filterable/paginated read-only users list at `/admin/users`, rekey `admin_user_roles` to `app_users(id) TEXT`, replace the UUID placeholder actor with the real session user id, and seed the full `users.*` permission set.

**Architecture:** Schema additions are appended to `EnsureSchema` as idempotent `ALTER ... ADD COLUMN IF NOT EXISTS`. `admin_user_roles` is dropped and recreated with TEXT FK to `app_users`. `appSessionActor` is rewired to read the validated session's `userId`. `GET /api/app/admin/users` grows filter/sort/pagination query params. The existing `/admin/users/page.tsx` is replaced with a filter-bar + paginated table built on the existing `AdminDataTable`. No mutations in this plan.

**Tech Stack:** Go 1.24, chi, pgx v5 · Next.js 16.2.3 (async searchParams), React 19, TypeScript, shadcn/ui · Vitest.

---

## File Structure

**Backend**
- Modify: `apps/backend/internal/store/postgres.go` — add columns + rekey admin_user_roles + drop users(UUID)
- Modify: `apps/backend/internal/admin/rbac/permissions.go` — new permission constants
- Modify: `apps/backend/internal/admin/rbac/roles.go` — three new seeded roles
- Modify: `apps/backend/internal/admin/rbac/store.go` — only if the seed code path needs touching
- Modify: `apps/backend/internal/http/admin_read_handlers.go` — expand `ListAdminUsers` with filters, sort, pagination, response envelope
- Modify: `apps/backend/internal/appauth/session.go` or wherever `appSessionActor` lives — use real session userId
- Modify: `apps/backend/internal/admin/rbac/resolver.go` and/or `store.go` — query `admin_user_roles` by TEXT user_id
- Test: `apps/backend/internal/admin/rbac/store_test.go` — seed idempotency + new perms
- Test: `apps/backend/internal/http/admin_read_handlers_test.go` — create if missing; filter/sort/pagination

**Frontend**
- Modify: `apps/frontend/lib/rustlens-api.ts` — update `AdminUserItem`, add filter params to `getAdminUsers`, return envelope with total/page
- Modify: `apps/frontend/app/admin/users/page.tsx` — replace with async-searchParams RSC using filter bar + AdminDataTable
- Create: `apps/frontend/app/admin/users/_components/users-filter-bar.tsx` — client component, URL-synced
- Create: `apps/frontend/app/admin/users/_components/users-pagination.tsx` — client component
- Test: `apps/frontend/app/admin/users/_components/__tests__/users-filter-bar.test.tsx`
- Test: `apps/frontend/app/admin/users/_components/__tests__/users-pagination.test.tsx`

---

## Prerequisites

Before starting, the implementer must have:
- `TEST_POSTGRES_URL` env var set for Go tests (an empty throwaway Postgres).
- Local Postgres running (`docker exec infra-postgres-1 psql ...` works).
- Node modules installed in `apps/frontend`.

---

## Task 1: Schema — add `app_users` lifecycle columns

**Files:**
- Modify: `apps/backend/internal/store/postgres.go` (the `EnsureSchema` method, in the `app_users`-related block)

- [ ] **Step 1: Read `apps/backend/internal/store/postgres.go` to locate the `app_users` block.** The file has `EnsureSchema` with a sequence of `CREATE TABLE IF NOT EXISTS` statements. Find the `app_users` block (approximately line 57 onwards per the exploration report).

- [ ] **Step 2: Add idempotent `ALTER TABLE` after the `CREATE TABLE app_users` block.** Insert this exact block:

```go
	if _, err := s.pool.Exec(ctx, `
		ALTER TABLE app_users
			ADD COLUMN IF NOT EXISTS suspended_at   TIMESTAMPTZ NULL,
			ADD COLUMN IF NOT EXISTS suspended_by   TEXT        NULL REFERENCES app_users(id),
			ADD COLUMN IF NOT EXISTS suspend_reason TEXT        NULL,
			ADD COLUMN IF NOT EXISTS deleted_at     TIMESTAMPTZ NULL,
			ADD COLUMN IF NOT EXISTS deleted_by     TEXT        NULL REFERENCES app_users(id),
			ADD COLUMN IF NOT EXISTS session_epoch  INT         NOT NULL DEFAULT 0;
	`); err != nil {
		return fmt.Errorf("alter app_users lifecycle columns: %w", err)
	}
	if _, err := s.pool.Exec(ctx, `
		CREATE INDEX IF NOT EXISTS app_users_active_idx
			ON app_users (created_at DESC)
			WHERE deleted_at IS NULL
	`); err != nil {
		return fmt.Errorf("create app_users_active_idx: %w", err)
	}
```

- [ ] **Step 3: Run `go build ./...` in `apps/backend/`.** Expected: compiles clean.

- [ ] **Step 4: Re-run the app locally (or call `EnsureSchema` from a test) and confirm the columns appear.**
  ```
  docker exec infra-postgres-1 psql -U rustlens -d rustlens -c '\d app_users'
  ```
  Expected: `suspended_at`, `suspended_by`, `suspend_reason`, `deleted_at`, `deleted_by`, `session_epoch` present.

- [ ] **Step 5: Commit.**
  ```
  git add apps/backend/internal/store/postgres.go
  git commit -m "feat(backend): add app_users lifecycle columns and active index"
  ```

---

## Task 2: Schema — rekey `admin_user_roles` to `app_users(id) TEXT`

**Context:** The live DB has `admin_user_roles.user_id UUID` with **no FK**. No production data. We drop-and-recreate with TEXT FK.

**Files:**
- Modify: `apps/backend/internal/store/postgres.go`

- [ ] **Step 1: Locate the `admin_user_roles` `CREATE TABLE` block in `EnsureSchema`.** Replace it with:

```go
	// admin_user_roles: rekeyed to app_users(id) TEXT. A one-time DROP removes
	// the old UUID-shaped table if present (no production data).
	if _, err := s.pool.Exec(ctx, `
		DO $$
		BEGIN
			IF EXISTS (
				SELECT 1 FROM information_schema.columns
				WHERE table_name = 'admin_user_roles' AND column_name = 'user_id' AND data_type = 'uuid'
			) THEN
				DROP TABLE admin_user_roles;
			END IF;
		END $$;
	`); err != nil {
		return fmt.Errorf("drop legacy admin_user_roles (uuid): %w", err)
	}
	if _, err := s.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS admin_user_roles (
			user_id    TEXT        NOT NULL REFERENCES app_users(id) ON DELETE CASCADE,
			role_id    UUID        NOT NULL REFERENCES admin_roles(id) ON DELETE CASCADE,
			granted_by TEXT        NULL     REFERENCES app_users(id),
			granted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			PRIMARY KEY (user_id, role_id)
		)
	`); err != nil {
		return fmt.Errorf("create admin_user_roles: %w", err)
	}
```

Note: `role_id` stays UUID because `admin_roles.id` is UUID in the live schema (per explore report). Leave `admin_roles` untouched.

- [ ] **Step 2: Run `go build ./...`.** Expected: clean.

- [ ] **Step 3: Confirm in DB.**
  ```
  docker exec infra-postgres-1 psql -U rustlens -d rustlens -c '\d admin_user_roles'
  ```
  Expected: `user_id TEXT`, FK to `app_users(id)`, composite PK.

- [ ] **Step 4: Commit.**
  ```
  git add apps/backend/internal/store/postgres.go
  git commit -m "feat(backend): rekey admin_user_roles to app_users(id) TEXT"
  ```

---

## Task 3: Schema — drop orphaned `users(UUID)` if unreferenced

**Files:**
- Modify: `apps/backend/internal/store/postgres.go`

- [ ] **Step 1: Grep for references to the `users` UUID table.**
  ```
  git grep -n '"users"\|from users\|FROM users\|REFERENCES users(' apps/backend
  ```
  If any non-trivial code reads `users(UUID)`, stop this task and report; otherwise continue.

- [ ] **Step 2: Remove the `CREATE TABLE users(...)` block from `EnsureSchema`** if present.

- [ ] **Step 3: Add a drop after the remove** (guard: only drop if row count is zero, to avoid losing data we don't know about):

```go
	if _, err := s.pool.Exec(ctx, `
		DO $$
		BEGIN
			IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'users')
			   AND NOT EXISTS (SELECT 1 FROM users LIMIT 1) THEN
				DROP TABLE users CASCADE;
			END IF;
		END $$;
	`); err != nil {
		return fmt.Errorf("drop legacy users(uuid): %w", err)
	}
```

- [ ] **Step 4: Run `go build ./...`.** Expected: clean.

- [ ] **Step 5: Commit.**
  ```
  git add apps/backend/internal/store/postgres.go
  git commit -m "refactor(backend): drop orphaned users(uuid) table"
  ```

---

## Task 4: Test — schema idempotency

**Files:**
- Modify or create: `apps/backend/internal/store/postgres_schema_test.go`

- [ ] **Step 1: Write a test that calls `EnsureSchema` twice against the same pool and asserts no error.** If a test file for the store already exists, add this test case.

```go
func TestEnsureSchema_Idempotent(t *testing.T) {
	ctx := context.Background()
	s := newTestPostgres(t)
	if err := s.EnsureSchema(ctx); err != nil {
		t.Fatalf("first EnsureSchema: %v", err)
	}
	if err := s.EnsureSchema(ctx); err != nil {
		t.Fatalf("second EnsureSchema: %v", err)
	}
	// Spot-check the new columns exist.
	var count int
	err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM information_schema.columns
		WHERE table_name = 'app_users'
		  AND column_name IN ('suspended_at','deleted_at','session_epoch')
	`).Scan(&count)
	if err != nil {
		t.Fatalf("count columns: %v", err)
	}
	if count != 3 {
		t.Fatalf("expected 3 lifecycle columns on app_users, got %d", count)
	}
}
```

- [ ] **Step 2: Run `go test ./internal/store/ -run TestEnsureSchema_Idempotent -v`.** Expected: PASS.

- [ ] **Step 3: Commit.**
  ```
  git add apps/backend/internal/store/postgres_schema_test.go
  git commit -m "test(backend): EnsureSchema is idempotent and installs lifecycle columns"
  ```

---

## Task 5: RBAC — add `users.*` permission constants

**Files:**
- Modify: `apps/backend/internal/admin/rbac/permissions.go`

- [ ] **Step 1: Read the current permissions file.** Note the existing constants: `users.suspend`, `users.force_unlink_steam`, `admins.grant`, etc.

- [ ] **Step 2: Add the full `users.*` set.** Replace the existing list of constants to include all of these (preserve existing keys):

```go
const (
	PermUsersRead             = "users.read"
	PermUsersEdit             = "users.edit"
	PermUsersRolesAssign      = "users.roles.assign"
	PermUsersSuspend          = "users.suspend"
	PermUsersSecurityReset    = "users.security.reset"
	PermUsersEmailManage      = "users.email.manage"
	PermUsersCreate           = "users.create"
	PermUsersConnectionsManage = "users.connections.manage"
	PermUsersDelete           = "users.delete"
	PermUsersExport           = "users.export"
	PermUsersAuditRead        = "users.audit.read"

	// Existing — keep:
	PermAdminsGrant       = "admins.grant"
	PermUsersForceUnlink  = "users.force_unlink_steam"
	PermProxiesEdit       = "proxies.edit"
	PermLinksReview       = "links.review"
	PermLinksApprove      = "links.approve"
	PermGuildsUntrackServer = "guilds.untrack_server"
	PermAuditRead         = "audit.read"
)
```

Adjust names to match the existing file's naming convention — if existing constants use a different prefix, follow suit. Just ensure every string literal from the table below is present:

```
users.read, users.edit, users.roles.assign, users.suspend,
users.security.reset, users.email.manage, users.create,
users.connections.manage, users.delete, users.export, users.audit.read
```

- [ ] **Step 3: Update `AllPermissions()` (the function that returns the seedable list)** to include every new key above.

- [ ] **Step 4: Run `go build ./...`.** Expected: clean.

- [ ] **Step 5: Commit.**
  ```
  git add apps/backend/internal/admin/rbac/permissions.go
  git commit -m "feat(backend): add users.* permission constants"
  ```

---

## Task 6: RBAC — seed `user_support`, `user_admin`, `user_auditor` roles

**Files:**
- Modify: `apps/backend/internal/admin/rbac/roles.go`

- [ ] **Step 1: Read the current roles file.** Note `SystemRoles()` shape.

- [ ] **Step 2: Add three new role definitions** to the `SystemRoles()` return. Preserve existing roles.

```go
{
	Key:         "user_support",
	Name:        "User Support",
	Description: "Read users + audit + trigger password resets and verification resends",
	IsSystem:    true,
	Permissions: []string{
		PermUsersRead, PermUsersAuditRead,
		PermUsersSecurityReset, PermUsersEmailManage, // resend-verify uses the same perm
		PermUsersEdit,
	},
},
{
	Key:         "user_admin",
	Name:        "User Admin",
	Description: "Full user lifecycle management (non-destructive)",
	IsSystem:    true,
	Permissions: []string{
		PermUsersRead, PermUsersAuditRead, PermUsersEdit,
		PermUsersRolesAssign, PermUsersSuspend,
		PermUsersSecurityReset, PermUsersEmailManage,
		PermUsersCreate, PermUsersConnectionsManage,
		PermUsersDelete, PermUsersExport,
	},
},
{
	Key:         "user_auditor",
	Name:        "User Auditor",
	Description: "Read-only users view with export",
	IsSystem:    true,
	Permissions: []string{PermUsersRead, PermUsersAuditRead, PermUsersExport},
},
```

- [ ] **Step 3: Run `go build ./...`.** Expected: clean.

- [ ] **Step 4: Commit.**
  ```
  git add apps/backend/internal/admin/rbac/roles.go
  git commit -m "feat(backend): seed user_support, user_admin, user_auditor roles"
  ```

---

## Task 7: Test — RBAC seed idempotency and new perms installed

**Files:**
- Modify: `apps/backend/internal/admin/rbac/store_test.go`

- [ ] **Step 1: Add a test that calls `SeedPermissionsAndRoles` twice against the same pool and verifies no error and the new rows exist.**

```go
func TestSeedPermissionsAndRoles_InstallsUsersPerms(t *testing.T) {
	ctx := context.Background()
	s := newTestPostgres(t)
	if err := s.EnsureSchema(ctx); err != nil { t.Fatal(err) }
	store := NewStore(s.Pool())
	if err := store.SeedPermissionsAndRoles(ctx); err != nil { t.Fatal(err) }
	if err := store.SeedPermissionsAndRoles(ctx); err != nil { t.Fatalf("second seed: %v", err) }

	want := []string{
		"users.read","users.edit","users.roles.assign","users.suspend",
		"users.security.reset","users.email.manage","users.create",
		"users.connections.manage","users.delete","users.export","users.audit.read",
	}
	for _, p := range want {
		var count int
		err := s.Pool().QueryRow(ctx,
			`SELECT COUNT(*) FROM admin_permissions WHERE key = $1`, p,
		).Scan(&count)
		if err != nil { t.Fatal(err) }
		if count != 1 { t.Errorf("permission %s: want 1 row, got %d", p, count) }
	}
	// New roles exist
	for _, r := range []string{"user_support","user_admin","user_auditor"} {
		var count int
		err := s.Pool().QueryRow(ctx,
			`SELECT COUNT(*) FROM admin_roles WHERE key = $1`, r,
		).Scan(&count)
		if err != nil { t.Fatal(err) }
		if count != 1 { t.Errorf("role %s: want 1 row, got %d", r, count) }
	}
}
```

- [ ] **Step 2: Run `go test ./internal/admin/rbac/ -run TestSeedPermissionsAndRoles_InstallsUsersPerms -v`.** Expected: PASS.

- [ ] **Step 3: Commit.**
  ```
  git add apps/backend/internal/admin/rbac/store_test.go
  git commit -m "test(backend): seed installs users.* perms and three new roles"
  ```

---

## Task 8: Fix actor resolution — use session `userId` (TEXT)

**Context:** `appSessionActor()` currently reads `X-Admin-User-ID` header as a UUID placeholder. The permission resolver queries `admin_user_roles` by that. After Task 2, `admin_user_roles.user_id` is TEXT; we need the actor to be the TEXT session user id.

**Files:**
- Modify: wherever `appSessionActor` lives (likely `apps/backend/internal/appauth/session.go` or `apps/backend/internal/http/middleware.go` — grep for the function).
- Modify: `apps/backend/internal/admin/rbac/resolver.go` and/or `store.go` — update any query that types `user_id` as UUID.

- [ ] **Step 1: Grep for `appSessionActor` and for `X-Admin-User-ID`.**
  ```
  git grep -n 'appSessionActor\|X-Admin-User-ID' apps/backend
  ```

- [ ] **Step 2: Rewrite `appSessionActor` to return the TEXT user id from the validated session cookie.** The session is already validated by `requireSession` middleware before the dispatcher runs; read the `SessionUser` from context (or re-validate the cookie if that's the pattern). Header fallback is removed.

```go
// appSessionActor returns the actor for admin action dispatch. Must be called
// after requireSession middleware has validated the cookie.
func appSessionActor(r *http.Request) (ActorID, error) {
	u, ok := appauth.SessionUserFromContext(r.Context())
	if !ok || u.UserID == "" {
		return "", errors.New("no session user in context")
	}
	return ActorID(u.UserID), nil
}
```

If `SessionUserFromContext` doesn't exist yet, add it as part of this task (it's a tiny helper — set/get via a private context key in `appauth`).

- [ ] **Step 3: Update the permission resolver's SQL** to treat `admin_user_roles.user_id` as `TEXT` (likely just removing a `::uuid` cast):

```
// in resolver.go or store.go, the query that looks up a user's roles:
SELECT r.key FROM admin_roles r
JOIN admin_user_roles aur ON aur.role_id = r.id
WHERE aur.user_id = $1   -- $1 is TEXT now
```

- [ ] **Step 4: Update any action that records `actor_user_id` or `target_user_id` in `audit_events`** — these columns are UUID per the DB report. **Leave `audit_events` as-is for now** (action dispatcher writes to it; we won't break it in this plan). The existing flow continues to work because the dispatcher already converts. If tests fail, coordinate with Plan 2 which introduces new actions.

  Note: this is a soft coupling. If actor-write to `audit_events` breaks due to UUID-vs-TEXT mismatch, patch by allowing the dispatcher to accept TEXT actor ids and NULL-out the UUID column for non-UUID actors, or change `audit_events.actor_user_id` to TEXT in a follow-up schema change. Add a TODO note in the code and surface it in the plan summary if needed.

- [ ] **Step 5: Run existing tests.** `go test ./internal/admin/... ./internal/http/... -v`. Expected: existing tests still pass. If they fail due to the UUID/TEXT mismatch in audit_events, change `audit_events.actor_user_id` and `audit_events.target_user_id` to `TEXT` in `EnsureSchema` (add a DO-block migration similar to Task 2) and proceed.

- [ ] **Step 6: Commit.**
  ```
  git add -u
  git commit -m "refactor(backend): resolve admin actor from validated session (TEXT user id)"
  ```

---

## Task 9: Backend — expand `ListAdminUsers` with filters, sort, pagination

**Files:**
- Modify: `apps/backend/internal/http/admin_read_handlers.go` (the `ListAdminUsers` function)

- [ ] **Step 1: Read the current handler** (lines ~61-123).

- [ ] **Step 2: Replace the handler with the expanded version.** Parse `q`, `role`, `status`, `discord`, `steam`, `emailVerified`, `sort`, `page`, `pageSize`. Build the WHERE clause with parameterized fragments. Return `{items, total, page, pageSize}`.

```go
// ListAdminUsers: GET /api/app/admin/users
// Query:
//   q=<substring>           matches display_name / primary email / discord username / app_users.id
//   role=<roleKey>          (repeatable) any matched role
//   status=active|suspended|deleted (default: active)
//   discord=any|linked|unlinked
//   steam=any|linked|unlinked
//   emailVerified=any|yes|no
//   sort=<field> or -<field>  field in {createdAt, lastLoginAt, displayName}
//   page=1 pageSize=50 (max 200)
func (h *Handler) ListAdminUsers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	pool := h.postgres.Pool()
	q := r.URL.Query()

	page := 1
	pageSize := 50
	if v := q.Get("page"); v != "" { parseLimit(v, &page); if page < 1 { page = 1 } }
	if v := q.Get("pageSize"); v != "" { parseLimit(v, &pageSize) }
	if pageSize < 1 || pageSize > 200 { pageSize = 50 }
	offset := (page - 1) * pageSize

	status := q.Get("status")
	if status == "" { status = "active" }

	args := []any{}
	where := []string{}

	switch status {
	case "active":
		where = append(where, "u.suspended_at IS NULL AND u.deleted_at IS NULL")
	case "suspended":
		where = append(where, "u.suspended_at IS NOT NULL AND u.deleted_at IS NULL")
	case "deleted":
		where = append(where, "u.deleted_at IS NOT NULL")
	case "any":
		// no filter
	default:
		writeError(w, http.StatusBadRequest, "invalid status")
		return
	}

	if s := strings.TrimSpace(q.Get("q")); s != "" {
		args = append(args, "%"+s+"%")
		p := fmt.Sprintf("$%d", len(args))
		where = append(where, fmt.Sprintf(`(
			u.display_name ILIKE %[1]s
			OR u.id ILIKE %[1]s
			OR EXISTS (SELECT 1 FROM user_emails e WHERE e.user_id = u.id AND e.email ILIKE %[1]s)
			OR EXISTS (SELECT 1 FROM user_connected_accounts d WHERE d.user_id = u.id AND d.provider='discord' AND (d.username ILIKE %[1]s OR d.provider_account_id ILIKE %[1]s))
		)`, p))
	}

	if roles, ok := q["role"]; ok && len(roles) > 0 {
		args = append(args, roles)
		p := fmt.Sprintf("$%d", len(args))
		where = append(where, fmt.Sprintf(`EXISTS (
			SELECT 1 FROM admin_user_roles aur JOIN admin_roles r ON r.id = aur.role_id
			WHERE aur.user_id = u.id AND r.key = ANY(%s)
		)`, p))
	}

	switch q.Get("discord") {
	case "", "any":
	case "linked":
		where = append(where, `EXISTS (SELECT 1 FROM user_connected_accounts d WHERE d.user_id = u.id AND d.provider='discord')`)
	case "unlinked":
		where = append(where, `NOT EXISTS (SELECT 1 FROM user_connected_accounts d WHERE d.user_id = u.id AND d.provider='discord')`)
	default:
		writeError(w, http.StatusBadRequest, "invalid discord"); return
	}

	switch q.Get("steam") {
	case "", "any":
	case "linked":
		where = append(where, `EXISTS (SELECT 1 FROM user_connected_accounts s WHERE s.user_id = u.id AND s.provider='steam')`)
	case "unlinked":
		where = append(where, `NOT EXISTS (SELECT 1 FROM user_connected_accounts s WHERE s.user_id = u.id AND s.provider='steam')`)
	default:
		writeError(w, http.StatusBadRequest, "invalid steam"); return
	}

	switch q.Get("emailVerified") {
	case "", "any":
	case "yes":
		where = append(where, `EXISTS (SELECT 1 FROM user_emails e WHERE e.user_id = u.id AND e.is_primary=true AND e.is_verified=true)`)
	case "no":
		where = append(where, `EXISTS (SELECT 1 FROM user_emails e WHERE e.user_id = u.id AND e.is_primary=true AND e.is_verified=false)`)
	default:
		writeError(w, http.StatusBadRequest, "invalid emailVerified"); return
	}

	orderBy := "u.created_at DESC"
	sort := q.Get("sort")
	desc := strings.HasPrefix(sort, "-")
	sortField := strings.TrimPrefix(sort, "-")
	switch sortField {
	case "", "createdAt":
		orderBy = "u.created_at"
	case "lastLoginAt":
		orderBy = "u.last_login_at"
	case "displayName":
		orderBy = "u.display_name"
	default:
		writeError(w, http.StatusBadRequest, "invalid sort"); return
	}
	if desc || sortField == "" { orderBy += " DESC" } else { orderBy += " ASC" }

	whereSQL := ""
	if len(where) > 0 { whereSQL = "WHERE " + strings.Join(where, " AND ") }

	// Total count
	var total int
	countSQL := fmt.Sprintf(`SELECT COUNT(*) FROM app_users u %s`, whereSQL)
	if err := pool.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error()); return
	}

	args = append(args, pageSize, offset)
	listSQL := fmt.Sprintf(`
		SELECT u.id, u.display_name, u.avatar_url, u.roles::text,
		       u.created_at, u.updated_at, u.last_login_at,
		       u.suspended_at, u.deleted_at,
		       COALESCE(d.provider_account_id, ''),
		       COALESCE(d.username, ''),
		       COALESCE((SELECT email FROM user_emails e WHERE e.user_id = u.id AND e.is_primary=true LIMIT 1), ''),
		       COALESCE((SELECT is_verified FROM user_emails e WHERE e.user_id = u.id AND e.is_primary=true LIMIT 1), false),
		       COALESCE((SELECT COUNT(*) FROM user_guilds g WHERE g.discord_id = d.provider_account_id AND g.left_at IS NULL), 0),
		       (SELECT COUNT(*) FROM user_connected_accounts s WHERE s.user_id = u.id AND s.provider='steam'),
		       COALESCE(array_agg(DISTINCT r.key) FILTER (WHERE r.key IS NOT NULL), ARRAY[]::text[])
		FROM app_users u
		LEFT JOIN user_connected_accounts d ON d.user_id = u.id AND d.provider='discord'
		LEFT JOIN admin_user_roles aur ON aur.user_id = u.id
		LEFT JOIN admin_roles r ON r.id = aur.role_id
		%s
		GROUP BY u.id, d.provider_account_id, d.username
		ORDER BY %s
		LIMIT $%d OFFSET $%d
	`, whereSQL, orderBy, len(args)-1, len(args))

	rows, err := pool.Query(ctx, listSQL, args...)
	if err != nil { writeError(w, http.StatusInternalServerError, err.Error()); return }
	defer rows.Close()

	items := []adminUserItem{}
	for rows.Next() {
		var it adminUserItem
		var rolesLegacyJSON string
		var suspendedAt, deletedAt *time.Time
		var guildCount, steamCount int64
		var emailVerified bool
		var adminRoleKeys []string
		if err := rows.Scan(
			&it.User.ID, &it.User.DisplayName, &it.User.AvatarURL, &rolesLegacyJSON,
			&it.User.CreatedAt, &it.User.UpdatedAt, &it.User.LastLoginAt,
			&suspendedAt, &deletedAt,
			&it.DiscordID, &it.DiscordUsername, &it.PrimaryEmail, &emailVerified,
			&guildCount, &steamCount, &adminRoleKeys,
		); err != nil { writeError(w, http.StatusInternalServerError, err.Error()); return }
		if rolesLegacyJSON != "" { _ = json.Unmarshal([]byte(rolesLegacyJSON), &it.User.Roles) }
		if it.User.Roles == nil { it.User.Roles = []string{} }
		it.GuildCount = int(guildCount); it.SteamConnectionCount = int(steamCount)
		it.EmailVerified = emailVerified
		it.AdminRoles = adminRoleKeys
		if suspendedAt != nil { t := suspendedAt.Format(time.RFC3339); it.SuspendedAt = &t }
		if deletedAt != nil  { t := deletedAt.Format(time.RFC3339);  it.DeletedAt  = &t }
		items = append(items, it)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items, "total": total, "page": page, "pageSize": pageSize,
	})
}
```

Extend the `adminUserItem` struct accordingly:

```go
type adminUserItem struct {
	User                 adminAppUser `json:"user"`
	DiscordID            string       `json:"discordId"`
	DiscordUsername      string       `json:"discordUsername"`
	PrimaryEmail         string       `json:"primaryEmail"`
	EmailVerified        bool         `json:"emailVerified"`
	GuildCount           int          `json:"guildCount"`
	SteamConnectionCount int          `json:"steamConnectionCount"`
	AdminRoles           []string     `json:"adminRoles"`
	SuspendedAt          *string      `json:"suspendedAt,omitempty"`
	DeletedAt            *string      `json:"deletedAt,omitempty"`
}
```

Add imports `strings`, `fmt` if missing.

- [ ] **Step 3: Run `go build ./...`.** Expected: clean.

- [ ] **Step 4: Commit.**
  ```
  git add apps/backend/internal/http/admin_read_handlers.go
  git commit -m "feat(backend): filter/sort/paginate GET /api/app/admin/users"
  ```

---

## Task 10: Test — list handler filters, sort, pagination

**Files:**
- Modify or create: `apps/backend/internal/http/admin_read_handlers_test.go`

- [ ] **Step 1: Write tests.** Seed 3+ users with different statuses/roles/connections, then assert each filter narrows correctly and pagination returns `total` + correct page slice.

```go
func TestListAdminUsers_FiltersAndPagination(t *testing.T) {
	ctx := context.Background()
	ts := newTestServer(t) // helper: spins up h.Handler against TEST_POSTGRES_URL, seeds schema
	seedUser(t, ts.pool, "usr_aaaa", "Alice", map[string]any{"primaryEmail": "a@x", "verified": true, "discord": "a#1"})
	seedUser(t, ts.pool, "usr_bbbb", "Bob",   map[string]any{"primaryEmail": "b@x", "verified": false})
	seedUser(t, ts.pool, "usr_cccc", "Carol", map[string]any{"suspended": true})
	seedUser(t, ts.pool, "usr_dddd", "Dan",   map[string]any{"deleted": true})

	// default: status=active hides suspended + deleted
	resp := ts.get(t, "/api/app/admin/users")
	if resp.total != 2 { t.Errorf("active total: want 2, got %d", resp.total) }

	// q=alice -> 1
	if r := ts.get(t, "/api/app/admin/users?q=alice"); r.total != 1 { t.Errorf("q=alice: want 1 got %d", r.total) }

	// status=suspended -> 1 (Carol)
	if r := ts.get(t, "/api/app/admin/users?status=suspended"); r.total != 1 { t.Errorf("suspended: want 1 got %d", r.total) }

	// discord=linked -> 1 (Alice)
	if r := ts.get(t, "/api/app/admin/users?discord=linked"); r.total != 1 { t.Errorf("discord=linked: want 1 got %d", r.total) }

	// emailVerified=yes -> 1 (Alice)
	if r := ts.get(t, "/api/app/admin/users?emailVerified=yes"); r.total != 1 { t.Errorf("emailVerified=yes: want 1 got %d", r.total) }

	// pagination
	r := ts.get(t, "/api/app/admin/users?status=any&pageSize=2&page=1")
	if len(r.items) != 2 || r.total != 4 { t.Errorf("page1: want len=2 total=4, got len=%d total=%d", len(r.items), r.total) }

	// sort by displayName asc
	r = ts.get(t, "/api/app/admin/users?status=any&sort=displayName")
	if r.items[0].User.DisplayName != "Alice" { t.Errorf("sort asc: want Alice first, got %s", r.items[0].User.DisplayName) }

	// invalid sort -> 400
	if code := ts.getCode(t, "/api/app/admin/users?sort=bogus"); code != 400 { t.Errorf("bogus sort: want 400 got %d", code) }
}
```

If `newTestServer`/`seedUser` helpers don't exist in this package, add them in the same commit; base them on the patterns already in `apps/backend/internal/admin/e2e_test.go`.

- [ ] **Step 2: Run `go test ./internal/http/ -run TestListAdminUsers_FiltersAndPagination -v`.** Expected: PASS.

- [ ] **Step 3: Commit.**
  ```
  git add apps/backend/internal/http/admin_read_handlers_test.go apps/backend/internal/http/testhelpers_test.go
  git commit -m "test(backend): filter/sort/pagination on GET /api/app/admin/users"
  ```

---

## Task 11: Frontend — update DTO and API client

**Files:**
- Modify: `apps/frontend/lib/rustlens-api.ts`

- [ ] **Step 1: Update the `AdminUserItem` type** to match the new JSON shape:

```ts
export type AdminUserItem = {
	user: {
		id: string;
		displayName: string;
		avatarUrl: string;
		roles: string[];               // legacy app_users.roles JSONB
		createdAt: string;
		updatedAt: string;
		lastLoginAt: string;
	};
	discordId: string;
	discordUsername: string;
	primaryEmail: string;
	emailVerified: boolean;
	guildCount: number;
	steamConnectionCount: number;
	adminRoles: string[];              // keys from admin_roles
	suspendedAt?: string;
	deletedAt?: string;
};

export type AdminUsersQuery = {
	q?: string;
	role?: string[];
	status?: "active" | "suspended" | "deleted" | "any";
	discord?: "any" | "linked" | "unlinked";
	steam?: "any" | "linked" | "unlinked";
	emailVerified?: "any" | "yes" | "no";
	sort?: string;
	page?: number;
	pageSize?: number;
};

export type AdminUsersPage = {
	items: AdminUserItem[];
	total: number;
	page: number;
	pageSize: number;
};
```

- [ ] **Step 2: Replace `getAdminUsers` with a version that accepts `AdminUsersQuery` and returns `AdminUsersPage`.**

```ts
export async function getAdminUsers(params: AdminUsersQuery = {}): Promise<AdminUsersPage> {
	const sp = new URLSearchParams();
	if (params.q) sp.set("q", params.q);
	for (const r of params.role ?? []) sp.append("role", r);
	if (params.status) sp.set("status", params.status);
	if (params.discord) sp.set("discord", params.discord);
	if (params.steam) sp.set("steam", params.steam);
	if (params.emailVerified) sp.set("emailVerified", params.emailVerified);
	if (params.sort) sp.set("sort", params.sort);
	if (params.page) sp.set("page", String(params.page));
	if (params.pageSize) sp.set("pageSize", String(params.pageSize));
	const qs = sp.toString();
	return backendFetch<AdminUsersPage>(
		`/api/app/admin/users${qs ? `?${qs}` : ""}`,
		undefined,
		{ requireSession: true },
	);
}
```

- [ ] **Step 3: Run `pnpm --filter frontend tsc --noEmit` (or `pnpm typecheck`, whatever the repo uses — check `apps/frontend/package.json` scripts).** Expected: clean. Any callers of `getAdminUsers()` that previously destructured `items` directly must be updated at their call sites (there's at least one: the current `/admin/users/page.tsx`, being rewritten in Task 12).

- [ ] **Step 4: Commit.**
  ```
  git add apps/frontend/lib/rustlens-api.ts
  git commit -m "feat(frontend): typed filter/pagination for getAdminUsers"
  ```

---

## Task 12: Frontend — new `/admin/users` page with filter bar + paginated table

**Files:**
- Modify: `apps/frontend/app/admin/users/page.tsx`
- Create: `apps/frontend/app/admin/users/_components/users-filter-bar.tsx`
- Create: `apps/frontend/app/admin/users/_components/users-pagination.tsx`

- [ ] **Step 1: Inspect the existing `AdminDataTable`** (`apps/frontend/components/admin/admin-data-table.tsx`) to see what props it takes. Read it in full before writing the page.

- [ ] **Step 2: Create the filter bar client component.**

```tsx
// apps/frontend/app/admin/users/_components/users-filter-bar.tsx
"use client";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useCallback, useState, useTransition } from "react";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import {
	DropdownMenu, DropdownMenuCheckboxItem, DropdownMenuContent, DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";

type Option = { value: string; label: string };

const STATUS_OPTIONS: Option[] = [
	{ value: "active", label: "Active" },
	{ value: "suspended", label: "Suspended" },
	{ value: "deleted", label: "Deleted" },
	{ value: "any", label: "Any" },
];
const TRISTATE: Option[] = [
	{ value: "any", label: "Any" },
	{ value: "linked", label: "Linked" },
	{ value: "unlinked", label: "Unlinked" },
];
const VERIFIED: Option[] = [
	{ value: "any", label: "Any" },
	{ value: "yes", label: "Verified" },
	{ value: "no", label: "Unverified" },
];

export function UsersFilterBar({ roleOptions }: { roleOptions: Option[] }) {
	const router = useRouter();
	const pathname = usePathname();
	const sp = useSearchParams();
	const [, startTransition] = useTransition();
	const [q, setQ] = useState(sp.get("q") ?? "");

	const updateParam = useCallback((key: string, value: string | string[] | null) => {
		const next = new URLSearchParams(sp.toString());
		next.delete(key);
		if (Array.isArray(value)) value.forEach((v) => next.append(key, v));
		else if (value) next.set(key, value);
		next.delete("page"); // reset to page 1 when filters change
		startTransition(() => router.replace(`${pathname}?${next.toString()}`));
	}, [sp, pathname, router]);

	const currentRoles = sp.getAll("role");
	const currentStatus = sp.get("status") ?? "active";
	const currentDiscord = sp.get("discord") ?? "any";
	const currentSteam = sp.get("steam") ?? "any";
	const currentVerified = sp.get("emailVerified") ?? "any";

	return (
		<div className="flex flex-wrap gap-2 items-center mb-4">
			<Input
				aria-label="Search users"
				placeholder="Search name, email, id, discord..."
				className="max-w-xs"
				value={q}
				onChange={(e) => setQ(e.target.value)}
				onKeyDown={(e) => { if (e.key === "Enter") updateParam("q", q || null); }}
			/>
			<DropdownMenu>
				<DropdownMenuTrigger asChild><Button variant="outline">Role</Button></DropdownMenuTrigger>
				<DropdownMenuContent>
					{roleOptions.map((o) => (
						<DropdownMenuCheckboxItem
							key={o.value}
							checked={currentRoles.includes(o.value)}
							onCheckedChange={(v) => {
								const next = v ? [...currentRoles, o.value] : currentRoles.filter((x) => x !== o.value);
								updateParam("role", next.length ? next : null);
							}}
						>{o.label}</DropdownMenuCheckboxItem>
					))}
				</DropdownMenuContent>
			</DropdownMenu>
			<SingleSelect label="Status"   value={currentStatus}   options={STATUS_OPTIONS} onChange={(v) => updateParam("status", v === "active" ? null : v)} />
			<SingleSelect label="Discord"  value={currentDiscord}  options={TRISTATE}       onChange={(v) => updateParam("discord", v === "any" ? null : v)} />
			<SingleSelect label="Steam"    value={currentSteam}    options={TRISTATE}       onChange={(v) => updateParam("steam", v === "any" ? null : v)} />
			<SingleSelect label="Verified" value={currentVerified} options={VERIFIED}       onChange={(v) => updateParam("emailVerified", v === "any" ? null : v)} />
			<Button variant="ghost" onClick={() => router.replace(pathname)}>Clear</Button>
		</div>
	);
}

function SingleSelect({ label, value, options, onChange }:
	{ label: string; value: string; options: Option[]; onChange: (v: string) => void }) {
	return (
		<DropdownMenu>
			<DropdownMenuTrigger asChild>
				<Button variant="outline">{label}: {options.find((o) => o.value === value)?.label ?? value}</Button>
			</DropdownMenuTrigger>
			<DropdownMenuContent>
				{options.map((o) => (
					<DropdownMenuCheckboxItem key={o.value} checked={value === o.value} onCheckedChange={() => onChange(o.value)}>
						{o.label}
					</DropdownMenuCheckboxItem>
				))}
			</DropdownMenuContent>
		</DropdownMenu>
	);
}
```

- [ ] **Step 3: Create the pagination client component.**

```tsx
// apps/frontend/app/admin/users/_components/users-pagination.tsx
"use client";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { Button } from "@/components/ui/button";

export function UsersPagination({ total, page, pageSize }:
	{ total: number; page: number; pageSize: number }) {
	const router = useRouter();
	const pathname = usePathname();
	const sp = useSearchParams();
	const lastPage = Math.max(1, Math.ceil(total / pageSize));
	const go = (p: number) => {
		const next = new URLSearchParams(sp.toString());
		if (p <= 1) next.delete("page"); else next.set("page", String(p));
		router.replace(`${pathname}?${next.toString()}`);
	};
	return (
		<div className="flex items-center gap-2 mt-4">
			<span className="text-sm text-muted-foreground">
				{total === 0 ? "No users" : `${(page - 1) * pageSize + 1}–${Math.min(page * pageSize, total)} of ${total}`}
			</span>
			<div className="flex-1" />
			<Button variant="outline" size="sm" disabled={page <= 1} onClick={() => go(page - 1)}>Previous</Button>
			<span className="text-sm">Page {page} / {lastPage}</span>
			<Button variant="outline" size="sm" disabled={page >= lastPage} onClick={() => go(page + 1)}>Next</Button>
		</div>
	);
}
```

- [ ] **Step 4: Rewrite `apps/frontend/app/admin/users/page.tsx`.**

```tsx
import Link from "next/link";
import {
	Table, TableBody, TableCell, TableHead, TableHeader, TableRow,
} from "@/components/ui/table";
import { Badge } from "@/components/ui/badge";
import { getAdminRoles, getAdminUsers, type AdminUsersQuery } from "@/lib/rustlens-api";
import { requirePermission } from "@/lib/session";
import { UsersFilterBar } from "./_components/users-filter-bar";
import { UsersPagination } from "./_components/users-pagination";

type SP = Record<string, string | string[] | undefined>;

function first(v: string | string[] | undefined): string | undefined {
	return Array.isArray(v) ? v[0] : v;
}

function parseQuery(sp: SP): AdminUsersQuery {
	const page = Number(first(sp.page) ?? "1") || 1;
	const pageSize = Math.min(Math.max(Number(first(sp.pageSize) ?? "50") || 50, 1), 200);
	const status = (first(sp.status) as AdminUsersQuery["status"]) ?? "active";
	const discord = (first(sp.discord) as AdminUsersQuery["discord"]) ?? undefined;
	const steam = (first(sp.steam) as AdminUsersQuery["steam"]) ?? undefined;
	const emailVerified = (first(sp.emailVerified) as AdminUsersQuery["emailVerified"]) ?? undefined;
	const role = sp.role ? (Array.isArray(sp.role) ? sp.role : [sp.role]) : undefined;
	return { q: first(sp.q), role, status, discord, steam, emailVerified, sort: first(sp.sort), page, pageSize };
}

export default async function AdminUsersPage({ searchParams }: { searchParams: Promise<SP> }) {
	await requirePermission("users.read");
	const sp = await searchParams;
	const query = parseQuery(sp);
	const [page, roles] = await Promise.all([getAdminUsers(query), getAdminRoles()]);
	const roleOptions = roles.map((r) => ({ value: r.key, label: r.name }));

	return (
		<div>
			<UsersFilterBar roleOptions={roleOptions} />
			<Table>
				<TableHeader>
					<TableRow>
						<TableHead>User</TableHead>
						<TableHead>Email</TableHead>
						<TableHead>Roles</TableHead>
						<TableHead>Status</TableHead>
						<TableHead>Discord</TableHead>
						<TableHead>Steam</TableHead>
						<TableHead>Guilds</TableHead>
						<TableHead>Last login</TableHead>
					</TableRow>
				</TableHeader>
				<TableBody>
					{page.items.map((u) => (
						<TableRow key={u.user.id}>
							<TableCell>
								<Link href={`/admin/users/${u.user.id}`} className="underline-offset-2 hover:underline">
									{u.user.displayName || u.user.id}
								</Link>
							</TableCell>
							<TableCell>
								{u.primaryEmail || <span className="text-muted-foreground">—</span>}
								{u.primaryEmail && !u.emailVerified && <Badge variant="secondary" className="ml-2">unverified</Badge>}
							</TableCell>
							<TableCell className="flex flex-wrap gap-1">
								{u.adminRoles.length === 0
									? <span className="text-muted-foreground">—</span>
									: u.adminRoles.map((r) => <Badge key={r}>{r}</Badge>)}
							</TableCell>
							<TableCell>
								{u.deletedAt ? <Badge variant="destructive">deleted</Badge>
									: u.suspendedAt ? <Badge variant="destructive">suspended</Badge>
										: <Badge variant="secondary">active</Badge>}
							</TableCell>
							<TableCell>{u.discordUsername || u.discordId || "—"}</TableCell>
							<TableCell>{u.steamConnectionCount}</TableCell>
							<TableCell>{u.guildCount}</TableCell>
							<TableCell>{u.user.lastLoginAt?.slice(0, 10) || "—"}</TableCell>
						</TableRow>
					))}
					{page.items.length === 0 && (
						<TableRow>
							<TableCell colSpan={8} className="text-center text-muted-foreground py-8">
								No users match the current filters.
							</TableCell>
						</TableRow>
					)}
				</TableBody>
			</Table>
			<UsersPagination total={page.total} page={page.page} pageSize={page.pageSize} />
		</div>
	);
}
```

Note: `requirePermission("users.read")` — the page is gated on the new permission. For existing `internal_admin` users to retain access after this change, the `superadmin` role (implicit-all) continues to work; non-super admins need the `user_support`/`user_admin`/`user_auditor` role or the perm explicitly. If the current admin user is on `internal_admin` legacy flag only, they may need to be granted `superadmin` or one of the new roles before they can view the page.

- [ ] **Step 5: Run `pnpm --filter frontend typecheck`.** Expected: clean.

- [ ] **Step 6: Commit.**
  ```
  git add apps/frontend/app/admin/users/
  git commit -m "feat(frontend): searchable/filterable/paginated users list"
  ```

---

## Task 13: Frontend — tests for filter bar and pagination

**Files:**
- Create: `apps/frontend/app/admin/users/_components/__tests__/users-filter-bar.test.tsx`
- Create: `apps/frontend/app/admin/users/_components/__tests__/users-pagination.test.tsx`

- [ ] **Step 1: Write a filter-bar test that asserts changing a filter writes to the URL via `router.replace` and resets the `page` param.**

```tsx
import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { UsersFilterBar } from "../users-filter-bar";

const replace = vi.fn();
vi.mock("next/navigation", () => ({
	useRouter: () => ({ replace }),
	usePathname: () => "/admin/users",
	useSearchParams: () => new URLSearchParams("page=5&status=active"),
}));

describe("UsersFilterBar", () => {
	it("selecting a status resets page and writes the URL", () => {
		render(<UsersFilterBar roleOptions={[{ value: "superadmin", label: "Superadmin" }]} />);
		fireEvent.click(screen.getByRole("button", { name: /Status:/ }));
		fireEvent.click(screen.getByText("Suspended"));
		expect(replace).toHaveBeenCalled();
		const arg = (replace.mock.calls.at(-1) ?? [""])[0] as string;
		expect(arg).toContain("status=suspended");
		expect(arg).not.toContain("page=");
	});
});
```

- [ ] **Step 2: Write a pagination test.**

```tsx
import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { UsersPagination } from "../users-pagination";

const replace = vi.fn();
vi.mock("next/navigation", () => ({
	useRouter: () => ({ replace }),
	usePathname: () => "/admin/users",
	useSearchParams: () => new URLSearchParams(""),
}));

describe("UsersPagination", () => {
	it("Next advances the page", () => {
		render(<UsersPagination total={120} page={1} pageSize={50} />);
		fireEvent.click(screen.getByRole("button", { name: /Next/i }));
		const arg = (replace.mock.calls.at(-1) ?? [""])[0] as string;
		expect(arg).toContain("page=2");
	});
	it("Previous disabled on page 1", () => {
		render(<UsersPagination total={120} page={1} pageSize={50} />);
		expect(screen.getByRole("button", { name: /Previous/i })).toBeDisabled();
	});
});
```

- [ ] **Step 3: Run tests.** `pnpm --filter frontend test -- users-` (or the repo's test command). Expected: both PASS.

- [ ] **Step 4: Commit.**
  ```
  git add apps/frontend/app/admin/users/_components/__tests__/
  git commit -m "test(frontend): users filter-bar and pagination URL sync"
  ```

---

## Done when

- [ ] `go test ./...` passes in `apps/backend`.
- [ ] `pnpm --filter frontend test` passes, `pnpm --filter frontend typecheck` passes.
- [ ] Visiting `/admin/users` with an admin session renders the paginated list with filters; changing a filter updates the URL; pagination works; clicking a row name links to `/admin/users/[id]` (404 until Plan 2 — that's expected).
- [ ] Logging in as a `user_auditor` seeded role grants read access.
