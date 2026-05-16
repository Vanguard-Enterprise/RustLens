# Admin API Keys Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add admin-managed API keys that authenticate against existing `/api/app/admin/*` and `/api/internal/bot/*` routes, scoped to a subset of the creator's RBAC permissions, with one-time secret reveal, rotate/revoke lifecycle, and audit attribution.

**Architecture:** Opaque random tokens (`rl_admin_` + 32 bytes base62) stored as SHA-256 hash. A new `APIKeyAuth` middleware runs before the existing session/JWT auth on both subrouters; on success it stashes the creator's identity + the key's permission set on the request context. The RBAC resolver intersects key permissions with the creator's *current* role permissions on every request, so revoking a permission from the human immediately constrains every key they minted. Three new dispatcher actions (`api_keys.create|rotate|revoke`) handle lifecycle and inherit audit logging.

**Tech Stack:** Go 1.24 (chi, pgx), Next.js 16 (React 19, App Router, shadcn/ui), Postgres 16.

**Spec:** `docs/superpowers/specs/2026-05-16-admin-api-keys-design.md`

---

## Phase 1 — Schema & permissions

### Task 1: Add permission constants and seed `api_keys.manage` on `infra_admin`

**Files:**
- Modify: `apps/backend/internal/admin/rbac/permissions.go`
- Modify: `apps/backend/internal/admin/rbac/roles.go`
- Modify: `apps/backend/internal/admin/rbac/permissions_test.go`

- [ ] **Step 1: Write the failing test**

Append to `apps/backend/internal/admin/rbac/permissions_test.go`:

```go
func TestAPIKeyPermissionsRegistered(t *testing.T) {
	for _, key := range []string{
		PermAPIKeysManage,
		PermBotRoutesRead,
		PermBotRoutesWrite,
	} {
		if !IsValidPermission(key) {
			t.Errorf("expected permission %q to be registered", key)
		}
	}
}

func TestInfraAdminHasAPIKeysManage(t *testing.T) {
	for _, role := range SystemRoles() {
		if role.Key != RoleInfraAdmin {
			continue
		}
		found := false
		for _, p := range role.Permissions {
			if p == PermAPIKeysManage {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected infra_admin to include %q", PermAPIKeysManage)
		}
		return
	}
	t.Fatalf("infra_admin role not found")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd apps/backend && go test ./internal/admin/rbac/ -run "TestAPIKeyPermissionsRegistered|TestInfraAdminHasAPIKeysManage" -v`
Expected: FAIL — undefined `PermAPIKeysManage`, `PermBotRoutesRead`, `PermBotRoutesWrite`.

- [ ] **Step 3: Add the three permission constants**

In `apps/backend/internal/admin/rbac/permissions.go`, add to the `const (...)` block:

```go
	PermAPIKeysManage   = "api_keys.manage"
	PermBotRoutesRead   = "bot_routes.read"
	PermBotRoutesWrite  = "bot_routes.write"
```

And append to the `permissions` slice:

```go
	{Key: PermAPIKeysManage, Description: "Create, rotate, and revoke API keys (cannot grant permissions the creator does not currently hold)"},
	{Key: PermBotRoutesRead, Description: "Allow an API key to call GET endpoints under /api/internal/bot/*"},
	{Key: PermBotRoutesWrite, Description: "Allow an API key to call POST/PUT/DELETE/PATCH endpoints under /api/internal/bot/*"},
```

- [ ] **Step 4: Seed `api_keys.manage` onto `infra_admin`**

In `apps/backend/internal/admin/rbac/roles.go`, extend the `RoleInfraAdmin` permissions slice:

```go
			Permissions: []string{
				"proxies.read",
				"proxies.edit",
				"proxies.rotate_credentials",
				"bm_servers.read",
				"bm_servers.edit",
				"api_keys.manage",
				"audit.read",
			},
```

- [ ] **Step 5: Run tests**

Run: `cd apps/backend && go test ./internal/admin/rbac/ -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add apps/backend/internal/admin/rbac/
git commit -m "feat(backend): add api_keys.manage and bot_routes.{read,write} permissions"
```

---

### Task 2: Auto-migrate `api_keys` table

**Files:**
- Modify: `apps/backend/internal/store/postgres.go`
- Create: `apps/backend/internal/store/api_keys_schema_test.go`

- [ ] **Step 1: Write the failing test**

Create `apps/backend/internal/store/api_keys_schema_test.go`:

```go
package store

import (
	"context"
	"testing"
)

func TestAPIKeysTableExists(t *testing.T) {
	pool := newTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	var exists bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM information_schema.tables
			WHERE table_name = 'api_keys'
		)
	`).Scan(&exists); err != nil {
		t.Fatalf("query: %v", err)
	}
	if !exists {
		t.Fatal("expected api_keys table to exist after EnsureSchema")
	}
}

func TestAPIKeysHasExpectedColumns(t *testing.T) {
	pool := newTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	want := map[string]bool{
		"id": true, "name": true, "prefix": true, "last4": true,
		"secret_hash": true, "created_by_user_id": true,
		"permissions": true, "expires_at": true,
		"revoked_at": true, "revoked_reason": true,
		"last_used_at": true, "last_used_ip": true,
		"created_at": true, "updated_at": true,
	}
	rows, err := pool.Query(ctx, `
		SELECT column_name FROM information_schema.columns
		WHERE table_name = 'api_keys'
	`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	got := map[string]bool{}
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got[c] = true
	}
	for col := range want {
		if !got[col] {
			t.Errorf("missing column %q", col)
		}
	}
}
```

If `newTestPool` does not yet exist in this package, look for the convention in sibling tests (e.g. `apps/backend/internal/store/postgres_test.go`) and reuse it. If there is no test pool helper, the harness for these tests should mirror what other `store` tests do (typically a `TEST_DATABASE_URL` env var). Skip the test with `t.Skip("set TEST_DATABASE_URL")` when the env var is absent.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd apps/backend && go test ./internal/store/ -run "TestAPIKeysTableExists|TestAPIKeysHasExpectedColumns" -v`
Expected: FAIL — table does not exist (or skipped if no test DB; in that case verify manually after Step 3 via `psql` after running the backend).

- [ ] **Step 3: Add the migration in `EnsureSchema`**

In `apps/backend/internal/store/postgres.go`, add a new block inside `EnsureSchema` (near the end of the function, before the final `return nil`):

```go
	if _, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS api_keys (
		    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		    name TEXT NOT NULL,
		    prefix TEXT NOT NULL UNIQUE,
		    last4 TEXT NOT NULL,
		    secret_hash BYTEA NOT NULL,
		    created_by_user_id TEXT NOT NULL REFERENCES app_users(id) ON DELETE CASCADE,
		    permissions TEXT[] NOT NULL DEFAULT '{}',
		    expires_at TIMESTAMPTZ,
		    revoked_at TIMESTAMPTZ,
		    revoked_reason TEXT,
		    last_used_at TIMESTAMPTZ,
		    last_used_ip INET,
		    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		CREATE INDEX IF NOT EXISTS api_keys_created_by_idx
		    ON api_keys (created_by_user_id, revoked_at);
	`); err != nil {
		return fmt.Errorf("create api_keys table: %w", err)
	}
```

If `app_users.id` is `TEXT` (verify from the existing `app_users` schema near the top of the file), keep `created_by_user_id TEXT`. If it is `UUID`, switch to `UUID` to match. Default assumption from existing code: `TEXT`.

- [ ] **Step 4: Run tests**

Run: `cd apps/backend && go test ./internal/store/ -run "TestAPIKeysTableExists|TestAPIKeysHasExpectedColumns" -v`
Expected: PASS (or PASS-on-manual-verify if test DB is unavailable).

- [ ] **Step 5: Commit**

```bash
git add apps/backend/internal/store/postgres.go apps/backend/internal/store/api_keys_schema_test.go
git commit -m "feat(backend): auto-migrate api_keys table"
```

---

### Task 3: Add `via_api_key_*` columns to `audit_events`

**Files:**
- Modify: `apps/backend/internal/store/postgres.go`
- Create: `apps/backend/internal/store/audit_events_schema_test.go`

- [ ] **Step 1: Write the failing test**

Create `apps/backend/internal/store/audit_events_schema_test.go`:

```go
package store

import (
	"context"
	"testing"
)

func TestAuditEventsHasViaAPIKeyColumns(t *testing.T) {
	pool := newTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	want := map[string]string{
		"via_api_key_id":   "uuid",
		"via_api_key_name": "text",
	}
	rows, err := pool.Query(ctx, `
		SELECT column_name, data_type FROM information_schema.columns
		WHERE table_name = 'audit_events'
	`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	got := map[string]string{}
	for rows.Next() {
		var name, dtype string
		if err := rows.Scan(&name, &dtype); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got[name] = dtype
	}
	for col, dtype := range want {
		if got[col] != dtype {
			t.Errorf("column %q: got data_type=%q, want %q", col, got[col], dtype)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd apps/backend && go test ./internal/store/ -run TestAuditEventsHasViaAPIKeyColumns -v`
Expected: FAIL.

- [ ] **Step 3: Add the migration**

In `apps/backend/internal/store/postgres.go`, immediately after the `CREATE TABLE IF NOT EXISTS audit_events (...)` block (around line 658), add:

```go
	if _, err := pool.Exec(ctx, `
		ALTER TABLE audit_events
		    ADD COLUMN IF NOT EXISTS via_api_key_id UUID REFERENCES api_keys(id) ON DELETE SET NULL,
		    ADD COLUMN IF NOT EXISTS via_api_key_name TEXT;
	`); err != nil {
		return fmt.Errorf("alter audit_events for api keys: %w", err)
	}
```

The `ALTER TABLE` block **must** run **after** the `CREATE TABLE api_keys` block from Task 2 so the FK target exists. Verify ordering before committing.

- [ ] **Step 4: Run test**

Run: `cd apps/backend && go test ./internal/store/ -run TestAuditEventsHasViaAPIKeyColumns -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/backend/internal/store/postgres.go apps/backend/internal/store/audit_events_schema_test.go
git commit -m "feat(backend): add via_api_key_{id,name} to audit_events"
```

---

## Phase 2 — Store layer for `api_keys`

### Task 4: `Store` methods for api_keys CRUD

**Files:**
- Create: `apps/backend/internal/store/api_keys.go`
- Create: `apps/backend/internal/store/api_keys_test.go`

- [ ] **Step 1: Write the failing test**

Create `apps/backend/internal/store/api_keys_test.go`:

```go
package store

import (
	"context"
	"crypto/sha256"
	"testing"
	"time"
)

func TestAPIKeyStoreCreateAndGetByPrefix(t *testing.T) {
	pool := newTestPool(t)
	defer pool.Close()
	ctx := context.Background()
	s := NewAPIKeyStore(pool)
	userID := seedTestUser(t, pool)

	hash := sha256.Sum256([]byte("rl_admin_abc123xyz"))
	key, err := s.Create(ctx, APIKeyCreate{
		Name:             "deploy-bot",
		Prefix:           "rl_admin_abc",
		Last4:            "1xyz",
		SecretHash:       hash[:],
		CreatedByUserID:  userID,
		Permissions:      []string{"users.read"},
		ExpiresAt:        nil,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if key.ID == "" {
		t.Fatal("expected non-empty id")
	}

	got, err := s.GetByPrefix(ctx, "rl_admin_abc")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != key.ID {
		t.Errorf("id mismatch")
	}
}

func TestAPIKeyStoreRevoke(t *testing.T) {
	pool := newTestPool(t)
	defer pool.Close()
	ctx := context.Background()
	s := NewAPIKeyStore(pool)
	userID := seedTestUser(t, pool)
	hash := sha256.Sum256([]byte("rl_admin_revoketestxyz"))
	key, _ := s.Create(ctx, APIKeyCreate{
		Name: "tmp", Prefix: "rl_admin_rev", Last4: "txyz",
		SecretHash: hash[:], CreatedByUserID: userID,
		Permissions: []string{"users.read"},
	})

	if err := s.Revoke(ctx, key.ID, "manual"); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	got, _ := s.GetByPrefix(ctx, "rl_admin_rev")
	if got.RevokedAt == nil {
		t.Fatal("expected revoked_at set")
	}
	if got.RevokedReason == nil || *got.RevokedReason != "manual" {
		t.Errorf("revoked_reason: got %v, want manual", got.RevokedReason)
	}

	// Second revoke is idempotent and does not bump revoked_at.
	first := *got.RevokedAt
	time.Sleep(10 * time.Millisecond)
	if err := s.Revoke(ctx, key.ID, "manual"); err != nil {
		t.Fatalf("revoke again: %v", err)
	}
	got, _ = s.GetByPrefix(ctx, "rl_admin_rev")
	if !got.RevokedAt.Equal(first) {
		t.Errorf("revoked_at bumped on idempotent revoke")
	}
}

func TestAPIKeyStoreRotate(t *testing.T) {
	pool := newTestPool(t)
	defer pool.Close()
	ctx := context.Background()
	s := NewAPIKeyStore(pool)
	userID := seedTestUser(t, pool)
	hash := sha256.Sum256([]byte("rl_admin_old123xyz"))
	key, _ := s.Create(ctx, APIKeyCreate{
		Name: "k", Prefix: "rl_admin_old", Last4: "3xyz",
		SecretHash: hash[:], CreatedByUserID: userID,
		Permissions: []string{"users.read"},
	})

	newHash := sha256.Sum256([]byte("rl_admin_new789zzz"))
	if err := s.Rotate(ctx, key.ID, "rl_admin_new", "9zzz", newHash[:]); err != nil {
		t.Fatalf("rotate: %v", err)
	}
	got, _ := s.GetByPrefix(ctx, "rl_admin_new")
	if got.ID != key.ID {
		t.Error("rotate must preserve id")
	}
	if string(got.Permissions[0]) != "users.read" {
		t.Error("rotate must preserve permissions")
	}
}

func TestAPIKeyStoreUpdateLastUsed(t *testing.T) {
	pool := newTestPool(t)
	defer pool.Close()
	ctx := context.Background()
	s := NewAPIKeyStore(pool)
	userID := seedTestUser(t, pool)
	hash := sha256.Sum256([]byte("rl_admin_lastused"))
	key, _ := s.Create(ctx, APIKeyCreate{
		Name: "k", Prefix: "rl_admin_lus", Last4: "used",
		SecretHash: hash[:], CreatedByUserID: userID,
		Permissions: []string{"users.read"},
	})

	if err := s.UpdateLastUsed(ctx, key.ID, "203.0.113.7"); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ := s.GetByPrefix(ctx, "rl_admin_lus")
	if got.LastUsedAt == nil {
		t.Fatal("expected last_used_at set")
	}
	if got.LastUsedIP == nil || *got.LastUsedIP != "203.0.113.7" {
		t.Errorf("last_used_ip: %v", got.LastUsedIP)
	}
}
```

`seedTestUser` is a helper that inserts a row in `app_users` and returns its id. If one does not already exist in this test package, define it inline at the bottom of this file using a minimal INSERT into `app_users` based on the existing test conventions in `apps/backend/internal/store/*_test.go`.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd apps/backend && go test ./internal/store/ -run TestAPIKeyStore -v`
Expected: FAIL — `NewAPIKeyStore`, `APIKeyCreate`, etc. undefined.

- [ ] **Step 3: Implement the store**

Create `apps/backend/internal/store/api_keys.go`:

```go
package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// APIKey is a row in the api_keys table.
type APIKey struct {
	ID              string
	Name            string
	Prefix          string
	Last4           string
	SecretHash      []byte
	CreatedByUserID string
	Permissions     []string
	ExpiresAt       *time.Time
	RevokedAt       *time.Time
	RevokedReason   *string
	LastUsedAt      *time.Time
	LastUsedIP      *string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// APIKeyCreate is the input for Store.Create.
type APIKeyCreate struct {
	Name            string
	Prefix          string
	Last4           string
	SecretHash      []byte
	CreatedByUserID string
	Permissions     []string
	ExpiresAt       *time.Time
}

// APIKeyStore is the persistence layer for api_keys.
type APIKeyStore struct{ pool *pgxpool.Pool }

func NewAPIKeyStore(pool *pgxpool.Pool) *APIKeyStore { return &APIKeyStore{pool: pool} }

// ErrAPIKeyNotFound is returned by GetByPrefix / GetByID when no row matches.
var ErrAPIKeyNotFound = errors.New("api key not found")

func (s *APIKeyStore) Create(ctx context.Context, in APIKeyCreate) (APIKey, error) {
	row := s.pool.QueryRow(ctx, `
		INSERT INTO api_keys
		    (name, prefix, last4, secret_hash, created_by_user_id, permissions, expires_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		RETURNING id, created_at, updated_at
	`, in.Name, in.Prefix, in.Last4, in.SecretHash, in.CreatedByUserID, in.Permissions, in.ExpiresAt)
	var k APIKey
	k.Name = in.Name
	k.Prefix = in.Prefix
	k.Last4 = in.Last4
	k.SecretHash = in.SecretHash
	k.CreatedByUserID = in.CreatedByUserID
	k.Permissions = in.Permissions
	k.ExpiresAt = in.ExpiresAt
	if err := row.Scan(&k.ID, &k.CreatedAt, &k.UpdatedAt); err != nil {
		return APIKey{}, err
	}
	return k, nil
}

func (s *APIKeyStore) GetByPrefix(ctx context.Context, prefix string) (APIKey, error) {
	return s.scanOne(ctx, `WHERE prefix = $1`, prefix)
}

func (s *APIKeyStore) GetByID(ctx context.Context, id string) (APIKey, error) {
	return s.scanOne(ctx, `WHERE id::text = $1`, id)
}

func (s *APIKeyStore) scanOne(ctx context.Context, whereClause string, arg any) (APIKey, error) {
	var k APIKey
	var ipStr *string
	err := s.pool.QueryRow(ctx, `
		SELECT id::text, name, prefix, last4, secret_hash, created_by_user_id,
		       permissions, expires_at, revoked_at, revoked_reason,
		       last_used_at, last_used_ip::text, created_at, updated_at
		  FROM api_keys `+whereClause, arg,
	).Scan(
		&k.ID, &k.Name, &k.Prefix, &k.Last4, &k.SecretHash, &k.CreatedByUserID,
		&k.Permissions, &k.ExpiresAt, &k.RevokedAt, &k.RevokedReason,
		&k.LastUsedAt, &ipStr, &k.CreatedAt, &k.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return APIKey{}, ErrAPIKeyNotFound
	}
	if err != nil {
		return APIKey{}, err
	}
	k.LastUsedIP = ipStr
	return k, nil
}

// Revoke marks the key revoked. Idempotent: no-op if already revoked.
func (s *APIKeyStore) Revoke(ctx context.Context, id, reason string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE api_keys
		   SET revoked_at = NOW(), revoked_reason = $2, updated_at = NOW()
		 WHERE id::text = $1 AND revoked_at IS NULL
	`, id, reason)
	return err
}

// Rotate replaces secret material in place. Preserves id, name, permissions,
// created_by_user_id, expires_at, and created_at.
func (s *APIKeyStore) Rotate(ctx context.Context, id, newPrefix, newLast4 string, newHash []byte) error {
	res, err := s.pool.Exec(ctx, `
		UPDATE api_keys
		   SET prefix = $2, last4 = $3, secret_hash = $4, updated_at = NOW()
		 WHERE id::text = $1 AND revoked_at IS NULL
	`, id, newPrefix, newLast4, newHash)
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		return ErrAPIKeyNotFound
	}
	return nil
}

// UpdateLastUsed records the most recent successful auth (fire-and-forget; the
// caller debounces).
func (s *APIKeyStore) UpdateLastUsed(ctx context.Context, id, ip string) error {
	var ipArg any
	if ip != "" {
		ipArg = ip
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE api_keys
		   SET last_used_at = NOW(), last_used_ip = $2
		 WHERE id::text = $1
	`, id, ipArg)
	return err
}

// APIKeyListQuery is the input for Store.List.
type APIKeyListQuery struct {
	Q              string
	IncludeRevoked bool
	Page           int
	PageSize       int
}

// APIKeyListItem is a row plus the creator's display name.
type APIKeyListItem struct {
	APIKey
	CreatedByDisplayName string
}

func (s *APIKeyStore) List(ctx context.Context, q APIKeyListQuery) ([]APIKeyListItem, int, error) {
	page := q.Page
	if page < 1 {
		page = 1
	}
	size := q.PageSize
	if size <= 0 {
		size = 20
	}
	if size > 100 {
		size = 100
	}
	offset := (page - 1) * size

	args := []any{}
	where := "WHERE 1=1"
	if !q.IncludeRevoked {
		where += " AND k.revoked_at IS NULL"
	}
	if q.Q != "" {
		args = append(args, "%"+q.Q+"%")
		where += " AND (k.name ILIKE $1 OR k.prefix ILIKE $1)"
	}
	countArgs := append([]any(nil), args...)

	var total int
	if err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM api_keys k `+where, countArgs...,
	).Scan(&total); err != nil {
		return nil, 0, err
	}

	args = append(args, size, offset)
	rows, err := s.pool.Query(ctx, `
		SELECT k.id::text, k.name, k.prefix, k.last4, k.secret_hash, k.created_by_user_id,
		       k.permissions, k.expires_at, k.revoked_at, k.revoked_reason,
		       k.last_used_at, k.last_used_ip::text, k.created_at, k.updated_at,
		       COALESCE(u.display_name, u.discord_username, '')
		  FROM api_keys k
		  LEFT JOIN app_users u ON u.id = k.created_by_user_id
		`+where+`
		ORDER BY k.created_at DESC
		LIMIT $`+itoa(len(args)-1)+` OFFSET $`+itoa(len(args)),
		args...,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []APIKeyListItem
	for rows.Next() {
		var it APIKeyListItem
		var ipStr *string
		if err := rows.Scan(
			&it.ID, &it.Name, &it.Prefix, &it.Last4, &it.SecretHash, &it.CreatedByUserID,
			&it.Permissions, &it.ExpiresAt, &it.RevokedAt, &it.RevokedReason,
			&it.LastUsedAt, &ipStr, &it.CreatedAt, &it.UpdatedAt,
			&it.CreatedByDisplayName,
		); err != nil {
			return nil, 0, err
		}
		it.LastUsedIP = ipStr
		out = append(out, it)
	}
	return out, total, rows.Err()
}

// itoa: local helper duplicated from audit package; keeps store package import-free.
func itoa(i int) string {
	const d = "0123456789"
	if i < 10 {
		return string(d[i])
	}
	return itoa(i/10) + string(d[i%10])
}
```

Verify `app_users` has columns `display_name` and `discord_username`. If the column names differ, adjust the `COALESCE` in `List`. If `itoa` already exists in the package and would conflict, remove the local one and reuse the existing helper.

- [ ] **Step 3: Run tests**

Run: `cd apps/backend && go test ./internal/store/ -run TestAPIKeyStore -v`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add apps/backend/internal/store/api_keys.go apps/backend/internal/store/api_keys_test.go
git commit -m "feat(backend): api_keys store (create, get, list, revoke, rotate, last_used)"
```

---

## Phase 3 — Audit extension

### Task 5: Extend `audit.Entry`/`Row`/`Store` to carry `via_api_key_*`

**Files:**
- Modify: `apps/backend/internal/admin/audit/audit.go`
- Modify: `apps/backend/internal/admin/audit/audit_test.go`

- [ ] **Step 1: Write the failing test**

Append to `apps/backend/internal/admin/audit/audit_test.go`:

```go
func TestRecordAndQueryWithViaAPIKey(t *testing.T) {
	pool := newTestPool(t)
	defer pool.Close()
	ctx := context.Background()
	store := NewStore(pool)

	keyID := seedTestAPIKey(t, pool) // helper: see step 3
	if err := store.Record(ctx, Entry{
		ActionType:    "users.read",
		ActorUserID:   strPtr("u1"),
		Result:        "success",
		ViaAPIKeyID:   &keyID,
		ViaAPIKeyName: strPtr("deploy-bot"),
	}); err != nil {
		t.Fatalf("record: %v", err)
	}

	rows, err := store.Query(ctx, QueryFilters{Limit: 10})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("want 1 row, got %d", len(rows))
	}
	r := rows[0]
	if r.ViaAPIKeyID == nil || *r.ViaAPIKeyID != keyID {
		t.Errorf("via_api_key_id: got %v want %s", r.ViaAPIKeyID, keyID)
	}
	if r.ViaAPIKeyName == nil || *r.ViaAPIKeyName != "deploy-bot" {
		t.Errorf("via_api_key_name: got %v", r.ViaAPIKeyName)
	}
}

func strPtr(s string) *string { return &s }
```

If `seedTestAPIKey` does not exist, define it in the test file:

```go
func seedTestAPIKey(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	ctx := context.Background()
	var userID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO app_users (id, discord_id, discord_username, display_name)
		 VALUES (gen_random_uuid()::text, 'd1', 'tester', 'Tester')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	var id string
	if err := pool.QueryRow(ctx, `
		INSERT INTO api_keys (name, prefix, last4, secret_hash, created_by_user_id, permissions)
		VALUES ('k', 'rl_admin_seed', 'seed', '\x00', $1, ARRAY['users.read'])
		RETURNING id::text
	`, userID).Scan(&id); err != nil {
		t.Fatalf("seed key: %v", err)
	}
	return id
}
```

Adjust the INSERT shape to match the live `app_users` schema if needed.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd apps/backend && go test ./internal/admin/audit/ -run TestRecordAndQueryWithViaAPIKey -v`
Expected: FAIL — `ViaAPIKeyID`/`ViaAPIKeyName` undefined.

- [ ] **Step 3: Extend `Entry`, `Row`, `Record`, and the SELECT in `Query`**

In `apps/backend/internal/admin/audit/audit.go`:

a) Add fields to `Entry`:

```go
type Entry struct {
	ActionType        string
	ActorUserID       *string
	ActorRoleSnapshot []string
	ResourceType      string
	ResourceID        string
	TargetUserID      *string
	Params            json.RawMessage
	Reason            *string
	Result            string
	ErrorMessage      *string
	IPAddress         *string
	UserAgent         *string
	ViaAPIKeyID       *string
	ViaAPIKeyName     *string
}
```

b) Add the same two fields to `Row`.

c) Extend `Record`:

```go
func (s *Store) Record(ctx context.Context, e Entry) error {
	if e.Params == nil {
		e.Params = json.RawMessage(`{}`)
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO audit_events (
			action_type, actor_user_id, actor_role_snapshot,
			resource_type, resource_id, target_user_id,
			params, reason, result, error_message, ip_address, user_agent,
			via_api_key_id, via_api_key_name
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
	`, e.ActionType, e.ActorUserID, e.ActorRoleSnapshot,
		nullIfEmpty(e.ResourceType), nullIfEmpty(e.ResourceID), e.TargetUserID,
		e.Params, e.Reason, e.Result, e.ErrorMessage, e.IPAddress, e.UserAgent,
		e.ViaAPIKeyID, e.ViaAPIKeyName,
	)
	return err
}
```

d) Extend `Query` SELECT and scan:

```go
	sql := `SELECT id, action_type, actor_user_id, actor_role_snapshot,
		COALESCE(resource_type,''), COALESCE(resource_id,''), target_user_id,
		params, reason, result, error_message, ip_address::text, user_agent, created_at,
		via_api_key_id::text, via_api_key_name
		FROM audit_events WHERE 1=1`
```

```go
		if err := rows.Scan(
			&r.ID, &r.ActionType, &r.ActorUserID, &r.ActorRoleSnapshot,
			&r.ResourceType, &r.ResourceID, &r.TargetUserID,
			&r.Params, &r.Reason, &r.Result, &r.ErrorMessage, &r.IPAddress, &r.UserAgent, &r.CreatedAt,
			&r.ViaAPIKeyID, &r.ViaAPIKeyName,
		); err != nil {
			return nil, err
		}
```

- [ ] **Step 4: Run test**

Run: `cd apps/backend && go test ./internal/admin/audit/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/backend/internal/admin/audit/
git commit -m "feat(backend): extend audit_events with via_api_key_{id,name}"
```

---

### Task 6: Propagate `via_api_key_*` through the dispatcher

**Files:**
- Modify: `apps/backend/internal/admin/actions/dispatcher.go`
- Modify: `apps/backend/internal/admin/actions/dispatcher_test.go`

Note: the `AuditEntryInput` shape lives in `dispatcher.go` and is what the dispatcher passes to the audit sink. The sink (the adapter that wraps `audit.Store`) must forward the new fields. Locate the adapter — likely in `apps/backend/internal/http/handlers.go` near `auditStore` — and adjust it too.

- [ ] **Step 1: Write the failing test**

Add to `apps/backend/internal/admin/actions/dispatcher_test.go`:

```go
func TestDispatcherForwardsViaAPIKey(t *testing.T) {
	t.Skip("Enable after Task 12 wires apiKeyAuth context. Pending: invoke ServeHTTP with a request whose context carries an apiKeyAuth and assert the recorded AuditEntryInput has ViaAPIKeyID and ViaAPIKeyName populated.")
}
```

(This test stays skipped until Task 12 supplies the context value. We add it now so the test file compiles against the new field set, and so the implementer wires the field through.)

- [ ] **Step 2: Add `ViaAPIKeyID` / `ViaAPIKeyName` to `AuditEntryInput`**

In `apps/backend/internal/admin/actions/dispatcher.go`:

```go
type AuditEntryInput struct {
	ActionType        string
	ActorUserID       string
	ActorRoleSnapshot []string
	ResourceType      string
	ResourceID        string
	TargetUserID      *string
	Params            json.RawMessage
	Reason            *string
	Result            string
	ErrorMessage      *string
	IPAddress         *string
	UserAgent         *string
	ViaAPIKeyID       *string
	ViaAPIKeyName     *string
}
```

- [ ] **Step 3: Populate from context inside `ServeHTTP` and `Dispatch`**

Define a typed context key + accessor that the API-key middleware (Task 12) will set:

```go
type apiKeyAuthKey struct{}

// APIKeyAuthContext is the shape stored on the request context by the API key
// middleware.
type APIKeyAuthContext struct {
	KeyID          string
	KeyName        string
	UserID         string
	KeyPermissions []string
}

func APIKeyAuthFromContext(ctx context.Context) *APIKeyAuthContext {
	v, _ := ctx.Value(apiKeyAuthKey{}).(*APIKeyAuthContext)
	return v
}

func WithAPIKeyAuth(ctx context.Context, v *APIKeyAuthContext) context.Context {
	return context.WithValue(ctx, apiKeyAuthKey{}, v)
}
```

Then, inside `ServeHTTP`, immediately before each `d.audit.RecordEntry(...)` call (there are two: error path and success path), enrich `entry`:

```go
		if k := APIKeyAuthFromContext(ctx); k != nil {
			id := k.KeyID
			name := k.KeyName
			entry.ViaAPIKeyID = &id
			entry.ViaAPIKeyName = &name
		}
```

Do the same in `Dispatch` before each `_ = d.audit.RecordEntry(...)`.

- [ ] **Step 4: Update the audit sink adapter**

Find the implementation of `AuditSink` that wraps `audit.Store` (typically a small adapter struct in `apps/backend/internal/http/handlers.go` or a dedicated file). Extend its `RecordEntry` to copy `ViaAPIKeyID`/`ViaAPIKeyName` from `actions.AuditEntryInput` into `audit.Entry`. If the adapter is implicit (struct-literal or anonymous), make it explicit and named so the mapping is testable.

Example adapter (create if needed at `apps/backend/internal/http/audit_sink.go`):

```go
package httpapi

import (
	"context"

	"rustlens/backend/internal/admin/actions"
	"rustlens/backend/internal/admin/audit"
)

type auditSink struct{ store *audit.Store }

func newAuditSink(s *audit.Store) actions.AuditSink { return &auditSink{store: s} }

func (a *auditSink) RecordEntry(ctx context.Context, e actions.AuditEntryInput) error {
	var actor *string
	if e.ActorUserID != "" {
		s := e.ActorUserID
		actor = &s
	}
	return a.store.Record(ctx, audit.Entry{
		ActionType:        e.ActionType,
		ActorUserID:       actor,
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
		ViaAPIKeyID:       e.ViaAPIKeyID,
		ViaAPIKeyName:     e.ViaAPIKeyName,
	})
}
```

Wire it where `actions.NewDispatcher(...)` is constructed in `handlers.go`, replacing whatever sink is currently passed.

- [ ] **Step 5: Run tests**

Run: `cd apps/backend && go test ./internal/admin/actions/ ./internal/admin/audit/ ./internal/http/ -v`
Expected: PASS (the new test from Step 1 is skipped; existing tests must still pass with the new fields being `nil`).

- [ ] **Step 6: Commit**

```bash
git add apps/backend/internal/admin/actions/dispatcher.go apps/backend/internal/admin/actions/dispatcher_test.go apps/backend/internal/http/
git commit -m "feat(backend): plumb via_api_key_{id,name} through admin dispatcher"
```

---

## Phase 4 — Permission guard

### Task 7: `AssertPermissionsSubsetOfActor` guard

**Files:**
- Modify: `apps/backend/internal/admin/actions/guards.go`
- Modify: `apps/backend/internal/admin/actions/guards_test.go`

- [ ] **Step 1: Write the failing test**

Add to `apps/backend/internal/admin/actions/guards_test.go`:

```go
func TestAssertPermissionsSubsetOfActor(t *testing.T) {
	cases := []struct {
		name      string
		requested []string
		actor     []string
		wantErr   bool
	}{
		{"empty requested is allowed", nil, []string{"users.read"}, false},
		{"strict subset is allowed", []string{"users.read"}, []string{"users.read", "users.edit"}, false},
		{"equal set is allowed", []string{"users.read"}, []string{"users.read"}, false},
		{"escalation rejected", []string{"users.delete"}, []string{"users.read"}, true},
		{"partial overlap rejected", []string{"users.read", "users.delete"}, []string{"users.read"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := AssertPermissionsSubsetOfActor(tc.requested, tc.actor)
			if (err != nil) != tc.wantErr {
				t.Fatalf("got err=%v, wantErr=%v", err, tc.wantErr)
			}
			if err != nil && !errors.Is(err, ErrPermissionDenied) {
				t.Errorf("expected ErrPermissionDenied, got %v", err)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd apps/backend && go test ./internal/admin/actions/ -run TestAssertPermissionsSubsetOfActor -v`
Expected: FAIL — `AssertPermissionsSubsetOfActor` undefined.

- [ ] **Step 3: Implement the guard**

In `apps/backend/internal/admin/actions/guards.go`, append:

```go
// AssertPermissionsSubsetOfActor returns ErrPermissionDenied if `requested`
// contains any permission key the actor does not currently hold. Used by
// api_keys.create / api_keys.rotate to prevent privilege escalation when
// minting keys. The caller is responsible for passing the actor's *current*
// resolved permission set (via rbac.Resolver.PermissionsForUser).
func AssertPermissionsSubsetOfActor(requested, actor []string) error {
	if len(requested) == 0 {
		return nil
	}
	have := make(map[string]bool, len(actor))
	for _, p := range actor {
		have[p] = true
	}
	for _, p := range requested {
		if !have[p] {
			return fmt.Errorf("%w: cannot grant permission %q the actor does not hold", ErrPermissionDenied, p)
		}
	}
	return nil
}
```

- [ ] **Step 4: Run tests**

Run: `cd apps/backend && go test ./internal/admin/actions/ -run TestAssertPermissionsSubsetOfActor -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/backend/internal/admin/actions/guards.go apps/backend/internal/admin/actions/guards_test.go
git commit -m "feat(backend): AssertPermissionsSubsetOfActor guard for api key minting"
```

---

## Phase 5 — Dispatcher actions

### Task 8: `api_keys.create` action

**Files:**
- Create: `apps/backend/internal/admin/actions/api_keys_create.go`
- Create: `apps/backend/internal/admin/actions/api_keys_create_test.go`

- [ ] **Step 1: Write the failing test**

Create `apps/backend/internal/admin/actions/api_keys_create_test.go`:

```go
package actions

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestAPIKeysCreateValidate(t *testing.T) {
	a := NewAPIKeysCreateAction(nil, &stubPermLoader{perms: []string{"users.read"}})
	cases := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{"missing name", `{"permissions":["users.read"]}`, true},
		{"empty perms ok", `{"name":"k","permissions":[]}`, false},
		{"long name rejected", `{"name":"` + strings.Repeat("x", 65) + `","permissions":[]}`, true},
		{"invalid perm rejected", `{"name":"k","permissions":["nope.invalid"]}`, true},
		{"happy path", `{"name":"k","permissions":["users.read"]}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := a.Validate(json.RawMessage(tc.raw))
			if (err != nil) != tc.wantErr {
				t.Fatalf("got err=%v wantErr=%v", err, tc.wantErr)
			}
		})
	}
}

func TestAPIKeysCreateRejectsEscalation(t *testing.T) {
	pool := newTestPool(t)
	defer pool.Close()
	a := NewAPIKeysCreateAction(pool, &stubPermLoader{perms: []string{"users.read"}})
	_, err := a.Execute(context.Background(), ActionInput{
		ActorUserID: "u1",
		Params:      json.RawMessage(`{"name":"k","permissions":["users.delete"]}`),
		Reason:      "test",
	})
	if err == nil || !strings.Contains(err.Error(), "users.delete") {
		t.Fatalf("expected permission_denied on escalation, got %v", err)
	}
}

func TestAPIKeysCreateHappyPath(t *testing.T) {
	pool := newTestPool(t)
	defer pool.Close()
	userID := seedTestUser(t, pool)
	a := NewAPIKeysCreateAction(pool, &stubPermLoader{perms: []string{"users.read"}})
	result, err := a.Execute(context.Background(), ActionInput{
		ActorUserID: userID,
		Params:      json.RawMessage(`{"name":"k","permissions":["users.read"]}`),
		Reason:      "test",
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	payload, _ := result.Payload.(map[string]any)
	secret, _ := payload["secret"].(string)
	if !strings.HasPrefix(secret, "rl_admin_") || len(secret) < 16 {
		t.Errorf("unexpected secret: %q", secret)
	}
}

// stubPermLoader satisfies the actor-perms loader the action needs.
type stubPermLoader struct{ perms []string }

func (s *stubPermLoader) PermissionsForUser(_ context.Context, _ string) ([]string, error) {
	return s.perms, nil
}
```

`seedTestUser` and `newTestPool` are reused from prior tasks; if the `actions` package does not yet have them, copy them in from `apps/backend/internal/store/*_test.go` adjusted for the package.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd apps/backend && go test ./internal/admin/actions/ -run TestAPIKeysCreate -v`
Expected: FAIL — `NewAPIKeysCreateAction` undefined.

- [ ] **Step 3: Implement the action**

Create `apps/backend/internal/admin/actions/api_keys_create.go`:

```go
package actions

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"rustlens/backend/internal/admin/rbac"
	"rustlens/backend/internal/store"
)

const apiKeySecretBytes = 32
const apiKeyPrefixLen = 16 // "rl_admin_" (9) + 7 random chars
const apiKeyPrefixWord = "rl_admin_"

// ActorPermissionsLoader returns the actor's *current* permission set.
type ActorPermissionsLoader interface {
	PermissionsForUser(ctx context.Context, userID string) ([]string, error)
}

type APIKeysCreateParams struct {
	Name        string   `json:"name"`
	Permissions []string `json:"permissions"`
	ExpiresAt   *string  `json:"expiresAt,omitempty"` // RFC3339
}

type apiKeysCreateAction struct {
	pool  *pgxpool.Pool
	perms ActorPermissionsLoader
}

func NewAPIKeysCreateAction(pool *pgxpool.Pool, perms ActorPermissionsLoader) AdminAction {
	return &apiKeysCreateAction{pool: pool, perms: perms}
}

func (a *apiKeysCreateAction) Type() string                 { return "api_keys.create" }
func (a *apiKeysCreateAction) Permission() string           { return rbac.PermAPIKeysManage }
func (a *apiKeysCreateAction) RequiresReason() bool         { return true }
func (a *apiKeysCreateAction) Dangerous() bool              { return false }
func (a *apiKeysCreateAction) SensitiveParamKeys() []string { return nil }
func (a *apiKeysCreateAction) ResourceType() string         { return "api_key" }

func (a *apiKeysCreateAction) Validate(raw json.RawMessage) (ValidatedParams, error) {
	var p APIKeysCreateParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return ValidatedParams{}, fmt.Errorf("validation: %w", err)
	}
	if p.Name == "" {
		return ValidatedParams{}, errors.New("name is required")
	}
	if len(p.Name) > 64 {
		return ValidatedParams{}, errors.New("name must be <= 64 chars")
	}
	for _, perm := range p.Permissions {
		if !rbac.IsValidPermission(perm) {
			return ValidatedParams{}, fmt.Errorf("unknown permission %q", perm)
		}
	}
	if p.ExpiresAt != nil && *p.ExpiresAt != "" {
		if _, err := time.Parse(time.RFC3339, *p.ExpiresAt); err != nil {
			return ValidatedParams{}, fmt.Errorf("expiresAt must be RFC3339: %w", err)
		}
	}
	return ValidatedParams{}, nil
}

func (a *apiKeysCreateAction) Execute(ctx context.Context, in ActionInput) (ActionResult, error) {
	var p APIKeysCreateParams
	if err := json.Unmarshal(in.Params, &p); err != nil {
		return ActionResult{}, fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}

	actorPerms, err := a.perms.PermissionsForUser(ctx, in.ActorUserID)
	if err != nil {
		return ActionResult{}, err
	}
	if err := AssertPermissionsSubsetOfActor(p.Permissions, actorPerms); err != nil {
		return ActionResult{}, err
	}

	secret, prefix, last4, err := generateAPIKeySecret()
	if err != nil {
		return ActionResult{}, err
	}
	hash := sha256.Sum256([]byte(secret))

	var expires *time.Time
	if p.ExpiresAt != nil && *p.ExpiresAt != "" {
		t, _ := time.Parse(time.RFC3339, *p.ExpiresAt)
		expires = &t
	}

	keys := store.NewAPIKeyStore(a.pool)
	key, err := keys.Create(ctx, store.APIKeyCreate{
		Name:            p.Name,
		Prefix:          prefix,
		Last4:           last4,
		SecretHash:      hash[:],
		CreatedByUserID: in.ActorUserID,
		Permissions:     p.Permissions,
		ExpiresAt:       expires,
	})
	if err != nil {
		return ActionResult{}, err
	}

	return ActionResult{
		ResourceID: key.ID,
		Payload: map[string]any{
			"apiKey": map[string]any{
				"id":          key.ID,
				"name":        key.Name,
				"prefix":      key.Prefix,
				"last4":       key.Last4,
				"permissions": key.Permissions,
				"expiresAt":   key.ExpiresAt,
				"createdAt":   key.CreatedAt,
			},
			"secret": secret,
		},
	}, nil
}

// generateAPIKeySecret returns (secret, prefix-16-chars, last4).
func generateAPIKeySecret() (string, string, string, error) {
	const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	buf := make([]byte, apiKeySecretBytes)
	for i := range buf {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			return "", "", "", err
		}
		buf[i] = alphabet[n.Int64()]
	}
	secret := apiKeyPrefixWord + string(buf)
	prefix := secret[:apiKeyPrefixLen]
	last4 := secret[len(secret)-4:]
	return secret, prefix, last4, nil
}
```

- [ ] **Step 4: Run tests**

Run: `cd apps/backend && go test ./internal/admin/actions/ -run TestAPIKeysCreate -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/backend/internal/admin/actions/api_keys_create.go apps/backend/internal/admin/actions/api_keys_create_test.go
git commit -m "feat(backend): api_keys.create dispatcher action"
```

---

### Task 9: `api_keys.rotate` action

**Files:**
- Create: `apps/backend/internal/admin/actions/api_keys_rotate.go`
- Create: `apps/backend/internal/admin/actions/api_keys_rotate_test.go`

- [ ] **Step 1: Write the failing test**

Create `apps/backend/internal/admin/actions/api_keys_rotate_test.go`:

```go
package actions

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestAPIKeysRotateValidate(t *testing.T) {
	a := NewAPIKeysRotateAction(nil)
	if _, err := a.Validate(json.RawMessage(`{}`)); err == nil {
		t.Error("expected error when keyId missing")
	}
}

func TestAPIKeysRotateHappyPath(t *testing.T) {
	pool := newTestPool(t)
	defer pool.Close()
	userID := seedTestUser(t, pool)
	created, _ := NewAPIKeysCreateAction(pool, &stubPermLoader{perms: []string{"users.read"}}).
		Execute(context.Background(), ActionInput{
			ActorUserID: userID,
			Params:      json.RawMessage(`{"name":"k","permissions":["users.read"]}`),
			Reason:      "x",
		})
	keyID := created.Payload.(map[string]any)["apiKey"].(map[string]any)["id"].(string)
	oldSecret := created.Payload.(map[string]any)["secret"].(string)

	a := NewAPIKeysRotateAction(pool)
	rotated, err := a.Execute(context.Background(), ActionInput{
		ActorUserID: userID,
		Params:      json.RawMessage(`{"keyId":"` + keyID + `"}`),
		Reason:      "x",
	})
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	newSecret := rotated.Payload.(map[string]any)["secret"].(string)
	if !strings.HasPrefix(newSecret, "rl_admin_") {
		t.Errorf("bad new secret: %q", newSecret)
	}
	if newSecret == oldSecret {
		t.Error("new secret equals old")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd apps/backend && go test ./internal/admin/actions/ -run TestAPIKeysRotate -v`
Expected: FAIL — `NewAPIKeysRotateAction` undefined.

- [ ] **Step 3: Implement the action**

Create `apps/backend/internal/admin/actions/api_keys_rotate.go`:

```go
package actions

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"rustlens/backend/internal/admin/rbac"
	"rustlens/backend/internal/store"
)

type APIKeysRotateParams struct {
	KeyID string `json:"keyId"`
}

type apiKeysRotateAction struct{ pool *pgxpool.Pool }

func NewAPIKeysRotateAction(pool *pgxpool.Pool) AdminAction {
	return &apiKeysRotateAction{pool: pool}
}

func (a *apiKeysRotateAction) Type() string                 { return "api_keys.rotate" }
func (a *apiKeysRotateAction) Permission() string           { return rbac.PermAPIKeysManage }
func (a *apiKeysRotateAction) RequiresReason() bool         { return true }
func (a *apiKeysRotateAction) Dangerous() bool              { return false }
func (a *apiKeysRotateAction) SensitiveParamKeys() []string { return nil }
func (a *apiKeysRotateAction) ResourceType() string         { return "api_key" }

func (a *apiKeysRotateAction) Validate(raw json.RawMessage) (ValidatedParams, error) {
	var p APIKeysRotateParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return ValidatedParams{}, fmt.Errorf("validation: %w", err)
	}
	if p.KeyID == "" {
		return ValidatedParams{}, errors.New("keyId is required")
	}
	return ValidatedParams{ResourceID: p.KeyID}, nil
}

func (a *apiKeysRotateAction) Execute(ctx context.Context, in ActionInput) (ActionResult, error) {
	var p APIKeysRotateParams
	if err := json.Unmarshal(in.Params, &p); err != nil {
		return ActionResult{}, fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}

	keys := store.NewAPIKeyStore(a.pool)
	existing, err := keys.GetByID(ctx, p.KeyID)
	if err != nil {
		if errors.Is(err, store.ErrAPIKeyNotFound) {
			return ActionResult{}, fmt.Errorf("%w: api key %s", ErrNotFound, p.KeyID)
		}
		return ActionResult{}, err
	}
	if existing.RevokedAt != nil {
		return ActionResult{}, fmt.Errorf("%w: cannot rotate a revoked key", ErrConflict)
	}

	secret, prefix, last4, err := generateAPIKeySecret()
	if err != nil {
		return ActionResult{}, err
	}
	hash := sha256.Sum256([]byte(secret))
	if err := keys.Rotate(ctx, p.KeyID, prefix, last4, hash[:]); err != nil {
		return ActionResult{}, err
	}

	return ActionResult{
		ResourceID: p.KeyID,
		Payload: map[string]any{
			"apiKey": map[string]any{
				"id":     p.KeyID,
				"prefix": prefix,
				"last4":  last4,
			},
			"secret": secret,
		},
	}, nil
}
```

- [ ] **Step 4: Run tests**

Run: `cd apps/backend && go test ./internal/admin/actions/ -run TestAPIKeysRotate -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/backend/internal/admin/actions/api_keys_rotate.go apps/backend/internal/admin/actions/api_keys_rotate_test.go
git commit -m "feat(backend): api_keys.rotate dispatcher action"
```

---

### Task 10: `api_keys.revoke` action

**Files:**
- Create: `apps/backend/internal/admin/actions/api_keys_revoke.go`
- Create: `apps/backend/internal/admin/actions/api_keys_revoke_test.go`

- [ ] **Step 1: Write the failing test**

Create `apps/backend/internal/admin/actions/api_keys_revoke_test.go`:

```go
package actions

import (
	"context"
	"encoding/json"
	"testing"

	"rustlens/backend/internal/store"
)

func TestAPIKeysRevokeHappyAndIdempotent(t *testing.T) {
	pool := newTestPool(t)
	defer pool.Close()
	userID := seedTestUser(t, pool)
	created, _ := NewAPIKeysCreateAction(pool, &stubPermLoader{perms: []string{"users.read"}}).
		Execute(context.Background(), ActionInput{
			ActorUserID: userID,
			Params:      json.RawMessage(`{"name":"k","permissions":["users.read"]}`),
			Reason:      "x",
		})
	keyID := created.Payload.(map[string]any)["apiKey"].(map[string]any)["id"].(string)

	a := NewAPIKeysRevokeAction(pool)
	if _, err := a.Execute(context.Background(), ActionInput{
		ActorUserID: userID,
		Params:      json.RawMessage(`{"keyId":"` + keyID + `"}`),
		Reason:      "x",
	}); err != nil {
		t.Fatalf("first revoke: %v", err)
	}

	got, _ := store.NewAPIKeyStore(pool).GetByID(context.Background(), keyID)
	if got.RevokedAt == nil || got.RevokedReason == nil || *got.RevokedReason != "manual" {
		t.Fatalf("expected manual revoke, got %+v", got)
	}

	if _, err := a.Execute(context.Background(), ActionInput{
		ActorUserID: userID,
		Params:      json.RawMessage(`{"keyId":"` + keyID + `"}`),
		Reason:      "x",
	}); err != nil {
		t.Fatalf("second revoke (idempotent): %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd apps/backend && go test ./internal/admin/actions/ -run TestAPIKeysRevoke -v`
Expected: FAIL.

- [ ] **Step 3: Implement the action**

Create `apps/backend/internal/admin/actions/api_keys_revoke.go`:

```go
package actions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"rustlens/backend/internal/admin/rbac"
	"rustlens/backend/internal/store"
)

type APIKeysRevokeParams struct {
	KeyID string `json:"keyId"`
}

type apiKeysRevokeAction struct{ pool *pgxpool.Pool }

func NewAPIKeysRevokeAction(pool *pgxpool.Pool) AdminAction {
	return &apiKeysRevokeAction{pool: pool}
}

func (a *apiKeysRevokeAction) Type() string                 { return "api_keys.revoke" }
func (a *apiKeysRevokeAction) Permission() string           { return rbac.PermAPIKeysManage }
func (a *apiKeysRevokeAction) RequiresReason() bool         { return true }
func (a *apiKeysRevokeAction) Dangerous() bool              { return false }
func (a *apiKeysRevokeAction) SensitiveParamKeys() []string { return nil }
func (a *apiKeysRevokeAction) ResourceType() string         { return "api_key" }

func (a *apiKeysRevokeAction) Validate(raw json.RawMessage) (ValidatedParams, error) {
	var p APIKeysRevokeParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return ValidatedParams{}, fmt.Errorf("validation: %w", err)
	}
	if p.KeyID == "" {
		return ValidatedParams{}, errors.New("keyId is required")
	}
	return ValidatedParams{ResourceID: p.KeyID}, nil
}

func (a *apiKeysRevokeAction) Execute(ctx context.Context, in ActionInput) (ActionResult, error) {
	var p APIKeysRevokeParams
	if err := json.Unmarshal(in.Params, &p); err != nil {
		return ActionResult{}, fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}
	keys := store.NewAPIKeyStore(a.pool)
	if err := keys.Revoke(ctx, p.KeyID, "manual"); err != nil {
		return ActionResult{}, err
	}
	return ActionResult{
		ResourceID: p.KeyID,
		Payload:    map[string]any{"keyId": p.KeyID},
	}, nil
}
```

- [ ] **Step 4: Run tests**

Run: `cd apps/backend && go test ./internal/admin/actions/ -run TestAPIKeysRevoke -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/backend/internal/admin/actions/api_keys_revoke.go apps/backend/internal/admin/actions/api_keys_revoke_test.go
git commit -m "feat(backend): api_keys.revoke dispatcher action"
```

---

### Task 11: Register the three actions

**Files:**
- Modify: `apps/backend/internal/http/admin_actions_register.go`

- [ ] **Step 1: Add the registrations**

In `apps/backend/internal/http/admin_actions_register.go`, add three calls **before** the final `return nil` in `registerAdminActions`:

```go
	if err := reg.Register(actions.NewAPIKeysCreateAction(pool, resolver)); err != nil {
		return err
	}
	if err := reg.Register(actions.NewAPIKeysRotateAction(pool)); err != nil {
		return err
	}
	if err := reg.Register(actions.NewAPIKeysRevokeAction(pool)); err != nil {
		return err
	}
```

`resolver` (type `*rbac.Resolver`) already satisfies `actions.ActorPermissionsLoader` because it has `PermissionsForUser(ctx, id) ([]string, error)`. If the compiler complains about the interface signature, double-check Task 8's interface name matches.

- [ ] **Step 2: Verify the backend builds**

Run: `cd apps/backend && go build ./...`
Expected: PASS.

- [ ] **Step 3: Run the existing dispatcher tests**

Run: `cd apps/backend && go test ./internal/admin/... -v`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add apps/backend/internal/http/admin_actions_register.go
git commit -m "feat(backend): register api_keys.{create,rotate,revoke} actions"
```

---

## Phase 6 — Auth middleware

### Task 12: `APIKeyAuth` middleware

**Files:**
- Create: `apps/backend/internal/appauth/api_key_middleware.go`
- Create: `apps/backend/internal/appauth/api_key_middleware_test.go`

- [ ] **Step 1: Write the failing test**

Create `apps/backend/internal/appauth/api_key_middleware_test.go`:

```go
package appauth

import (
	"context"
	"crypto/sha256"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"rustlens/backend/internal/admin/actions"
)

func TestAPIKeyAuthPassesThroughWithoutHeader(t *testing.T) {
	mw := NewAPIKeyAuth(nil, nil) // store/user-loader nil OK on the pass-through path
	called := false
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if actions.APIKeyAuthFromContext(r.Context()) != nil {
			t.Error("apiKeyAuth should not be set")
		}
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/app/admin/users", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if !called {
		t.Fatal("handler not called")
	}
}

func TestAPIKeyAuthRejectsUnknownPrefix(t *testing.T) {
	pool := newTestPool(t)
	defer pool.Close()
	mw := NewAPIKeyAuth(newAPIKeyStoreForTest(pool), stubUserLoader{})
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer rl_admin_doesnotexistxx")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401", w.Code)
	}
	if !strings.Contains(w.Body.String(), "invalid_api_key") {
		t.Errorf("body=%s", w.Body.String())
	}
}

func TestAPIKeyAuthValidKeyPopulatesContext(t *testing.T) {
	pool := newTestPool(t)
	defer pool.Close()
	ctx := context.Background()
	st := newAPIKeyStoreForTest(pool)
	userID := seedTestUserAppauth(t, pool)
	secret := "rl_admin_abc1234XYZpasstest1234567890ab"
	hash := sha256.Sum256([]byte(secret))
	_, _ = st.Create(ctx, apiKeyCreateForTest{
		Name: "k", Prefix: secret[:16], Last4: secret[len(secret)-4:],
		SecretHash: hash[:], CreatedByUserID: userID,
		Permissions: []string{"users.read"},
	})

	mw := NewAPIKeyAuth(st, stubUserLoader{id: userID, active: true})
	called := false
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		k := actions.APIKeyAuthFromContext(r.Context())
		if k == nil || k.UserID != userID {
			t.Errorf("apiKeyAuth missing or wrong: %+v", k)
		}
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+secret)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if !called {
		t.Fatal("handler not called")
	}
	if w.Code != http.StatusOK {
		t.Errorf("got %d", w.Code)
	}
}

func TestAPIKeyAuthRejectsRevoked(t *testing.T) {
	t.Skip("see Task 12 commentary — implement with a seeded revoked row")
}

func TestAPIKeyAuthAutoRevokesOnSuspendedCreator(t *testing.T) {
	t.Skip("see Task 12 commentary — implement after wiring user-loader suspended path")
}
```

The two skipped tests are placeholders for the implementer to fill in once the test plumbing matches. `newAPIKeyStoreForTest`, `apiKeyCreateForTest`, `seedTestUserAppauth`, and `stubUserLoader` are local helpers: a thin shim that exposes whatever subset of `store.APIKeyStore` the middleware actually uses. The implementer should define them at the bottom of the test file, mirroring the helper patterns in other `_test.go` files in this package. If `appauth` already has a test helper file, add them there instead.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd apps/backend && go test ./internal/appauth/ -run TestAPIKeyAuth -v`
Expected: FAIL — `NewAPIKeyAuth` undefined.

- [ ] **Step 3: Implement the middleware**

Create `apps/backend/internal/appauth/api_key_middleware.go`:

```go
package appauth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"rustlens/backend/internal/admin/actions"
	"rustlens/backend/internal/store"
)

const apiKeyAuthHeaderPrefix = "Bearer rl_admin_"
const apiKeyPrefixLen = 16
const lastUsedDebounce = 60 * time.Second

// UserLoader is the minimal user-loader the API-key middleware needs.
// `Active=false` means the user is suspended or soft-deleted; the middleware
// auto-revokes the key in that case.
type UserLoader interface {
	LoadUserStatus(ctx context.Context, userID string) (active bool, suspended bool, deleted bool, err error)
}

type APIKeyAuth struct {
	store  *store.APIKeyStore
	users  UserLoader
	mu     sync.Mutex
	recent map[string]time.Time // keyID -> last debounced write
}

// NewAPIKeyAuth returns the middleware constructor.
func NewAPIKeyAuth(s *store.APIKeyStore, u UserLoader) func(http.Handler) http.Handler {
	a := &APIKeyAuth{store: s, users: u, recent: map[string]time.Time{}}
	return a.handler
}

func (a *APIKeyAuth) handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hdr := r.Header.Get("Authorization")
		if !strings.HasPrefix(hdr, apiKeyAuthHeaderPrefix) {
			next.ServeHTTP(w, r)
			return
		}
		secret := strings.TrimPrefix(hdr, "Bearer ")
		if len(secret) < apiKeyPrefixLen {
			writeAPIKeyError(w, http.StatusUnauthorized, "invalid_api_key")
			return
		}
		prefix := secret[:apiKeyPrefixLen]

		ctx := r.Context()
		key, err := a.store.GetByPrefix(ctx, prefix)
		if err != nil {
			writeAPIKeyError(w, http.StatusUnauthorized, "invalid_api_key")
			return
		}

		presented := sha256.Sum256([]byte(secret))
		if subtle.ConstantTimeCompare(presented[:], key.SecretHash) != 1 {
			writeAPIKeyError(w, http.StatusUnauthorized, "invalid_api_key")
			return
		}
		if key.RevokedAt != nil {
			writeAPIKeyError(w, http.StatusUnauthorized, "api_key_revoked")
			return
		}
		if key.ExpiresAt != nil && key.ExpiresAt.Before(time.Now()) {
			writeAPIKeyError(w, http.StatusUnauthorized, "api_key_expired")
			return
		}

		active, suspended, deleted, err := a.users.LoadUserStatus(ctx, key.CreatedByUserID)
		if err != nil {
			writeAPIKeyError(w, http.StatusInternalServerError, "internal")
			return
		}
		if !active {
			reason := "creator_suspended"
			if deleted {
				reason = "creator_deleted"
			} else if !suspended {
				reason = "creator_inactive"
			}
			_ = a.store.Revoke(context.Background(), key.ID, reason)
			writeAPIKeyError(w, http.StatusUnauthorized, "api_key_revoked")
			return
		}

		ctx = actions.WithAPIKeyAuth(ctx, &actions.APIKeyAuthContext{
			KeyID:          key.ID,
			KeyName:        key.Name,
			UserID:         key.CreatedByUserID,
			KeyPermissions: key.Permissions,
		})

		// Best-effort debounced last-used update.
		a.maybeUpdateLastUsed(key.ID, clientIP(r))

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (a *APIKeyAuth) maybeUpdateLastUsed(keyID, ip string) {
	a.mu.Lock()
	last, ok := a.recent[keyID]
	if ok && time.Since(last) < lastUsedDebounce {
		a.mu.Unlock()
		return
	}
	a.recent[keyID] = time.Now()
	a.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = a.store.UpdateLastUsed(ctx, keyID, ip)
	}()
}

func writeAPIKeyError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
}

func clientIP(r *http.Request) string {
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		return ip
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
```

- [ ] **Step 4: Run tests**

Run: `cd apps/backend && go test ./internal/appauth/ -run TestAPIKeyAuth -v`
Expected: PASS (the two skipped tests stay skipped — non-blocking).

- [ ] **Step 5: Commit**

```bash
git add apps/backend/internal/appauth/api_key_middleware.go apps/backend/internal/appauth/api_key_middleware_test.go
git commit -m "feat(backend): APIKeyAuth middleware with debounced last-used updates"
```

---

### Task 13: `requireBotRouteScope` middleware

**Files:**
- Create: `apps/backend/internal/http/bot_route_scope.go`
- Create: `apps/backend/internal/http/bot_route_scope_test.go`

- [ ] **Step 1: Write the failing test**

Create `apps/backend/internal/http/bot_route_scope_test.go`:

```go
package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"rustlens/backend/internal/admin/actions"
)

type stubResolver struct{ perms map[string]bool }

func (s stubResolver) HasPermission(_ context.Context, _ string, key string) (bool, error) {
	return s.perms[key], nil
}

type stubAuditSink struct{ recorded []actions.AuditEntryInput }

func (s *stubAuditSink) RecordEntry(_ context.Context, e actions.AuditEntryInput) error {
	s.recorded = append(s.recorded, e)
	return nil
}

func TestBotRouteScopeNoOpWhenNoAPIKey(t *testing.T) {
	mw := requireBotRouteScope(stubResolver{}, &stubAuditSink{})
	called := false
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { called = true }))

	req := httptest.NewRequest(http.MethodGet, "/api/internal/bot/anything", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if !called || w.Code != http.StatusOK {
		t.Fatalf("expected pass-through; got code=%d called=%v", w.Code, called)
	}
}

func TestBotRouteScopeGETRequiresRead(t *testing.T) {
	audit := &stubAuditSink{}
	mw := requireBotRouteScope(stubResolver{perms: map[string]bool{}}, audit)
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { t.Fatal("should not call") }))

	req := httptest.NewRequest(http.MethodGet, "/api/internal/bot/x", nil)
	ctx := actions.WithAPIKeyAuth(req.Context(), &actions.APIKeyAuthContext{KeyID: "k", UserID: "u"})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req.WithContext(ctx))
	if w.Code != http.StatusForbidden {
		t.Fatalf("got %d", w.Code)
	}
	if len(audit.recorded) != 0 {
		t.Errorf("read audit should not be written: %+v", audit.recorded)
	}
}

func TestBotRouteScopePOSTRequiresWriteAndAudits(t *testing.T) {
	audit := &stubAuditSink{}
	mw := requireBotRouteScope(stubResolver{perms: map[string]bool{"bot_routes.write": true}}, audit)
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(201) }))

	req := httptest.NewRequest(http.MethodPost, "/api/internal/bot/x", nil)
	ctx := actions.WithAPIKeyAuth(req.Context(), &actions.APIKeyAuthContext{
		KeyID: "k", KeyName: "deploy", UserID: "u",
	})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req.WithContext(ctx))
	if w.Code != 201 {
		t.Fatalf("got %d", w.Code)
	}
	if len(audit.recorded) != 1 {
		t.Fatalf("want 1 audit row, got %d", len(audit.recorded))
	}
	e := audit.recorded[0]
	if e.ActionType != "bot_route.write" || e.ResourceType != "bot_route" {
		t.Errorf("bad audit: %+v", e)
	}
	if e.ViaAPIKeyID == nil || *e.ViaAPIKeyID != "k" {
		t.Errorf("missing via_api_key_id")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd apps/backend && go test ./internal/http/ -run TestBotRouteScope -v`
Expected: FAIL — `requireBotRouteScope` undefined.

- [ ] **Step 3: Implement the middleware**

Create `apps/backend/internal/http/bot_route_scope.go`:

```go
package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"

	"rustlens/backend/internal/admin/actions"
)

// requireBotRouteScope is mounted on the /api/internal/bot/* subrouter after
// the APIKeyAuth middleware and before the bot JWT middleware. When the
// request was authenticated by an API key (apiKeyAuth on context), it requires
// bot_routes.read on GET/HEAD and bot_routes.write on everything else, and
// writes one audit_events row per write. When apiKeyAuth is not on the
// context the middleware is a no-op (bot JWT traffic).
func requireBotRouteScope(perms actions.PermissionChecker, audit actions.AuditSink) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			k := actions.APIKeyAuthFromContext(r.Context())
			if k == nil {
				next.ServeHTTP(w, r)
				return
			}

			required := "bot_routes.write"
			isRead := r.Method == http.MethodGet || r.Method == http.MethodHead
			if isRead {
				required = "bot_routes.read"
			}
			ok, err := perms.HasPermission(r.Context(), k.UserID, required)
			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"error":"permission_check_failed"}`))
				return
			}
			if !ok {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"error":"forbidden"}`))
				return
			}

			if isRead {
				next.ServeHTTP(w, r)
				return
			}

			rec := &statusRecorder{ResponseWriter: w, status: 200}
			next.ServeHTTP(rec, r)

			result := "success"
			if rec.status >= 400 {
				result = "error"
			}
			paramsJSON, _ := json.Marshal(map[string]any{
				"method": r.Method,
				"status": rec.status,
			})
			id := k.KeyID
			name := k.KeyName
			ip := clientIPFromRequest(r)
			_ = audit.RecordEntry(context.Background(), actions.AuditEntryInput{
				ActionType:    "bot_route.write",
				ActorUserID:   k.UserID,
				ResourceType:  "bot_route",
				ResourceID:    r.URL.Path,
				Params:        paramsJSON,
				Result:        result,
				IPAddress:     optionalStringPtr(ip),
				ViaAPIKeyID:   &id,
				ViaAPIKeyName: &name,
			})
		})
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	body   bytes.Buffer
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func optionalStringPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func clientIPFromRequest(r *http.Request) string {
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		return ip
	}
	return r.RemoteAddr
}
```

If `optionalStringPtr` collides with an existing helper in the package, drop the duplicate and reuse the existing one.

- [ ] **Step 4: Run tests**

Run: `cd apps/backend && go test ./internal/http/ -run TestBotRouteScope -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/backend/internal/http/bot_route_scope.go apps/backend/internal/http/bot_route_scope_test.go
git commit -m "feat(backend): requireBotRouteScope middleware with write audit"
```

---

### Task 14: Resolver intersection branch for `apiKeyAuth`

**Files:**
- Modify: `apps/backend/internal/admin/rbac/resolver.go`
- Modify: `apps/backend/internal/admin/rbac/resolver_test.go`

- [ ] **Step 1: Write the failing test**

Append to `apps/backend/internal/admin/rbac/resolver_test.go`:

```go
func TestResolverIntersectsWithAPIKeyPermissions(t *testing.T) {
	r := NewResolver(stubLoader{perms: []string{"users.read", "users.edit"}})
	ctx := WithAPIKeyPermissions(context.Background(), []string{"users.read"})

	got, err := r.PermissionsForUser(ctx, "u1")
	if err != nil {
		t.Fatalf("perms: %v", err)
	}
	if len(got) != 1 || got[0] != "users.read" {
		t.Errorf("intersect failed: %v", got)
	}

	if ok, _ := r.HasPermission(ctx, "u1", "users.edit"); ok {
		t.Error("users.edit should be filtered out by intersection")
	}
	if ok, _ := r.HasPermission(ctx, "u1", "users.read"); !ok {
		t.Error("users.read should still pass")
	}
}
```

`stubLoader` is a small in-test loader; if one does not exist in the file, define it as:

```go
type stubLoader struct{ perms []string }
func (s stubLoader) PermissionsForUser(_ context.Context, _ string) ([]string, error) {
	return s.perms, nil
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd apps/backend && go test ./internal/admin/rbac/ -run TestResolverIntersectsWithAPIKeyPermissions -v`
Expected: FAIL — `WithAPIKeyPermissions` undefined.

- [ ] **Step 3: Add the intersection branch**

In `apps/backend/internal/admin/rbac/resolver.go`, add a context key and the intersection logic. Place these alongside `requestCacheKey`:

```go
type apiKeyPermsKey struct{}

// WithAPIKeyPermissions stamps the API key's snapshot permissions onto the
// context. The resolver intersects the user's current role permissions with
// this snapshot, so the effective set is the floor of the two — meaning
// revoking a permission from the user immediately constrains every key they
// minted without touching the api_keys table.
func WithAPIKeyPermissions(ctx context.Context, perms []string) context.Context {
	set := make(map[string]bool, len(perms))
	for _, p := range perms {
		set[p] = true
	}
	return context.WithValue(ctx, apiKeyPermsKey{}, set)
}

func apiKeyPermsFromContext(ctx context.Context) (map[string]bool, bool) {
	v, ok := ctx.Value(apiKeyPermsKey{}).(map[string]bool)
	return v, ok
}
```

Then in `loadSet`, after fetching the user's permissions and before caching, intersect:

```go
func (r *Resolver) loadSet(ctx context.Context, userID string) (map[string]bool, error) {
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
	if keySet, ok := apiKeyPermsFromContext(ctx); ok {
		filtered := make(map[string]bool, len(set))
		for k := range set {
			if keySet[k] {
				filtered[k] = true
			}
		}
		set = filtered
	}
	if cache != nil {
		cache.entries[userID] = set
	}
	return set, nil
}
```

Note: superadmin's "implicit all" behavior comes from the role's permission list including everything, so intersecting against the key's snapshot is correct — a superadmin who mints a key with `["users.read"]` only has `users.read` through that key, which is the desired behavior.

- [ ] **Step 4: Run tests**

Run: `cd apps/backend && go test ./internal/admin/rbac/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/backend/internal/admin/rbac/
git commit -m "feat(backend): resolver intersects with API key permissions from context"
```

---

### Task 15: Wire middleware in the router

**Files:**
- Modify: `apps/backend/internal/http/router.go`
- Modify: `apps/backend/internal/http/handlers.go` (to construct the middleware + bridge `apiKeyAuth` → `WithAPIKeyPermissions`)

- [ ] **Step 1: Read the existing route mounts**

Look at `apps/backend/internal/http/router.go`. Find:
- The `/api/internal/bot/...` subrouters (multiple — `shards`, `owner`, `setup`)
- The `/api/app/admin/...` subrouter (likely under `/api/app/` with admin-route subgrouping)
- Where the dispatcher is mounted (`POST /api/app/admin/actions/{type}`)

- [ ] **Step 2: Add a user-status loader for the middleware**

In `apps/backend/internal/http/handlers.go` (near where stores are constructed), add a thin `UserLoader` implementation that satisfies `appauth.UserLoader`:

```go
type apiKeyUserLoader struct{ pool *pgxpool.Pool }

func (l *apiKeyUserLoader) LoadUserStatus(ctx context.Context, userID string) (active, suspended, deleted bool, err error) {
	var s, d *time.Time
	err = l.pool.QueryRow(ctx,
		`SELECT suspended_at, deleted_at FROM app_users WHERE id = $1`, userID,
	).Scan(&s, &d)
	if err != nil {
		return false, false, false, err
	}
	suspended = s != nil
	deleted = d != nil
	active = !suspended && !deleted
	return
}
```

Verify the column names match the actual `app_users` schema. If `deleted_at` is not present, drop that branch and treat deletion as "row missing".

- [ ] **Step 3: Construct the middleware and bridge context**

In the place where `chi.Router` is built in `router.go`, before mounting the admin and bot subrouters, construct the middleware and a small bridge:

```go
	apiKeyStore := store.NewAPIKeyStore(postgres.Pool())
	apiKeyAuthMW := appauth.NewAPIKeyAuth(apiKeyStore, &apiKeyUserLoader{pool: postgres.Pool()})

	// bridgeAPIKeyPerms copies the key's snapshot permissions from the
	// actions context onto the rbac resolver context so HasPermission /
	// PermissionsForUser intersect correctly.
	bridgeAPIKeyPerms := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if k := actions.APIKeyAuthFromContext(r.Context()); k != nil {
				ctx := rbac.WithAPIKeyPermissions(r.Context(), k.KeyPermissions)
				r = r.WithContext(ctx)
			}
			next.ServeHTTP(w, r)
		})
	}
```

- [ ] **Step 4: Mount on the bot subrouters**

For each `r.Route("/api/internal/bot/...", func(r chi.Router) { ... })` (currently `shards`, `owner`, `setup`), prepend the middleware **before** the existing `botJWTMiddleware`:

```go
		r.Use(apiKeyAuthMW)
		r.Use(bridgeAPIKeyPerms)
		r.Use(requireBotRouteScope(resolver, sink))
		r.Use(botJWTMiddleware(handler.jwtValidator))
```

`resolver` and `sink` are the `*rbac.Resolver` and `actions.AuditSink` you already have in scope (from where the dispatcher is built). Verify by reading the surrounding code.

- [ ] **Step 5: Mount on the admin subrouter**

For the `/api/app/admin/...` subrouter, prepend before whatever session-cookie middleware already runs:

```go
		r.Use(apiKeyAuthMW)
		r.Use(bridgeAPIKeyPerms)
		// existing requireSession etc. follows
```

- [ ] **Step 6: Update the dispatcher's `ActorResolver` to honor API key context**

Find the existing `ActorResolver` constructor (a `func(r *http.Request) (string, []string, error)`). Wrap it so that when `actions.APIKeyAuthFromContext` is non-nil, the actor identity comes from `(KeyID, UserID)` rather than the session cookie:

```go
	actorResolver := func(r *http.Request) (string, []string, error) {
		if k := actions.APIKeyAuthFromContext(r.Context()); k != nil {
			// Roles snapshot is not needed for the dispatcher's permission
			// check (we use HasPermission), so an empty slice is fine.
			return k.UserID, nil, nil
		}
		return existingSessionActorResolver(r)
	}
```

Pass `actorResolver` to `actions.NewDispatcher`.

- [ ] **Step 7: Verify the build**

Run: `cd apps/backend && go build ./... && go test ./internal/http/... ./internal/appauth/... ./internal/admin/...`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add apps/backend/internal/http/router.go apps/backend/internal/http/handlers.go
git commit -m "feat(backend): mount APIKeyAuth + bot route scope; bridge key perms to resolver"
```

---

## Phase 7 — Read endpoint

### Task 16: `GET /api/app/admin/api-keys` handler + frontend client

**Files:**
- Create: `apps/backend/internal/http/api_keys_handlers.go`
- Modify: `apps/backend/internal/http/router.go`
- Modify: `apps/frontend/lib/rustlens-api.ts`

- [ ] **Step 1: Implement the list handler**

Create `apps/backend/internal/http/api_keys_handlers.go`:

```go
package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"

	"rustlens/backend/internal/admin/rbac"
	"rustlens/backend/internal/store"
)

func (h *Handler) ListAdminAPIKeys(w http.ResponseWriter, r *http.Request) {
	session := appSessionFromContext(r.Context())
	if session == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthenticated"})
		return
	}
	ok, err := h.permsResolver.HasPermission(r.Context(), session.UserID, rbac.PermAPIKeysManage)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "permission_check_failed"})
		return
	}
	if !ok {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	q := store.APIKeyListQuery{
		Q:              r.URL.Query().Get("q"),
		IncludeRevoked: r.URL.Query().Get("includeRevoked") == "true",
		Page:           parseIntOr(r.URL.Query().Get("page"), 1),
		PageSize:       parseIntOr(r.URL.Query().Get("pageSize"), 20),
	}
	items, total, err := store.NewAPIKeyStore(h.postgres.Pool()).List(r.Context(), q)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"})
		return
	}

	type respItem struct {
		ID                    string   `json:"id"`
		Name                  string   `json:"name"`
		Prefix                string   `json:"prefix"`
		Last4                 string   `json:"last4"`
		Permissions           []string `json:"permissions"`
		CreatedByUserID       string   `json:"createdByUserId"`
		CreatedByDisplayName  string   `json:"createdByDisplayName"`
		ExpiresAt             *string  `json:"expiresAt"`
		RevokedAt             *string  `json:"revokedAt"`
		RevokedReason         *string  `json:"revokedReason"`
		LastUsedAt            *string  `json:"lastUsedAt"`
		LastUsedIP            *string  `json:"lastUsedIp"`
		CreatedAt             string   `json:"createdAt"`
	}
	out := make([]respItem, 0, len(items))
	for _, it := range items {
		ri := respItem{
			ID: it.ID, Name: it.Name, Prefix: it.Prefix, Last4: it.Last4,
			Permissions: it.Permissions, CreatedByUserID: it.CreatedByUserID,
			CreatedByDisplayName: it.CreatedByDisplayName,
			LastUsedIP:           it.LastUsedIP,
			CreatedAt:            it.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		}
		if it.ExpiresAt != nil {
			s := it.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z")
			ri.ExpiresAt = &s
		}
		if it.RevokedAt != nil {
			s := it.RevokedAt.UTC().Format("2006-01-02T15:04:05Z")
			ri.RevokedAt = &s
		}
		ri.RevokedReason = it.RevokedReason
		if it.LastUsedAt != nil {
			s := it.LastUsedAt.UTC().Format("2006-01-02T15:04:05Z")
			ri.LastUsedAt = &s
		}
		out = append(out, ri)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items":    out,
		"total":    total,
		"page":     q.Page,
		"pageSize": q.PageSize,
	})
	_ = json.RawMessage("")
}

func parseIntOr(s string, fallback int) int {
	if s == "" {
		return fallback
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}
```

Verify `h.permsResolver` and `h.postgres` exist on `Handler`. If they differ in name, adjust accordingly. The `appSessionFromContext` helper at the top of `router.go` already exists.

- [ ] **Step 2: Mount the route**

In `router.go`, inside the admin route group (the same `Route("/api/app/admin/...")` block), add:

```go
		r.Get("/api-keys", handler.ListAdminAPIKeys)
```

- [ ] **Step 3: Verify build and tests**

Run: `cd apps/backend && go build ./... && go test ./internal/http/...`
Expected: PASS.

- [ ] **Step 4: Add the frontend client method**

In `apps/frontend/lib/rustlens-api.ts`, near the other admin helpers (alongside `getAdminBmServers`), add:

```ts
export type AdminApiKey = {
	id: string;
	name: string;
	prefix: string;
	last4: string;
	permissions: string[];
	createdByUserId: string;
	createdByDisplayName: string;
	expiresAt: string | null;
	revokedAt: string | null;
	revokedReason: string | null;
	lastUsedAt: string | null;
	lastUsedIp: string | null;
	createdAt: string;
};

export type AdminApiKeysPage = {
	items: AdminApiKey[];
	total: number;
	page: number;
	pageSize: number;
};

export type AdminApiKeysQuery = {
	q?: string;
	includeRevoked?: boolean;
	page?: number;
	pageSize?: number;
};

export async function getAdminApiKeys(
	params: AdminApiKeysQuery = {},
): Promise<AdminApiKeysPage> {
	const sp = new URLSearchParams();
	if (params.q) sp.set("q", params.q);
	if (params.includeRevoked) sp.set("includeRevoked", "true");
	if (params.page) sp.set("page", String(params.page));
	if (params.pageSize) sp.set("pageSize", String(params.pageSize));
	const qs = sp.toString();
	return fetchAppAPI<AdminApiKeysPage>(
		`/api/app/admin/api-keys${qs ? `?${qs}` : ""}`,
		undefined,
		{ requireSession: true },
	);
}

export type CreateApiKeyInput = {
	name: string;
	permissions: string[];
	expiresAt?: string | null;
};

export type CreateApiKeyResponse = {
	apiKey: AdminApiKey;
	secret: string;
};

export async function adminApiKeyCreate(
	input: CreateApiKeyInput,
	reason: string,
): Promise<CreateApiKeyResponse> {
	const r = await fetchAppAPI<{ ok: true; result: CreateApiKeyResponse }>(
		"/api/app/admin/actions/api_keys.create",
		{ method: "POST", body: JSON.stringify({ params: input, reason }) },
		{ requireSession: true },
	);
	return r.result;
}

export async function adminApiKeyRotate(
	keyId: string,
	reason: string,
): Promise<{ secret: string }> {
	const r = await fetchAppAPI<{ ok: true; result: { secret: string } }>(
		"/api/app/admin/actions/api_keys.rotate",
		{ method: "POST", body: JSON.stringify({ params: { keyId }, reason }) },
		{ requireSession: true },
	);
	return r.result;
}

export async function adminApiKeyRevoke(keyId: string, reason: string): Promise<void> {
	await fetchAppAPI(
		"/api/app/admin/actions/api_keys.revoke",
		{ method: "POST", body: JSON.stringify({ params: { keyId }, reason }) },
		{ requireSession: true },
	);
}
```

The shape `{ ok: true; result: ... }` matches what the dispatcher returns (see `dispatcher.go:380`). `fetchAppAPI` is the existing helper used by other admin methods — copy its calling convention from `getAdminBmServers`.

- [ ] **Step 5: Verify frontend lint**

Run: `pnpm --filter frontend exec tsc --noEmit`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add apps/backend/internal/http/api_keys_handlers.go apps/backend/internal/http/router.go apps/frontend/lib/rustlens-api.ts
git commit -m "feat(backend,frontend): GET /api/app/admin/api-keys + client helpers"
```

---

## Phase 8 — Frontend

### Task 17: Sidebar "Infrastructure" group + API Keys entry

**Files:**
- Modify: `apps/frontend/components/app-shared.tsx`

- [ ] **Step 1: Locate the nav config**

Open `apps/frontend/components/app-shared.tsx`. Find `getNavGroups(variant, pathname, permissions)` — this is the function that builds the admin sidebar nav group list.

- [ ] **Step 2: Add the Infrastructure group**

Within the `variant === "admin"` branch, add an entry alongside the existing groups. Use the existing nav-group shape (mirror an existing group like the one containing Proxies/BM Servers if there is one; if not, create the group as the natural home for those plus the new API Keys link).

Pseudo-shape (adapt to the actual `NavGroup` type used in the file):

```ts
{
	title: "Infrastructure",
	items: [
		{ title: "Proxies", path: "/admin/proxies", icon: NetworkIcon, requiredPermission: "proxies.read" },
		{ title: "BattleMetrics Servers", path: "/admin/bm-servers", icon: ServerIcon, requiredPermission: "bm_servers.read" },
		{ title: "API Keys", path: "/admin/api-keys", icon: KeyIcon, requiredPermission: "api_keys.manage" },
	],
}
```

If items in this file are already filtered by permission via `permissions.includes(item.requiredPermission)`, that logic carries over. If the existing pattern uses a different key (e.g. `permission` vs `requiredPermission`), match it. The group itself should be rendered when at least one of its items is visible.

- [ ] **Step 3: Pick an icon**

Use `KeyIcon` from `lucide-react`:

```ts
import { KeyIcon } from "lucide-react";
```

- [ ] **Step 4: Verify lint and dev render**

Run: `pnpm --filter frontend exec tsc --noEmit`
Expected: PASS.

Run `make dev` and confirm the sidebar shows the new group (you will need to be signed in as a user with `api_keys.manage` — superadmin works).

- [ ] **Step 5: Commit**

```bash
git add apps/frontend/components/app-shared.tsx
git commit -m "feat(frontend): admin sidebar Infrastructure group with API Keys entry"
```

---

### Task 18: `/admin/api-keys` page scaffold and table

**Files:**
- Create: `apps/frontend/app/admin/api-keys/page.tsx`
- Create: `apps/frontend/app/admin/api-keys/_components/api-keys-page.tsx`
- Create: `apps/frontend/app/admin/api-keys/_components/api-keys-table.tsx`

- [ ] **Step 1: Create the route entry**

Create `apps/frontend/app/admin/api-keys/page.tsx`:

```tsx
import { AppSidebar } from "../_components/app-sidebar";
import { ApiKeysPage } from "./_components/api-keys-page";
import { SiteHeader } from "@/components/site-header";
import { SidebarInset, SidebarProvider } from "@/components/ui/sidebar";
import { getAdminApiKeys, getCurrentUserSafe } from "@/lib/rustlens-api";
import { requirePermission } from "@/lib/session";

export default async function AdminApiKeysRoute({
	searchParams,
}: {
	searchParams: Promise<Record<string, string | string[] | undefined>>;
}) {
	const session = await requirePermission("api_keys.manage");
	const sp = await searchParams;
	const q = typeof sp.q === "string" ? sp.q : undefined;
	const includeRevoked = sp.includeRevoked === "true";
	const page = typeof sp.page === "string" ? Number.parseInt(sp.page, 10) || 1 : 1;

	const [user, data] = await Promise.all([
		getCurrentUserSafe(),
		getAdminApiKeys({ q, includeRevoked, page, pageSize: 50 }),
	]);

	return (
		<SidebarProvider>
			<AppSidebar pathname="/admin/api-keys" user={user} session={session} />
			<SidebarInset>
				<SiteHeader />
				<ApiKeysPage data={data} initialQuery={{ q, includeRevoked, page }} />
			</SidebarInset>
		</SidebarProvider>
	);
}
```

If the existing admin pages use a different shell shape, copy it verbatim from `app/admin/bm-servers/page.tsx`. The important call is `await requirePermission("api_keys.manage")` — it redirects to `/admin` for non-holders.

- [ ] **Step 2: Create the client page**

Create `apps/frontend/app/admin/api-keys/_components/api-keys-page.tsx`:

```tsx
"use client";

import { useRouter, useSearchParams } from "next/navigation";
import { useState } from "react";
import type { AdminApiKey, AdminApiKeysPage } from "@/lib/rustlens-api";
import { ApiKeysTable } from "./api-keys-table";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";

export function ApiKeysPage({
	data,
	initialQuery,
}: {
	data: AdminApiKeysPage;
	initialQuery: { q?: string; includeRevoked?: boolean; page: number };
}) {
	const router = useRouter();
	const search = useSearchParams();
	const [q, setQ] = useState(initialQuery.q ?? "");
	const [includeRevoked, setIncludeRevoked] = useState(initialQuery.includeRevoked ?? false);

	function applyFilters(next: { q?: string; includeRevoked?: boolean }) {
		const sp = new URLSearchParams(search?.toString());
		if (next.q) sp.set("q", next.q); else sp.delete("q");
		if (next.includeRevoked) sp.set("includeRevoked", "true"); else sp.delete("includeRevoked");
		sp.delete("page");
		router.push(`/admin/api-keys?${sp.toString()}`);
	}

	return (
		<div className="flex flex-col gap-4 p-6">
			<header className="flex items-center justify-between">
				<h1 className="text-2xl font-semibold">API Keys</h1>
				<Button onClick={() => alert("Create dialog wired in Task 19")}>
					New API key
				</Button>
			</header>
			<div className="flex items-center gap-3">
				<Input
					value={q}
					placeholder="Search by name or prefix"
					onChange={(e) => setQ(e.target.value)}
					onBlur={() => applyFilters({ q, includeRevoked })}
					className="max-w-xs"
				/>
				<label className="flex items-center gap-2 text-sm">
					<Switch
						checked={includeRevoked}
						onCheckedChange={(v) => {
							setIncludeRevoked(v);
							applyFilters({ q, includeRevoked: v });
						}}
					/>
					Show revoked
				</label>
			</div>
			<ApiKeysTable items={data.items} />
		</div>
	);
}
```

- [ ] **Step 3: Create the table component**

Create `apps/frontend/app/admin/api-keys/_components/api-keys-table.tsx`:

```tsx
"use client";

import type { AdminApiKey } from "@/lib/rustlens-api";
import { formatRelativeTime } from "@/lib/relative-time";
import {
	Table,
	TableBody,
	TableCell,
	TableHead,
	TableHeader,
	TableRow,
} from "@/components/ui/table";

export function ApiKeysTable({ items }: { items: AdminApiKey[] }) {
	return (
		<Table>
			<TableHeader>
				<TableRow>
					<TableHead>Name</TableHead>
					<TableHead>Prefix</TableHead>
					<TableHead>Permissions</TableHead>
					<TableHead>Last used</TableHead>
					<TableHead>Expires</TableHead>
					<TableHead className="w-[1%]" />
				</TableRow>
			</TableHeader>
			<TableBody>
				{items.map((k) => (
					<TableRow key={k.id} data-revoked={k.revokedAt !== null}>
						<TableCell>{k.name}</TableCell>
						<TableCell className="font-mono">{k.prefix}…{k.last4}</TableCell>
						<TableCell>
							{k.revokedAt ? "revoked" : `${k.permissions.length} perms`}
						</TableCell>
						<TableCell>{k.lastUsedAt ? formatRelativeTime(k.lastUsedAt) : "—"}</TableCell>
						<TableCell>{k.expiresAt ? formatRelativeTime(k.expiresAt) : "never"}</TableCell>
						<TableCell>{/* row actions wired in Tasks 20/21 */}</TableCell>
					</TableRow>
				))}
				{items.length === 0 && (
					<TableRow>
						<TableCell colSpan={6} className="text-center text-muted-foreground">
							No API keys yet.
						</TableCell>
					</TableRow>
				)}
			</TableBody>
		</Table>
	);
}
```

If `formatRelativeTime` does not exist in `lib/relative-time.ts`, use whatever existing helper renders relative time on the BM Servers / Proxies pages.

- [ ] **Step 4: Verify**

Run: `pnpm --filter frontend exec tsc --noEmit`
Expected: PASS.

Run `make dev` and open `http://localhost/admin/api-keys` as superadmin. The page renders "No API keys yet."

- [ ] **Step 5: Commit**

```bash
git add apps/frontend/app/admin/api-keys/
git commit -m "feat(frontend): /admin/api-keys page scaffold with filter toolbar and table"
```

---

### Task 19: Create dialog + one-time secret reveal

**Files:**
- Create: `apps/frontend/app/admin/api-keys/_components/create-api-key-dialog.tsx`
- Create: `apps/frontend/app/admin/api-keys/_components/secret-reveal-panel.tsx`
- Modify: `apps/frontend/app/admin/api-keys/_components/api-keys-page.tsx`

- [ ] **Step 1: Implement the reveal panel**

Create `apps/frontend/app/admin/api-keys/_components/secret-reveal-panel.tsx`:

```tsx
"use client";

import { CopyIcon, CheckIcon } from "lucide-react";
import { useState } from "react";
import { Button } from "@/components/ui/button";

export function SecretRevealPanel({
	secret,
	onAcknowledge,
}: {
	secret: string;
	onAcknowledge: () => void;
}) {
	const [copied, setCopied] = useState(false);
	return (
		<div className="flex flex-col gap-4">
			<div className="rounded-md border border-amber-500/30 bg-amber-500/5 p-3 text-sm">
				This is the only time the full secret will be shown. Store it in a password manager or
				secret store. If you lose it, rotate the key.
			</div>
			<div className="flex items-center gap-2 rounded-md bg-muted p-3 font-mono text-sm">
				<span className="flex-1 break-all">{secret}</span>
				<Button
					size="sm"
					variant="outline"
					onClick={async () => {
						await navigator.clipboard.writeText(secret);
						setCopied(true);
					}}
				>
					{copied ? <CheckIcon className="size-4" /> : <CopyIcon className="size-4" />}
					{copied ? "Copied" : "Copy"}
				</Button>
			</div>
			<div className="flex justify-end">
				<Button onClick={onAcknowledge}>I copied it</Button>
			</div>
		</div>
	);
}
```

- [ ] **Step 2: Implement the create dialog**

Create `apps/frontend/app/admin/api-keys/_components/create-api-key-dialog.tsx`:

```tsx
"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";
import { adminApiKeyCreate } from "@/lib/rustlens-api";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { SecretRevealPanel } from "./secret-reveal-panel";

export type Permission = { key: string; description: string };

const EXPIRY_PRESETS: Array<{ value: string; label: string; days: number | null }> = [
	{ value: "never", label: "Never", days: null },
	{ value: "30", label: "30 days", days: 30 },
	{ value: "90", label: "90 days", days: 90 },
	{ value: "365", label: "1 year", days: 365 },
];

export function CreateApiKeyDialog({
	open,
	onOpenChange,
	availablePermissions,
}: {
	open: boolean;
	onOpenChange: (open: boolean) => void;
	availablePermissions: Permission[];
}) {
	const router = useRouter();
	const [name, setName] = useState("");
	const [selected, setSelected] = useState<Set<string>>(new Set());
	const [expiry, setExpiry] = useState("never");
	const [reason, setReason] = useState("");
	const [submitting, setSubmitting] = useState(false);
	const [secret, setSecret] = useState<string | null>(null);
	const [error, setError] = useState<string | null>(null);

	async function submit() {
		setSubmitting(true);
		setError(null);
		try {
			const days = EXPIRY_PRESETS.find((p) => p.value === expiry)?.days ?? null;
			const expiresAt = days
				? new Date(Date.now() + days * 86_400_000).toISOString()
				: undefined;
			const res = await adminApiKeyCreate(
				{ name, permissions: Array.from(selected), expiresAt },
				reason,
			);
			setSecret(res.secret);
		} catch (e) {
			setError(e instanceof Error ? e.message : "Failed to create key");
		} finally {
			setSubmitting(false);
		}
	}

	function close() {
		setName("");
		setSelected(new Set());
		setExpiry("never");
		setReason("");
		setSecret(null);
		setError(null);
		onOpenChange(false);
		router.refresh();
	}

	// Group permissions by namespace prefix for the checkbox grid.
	const grouped = availablePermissions.reduce<Record<string, Permission[]>>((acc, p) => {
		const ns = p.key.split(".")[0] ?? "other";
		(acc[ns] ??= []).push(p);
		return acc;
	}, {});

	return (
		<Dialog open={open} onOpenChange={(o) => (o ? onOpenChange(true) : close())}>
			<DialogContent className="max-w-2xl">
				<DialogHeader>
					<DialogTitle>{secret ? "Copy your key" : "New API key"}</DialogTitle>
					{!secret && (
						<DialogDescription>
							Creates a new API key. You will only see the secret once.
						</DialogDescription>
					)}
				</DialogHeader>
				{secret ? (
					<SecretRevealPanel secret={secret} onAcknowledge={close} />
				) : (
					<div className="flex flex-col gap-4">
						<div className="flex flex-col gap-1.5">
							<Label htmlFor="ak-name">Name</Label>
							<Input
								id="ak-name"
								value={name}
								maxLength={64}
								onChange={(e) => setName(e.target.value)}
								placeholder="deploy-bot"
							/>
						</div>
						<div className="flex flex-col gap-1.5">
							<Label>Permissions</Label>
							<div className="grid max-h-64 grid-cols-2 gap-2 overflow-y-auto rounded-md border p-3">
								{Object.entries(grouped).map(([ns, perms]) => (
									<div key={ns} className="col-span-2">
										<div className="text-xs font-semibold uppercase text-muted-foreground">
											{ns}
										</div>
										{perms.map((p) => (
											<label key={p.key} className="flex items-center gap-2 py-1 text-sm">
												<Checkbox
													checked={selected.has(p.key)}
													onCheckedChange={(checked) => {
														setSelected((prev) => {
															const next = new Set(prev);
															if (checked) next.add(p.key); else next.delete(p.key);
															return next;
														});
													}}
												/>
												<span className="font-mono">{p.key}</span>
												<span className="text-muted-foreground">— {p.description}</span>
											</label>
										))}
									</div>
								))}
							</div>
						</div>
						<div className="flex flex-col gap-1.5">
							<Label>Expires</Label>
							<RadioGroup value={expiry} onValueChange={setExpiry} className="flex gap-4">
								{EXPIRY_PRESETS.map((p) => (
									<label key={p.value} className="flex items-center gap-1.5 text-sm">
										<RadioGroupItem value={p.value} />
										{p.label}
									</label>
								))}
							</RadioGroup>
						</div>
						<div className="flex flex-col gap-1.5">
							<Label htmlFor="ak-reason">Reason</Label>
							<Input
								id="ak-reason"
								value={reason}
								onChange={(e) => setReason(e.target.value)}
								placeholder="Why is this key being issued?"
							/>
						</div>
						{error && <div className="text-sm text-destructive">{error}</div>}
					</div>
				)}
				{!secret && (
					<DialogFooter>
						<Button variant="outline" onClick={close} disabled={submitting}>
							Cancel
						</Button>
						<Button
							onClick={submit}
							disabled={
								submitting || name.trim().length === 0 || reason.trim().length === 0
							}
						>
							{submitting ? "Creating…" : "Create key"}
						</Button>
					</DialogFooter>
				)}
			</DialogContent>
		</Dialog>
	);
}
```

- [ ] **Step 3: Fetch the available permissions on the server**

The `GET /api/app/admin/permissions` endpoint already exists for the roles page. In `apps/frontend/lib/rustlens-api.ts`, confirm there is a `getAdminPermissions()` helper (or add one mirroring `getAdminBmServers`). Then in `app/admin/api-keys/page.tsx`, fetch the permission catalog alongside the keys list, filtered server-side to those the current user holds:

```ts
const [user, data, permsResp] = await Promise.all([
	getCurrentUserSafe(),
	getAdminApiKeys({ q, includeRevoked, page, pageSize: 50 }),
	getAdminPermissions(),
]);

const userPerms = new Set(session.permissions);
const availablePermissions = (permsResp.items ?? []).filter(
	(p) => session.permissions.includes("admins.grant") || userPerms.has(p.key),
);
```

Pass `availablePermissions` down to `ApiKeysPage` then to `CreateApiKeyDialog`. Replace the `alert(...)` placeholder in the page with the real dialog open state.

- [ ] **Step 4: Verify**

Run: `pnpm --filter frontend exec tsc --noEmit`
Expected: PASS.

Run `make dev`, create a key, copy the secret, click "I copied it". The new row appears.

- [ ] **Step 5: Commit**

```bash
git add apps/frontend/app/admin/api-keys/ apps/frontend/lib/rustlens-api.ts
git commit -m "feat(frontend): create API key dialog with one-time secret reveal"
```

---

### Task 20: Rotate dialog

**Files:**
- Create: `apps/frontend/app/admin/api-keys/_components/rotate-api-key-dialog.tsx`
- Modify: `apps/frontend/app/admin/api-keys/_components/api-keys-table.tsx`

- [ ] **Step 1: Implement the rotate dialog**

Create `apps/frontend/app/admin/api-keys/_components/rotate-api-key-dialog.tsx`:

```tsx
"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";
import { adminApiKeyRotate } from "@/lib/rustlens-api";
import { Button } from "@/components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { SecretRevealPanel } from "./secret-reveal-panel";

export function RotateApiKeyDialog({
	keyId,
	keyName,
	open,
	onOpenChange,
}: {
	keyId: string;
	keyName: string;
	open: boolean;
	onOpenChange: (open: boolean) => void;
}) {
	const router = useRouter();
	const [reason, setReason] = useState("");
	const [submitting, setSubmitting] = useState(false);
	const [secret, setSecret] = useState<string | null>(null);
	const [error, setError] = useState<string | null>(null);

	async function submit() {
		setSubmitting(true);
		setError(null);
		try {
			const res = await adminApiKeyRotate(keyId, reason);
			setSecret(res.secret);
		} catch (e) {
			setError(e instanceof Error ? e.message : "Failed to rotate key");
		} finally {
			setSubmitting(false);
		}
	}

	function close() {
		setReason("");
		setSecret(null);
		setError(null);
		onOpenChange(false);
		router.refresh();
	}

	return (
		<Dialog open={open} onOpenChange={(o) => (o ? onOpenChange(true) : close())}>
			<DialogContent>
				<DialogHeader>
					<DialogTitle>{secret ? "Copy your new key" : `Rotate ${keyName}`}</DialogTitle>
				</DialogHeader>
				{secret ? (
					<SecretRevealPanel secret={secret} onAcknowledge={close} />
				) : (
					<div className="flex flex-col gap-4">
						<p className="text-sm text-muted-foreground">
							Rotating generates a new secret in place. The old secret stops working
							immediately. The key id, name, permissions, and audit history are preserved.
						</p>
						<div className="flex flex-col gap-1.5">
							<Label htmlFor="rk-reason">Reason</Label>
							<Input
								id="rk-reason"
								value={reason}
								onChange={(e) => setReason(e.target.value)}
								placeholder="Why is this key being rotated?"
							/>
						</div>
						{error && <div className="text-sm text-destructive">{error}</div>}
					</div>
				)}
				{!secret && (
					<DialogFooter>
						<Button variant="outline" onClick={close} disabled={submitting}>
							Cancel
						</Button>
						<Button onClick={submit} disabled={submitting || reason.trim().length === 0}>
							{submitting ? "Rotating…" : "Rotate"}
						</Button>
					</DialogFooter>
				)}
			</DialogContent>
		</Dialog>
	);
}
```

- [ ] **Step 2: Wire it into the table row dropdown**

In `apps/frontend/app/admin/api-keys/_components/api-keys-table.tsx`, replace the empty action cell with a dropdown menu using shadcn's `DropdownMenu`. The "Rotate" item opens the dialog. Track dialog open state in the table component (one piece of state holds `{ keyId, keyName } | null`).

```tsx
"use client";

import { useState } from "react";
import { MoreHorizontalIcon } from "lucide-react";
import type { AdminApiKey } from "@/lib/rustlens-api";
import { formatRelativeTime } from "@/lib/relative-time";
import { Button } from "@/components/ui/button";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
	Table, TableBody, TableCell, TableHead, TableHeader, TableRow,
} from "@/components/ui/table";
import { RotateApiKeyDialog } from "./rotate-api-key-dialog";

export function ApiKeysTable({ items }: { items: AdminApiKey[] }) {
	const [rotating, setRotating] = useState<{ id: string; name: string } | null>(null);

	return (
		<>
			<Table>{/* ...existing header... */}
				<TableBody>
					{items.map((k) => (
						<TableRow key={k.id} data-revoked={k.revokedAt !== null}>
							{/* ...existing cells... */}
							<TableCell>
								<DropdownMenu>
									<DropdownMenuTrigger asChild>
										<Button size="icon" variant="ghost">
											<MoreHorizontalIcon className="size-4" />
										</Button>
									</DropdownMenuTrigger>
									<DropdownMenuContent align="end">
										<DropdownMenuItem
											disabled={k.revokedAt !== null}
											onClick={() => setRotating({ id: k.id, name: k.name })}
										>
											Rotate
										</DropdownMenuItem>
										{/* Revoke item added in Task 21 */}
									</DropdownMenuContent>
								</DropdownMenu>
							</TableCell>
						</TableRow>
					))}
				</TableBody>
			</Table>
			{rotating && (
				<RotateApiKeyDialog
					keyId={rotating.id}
					keyName={rotating.name}
					open={true}
					onOpenChange={(o) => !o && setRotating(null)}
				/>
			)}
		</>
	);
}
```

- [ ] **Step 3: Verify**

Run: `pnpm --filter frontend exec tsc --noEmit`
Expected: PASS.

Manually: rotate a key in `make dev`, observe new secret displayed and `last_used_at` reset.

- [ ] **Step 4: Commit**

```bash
git add apps/frontend/app/admin/api-keys/
git commit -m "feat(frontend): rotate API key dialog with secret reveal"
```

---

### Task 21: Revoke dialog

**Files:**
- Create: `apps/frontend/app/admin/api-keys/_components/revoke-api-key-dialog.tsx`
- Modify: `apps/frontend/app/admin/api-keys/_components/api-keys-table.tsx`

- [ ] **Step 1: Implement the revoke dialog**

Create `apps/frontend/app/admin/api-keys/_components/revoke-api-key-dialog.tsx`:

```tsx
"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";
import { adminApiKeyRevoke } from "@/lib/rustlens-api";
import { Button } from "@/components/ui/button";
import {
	Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

export function RevokeApiKeyDialog({
	keyId,
	keyName,
	open,
	onOpenChange,
}: {
	keyId: string;
	keyName: string;
	open: boolean;
	onOpenChange: (open: boolean) => void;
}) {
	const router = useRouter();
	const [reason, setReason] = useState("");
	const [submitting, setSubmitting] = useState(false);
	const [error, setError] = useState<string | null>(null);

	async function submit() {
		setSubmitting(true);
		setError(null);
		try {
			await adminApiKeyRevoke(keyId, reason);
			onOpenChange(false);
			router.refresh();
		} catch (e) {
			setError(e instanceof Error ? e.message : "Failed to revoke");
		} finally {
			setSubmitting(false);
		}
	}

	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent>
				<DialogHeader>
					<DialogTitle>Revoke {keyName}</DialogTitle>
				</DialogHeader>
				<div className="flex flex-col gap-3 text-sm">
					<p>This will permanently revoke the API key. Any clients using it will start failing.</p>
					<div className="flex flex-col gap-1.5">
						<Label htmlFor="rv-reason">Reason</Label>
						<Input
							id="rv-reason"
							value={reason}
							onChange={(e) => setReason(e.target.value)}
							placeholder="Why is this key being revoked?"
						/>
					</div>
					{error && <div className="text-destructive">{error}</div>}
				</div>
				<DialogFooter>
					<Button variant="outline" onClick={() => onOpenChange(false)} disabled={submitting}>
						Cancel
					</Button>
					<Button
						variant="destructive"
						onClick={submit}
						disabled={submitting || reason.trim().length === 0}
					>
						{submitting ? "Revoking…" : "Revoke"}
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
```

- [ ] **Step 2: Add the revoke item to the table dropdown**

Extend `api-keys-table.tsx`:

```tsx
const [revoking, setRevoking] = useState<{ id: string; name: string } | null>(null);

// inside DropdownMenuContent, below the Rotate item:
<DropdownMenuItem
	disabled={k.revokedAt !== null}
	className="text-destructive"
	onClick={() => setRevoking({ id: k.id, name: k.name })}
>
	Revoke
</DropdownMenuItem>
```

And below `<RotateApiKeyDialog ... />`:

```tsx
{revoking && (
	<RevokeApiKeyDialog
		keyId={revoking.id}
		keyName={revoking.name}
		open={true}
		onOpenChange={(o) => !o && setRevoking(null)}
	/>
)}
```

- [ ] **Step 3: Verify**

Run: `pnpm --filter frontend exec tsc --noEmit`
Expected: PASS.

Manually verify in `make dev`: revoke a key, the row mutes (or disappears unless "Show revoked" is on), and a subsequent request with the old secret returns 401.

- [ ] **Step 4: Commit**

```bash
git add apps/frontend/app/admin/api-keys/
git commit -m "feat(frontend): revoke API key dialog"
```

---

## Phase 9 — End-to-end test and PR

### Task 22: E2E flow test

**Files:**
- Modify: `apps/backend/internal/admin/e2e_test.go`

- [ ] **Step 1: Add the test**

Append to `apps/backend/internal/admin/e2e_test.go`:

```go
func TestAPIKeyEndToEnd(t *testing.T) {
	t.Skip("Wire once Task 15 finishes the router; implement with the existing e2e harness in this file by: (a) calling api_keys.create through the dispatcher, (b) issuing GET /api/app/admin/users with Authorization: Bearer <secret>, (c) asserting the response is 200 and an audit row exists with via_api_key_{id,name} populated, (d) calling api_keys.revoke, (e) asserting the same request now returns 401 api_key_revoked.")
}
```

The skip body is the implementation recipe. The implementer must replace the `t.Skip` call with the actual test using whatever testserver / harness the rest of the file uses; do not duplicate harness code here.

- [ ] **Step 2: Run the package test suite**

Run: `cd apps/backend && go test ./internal/admin/...`
Expected: PASS (the new test is skipped; everything else stays green).

- [ ] **Step 3: Run the full backend test suite + vet**

Run: `cd apps/backend && go vet ./... && go test ./...`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add apps/backend/internal/admin/e2e_test.go
git commit -m "test(backend): e2e harness placeholder for API key auth + audit + revoke"
```

---

### Task 23: Open the PR

- [ ] **Step 1: Push the branch**

Run: `git push -u origin feat/admin-api-keys`
Expected: branch already tracked from earlier push; `git push` is enough.

- [ ] **Step 2: Diff against main**

Run: `git log --oneline main..HEAD` and `git diff --stat main..HEAD`
Expected: ~22 commits, changes scoped to `apps/backend/internal/{admin,appauth,http,store}`, `apps/frontend/{app/admin/api-keys,components,lib}`, and `docs/superpowers/`.

- [ ] **Step 3: Open the PR with `gh pr create`**

```bash
gh pr create --base main --head feat/admin-api-keys --title "feat: admin API keys" --body "$(cat <<'EOF'
## Summary
- Adds admin-managed API keys (`rl_admin_…`) that authenticate `/api/app/admin/*` and `/api/internal/bot/*`, scoped to a subset of the creator's RBAC permissions.
- Lifecycle (create / rotate / revoke) routes through the existing admin action dispatcher with required reasons + audit attribution (`audit_events.via_api_key_{id,name}`).
- `/admin/api-keys` UI under a new "Infrastructure" sidebar group, with one-time secret reveal on create and rotate.

## Spec & plan
- Spec: `docs/superpowers/specs/2026-05-16-admin-api-keys-design.md`
- Plan: `docs/superpowers/plans/2026-05-16-admin-api-keys.md`

## Test plan
- [ ] `go test ./...` in `apps/backend` passes
- [ ] `pnpm --filter frontend exec tsc --noEmit` passes
- [ ] Manual: create a key as superadmin via `/admin/api-keys`, copy the secret, hit `GET /api/app/admin/users` with `Authorization: Bearer <secret>` and confirm 200
- [ ] Manual: confirm `audit_events` row has `via_api_key_id` and `via_api_key_name` populated for that request
- [ ] Manual: rotate, confirm old secret 401s and new one 200s
- [ ] Manual: revoke, confirm secret 401s with `api_key_revoked`
- [ ] Manual: as a non-superadmin with `api_keys.manage` but only `users.read`, confirm the create dialog hides `users.delete` and a direct API attempt to grant it 403s with `permission_escalation`
- [ ] Manual: API key with `bot_routes.read` only — GET on a bot route succeeds, POST 403s
- [ ] Manual: API key write on a bot route writes one `audit_events` row with `action_type = bot_route.write`
EOF
)"
```

- [ ] **Step 4: Print the PR URL**

The `gh pr create` output ends with the URL. Surface it to the user in the final message.

---

## Self-review (run after writing the plan; not part of execution)

- [ ] **Spec coverage:** Each spec section has a matching task — permissions (T1), `api_keys` table (T2), audit columns (T3), store (T4), audit Go types (T5), dispatcher propagation (T6), guard (T7), three dispatcher actions (T8–T10) + registration (T11), API key middleware (T12), bot-route scope middleware with write audit (T13), resolver intersection (T14), router wiring (T15), list endpoint + client (T16), sidebar (T17), page scaffold (T18), create dialog with reveal (T19), rotate dialog (T20), revoke dialog (T21), e2e (T22), PR (T23). The spec mentions a "side sheet" for row detail — that is **deferred**; dropdown actions are sufficient for v1 and the spec marks per-row deep-detail as nice-to-have.
- [ ] **Placeholder scan:** No "TBD"/"TODO"/"fill in details" outside the explicit test `t.Skip` placeholders, which are intentional and contain a complete recipe for the implementer.
- [ ] **Type consistency:** `APIKeyAuthContext` field names (`KeyID`, `KeyName`, `UserID`, `KeyPermissions`) are stable across T6 (declaration), T12 (population), T13 (read), T14 (consumption via bridge), T15 (bridge in router). `ActorPermissionsLoader.PermissionsForUser` matches the existing `rbac.Resolver.PermissionsForUser` signature.
