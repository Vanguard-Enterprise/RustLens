# Discord Linked Roles PRO Sync Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Sync the dashboard `pro` admin role into a Discord-side linked role via Discord's Linked Roles application metadata. When PRO is granted, push `is_pro=1` + `pro_since=<grant timestamp>` to Discord; when revoked, push `is_pro=0`. Discord re-evaluates the linked role automatically.

**Architecture:** New `internal/roleconnection` Go package owns OAuth (separate flow with `role_connections.write` scope), token refresh, and the PUT-metadata call. A new `discord_role_connections` table stores AES-GCM-encrypted access + refresh tokens per app user. Metadata pushes fire from three places: immediately after OAuth callback, asynchronously from the `users.roles.grant`/`users.roles.revoke` admin actions (only when role key is `pro`), and from a 6 h background reconciler protected by a Postgres advisory lock. Frontend adds one dashboard card with Connect/Disconnect.

**Tech Stack:** Go 1.24 (chi, pgx, go-redis), Next.js 16 / React 19 / Tailwind v4 / shadcn / sonner, Postgres 16.

**Pre-implementation notes:**
- Frontend uses a version of Next.js *newer than your training data*. Per `apps/frontend/AGENTS.md`, before touching any frontend file read the relevant guide in `apps/frontend/node_modules/next/dist/docs/` (server actions, cookies, redirect, server-side fetch). When in doubt, copy patterns from the existing `apps/frontend/app/dashboard/` and `apps/frontend/lib/` files.
- Backend tests use a real Postgres via `TEST_POSTGRES_URL` and the `newTestPostgres(t)` helper in `apps/backend/internal/store/postgres_admin_test.go`. Tests are skipped when the env var is unset, which is fine for CI gating. Run with: `TEST_POSTGRES_URL=postgres://rustlens:rustlens@localhost:5432/rustlens?sslmode=disable go test ./internal/store/...`.
- The repo prefers `pnpm` over `npm`, `uv` over `pip`, `rg` over `grep`. Conventional commits: `feat`, `fix`, `chore`, etc. Commits must NOT include Co-Authored-By or "Generated with Claude" trailers (per global user instructions).
- `pnpm lint` and `pnpm test` run the pnpm workspace; for the Go side use `go vet ./...` / `go test ./...` inside `apps/backend/`.
- Always commit AND push after a task per repo memory: "commit + push as one step".

---

## Phase 1 — Database schema

### Task 1: Add `discord_role_connections` table to EnsureSchema

**Files:**
- Modify: `apps/backend/internal/store/postgres.go` (extend `EnsureSchema`)

- [ ] **Step 1: Locate the existing migration block to insert after**

Run: `rg -n "admin_user_roles" apps/backend/internal/store/postgres.go`
Note the line where the `admin_user_roles` CREATE TABLE block ends (search "create admin_user_roles:" in the error-wrap line). Insert the new block AFTER that one so the FK on `app_users(id)` resolves cleanly.

- [ ] **Step 2: Add the new migration block**

Add this immediately after the existing `admin_user_roles` migration (after its `return fmt.Errorf("create admin_user_roles: %w", err)` block):

```go
	if _, err := p.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS discord_role_connections (
			user_id            TEXT        NOT NULL PRIMARY KEY
			                    REFERENCES app_users(id) ON DELETE CASCADE,
			discord_id         TEXT        NOT NULL,
			access_token_ct    BYTEA       NOT NULL,
			access_token_nonce BYTEA       NOT NULL,
			refresh_token_ct   BYTEA       NOT NULL,
			refresh_token_nonce BYTEA      NOT NULL,
			access_expires_at  TIMESTAMPTZ NOT NULL,
			scopes             TEXT        NOT NULL,
			linked_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
			last_pushed_at     TIMESTAMPTZ NULL,
			last_pushed_hash   TEXT        NULL,
			last_push_error    TEXT        NULL,
			revoked_at         TIMESTAMPTZ NULL
		)
	`); err != nil {
		return fmt.Errorf("create discord_role_connections: %w", err)
	}
	if _, err := p.pool.Exec(ctx, `
		CREATE INDEX IF NOT EXISTS idx_discord_role_connections_active
		ON discord_role_connections (revoked_at)
		WHERE revoked_at IS NULL
	`); err != nil {
		return fmt.Errorf("index discord_role_connections active: %w", err)
	}
```

The two-column-per-encrypted-field pattern (`*_ct` + `*_nonce`) matches the existing `outbound_proxies` table — see `apps/backend/internal/proxycrypto/crypto.go` where `Encrypt` returns `(ciphertext, nonce, error)`.

- [ ] **Step 3: Verify build**

Run: `cd apps/backend && go build ./...`
Expected: exit 0.

- [ ] **Step 4: Verify migration applies cleanly against a live Postgres**

Run (against the dev Postgres started by `pnpm infra:up`):
```bash
cd apps/backend && TEST_POSTGRES_URL=postgres://rustlens:rustlens@localhost:5432/rustlens?sslmode=disable go test ./internal/store/ -run TestEnsureSchema_CreatesAdminTables -v
```
Expected: PASS (the existing test only checks admin tables; it will at minimum prove EnsureSchema doesn't error after our addition).

- [ ] **Step 5: Add a coverage test for the new table**

Append to `apps/backend/internal/store/postgres_admin_test.go`:

```go
func TestEnsureSchema_CreatesDiscordRoleConnectionsTable(t *testing.T) {
	ctx := context.Background()
	store := newTestPostgres(t)
	var exists bool
	if err := store.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = $1)`,
		"discord_role_connections",
	).Scan(&exists); err != nil {
		t.Fatalf("query: %v", err)
	}
	if !exists {
		t.Fatal("expected discord_role_connections table to exist")
	}
}
```

Run: `cd apps/backend && TEST_POSTGRES_URL=... go test ./internal/store/ -run TestEnsureSchema_CreatesDiscordRoleConnectionsTable -v`
Expected: PASS.

- [ ] **Step 6: Commit and push**

```bash
git add apps/backend/internal/store/postgres.go apps/backend/internal/store/postgres_admin_test.go
git commit -m "feat(backend): add discord_role_connections schema"
git push
```

---

## Phase 2 — Store CRUD for role connections

### Task 2: Define the row type and Upsert

**Files:**
- Create: `apps/backend/internal/store/role_connections.go`
- Create: `apps/backend/internal/store/role_connections_test.go`

- [ ] **Step 1: Write the failing test for Upsert + Get**

Create `apps/backend/internal/store/role_connections_test.go`:

```go
package store

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"
)

func insertTestAppUser(t *testing.T, s *PostgresStore, userID, discordID string) {
	t.Helper()
	if _, err := s.pool.Exec(context.Background(), `
		INSERT INTO app_users (id, discord_id, discord_username, display_name)
		VALUES ($1, $2, $3, $3)
	`, userID, discordID, "test_"+userID); err != nil {
		t.Fatalf("insert app_user: %v", err)
	}
}

func TestDiscordRoleConnection_UpsertAndGet(t *testing.T) {
	ctx := context.Background()
	s := newTestPostgres(t)

	suffix := time.Now().UnixNano() % 1_000_000_000
	userID := fmt.Sprintf("usr_rc_%d", suffix)
	discordID := fmt.Sprintf("3000000%010d", suffix)
	insertTestAppUser(t, s, userID, discordID)

	expires := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	row := DiscordRoleConnection{
		UserID:            userID,
		DiscordID:         discordID,
		AccessTokenCT:     []byte("ct-access"),
		AccessTokenNonce:  []byte("nonce-access"),
		RefreshTokenCT:    []byte("ct-refresh"),
		RefreshTokenNonce: []byte("nonce-refresh"),
		AccessExpiresAt:   expires,
		Scopes:            "identify role_connections.write",
	}
	if err := s.UpsertDiscordRoleConnection(ctx, row); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got, err := s.GetDiscordRoleConnection(ctx, userID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil {
		t.Fatal("expected row, got nil")
	}
	if got.DiscordID != discordID {
		t.Errorf("DiscordID = %q, want %q", got.DiscordID, discordID)
	}
	if !bytes.Equal(got.AccessTokenCT, row.AccessTokenCT) {
		t.Errorf("AccessTokenCT mismatch")
	}
	if !got.AccessExpiresAt.Equal(expires) {
		t.Errorf("AccessExpiresAt = %v, want %v", got.AccessExpiresAt, expires)
	}
	if got.RevokedAt != nil {
		t.Errorf("RevokedAt = %v, want nil", got.RevokedAt)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd apps/backend && TEST_POSTGRES_URL=... go test ./internal/store/ -run TestDiscordRoleConnection_UpsertAndGet -v`
Expected: FAIL (compile error — `DiscordRoleConnection`, `UpsertDiscordRoleConnection`, `GetDiscordRoleConnection` undefined).

- [ ] **Step 3: Implement the row type and the two methods**

Create `apps/backend/internal/store/role_connections.go`:

```go
package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// DiscordRoleConnection mirrors the discord_role_connections table row.
// Token fields are AES-GCM ciphertext + nonce per the proxycrypto helper;
// see internal/roleconnection for the encryption layer.
type DiscordRoleConnection struct {
	UserID            string
	DiscordID         string
	AccessTokenCT     []byte
	AccessTokenNonce  []byte
	RefreshTokenCT    []byte
	RefreshTokenNonce []byte
	AccessExpiresAt   time.Time
	Scopes            string
	LinkedAt          time.Time
	LastPushedAt      *time.Time
	LastPushedHash    *string
	LastPushError     *string
	RevokedAt         *time.Time
}

// UpsertDiscordRoleConnection inserts or replaces the row keyed by user_id.
// It always clears revoked_at on insert/update so a successful re-link from
// the user wipes the revoked state from a prior failed token-refresh.
func (p *PostgresStore) UpsertDiscordRoleConnection(ctx context.Context, in DiscordRoleConnection) error {
	_, err := p.pool.Exec(ctx, `
		INSERT INTO discord_role_connections (
			user_id, discord_id,
			access_token_ct, access_token_nonce,
			refresh_token_ct, refresh_token_nonce,
			access_expires_at, scopes, linked_at, revoked_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, now(), NULL)
		ON CONFLICT (user_id) DO UPDATE SET
			discord_id          = EXCLUDED.discord_id,
			access_token_ct     = EXCLUDED.access_token_ct,
			access_token_nonce  = EXCLUDED.access_token_nonce,
			refresh_token_ct    = EXCLUDED.refresh_token_ct,
			refresh_token_nonce = EXCLUDED.refresh_token_nonce,
			access_expires_at   = EXCLUDED.access_expires_at,
			scopes              = EXCLUDED.scopes,
			revoked_at          = NULL
	`,
		in.UserID, in.DiscordID,
		in.AccessTokenCT, in.AccessTokenNonce,
		in.RefreshTokenCT, in.RefreshTokenNonce,
		in.AccessExpiresAt, in.Scopes,
	)
	return err
}

// GetDiscordRoleConnection returns the row for user_id, or (nil, nil) if absent.
func (p *PostgresStore) GetDiscordRoleConnection(ctx context.Context, userID string) (*DiscordRoleConnection, error) {
	var row DiscordRoleConnection
	err := p.pool.QueryRow(ctx, `
		SELECT user_id, discord_id,
			access_token_ct, access_token_nonce,
			refresh_token_ct, refresh_token_nonce,
			access_expires_at, scopes, linked_at,
			last_pushed_at, last_pushed_hash, last_push_error, revoked_at
		FROM discord_role_connections
		WHERE user_id = $1
	`, userID).Scan(
		&row.UserID, &row.DiscordID,
		&row.AccessTokenCT, &row.AccessTokenNonce,
		&row.RefreshTokenCT, &row.RefreshTokenNonce,
		&row.AccessExpiresAt, &row.Scopes, &row.LinkedAt,
		&row.LastPushedAt, &row.LastPushedHash, &row.LastPushError, &row.RevokedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}
```

- [ ] **Step 4: Run test, verify pass**

Run: `cd apps/backend && TEST_POSTGRES_URL=... go test ./internal/store/ -run TestDiscordRoleConnection_UpsertAndGet -v`
Expected: PASS.

- [ ] **Step 5: Commit and push**

```bash
git add apps/backend/internal/store/role_connections.go apps/backend/internal/store/role_connections_test.go
git commit -m "feat(backend): add discord_role_connections store CRUD (upsert+get)"
git push
```

### Task 3: Delete, MarkRevoked, RecordPushed, ListActive

**Files:**
- Modify: `apps/backend/internal/store/role_connections.go`
- Modify: `apps/backend/internal/store/role_connections_test.go`

- [ ] **Step 1: Add tests**

Append to `role_connections_test.go`:

```go
func TestDiscordRoleConnection_Delete(t *testing.T) {
	ctx := context.Background()
	s := newTestPostgres(t)
	suffix := time.Now().UnixNano() % 1_000_000_000
	userID := fmt.Sprintf("usr_rc_del_%d", suffix)
	insertTestAppUser(t, s, userID, fmt.Sprintf("4000000%010d", suffix))
	if err := s.UpsertDiscordRoleConnection(ctx, DiscordRoleConnection{
		UserID: userID, DiscordID: "ignored",
		AccessTokenCT: []byte("x"), AccessTokenNonce: []byte("y"),
		RefreshTokenCT: []byte("x"), RefreshTokenNonce: []byte("y"),
		AccessExpiresAt: time.Now().Add(time.Hour), Scopes: "identify",
	}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if err := s.DeleteDiscordRoleConnection(ctx, userID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	got, err := s.GetDiscordRoleConnection(ctx, userID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil after delete")
	}
}

func TestDiscordRoleConnection_MarkRevoked(t *testing.T) {
	ctx := context.Background()
	s := newTestPostgres(t)
	suffix := time.Now().UnixNano() % 1_000_000_000
	userID := fmt.Sprintf("usr_rc_rev_%d", suffix)
	insertTestAppUser(t, s, userID, fmt.Sprintf("5000000%010d", suffix))
	if err := s.UpsertDiscordRoleConnection(ctx, DiscordRoleConnection{
		UserID: userID, DiscordID: "ignored",
		AccessTokenCT: []byte("x"), AccessTokenNonce: []byte("y"),
		RefreshTokenCT: []byte("x"), RefreshTokenNonce: []byte("y"),
		AccessExpiresAt: time.Now().Add(time.Hour), Scopes: "identify",
	}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if err := s.MarkDiscordRoleConnectionRevoked(ctx, userID, "invalid_grant"); err != nil {
		t.Fatalf("MarkRevoked: %v", err)
	}
	got, err := s.GetDiscordRoleConnection(ctx, userID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil || got.RevokedAt == nil {
		t.Fatal("expected RevokedAt to be set")
	}
	if got.LastPushError == nil || *got.LastPushError != "invalid_grant" {
		t.Fatalf("LastPushError = %v, want 'invalid_grant'", got.LastPushError)
	}
}

func TestDiscordRoleConnection_RecordPushed(t *testing.T) {
	ctx := context.Background()
	s := newTestPostgres(t)
	suffix := time.Now().UnixNano() % 1_000_000_000
	userID := fmt.Sprintf("usr_rc_push_%d", suffix)
	insertTestAppUser(t, s, userID, fmt.Sprintf("6000000%010d", suffix))
	if err := s.UpsertDiscordRoleConnection(ctx, DiscordRoleConnection{
		UserID: userID, DiscordID: "ignored",
		AccessTokenCT: []byte("x"), AccessTokenNonce: []byte("y"),
		RefreshTokenCT: []byte("x"), RefreshTokenNonce: []byte("y"),
		AccessExpiresAt: time.Now().Add(time.Hour), Scopes: "identify",
	}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if err := s.RecordDiscordRoleConnectionPushed(ctx, userID, "hash-abc"); err != nil {
		t.Fatalf("RecordPushed: %v", err)
	}
	got, err := s.GetDiscordRoleConnection(ctx, userID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.LastPushedHash == nil || *got.LastPushedHash != "hash-abc" {
		t.Fatalf("LastPushedHash = %v", got.LastPushedHash)
	}
	if got.LastPushedAt == nil {
		t.Fatal("expected LastPushedAt to be set")
	}
	if got.LastPushError != nil {
		t.Fatalf("LastPushError should be cleared, got %v", got.LastPushError)
	}
}

func TestDiscordRoleConnection_ListActive(t *testing.T) {
	ctx := context.Background()
	s := newTestPostgres(t)
	suffix := time.Now().UnixNano() % 1_000_000_000
	activeID := fmt.Sprintf("usr_rc_active_%d", suffix)
	revokedID := fmt.Sprintf("usr_rc_revoked_%d", suffix)
	insertTestAppUser(t, s, activeID, fmt.Sprintf("7000000%010d", suffix))
	insertTestAppUser(t, s, revokedID, fmt.Sprintf("8000000%010d", suffix))
	upsert := func(uid string) {
		if err := s.UpsertDiscordRoleConnection(ctx, DiscordRoleConnection{
			UserID: uid, DiscordID: "d",
			AccessTokenCT: []byte("x"), AccessTokenNonce: []byte("y"),
			RefreshTokenCT: []byte("x"), RefreshTokenNonce: []byte("y"),
			AccessExpiresAt: time.Now().Add(time.Hour), Scopes: "identify",
		}); err != nil {
			t.Fatalf("upsert %s: %v", uid, err)
		}
	}
	upsert(activeID)
	upsert(revokedID)
	if err := s.MarkDiscordRoleConnectionRevoked(ctx, revokedID, "test"); err != nil {
		t.Fatalf("MarkRevoked: %v", err)
	}
	rows, err := s.ListActiveDiscordRoleConnections(ctx)
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	var sawActive, sawRevoked bool
	for _, r := range rows {
		if r.UserID == activeID {
			sawActive = true
		}
		if r.UserID == revokedID {
			sawRevoked = true
		}
	}
	if !sawActive {
		t.Error("expected active row in ListActive result")
	}
	if sawRevoked {
		t.Error("revoked row should be excluded")
	}
}
```

- [ ] **Step 2: Verify tests fail**

Run: `cd apps/backend && TEST_POSTGRES_URL=... go test ./internal/store/ -run "TestDiscordRoleConnection_(Delete|MarkRevoked|RecordPushed|ListActive)" -v`
Expected: FAIL (functions undefined).

- [ ] **Step 3: Add the four methods to `role_connections.go`**

Append:

```go
// DeleteDiscordRoleConnection removes the row for user_id. No-op if absent.
func (p *PostgresStore) DeleteDiscordRoleConnection(ctx context.Context, userID string) error {
	_, err := p.pool.Exec(ctx, `DELETE FROM discord_role_connections WHERE user_id = $1`, userID)
	return err
}

// MarkDiscordRoleConnectionRevoked flips revoked_at to now() and stores the
// last error (typically "invalid_grant" from a failed refresh). Used when the
// refresh token is permanently invalid; the row is kept so the dashboard can
// show "Reconnect" and the user can re-OAuth without losing history.
func (p *PostgresStore) MarkDiscordRoleConnectionRevoked(ctx context.Context, userID, errMsg string) error {
	_, err := p.pool.Exec(ctx, `
		UPDATE discord_role_connections
		SET revoked_at = now(), last_push_error = $2
		WHERE user_id = $1
	`, userID, errMsg)
	return err
}

// RecordDiscordRoleConnectionPushed updates last_pushed_at / last_pushed_hash
// after a successful push, and clears last_push_error.
func (p *PostgresStore) RecordDiscordRoleConnectionPushed(ctx context.Context, userID, payloadHash string) error {
	_, err := p.pool.Exec(ctx, `
		UPDATE discord_role_connections
		SET last_pushed_at = now(),
		    last_pushed_hash = $2,
		    last_push_error = NULL
		WHERE user_id = $1
	`, userID, payloadHash)
	return err
}

// ListActiveDiscordRoleConnections returns all rows with revoked_at IS NULL.
// Used by the periodic reconciler. Not paginated — the expected cardinality
// is hundreds, not millions; revisit if that changes.
func (p *PostgresStore) ListActiveDiscordRoleConnections(ctx context.Context) ([]DiscordRoleConnection, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT user_id, discord_id,
			access_token_ct, access_token_nonce,
			refresh_token_ct, refresh_token_nonce,
			access_expires_at, scopes, linked_at,
			last_pushed_at, last_pushed_hash, last_push_error, revoked_at
		FROM discord_role_connections
		WHERE revoked_at IS NULL
		ORDER BY linked_at ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DiscordRoleConnection{}
	for rows.Next() {
		var r DiscordRoleConnection
		if err := rows.Scan(
			&r.UserID, &r.DiscordID,
			&r.AccessTokenCT, &r.AccessTokenNonce,
			&r.RefreshTokenCT, &r.RefreshTokenNonce,
			&r.AccessExpiresAt, &r.Scopes, &r.LinkedAt,
			&r.LastPushedAt, &r.LastPushedHash, &r.LastPushError, &r.RevokedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// RecordDiscordRoleConnectionPushError stores a soft failure (Discord 5xx,
// timeout, etc.) without revoking the row. The reconciler will retry later.
func (p *PostgresStore) RecordDiscordRoleConnectionPushError(ctx context.Context, userID, errMsg string) error {
	_, err := p.pool.Exec(ctx, `
		UPDATE discord_role_connections
		SET last_push_error = $2
		WHERE user_id = $1
	`, userID, errMsg)
	return err
}
```

- [ ] **Step 4: Run tests, verify pass**

Run: `cd apps/backend && TEST_POSTGRES_URL=... go test ./internal/store/ -run "TestDiscordRoleConnection_" -v`
Expected: PASS for all four.

- [ ] **Step 5: Commit and push**

```bash
git add apps/backend/internal/store/role_connections.go apps/backend/internal/store/role_connections_test.go
git commit -m "feat(backend): add delete/revoke/push-record/list-active for role connections"
git push
```

### Task 4: Add a `UserHasProRoleGrantedAt` lookup

We need to read `admin_user_roles.granted_at` for the `pro` role to build the metadata. Add a focused helper rather than reusing the broader resolver — the metadata builder only needs this one fact and is on the hot path of every push.

**Files:**
- Modify: `apps/backend/internal/store/role_connections.go`
- Modify: `apps/backend/internal/store/role_connections_test.go`

- [ ] **Step 1: Add test**

Append to `role_connections_test.go`:

```go
func TestGetUserProGrantedAt(t *testing.T) {
	ctx := context.Background()
	s := newTestPostgres(t)
	suffix := time.Now().UnixNano() % 1_000_000_000
	proID := fmt.Sprintf("usr_pro_gat_%d", suffix)
	nonProID := fmt.Sprintf("usr_nopro_gat_%d", suffix)
	insertTestAppUser(t, s, proID, fmt.Sprintf("9000000%010d", suffix))
	insertTestAppUser(t, s, nonProID, fmt.Sprintf("9100000%010d", suffix))
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO admin_user_roles (user_id, role_id)
		SELECT $1, id FROM admin_roles WHERE key = 'pro'
	`, proID); err != nil {
		t.Fatalf("grant pro: %v", err)
	}
	gotPro, err := s.GetUserProGrantedAt(ctx, proID)
	if err != nil {
		t.Fatalf("GetUserProGrantedAt(pro): %v", err)
	}
	if gotPro == nil {
		t.Fatal("expected non-nil for pro user")
	}
	gotNon, err := s.GetUserProGrantedAt(ctx, nonProID)
	if err != nil {
		t.Fatalf("GetUserProGrantedAt(non-pro): %v", err)
	}
	if gotNon != nil {
		t.Fatalf("expected nil for non-pro user, got %v", gotNon)
	}
}
```

- [ ] **Step 2: Verify fail**

Run: `cd apps/backend && TEST_POSTGRES_URL=... go test ./internal/store/ -run TestGetUserProGrantedAt -v`
Expected: FAIL (undefined).

- [ ] **Step 3: Add implementation**

Append to `role_connections.go`:

```go
// GetUserProGrantedAt returns the granted_at timestamp for the PRO admin role
// on a given user, or (nil, nil) if the user does not hold the role. The role
// key is hardcoded to "pro" because that is the only role this lookup serves.
func (p *PostgresStore) GetUserProGrantedAt(ctx context.Context, userID string) (*time.Time, error) {
	var t time.Time
	err := p.pool.QueryRow(ctx, `
		SELECT ur.granted_at
		FROM admin_user_roles ur
		JOIN admin_roles ar ON ar.id = ur.role_id
		WHERE ur.user_id = $1 AND ar.key = 'pro'
	`, userID).Scan(&t)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}
```

- [ ] **Step 4: Run, verify pass**

Run: `cd apps/backend && TEST_POSTGRES_URL=... go test ./internal/store/ -run TestGetUserProGrantedAt -v`
Expected: PASS.

- [ ] **Step 5: Commit and push**

```bash
git add apps/backend/internal/store/role_connections.go apps/backend/internal/store/role_connections_test.go
git commit -m "feat(backend): add GetUserProGrantedAt lookup for metadata builder"
git push
```

---

## Phase 3 — Extend the Discord client with the calls Linked Roles needs

The existing `internal/oauth/discord/client.go` covers `ExchangeCode`, `FetchUserGuilds`, `FetchProfile`. Three more calls are needed:
- `RefreshToken(ctx, refreshToken) (newAccess, newRefresh, expiresIn, error)` — exchanges a refresh token for a new access token.
- `PutRoleConnection(ctx, accessToken, applicationID, body) error` — pushes per-user metadata.
- `RegisterApplicationMetadata(ctx, applicationID, botToken, records) error` — one-time schema PUT using `Authorization: Bot {token}` (NOT the OAuth client secret).

### Task 5: Add `RefreshToken` with httptest coverage

**Files:**
- Modify: `apps/backend/internal/oauth/discord/client.go`
- Modify: `apps/backend/internal/oauth/discord/client_test.go`

- [ ] **Step 1: Inspect existing test patterns**

Run: `rg -n "TestExchangeCode|httptest" apps/backend/internal/oauth/discord/client_test.go`
Read the file to see how it builds an httptest server with a JSON response. Mirror that pattern.

- [ ] **Step 2: Add failing test**

Append to `client_test.go`:

```go
func TestRefreshToken_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/api/oauth2/token" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm: %v", err)
		}
		if got := r.Form.Get("grant_type"); got != "refresh_token" {
			t.Errorf("grant_type = %s", got)
		}
		if got := r.Form.Get("refresh_token"); got != "rt-old" {
			t.Errorf("refresh_token = %s", got)
		}
		_, _ = w.Write([]byte(`{
			"access_token": "at-new", "refresh_token": "rt-new",
			"expires_in": 604800, "token_type": "Bearer", "scope": "identify role_connections.write"
		}`))
	}))
	defer srv.Close()

	c := NewClient("cid", "csecret", WithBaseURL(srv.URL))
	access, refresh, expiresIn, err := c.RefreshToken(context.Background(), "rt-old")
	if err != nil {
		t.Fatalf("RefreshToken: %v", err)
	}
	if access != "at-new" || refresh != "rt-new" || expiresIn != 604800 {
		t.Fatalf("got %q %q %d", access, refresh, expiresIn)
	}
}

func TestRefreshToken_InvalidGrant(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer srv.Close()
	c := NewClient("cid", "csecret", WithBaseURL(srv.URL))
	_, _, _, err := c.RefreshToken(context.Background(), "rt-old")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrRefreshRevoked) {
		t.Fatalf("err = %v, want ErrRefreshRevoked", err)
	}
}
```

(Add `"errors"` to the imports if not present.)

- [ ] **Step 3: Run, verify fail**

Run: `cd apps/backend && go test ./internal/oauth/discord/ -run TestRefreshToken -v`
Expected: FAIL (`RefreshToken` and `ErrRefreshRevoked` undefined).

- [ ] **Step 4: Implement**

Append to `client.go`:

```go
// ErrRefreshRevoked indicates that Discord rejected the refresh token as
// no longer valid. Callers should mark the row revoked and prompt re-link.
var ErrRefreshRevoked = errors.New("discord refresh token revoked")

// RefreshToken exchanges a long-lived refresh_token for a new access_token
// (and possibly a rotated refresh_token). Returns ErrRefreshRevoked when
// Discord responds 400 with error="invalid_grant".
func (c *Client) RefreshToken(ctx context.Context, refreshToken string) (access, refresh string, expiresIn int, err error) {
	form := url.Values{
		"client_id":     {c.clientID},
		"client_secret": {c.clientSecret},
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/api/oauth2/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", "", 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cache-Control", "no-store")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", "", 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusBadRequest && strings.Contains(string(body), "invalid_grant") {
		return "", "", 0, ErrRefreshRevoked
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", 0, fmt.Errorf("discord refresh: %d %s", resp.StatusCode, string(body))
	}
	var out struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", "", 0, err
	}
	if out.AccessToken == "" || out.RefreshToken == "" {
		return "", "", 0, errors.New("discord refresh: empty access_token or refresh_token")
	}
	return out.AccessToken, out.RefreshToken, out.ExpiresIn, nil
}
```

- [ ] **Step 5: Run, verify pass**

Run: `cd apps/backend && go test ./internal/oauth/discord/ -run TestRefreshToken -v`
Expected: PASS for both.

- [ ] **Step 6: Commit and push**

```bash
git add apps/backend/internal/oauth/discord/client.go apps/backend/internal/oauth/discord/client_test.go
git commit -m "feat(backend): add Discord OAuth RefreshToken with invalid_grant detection"
git push
```

### Task 6: Add `PutRoleConnection`

**Files:**
- Modify: `apps/backend/internal/oauth/discord/client.go`
- Modify: `apps/backend/internal/oauth/discord/client_test.go`

- [ ] **Step 1: Add test**

Append:

```go
func TestPutRoleConnection_Success(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("method = %s", r.Method)
		}
		if r.URL.Path != "/api/users/@me/applications/APP-ID/role-connection" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer at-1" {
			t.Errorf("auth = %s", got)
		}
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"platform_name":"RustLens"}`))
	}))
	defer srv.Close()
	c := NewClient("cid", "csecret", WithBaseURL(srv.URL))
	body := RoleConnectionBody{
		PlatformName:     "RustLens",
		PlatformUsername: "sem",
		Metadata: map[string]string{
			"is_pro":    "1",
			"pro_since": "2026-05-23T12:00:00+00:00",
		},
	}
	if err := c.PutRoleConnection(context.Background(), "at-1", "APP-ID", body); err != nil {
		t.Fatalf("PutRoleConnection: %v", err)
	}
	if !strings.Contains(string(gotBody), `"is_pro":"1"`) {
		t.Errorf("body missing is_pro: %s", string(gotBody))
	}
}

func TestPutRoleConnection_Unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"401: Unauthorized","code":0}`))
	}))
	defer srv.Close()
	c := NewClient("cid", "csecret", WithBaseURL(srv.URL))
	err := c.PutRoleConnection(context.Background(), "expired", "APP-ID", RoleConnectionBody{PlatformName: "x"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrAccessTokenInvalid) {
		t.Fatalf("err = %v, want ErrAccessTokenInvalid", err)
	}
}
```

- [ ] **Step 2: Verify fail**

Run: `cd apps/backend && go test ./internal/oauth/discord/ -run TestPutRoleConnection -v`
Expected: FAIL.

- [ ] **Step 3: Implement**

Append to `client.go`:

```go
// ErrAccessTokenInvalid indicates the access token is expired or revoked.
// Callers should attempt a refresh, then retry once.
var ErrAccessTokenInvalid = errors.New("discord access token invalid")

// RoleConnectionBody is the JSON payload for PUT /users/@me/applications/{id}/role-connection.
type RoleConnectionBody struct {
	PlatformName     string            `json:"platform_name"`
	PlatformUsername string            `json:"platform_username,omitempty"`
	Metadata         map[string]string `json:"metadata"`
}

// PutRoleConnection pushes the caller's role-connection metadata for our app.
// Returns ErrAccessTokenInvalid on 401 so the caller can refresh-and-retry.
func (c *Client) PutRoleConnection(ctx context.Context, accessToken, applicationID string, body RoleConnectionBody) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut,
		c.baseURL+"/api/users/@me/applications/"+applicationID+"/role-connection",
		strings.NewReader(string(payload)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Cache-Control", "no-store")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return ErrAccessTokenInvalid
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("discord put role-connection: %d %s", resp.StatusCode, string(respBody))
	}
	return nil
}
```

- [ ] **Step 4: Run, verify pass**

Run: `cd apps/backend && go test ./internal/oauth/discord/ -run TestPutRoleConnection -v`
Expected: PASS for both.

- [ ] **Step 5: Commit and push**

```bash
git add apps/backend/internal/oauth/discord/client.go apps/backend/internal/oauth/discord/client_test.go
git commit -m "feat(backend): add PutRoleConnection with 401 detection"
git push
```

### Task 7: Add `RegisterApplicationMetadata`

The metadata schema PUT authenticates as the bot (`Authorization: Bot {token}`), NOT with the OAuth client secret. We will pass the bot token from a new config var.

**Files:**
- Modify: `apps/backend/internal/oauth/discord/client.go`
- Modify: `apps/backend/internal/oauth/discord/client_test.go`

- [ ] **Step 1: Add test**

Append:

```go
func TestRegisterApplicationMetadata_Success(t *testing.T) {
	var gotBody []byte
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("method = %s", r.Method)
		}
		if r.URL.Path != "/api/applications/APP-ID/role-connections/metadata" {
			t.Errorf("path = %s", r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()
	c := NewClient("cid", "csecret", WithBaseURL(srv.URL))
	records := []ApplicationMetadataRecord{
		{Type: 7, Key: "is_pro", Name: "PRO subscriber", Description: "Active PRO status"},
		{Type: 6, Key: "pro_since", Name: "PRO since", Description: "Date PRO was granted"},
	}
	if err := c.RegisterApplicationMetadata(context.Background(), "APP-ID", "bot-token-123", records); err != nil {
		t.Fatalf("RegisterApplicationMetadata: %v", err)
	}
	if gotAuth != "Bot bot-token-123" {
		t.Errorf("auth = %q, want \"Bot bot-token-123\"", gotAuth)
	}
	if !strings.Contains(string(gotBody), `"key":"is_pro"`) || !strings.Contains(string(gotBody), `"key":"pro_since"`) {
		t.Errorf("body missing records: %s", string(gotBody))
	}
}
```

- [ ] **Step 2: Run, verify fail**

Run: `cd apps/backend && go test ./internal/oauth/discord/ -run TestRegisterApplicationMetadata -v`
Expected: FAIL.

- [ ] **Step 3: Implement**

Append to `client.go`:

```go
// ApplicationMetadataRecord is one entry in the role-connections metadata schema.
// Type values per https://discord.com/developers/docs/resources/application-role-connection-metadata
// (6 = datetime_greater_than_or_equal, 7 = boolean_equal).
type ApplicationMetadataRecord struct {
	Type        int    `json:"type"`
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// RegisterApplicationMetadata PUTs the role-connections metadata schema for
// the application. Idempotent — PUT replaces the entire schema. Authenticated
// with the bot token, NOT the OAuth client secret.
func (c *Client) RegisterApplicationMetadata(ctx context.Context, applicationID, botToken string, records []ApplicationMetadataRecord) error {
	payload, err := json.Marshal(records)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut,
		c.baseURL+"/api/applications/"+applicationID+"/role-connections/metadata",
		strings.NewReader(string(payload)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bot "+botToken)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("discord register app metadata: %d %s", resp.StatusCode, string(body))
	}
	return nil
}
```

- [ ] **Step 4: Run, verify pass**

Run: `cd apps/backend && go test ./internal/oauth/discord/ -run TestRegisterApplicationMetadata -v`
Expected: PASS.

- [ ] **Step 5: Commit and push**

```bash
git add apps/backend/internal/oauth/discord/client.go apps/backend/internal/oauth/discord/client_test.go
git commit -m "feat(backend): add RegisterApplicationMetadata for linked roles schema"
git push
```

### Task 8: Add `role_connections.write` scope to the existing initiate helper

The role-connection OAuth flow uses a different scope set. Rather than hardcoding it later, expose a constants block.

**Files:**
- Modify: `apps/backend/internal/oauth/discord/oauth.go`

- [ ] **Step 1: Read the file to find where existing constants live**

Run: `rg -n "RedirectURI|CookieState|NewState|PkceChallenge" apps/backend/internal/oauth/discord/oauth.go`

- [ ] **Step 2: Add a role-connection redirect helper**

At the end of `oauth.go`, add:

```go
// RoleConnectionRedirectURI is the OAuth redirect_uri for the linked-roles
// flow. Distinct from the sign-in flow so Discord can differentiate the two
// callbacks; both must be registered in the Discord Developer Portal.
func RoleConnectionRedirectURI(appOrigin string) string {
	return strings.TrimRight(appOrigin, "/") + "/api/auth/discord/role-connection/callback"
}

// Scopes used for the role-connection OAuth flow. identify lets us confirm
// the linking Discord account matches the signed-in app user; role_connections.write
// lets us PUT /users/@me/applications/{id}/role-connection.
const RoleConnectionScopes = "identify role_connections.write"

// Cookie names for the role-connection flow. Kept separate from the sign-in
// flow cookies so a user mid-link does not collide with a concurrent re-login.
const (
	CookieRCState    = "rl_rc_state"
	CookieRCVerifier = "rl_rc_verifier"
	CookieRCReturn   = "rl_rc_return"
)
```

(Verify `strings` is already imported — it should be from `RedirectURI`.)

- [ ] **Step 3: Build**

Run: `cd apps/backend && go build ./...`
Expected: exit 0.

- [ ] **Step 4: Commit and push**

```bash
git add apps/backend/internal/oauth/discord/oauth.go
git commit -m "feat(backend): add role-connection redirect URI, scopes, cookie names"
git push
```

---

## Phase 4 — `roleconnection` package: Syncer core

The Syncer is the orchestrator: it takes a userID, looks up the row, refreshes the token if expired, builds the metadata, computes the hash, skips no-ops, PUTs to Discord, and records the outcome.

### Task 9: Package skeleton + types + metadata builder

**Files:**
- Create: `apps/backend/internal/roleconnection/syncer.go`
- Create: `apps/backend/internal/roleconnection/syncer_test.go`

- [ ] **Step 1: Add failing test for metadata builder**

Create `apps/backend/internal/roleconnection/syncer_test.go`:

```go
package roleconnection

import (
	"testing"
	"time"
)

func TestBuildMetadata_NotPro(t *testing.T) {
	body := BuildMetadata("displayname", nil)
	if body.Metadata["is_pro"] != "0" {
		t.Errorf("is_pro = %q, want 0", body.Metadata["is_pro"])
	}
	if _, hasSince := body.Metadata["pro_since"]; hasSince {
		t.Error("pro_since should be absent when not pro")
	}
	if body.PlatformName != "RustLens" || body.PlatformUsername != "displayname" {
		t.Errorf("platform fields = %+v", body)
	}
}

func TestBuildMetadata_Pro(t *testing.T) {
	at := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)
	body := BuildMetadata("sem", &at)
	if body.Metadata["is_pro"] != "1" {
		t.Errorf("is_pro = %q, want 1", body.Metadata["is_pro"])
	}
	if body.Metadata["pro_since"] != "2026-05-23T12:00:00+00:00" {
		t.Errorf("pro_since = %q", body.Metadata["pro_since"])
	}
}

func TestHashMetadata_Stable(t *testing.T) {
	at := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)
	a := HashMetadata(BuildMetadata("x", &at))
	b := HashMetadata(BuildMetadata("x", &at))
	if a != b {
		t.Errorf("hash unstable: %s != %s", a, b)
	}
	c := HashMetadata(BuildMetadata("x", nil))
	if a == c {
		t.Error("hash collision pro vs not-pro")
	}
}
```

- [ ] **Step 2: Verify fail**

Run: `cd apps/backend && go test ./internal/roleconnection/ -v`
Expected: FAIL (package does not exist).

- [ ] **Step 3: Implement**

Create `apps/backend/internal/roleconnection/syncer.go`:

```go
// Package roleconnection orchestrates Discord's linked-roles application
// metadata for the dashboard PRO role: storing per-user OAuth tokens,
// refreshing them, and pushing metadata so Discord re-evaluates the
// linked role membership. See docs/superpowers/plans/2026-05-23-discord-linked-roles-pro-sync.md.
package roleconnection

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"rustlens/backend/internal/oauth/discord"
)

// PlatformName is the user-visible label shown in Discord's "Connections" UI
// for the linked account. Capped at 50 chars by Discord; keep it stable.
const PlatformName = "RustLens"

// BuildMetadata translates a user's PRO state into Discord's role-connection
// metadata. proGrantedAt nil => not PRO. Times are formatted as RFC3339 with
// numeric timezone offset (Discord rejects 'Z' suffix in some clients).
func BuildMetadata(platformUsername string, proGrantedAt *time.Time) discord.RoleConnectionBody {
	md := map[string]string{}
	if proGrantedAt != nil {
		md["is_pro"] = "1"
		md["pro_since"] = proGrantedAt.UTC().Format("2006-01-02T15:04:05-07:00")
	} else {
		md["is_pro"] = "0"
	}
	return discord.RoleConnectionBody{
		PlatformName:     PlatformName,
		PlatformUsername: platformUsername,
		Metadata:         md,
	}
}

// HashMetadata returns a stable sha256 hex digest of the JSON-marshaled body.
// Used to skip no-op pushes when the user's metadata is unchanged.
func HashMetadata(body discord.RoleConnectionBody) string {
	raw, _ := json.Marshal(body)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
```

- [ ] **Step 4: Run, verify pass**

Run: `cd apps/backend && go test ./internal/roleconnection/ -v`
Expected: PASS.

- [ ] **Step 5: Commit and push**

```bash
git add apps/backend/internal/roleconnection/
git commit -m "feat(backend): roleconnection metadata builder and hasher"
git push
```

### Task 10: Token encryption helper inside the package

Wrap proxycrypto with role-connection-specific encrypt/decrypt helpers so callers don't have to remember which ciphertext belongs to which nonce.

**Files:**
- Create: `apps/backend/internal/roleconnection/crypto.go`
- Create: `apps/backend/internal/roleconnection/crypto_test.go`

- [ ] **Step 1: Add failing test**

```go
package roleconnection

import (
	"crypto/rand"
	"encoding/base64"
	"testing"

	"rustlens/backend/internal/proxycrypto"
)

func newTestCrypto(t *testing.T) *proxycrypto.Crypto {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	c, err := proxycrypto.New(base64.StdEncoding.EncodeToString(key))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestTokenCipher_RoundTrip(t *testing.T) {
	c := newTestCrypto(t)
	tc := TokenCipher{Crypto: c}
	ct, nonce, err := tc.Encrypt("secret-access-token")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	got, err := tc.Decrypt(ct, nonce)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if got != "secret-access-token" {
		t.Errorf("got %q", got)
	}
}

func TestTokenCipher_NilCryptoReturnsError(t *testing.T) {
	tc := TokenCipher{Crypto: nil}
	_, _, err := tc.Encrypt("x")
	if err == nil {
		t.Error("expected error from nil crypto")
	}
}
```

- [ ] **Step 2: Run, verify fail**

Run: `cd apps/backend && go test ./internal/roleconnection/ -run TestTokenCipher -v`
Expected: FAIL.

- [ ] **Step 3: Implement**

Create `apps/backend/internal/roleconnection/crypto.go`:

```go
package roleconnection

import "rustlens/backend/internal/proxycrypto"

// TokenCipher is a thin wrapper over proxycrypto.Crypto for OAuth tokens.
// It exists so call sites don't accidentally swap ciphertext and nonce
// between access and refresh tokens.
type TokenCipher struct {
	Crypto *proxycrypto.Crypto
}

func (t TokenCipher) Encrypt(plaintext string) (ciphertext, nonce []byte, err error) {
	return t.Crypto.Encrypt(plaintext)
}

func (t TokenCipher) Decrypt(ciphertext, nonce []byte) (string, error) {
	return t.Crypto.Decrypt(ciphertext, nonce)
}
```

- [ ] **Step 4: Verify pass**

Run: `cd apps/backend && go test ./internal/roleconnection/ -v`
Expected: PASS for all.

- [ ] **Step 5: Commit and push**

```bash
git add apps/backend/internal/roleconnection/crypto.go apps/backend/internal/roleconnection/crypto_test.go
git commit -m "feat(backend): roleconnection token cipher wrapper"
git push
```

### Task 11: Syncer.Push — happy path

**Files:**
- Modify: `apps/backend/internal/roleconnection/syncer.go`
- Create: `apps/backend/internal/roleconnection/push_test.go`

- [ ] **Step 1: Add failing test**

Create `apps/backend/internal/roleconnection/push_test.go`:

```go
package roleconnection

import (
	"context"
	"errors"
	"testing"
	"time"

	"rustlens/backend/internal/oauth/discord"
	"rustlens/backend/internal/store"
)

type fakeStore struct {
	row             *store.DiscordRoleConnection
	proGrantedAt    *time.Time
	displayName     string
	upsertCalled    int
	recordedHash    string
	recordedErrMsg  string
	revokeReason    string
	deleteCalled    int
	listActive      []store.DiscordRoleConnection
}

func (f *fakeStore) GetDiscordRoleConnection(_ context.Context, _ string) (*store.DiscordRoleConnection, error) {
	return f.row, nil
}
func (f *fakeStore) UpsertDiscordRoleConnection(_ context.Context, r store.DiscordRoleConnection) error {
	f.upsertCalled++
	f.row = &r
	return nil
}
func (f *fakeStore) DeleteDiscordRoleConnection(_ context.Context, _ string) error {
	f.deleteCalled++
	f.row = nil
	return nil
}
func (f *fakeStore) MarkDiscordRoleConnectionRevoked(_ context.Context, _, reason string) error {
	f.revokeReason = reason
	if f.row != nil {
		now := time.Now()
		f.row.RevokedAt = &now
	}
	return nil
}
func (f *fakeStore) RecordDiscordRoleConnectionPushed(_ context.Context, _, hash string) error {
	f.recordedHash = hash
	return nil
}
func (f *fakeStore) RecordDiscordRoleConnectionPushError(_ context.Context, _, msg string) error {
	f.recordedErrMsg = msg
	return nil
}
func (f *fakeStore) ListActiveDiscordRoleConnections(_ context.Context) ([]store.DiscordRoleConnection, error) {
	return f.listActive, nil
}
func (f *fakeStore) GetUserProGrantedAt(_ context.Context, _ string) (*time.Time, error) {
	return f.proGrantedAt, nil
}
func (f *fakeStore) GetAppUserDisplayName(_ context.Context, _ string) (string, error) {
	return f.displayName, nil
}

type fakeDiscord struct {
	putCalls           int
	putBody            discord.RoleConnectionBody
	putErr             error
	refreshCalls       int
	refreshAccess      string
	refreshRefresh     string
	refreshExpiresIn   int
	refreshErr         error
	receivedAccessTok  string
}

func (f *fakeDiscord) PutRoleConnection(_ context.Context, accessToken, _ string, body discord.RoleConnectionBody) error {
	f.putCalls++
	f.receivedAccessTok = accessToken
	f.putBody = body
	return f.putErr
}
func (f *fakeDiscord) RefreshToken(_ context.Context, _ string) (string, string, int, error) {
	f.refreshCalls++
	return f.refreshAccess, f.refreshRefresh, f.refreshExpiresIn, f.refreshErr
}

func TestSyncerPush_NoRow_NoOp(t *testing.T) {
	s := &fakeStore{}
	syncer := NewSyncer(SyncerConfig{
		Store: s, Discord: &fakeDiscord{}, ApplicationID: "APP",
		Cipher: TokenCipher{Crypto: newTestCrypto(t)},
	})
	if err := syncer.Push(context.Background(), "uid-1"); err != nil {
		t.Fatalf("Push: %v", err)
	}
}

func TestSyncerPush_HappyPath_TokenStillValid(t *testing.T) {
	ctx := context.Background()
	cipher := TokenCipher{Crypto: newTestCrypto(t)}
	at, atNonce, err := cipher.Encrypt("access-1")
	if err != nil {
		t.Fatalf("encrypt access: %v", err)
	}
	rt, rtNonce, err := cipher.Encrypt("refresh-1")
	if err != nil {
		t.Fatalf("encrypt refresh: %v", err)
	}
	now := time.Now()
	s := &fakeStore{
		row: &store.DiscordRoleConnection{
			UserID:            "uid-1",
			DiscordID:         "d1",
			AccessTokenCT:     at,
			AccessTokenNonce:  atNonce,
			RefreshTokenCT:    rt,
			RefreshTokenNonce: rtNonce,
			AccessExpiresAt:   now.Add(time.Hour),
			Scopes:            "identify role_connections.write",
		},
		proGrantedAt: &now,
		displayName:  "sem",
	}
	d := &fakeDiscord{}
	syncer := NewSyncer(SyncerConfig{
		Store: s, Discord: d, ApplicationID: "APP", Cipher: cipher,
	})
	if err := syncer.Push(ctx, "uid-1"); err != nil {
		t.Fatalf("Push: %v", err)
	}
	if d.refreshCalls != 0 {
		t.Errorf("refresh called %d times, want 0", d.refreshCalls)
	}
	if d.putCalls != 1 {
		t.Errorf("put called %d times, want 1", d.putCalls)
	}
	if d.receivedAccessTok != "access-1" {
		t.Errorf("access token = %q", d.receivedAccessTok)
	}
	if d.putBody.Metadata["is_pro"] != "1" {
		t.Errorf("is_pro = %q", d.putBody.Metadata["is_pro"])
	}
	if s.recordedHash == "" {
		t.Error("expected hash to be recorded")
	}
}

func TestSyncerPush_SkipsWhenHashUnchanged(t *testing.T) {
	ctx := context.Background()
	cipher := TokenCipher{Crypto: newTestCrypto(t)}
	at, atNonce, _ := cipher.Encrypt("access-1")
	rt, rtNonce, _ := cipher.Encrypt("refresh-1")
	now := time.Now()
	tenMinAgo := now.Add(-10 * time.Minute)
	pre := BuildMetadata("sem", &now)
	preHash := HashMetadata(pre)
	row := store.DiscordRoleConnection{
		UserID: "uid-1", DiscordID: "d1",
		AccessTokenCT: at, AccessTokenNonce: atNonce,
		RefreshTokenCT: rt, RefreshTokenNonce: rtNonce,
		AccessExpiresAt: now.Add(time.Hour),
		Scopes:          "identify role_connections.write",
		LastPushedAt:    &tenMinAgo,
		LastPushedHash:  &preHash,
	}
	s := &fakeStore{row: &row, proGrantedAt: &now, displayName: "sem"}
	d := &fakeDiscord{}
	syncer := NewSyncer(SyncerConfig{
		Store: s, Discord: d, ApplicationID: "APP", Cipher: cipher,
	})
	if err := syncer.Push(ctx, "uid-1"); err != nil {
		t.Fatalf("Push: %v", err)
	}
	if d.putCalls != 0 {
		t.Errorf("expected push skip, got %d puts", d.putCalls)
	}
}

func TestSyncerPush_RefreshesExpiredToken(t *testing.T) {
	ctx := context.Background()
	cipher := TokenCipher{Crypto: newTestCrypto(t)}
	at, atNonce, _ := cipher.Encrypt("access-old")
	rt, rtNonce, _ := cipher.Encrypt("refresh-old")
	now := time.Now()
	s := &fakeStore{
		row: &store.DiscordRoleConnection{
			UserID: "uid-1", DiscordID: "d1",
			AccessTokenCT: at, AccessTokenNonce: atNonce,
			RefreshTokenCT: rt, RefreshTokenNonce: rtNonce,
			AccessExpiresAt: now.Add(-time.Minute), // expired
			Scopes:          "identify role_connections.write",
		},
		proGrantedAt: &now, displayName: "sem",
	}
	d := &fakeDiscord{
		refreshAccess:    "access-new",
		refreshRefresh:   "refresh-new",
		refreshExpiresIn: 604800,
	}
	syncer := NewSyncer(SyncerConfig{
		Store: s, Discord: d, ApplicationID: "APP", Cipher: cipher,
	})
	if err := syncer.Push(ctx, "uid-1"); err != nil {
		t.Fatalf("Push: %v", err)
	}
	if d.refreshCalls != 1 {
		t.Errorf("refresh calls = %d", d.refreshCalls)
	}
	if d.receivedAccessTok != "access-new" {
		t.Errorf("put used %q, want access-new", d.receivedAccessTok)
	}
	if s.upsertCalled != 1 {
		t.Errorf("expected token row upsert after refresh, got %d", s.upsertCalled)
	}
}

func TestSyncerPush_RefreshRevoked_MarksRow(t *testing.T) {
	ctx := context.Background()
	cipher := TokenCipher{Crypto: newTestCrypto(t)}
	at, atNonce, _ := cipher.Encrypt("access")
	rt, rtNonce, _ := cipher.Encrypt("refresh")
	now := time.Now()
	s := &fakeStore{
		row: &store.DiscordRoleConnection{
			UserID: "uid-1", DiscordID: "d1",
			AccessTokenCT: at, AccessTokenNonce: atNonce,
			RefreshTokenCT: rt, RefreshTokenNonce: rtNonce,
			AccessExpiresAt: now.Add(-time.Minute),
			Scopes:          "identify role_connections.write",
		},
		proGrantedAt: &now, displayName: "sem",
	}
	d := &fakeDiscord{refreshErr: discord.ErrRefreshRevoked}
	syncer := NewSyncer(SyncerConfig{
		Store: s, Discord: d, ApplicationID: "APP", Cipher: cipher,
	})
	err := syncer.Push(ctx, "uid-1")
	if !errors.Is(err, discord.ErrRefreshRevoked) {
		t.Fatalf("err = %v, want ErrRefreshRevoked", err)
	}
	if s.revokeReason != "invalid_grant" {
		t.Errorf("revokeReason = %q", s.revokeReason)
	}
}
```

- [ ] **Step 2: Verify fail**

Run: `cd apps/backend && go test ./internal/roleconnection/ -run TestSyncerPush -v`
Expected: FAIL (`NewSyncer`, `SyncerConfig`, `Syncer.Push` undefined).

- [ ] **Step 3: Implement**

Append to `syncer.go`:

```go
import (
	"context"
	"errors"
	"time"

	"rustlens/backend/internal/store"
)

// SyncerStore is the subset of store calls the Syncer needs. Defined as an
// interface so the Syncer can be tested with a fake.
type SyncerStore interface {
	GetDiscordRoleConnection(ctx context.Context, userID string) (*store.DiscordRoleConnection, error)
	UpsertDiscordRoleConnection(ctx context.Context, in store.DiscordRoleConnection) error
	DeleteDiscordRoleConnection(ctx context.Context, userID string) error
	MarkDiscordRoleConnectionRevoked(ctx context.Context, userID, errMsg string) error
	RecordDiscordRoleConnectionPushed(ctx context.Context, userID, payloadHash string) error
	RecordDiscordRoleConnectionPushError(ctx context.Context, userID, errMsg string) error
	ListActiveDiscordRoleConnections(ctx context.Context) ([]store.DiscordRoleConnection, error)
	GetUserProGrantedAt(ctx context.Context, userID string) (*time.Time, error)
	GetAppUserDisplayName(ctx context.Context, userID string) (string, error)
}

// DiscordAPI is the subset of discord.Client calls the Syncer needs.
type DiscordAPI interface {
	PutRoleConnection(ctx context.Context, accessToken, applicationID string, body discord.RoleConnectionBody) error
	RefreshToken(ctx context.Context, refreshToken string) (newAccess, newRefresh string, expiresIn int, err error)
}

type SyncerConfig struct {
	Store         SyncerStore
	Discord       DiscordAPI
	ApplicationID string
	Cipher        TokenCipher
}

type Syncer struct {
	cfg SyncerConfig
}

func NewSyncer(cfg SyncerConfig) *Syncer { return &Syncer{cfg: cfg} }

// pushSkipDuration controls how often Push runs even when the hash is unchanged
// (the periodic reconciler always re-pushes after this many hours regardless
// of hash equality, so any silent Discord-side drift gets corrected).
const pushSkipDuration = 24 * time.Hour

// tokenSkew is the leeway against access-token expiry. We refresh slightly
// before actual expiry to avoid races with Discord clock skew.
const tokenSkew = 60 * time.Second

// Push synchronously pushes the current PRO metadata for userID to Discord.
// Returns nil (no-op) when there is no role-connection row for the user, or
// when the row is revoked. Returns discord.ErrRefreshRevoked when the row
// had to be marked revoked during this call (caller may use this to surface
// a user-facing reconnect prompt).
func (s *Syncer) Push(ctx context.Context, userID string) error {
	row, err := s.cfg.Store.GetDiscordRoleConnection(ctx, userID)
	if err != nil {
		return err
	}
	if row == nil || row.RevokedAt != nil {
		return nil
	}

	access := row.AccessTokenCT
	accessNonce := row.AccessTokenNonce
	expiresAt := row.AccessExpiresAt

	if time.Now().Add(tokenSkew).After(expiresAt) {
		// Decrypt refresh, exchange, re-encrypt both, upsert row.
		refreshPT, err := s.cfg.Cipher.Decrypt(row.RefreshTokenCT, row.RefreshTokenNonce)
		if err != nil {
			return err
		}
		newAccess, newRefresh, expiresIn, err := s.cfg.Discord.RefreshToken(ctx, refreshPT)
		if err != nil {
			if errors.Is(err, discord.ErrRefreshRevoked) {
				_ = s.cfg.Store.MarkDiscordRoleConnectionRevoked(ctx, userID, "invalid_grant")
			}
			return err
		}
		newAccessCT, newAccessNonce, err := s.cfg.Cipher.Encrypt(newAccess)
		if err != nil {
			return err
		}
		newRefreshCT, newRefreshNonce, err := s.cfg.Cipher.Encrypt(newRefresh)
		if err != nil {
			return err
		}
		newExpiresAt := time.Now().Add(time.Duration(expiresIn) * time.Second)
		updated := *row
		updated.AccessTokenCT = newAccessCT
		updated.AccessTokenNonce = newAccessNonce
		updated.RefreshTokenCT = newRefreshCT
		updated.RefreshTokenNonce = newRefreshNonce
		updated.AccessExpiresAt = newExpiresAt
		if err := s.cfg.Store.UpsertDiscordRoleConnection(ctx, updated); err != nil {
			return err
		}
		access = newAccessCT
		accessNonce = newAccessNonce
		expiresAt = newExpiresAt
		_ = expiresAt
	}

	accessPT, err := s.cfg.Cipher.Decrypt(access, accessNonce)
	if err != nil {
		return err
	}

	grantedAt, err := s.cfg.Store.GetUserProGrantedAt(ctx, userID)
	if err != nil {
		return err
	}
	displayName, err := s.cfg.Store.GetAppUserDisplayName(ctx, userID)
	if err != nil {
		return err
	}
	body := BuildMetadata(displayName, grantedAt)
	hash := HashMetadata(body)

	// Skip when nothing changed AND we pushed recently. The reconciler will
	// re-push past the pushSkipDuration window to catch silent drift.
	if row.LastPushedHash != nil && *row.LastPushedHash == hash &&
		row.LastPushedAt != nil && time.Since(*row.LastPushedAt) < pushSkipDuration {
		return nil
	}

	if err := s.cfg.Discord.PutRoleConnection(ctx, accessPT, s.cfg.ApplicationID, body); err != nil {
		_ = s.cfg.Store.RecordDiscordRoleConnectionPushError(ctx, userID, err.Error())
		return err
	}
	return s.cfg.Store.RecordDiscordRoleConnectionPushed(ctx, userID, hash)
}
```

Also add the `GetAppUserDisplayName` store method. In `apps/backend/internal/store/role_connections.go`, append:

```go
// GetAppUserDisplayName returns app_users.display_name, used as the platform
// username when pushing metadata. Falls back to discord_username if display_name
// is empty.
func (p *PostgresStore) GetAppUserDisplayName(ctx context.Context, userID string) (string, error) {
	var displayName, discordUsername *string
	err := p.pool.QueryRow(ctx, `
		SELECT display_name, discord_username FROM app_users WHERE id = $1
	`, userID).Scan(&displayName, &discordUsername)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if displayName != nil && *displayName != "" {
		return *displayName, nil
	}
	if discordUsername != nil {
		return *discordUsername, nil
	}
	return "", nil
}
```

Reorganize imports as needed.

- [ ] **Step 4: Run, verify pass**

Run: `cd apps/backend && go test ./internal/roleconnection/ -v && go test ./internal/store/ -run TestDiscordRoleConnection -v`
Expected: PASS.

- [ ] **Step 5: Commit and push**

```bash
git add apps/backend/internal/roleconnection/syncer.go apps/backend/internal/roleconnection/push_test.go apps/backend/internal/store/role_connections.go
git commit -m "feat(backend): roleconnection.Syncer.Push with refresh + no-op skip"
git push
```

### Task 12: PushAsync helper for fire-and-forget admin-action hooks

**Files:**
- Modify: `apps/backend/internal/roleconnection/syncer.go`

- [ ] **Step 1: Add the method**

Append to `syncer.go`:

```go
// PushAsync schedules a Push for userID in a fresh detached context. The admin
// action that grants/revokes PRO calls this so role-action latency does not
// depend on Discord round-trip time. Errors are logged, not returned.
func (s *Syncer) PushAsync(userID string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := s.Push(ctx, userID); err != nil {
			slog.Warn("roleconnection async push failed",
				slog.String("user_id", userID), slog.Any("error", err))
		}
	}()
}
```

Add `"log/slog"` to the imports.

- [ ] **Step 2: Build**

Run: `cd apps/backend && go build ./...`
Expected: exit 0.

- [ ] **Step 3: Commit and push**

```bash
git add apps/backend/internal/roleconnection/syncer.go
git commit -m "feat(backend): roleconnection PushAsync for admin-action hooks"
git push
```

---

## Phase 5 — Schema registration at startup

### Task 13: `RegisterSchema` function

**Files:**
- Create: `apps/backend/internal/roleconnection/register.go`
- Create: `apps/backend/internal/roleconnection/register_test.go`

- [ ] **Step 1: Add failing test**

Create `register_test.go`:

```go
package roleconnection

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"rustlens/backend/internal/oauth/discord"
)

func TestRegisterSchema_PutsExpectedRecords(t *testing.T) {
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/applications/APP/role-connections/metadata" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bot tok" {
			t.Errorf("auth = %q", got)
		}
		body, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()
	c := discord.NewClient("cid", "csec", discord.WithBaseURL(srv.URL))
	if err := RegisterSchema(context.Background(), c, "APP", "tok"); err != nil {
		t.Fatalf("RegisterSchema: %v", err)
	}
	if !strings.Contains(string(body), `"key":"is_pro"`) {
		t.Errorf("body missing is_pro: %s", string(body))
	}
	if !strings.Contains(string(body), `"key":"pro_since"`) {
		t.Errorf("body missing pro_since: %s", string(body))
	}
}
```

Add `"io"` import.

- [ ] **Step 2: Run, verify fail**

Run: `cd apps/backend && go test ./internal/roleconnection/ -run TestRegisterSchema -v`
Expected: FAIL (undefined).

- [ ] **Step 3: Implement**

Create `register.go`:

```go
package roleconnection

import (
	"context"

	"rustlens/backend/internal/oauth/discord"
)

// RegisterSchema PUTs our application metadata schema (is_pro boolean,
// pro_since datetime) to Discord. Idempotent: calling repeatedly with the
// same records is a no-op as far as Discord is concerned.
func RegisterSchema(ctx context.Context, client *discord.Client, applicationID, botToken string) error {
	records := []discord.ApplicationMetadataRecord{
		{Type: 7, Key: "is_pro", Name: "PRO subscriber", Description: "Active RustLens PRO status"},
		{Type: 6, Key: "pro_since", Name: "PRO since", Description: "Date PRO was granted"},
	}
	return client.RegisterApplicationMetadata(ctx, applicationID, botToken, records)
}
```

- [ ] **Step 4: Verify pass**

Run: `cd apps/backend && go test ./internal/roleconnection/ -v`
Expected: PASS.

- [ ] **Step 5: Commit and push**

```bash
git add apps/backend/internal/roleconnection/register.go apps/backend/internal/roleconnection/register_test.go
git commit -m "feat(backend): roleconnection.RegisterSchema for is_pro+pro_since"
git push
```

### Task 14: Add `DISCORD_BOT_TOKEN` config

The schema PUT needs the bot token. The bot already runs with `DISCORD_TOKEN`; we'll add the same value to the backend as `DISCORD_BOT_TOKEN`.

**Files:**
- Modify: `apps/backend/internal/config/config.go`
- Modify: `apps/backend/.env.example` (if present; otherwise mention it in AGENTS.md later)

- [ ] **Step 1: Read current config struct + Load**

Run: `rg -n "DiscordClientID|DiscordClientSecret" apps/backend/internal/config/config.go`
Locate the two existing fields.

- [ ] **Step 2: Add new field**

In the `Config` struct, add `DiscordBotToken string` next to the other Discord fields.

In `Load()`, add:
```go
		DiscordBotToken:      strings.TrimSpace(getEnv("DISCORD_BOT_TOKEN", "")),
```

- [ ] **Step 3: Update env example if present**

Run: `ls apps/backend/.env.example 2>/dev/null && cat apps/backend/.env.example`.
If the file exists, append (without surrounding comments):
```
DISCORD_BOT_TOKEN=
```

- [ ] **Step 4: Build**

Run: `cd apps/backend && go build ./...`
Expected: exit 0.

- [ ] **Step 5: Commit and push**

```bash
git add apps/backend/internal/config/config.go apps/backend/.env.example
git commit -m "feat(backend): add DISCORD_BOT_TOKEN config for linked-roles schema"
git push
```

### Task 15: Wire schema registration into main.go

**Files:**
- Modify: `apps/backend/cmd/server/main.go`

- [ ] **Step 1: Read main.go around the discord oauth wiring**

Run: `rg -n "EnsureSchema|discord\." apps/backend/cmd/server/main.go`

- [ ] **Step 2: Add the registration call after EnsureSchema succeeds**

After the `if err := postgresStore.EnsureSchema(ctx); err != nil` block, add:

```go
	// Best-effort Discord linked-roles schema registration. Idempotent: PUT
	// replaces the entire schema. We log-and-continue on failure so a Discord
	// outage at boot does not crash the backend; the per-user push pipeline
	// is unaffected and the next deploy will re-attempt the PUT.
	if cfg.DiscordClientID != "" && cfg.DiscordBotToken != "" {
		discordClient := discord.NewClient(cfg.DiscordClientID, cfg.DiscordClientSecret)
		regCtx, regCancel := context.WithTimeout(ctx, 10*time.Second)
		if err := roleconnection.RegisterSchema(regCtx, discordClient, cfg.DiscordClientID, cfg.DiscordBotToken); err != nil {
			log.Printf("[backend] linked-roles schema registration failed: %v", err)
		} else {
			log.Printf("[backend] linked-roles schema registered with discord")
		}
		regCancel()
	}
```

Add imports at the top (alongside existing `internal/oauth/discord`):
```go
	"rustlens/backend/internal/oauth/discord"
	"rustlens/backend/internal/roleconnection"
```

(Verify `time` and `context` are already imported — they are.)

- [ ] **Step 3: Build**

Run: `cd apps/backend && go build ./...`
Expected: exit 0.

- [ ] **Step 4: Smoke-test locally**

If you have a real bot token + client ID handy:
```bash
DISCORD_BOT_TOKEN=... DISCORD_CLIENT_ID=... DISCORD_CLIENT_SECRET=... cd apps/backend && go run ./cmd/server
```
Look for `linked-roles schema registered with discord` in the log on boot. Stop with Ctrl+C.

If no token is available, skip — the build verification above is sufficient.

- [ ] **Step 5: Commit and push**

```bash
git add apps/backend/cmd/server/main.go
git commit -m "feat(backend): register linked-roles schema with Discord at startup"
git push
```

---

## Phase 6 — OAuth flow for role-connection linking

### Task 16: OAuth handler — Initiate

**Files:**
- Create: `apps/backend/internal/roleconnection/handler.go`
- Create: `apps/backend/internal/roleconnection/handler_test.go`

- [ ] **Step 1: Write failing test for Initiate**

```go
package roleconnection

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"rustlens/backend/internal/appauth"
)

func TestHandler_Initiate_RequiresSession(t *testing.T) {
	h := NewHandler(HandlerConfig{
		ClientID: "cid", AppOrigin: "http://localhost",
	})
	req := httptest.NewRequest(http.MethodGet, "/api/auth/discord/role-connection", nil)
	rec := httptest.NewRecorder()
	h.Initiate(rec, req)
	if rec.Code != http.StatusFound {
		t.Errorf("status = %d, want 302", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "/login") {
		t.Errorf("expected redirect to /login, got %s", loc)
	}
}

func TestHandler_Initiate_WithSession_RedirectsToDiscord(t *testing.T) {
	h := NewHandler(HandlerConfig{
		ClientID: "cid", AppOrigin: "http://localhost",
	})
	req := httptest.NewRequest(http.MethodGet, "/api/auth/discord/role-connection", nil)
	ctx := appauth.WithSession(context.Background(), &appauth.SessionUser{UserID: "uid-1"})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	h.Initiate(rec, req)
	if rec.Code != http.StatusFound {
		t.Errorf("status = %d, want 302", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "https://discord.com/oauth2/authorize") {
		t.Errorf("expected discord authorize URL, got %s", loc)
	}
	if !strings.Contains(loc, "scope=identify+role_connections.write") &&
		!strings.Contains(loc, "scope=identify%20role_connections.write") {
		t.Errorf("expected scopes in URL, got %s", loc)
	}
	// State + verifier cookies must be set.
	cookies := rec.Result().Cookies()
	var sawState, sawVerifier bool
	for _, c := range cookies {
		if c.Name == "rl_rc_state" {
			sawState = true
		}
		if c.Name == "rl_rc_verifier" {
			sawVerifier = true
		}
	}
	if !sawState || !sawVerifier {
		t.Errorf("missing state/verifier cookies: %v", cookies)
	}
}
```

`appauth.WithSession` may not exist yet. Check first:

Run: `rg -n "func WithSession" apps/backend/internal/appauth/`

If absent, add it (next step shows how). If present, use the existing signature.

- [ ] **Step 2: Add `appauth.WithSession` if needed**

If `WithSession` does not exist in `internal/appauth/middleware.go`, append:

```go
// WithSession is a test/helper that attaches a SessionUser to ctx. Production
// code paths set this via RequireSession middleware.
func WithSession(ctx context.Context, s *SessionUser) context.Context {
	return context.WithValue(ctx, sessionContextKey, s)
}
```

(Check the existing name of the context key by reading middleware.go.)

- [ ] **Step 3: Run test, verify fail**

Run: `cd apps/backend && go test ./internal/roleconnection/ -run TestHandler_Initiate -v`
Expected: FAIL (`NewHandler`, `HandlerConfig` undefined).

- [ ] **Step 4: Implement Initiate**

Create `handler.go`:

```go
package roleconnection

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"rustlens/backend/internal/appauth"
	"rustlens/backend/internal/oauth/discord"
)

// HandlerConfig wires runtime dependencies for the OAuth + disconnect handlers.
type HandlerConfig struct {
	ClientID     string
	ClientSecret string
	AppOrigin    string
	Syncer       *Syncer
	DiscordAPI   *discord.Client
}

type Handler struct {
	cfg HandlerConfig
}

func NewHandler(cfg HandlerConfig) *Handler { return &Handler{cfg: cfg} }

func (h *Handler) frontendURL(path string) string {
	return strings.TrimRight(h.cfg.AppOrigin, "/") + path
}

// Initiate handles GET /api/auth/discord/role-connection.
// Requires a valid session; sets short-lived PKCE/state/return cookies; 302s to Discord.
func (h *Handler) Initiate(w http.ResponseWriter, r *http.Request) {
	if appauth.SessionFromContext(r.Context()) == nil {
		http.Redirect(w, r, h.frontendURL("/login?notice=session-required"), http.StatusFound)
		return
	}
	if h.cfg.ClientID == "" {
		http.Redirect(w, r, h.frontendURL("/dashboard?error=discord-config"), http.StatusFound)
		return
	}
	state, err := discord.NewState()
	if err != nil {
		http.Redirect(w, r, h.frontendURL("/dashboard?error=discord-config"), http.StatusFound)
		return
	}
	verifier, err := discord.NewPkceVerifier()
	if err != nil {
		http.Redirect(w, r, h.frontendURL("/dashboard?error=discord-config"), http.StatusFound)
		return
	}
	// The "return" cookie tells Callback where to send the user afterwards.
	// "discord" => let Discord supply the redirect (linked-roles flow).
	// anything else => redirect back to /dashboard.
	returnTo := r.URL.Query().Get("return")
	if returnTo == "" {
		returnTo = "dashboard"
	}

	setShortCookie(w, discord.CookieRCState, state)
	setShortCookie(w, discord.CookieRCVerifier, verifier)
	setShortCookie(w, discord.CookieRCReturn, returnTo)

	authorize, _ := url.Parse("https://discord.com/oauth2/authorize")
	q := authorize.Query()
	q.Set("client_id", h.cfg.ClientID)
	q.Set("response_type", "code")
	q.Set("redirect_uri", discord.RoleConnectionRedirectURI(h.cfg.AppOrigin))
	q.Set("scope", discord.RoleConnectionScopes)
	q.Set("prompt", "consent")
	q.Set("state", state)
	q.Set("code_challenge", discord.PkceChallenge(verifier))
	q.Set("code_challenge_method", "S256")
	authorize.RawQuery = q.Encode()

	http.Redirect(w, r, authorize.String(), http.StatusFound)
}

func setShortCookie(w http.ResponseWriter, name, value string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		MaxAge:   600, // 10 minutes
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
}

func clearCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{
		Name: name, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode,
	})
}

// helper to keep linker happy if not used yet
var _ = time.Now
```

- [ ] **Step 5: Run, verify pass**

Run: `cd apps/backend && go test ./internal/roleconnection/ -run TestHandler_Initiate -v`
Expected: PASS.

- [ ] **Step 6: Commit and push**

```bash
git add apps/backend/internal/roleconnection/handler.go apps/backend/internal/roleconnection/handler_test.go apps/backend/internal/appauth/middleware.go
git commit -m "feat(backend): roleconnection OAuth initiate handler"
git push
```

### Task 17: OAuth handler — Callback

**Files:**
- Modify: `apps/backend/internal/roleconnection/handler.go`
- Modify: `apps/backend/internal/roleconnection/handler_test.go`

- [ ] **Step 1: Add failing test for callback happy path**

Append to `handler_test.go`:

```go
func TestHandler_Callback_HappyPath(t *testing.T) {
	// Discord stand-in: serves token + profile.
	discordSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/oauth2/token":
			_, _ = w.Write([]byte(`{
				"access_token":"at","refresh_token":"rt","expires_in":604800,
				"token_type":"Bearer","scope":"identify role_connections.write"
			}`))
		case "/api/users/@me":
			_, _ = w.Write([]byte(`{"id":"d1","username":"sem","global_name":"Sem","avatar":null}`))
		case "/api/users/@me/applications/APP/role-connection":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		default:
			t.Errorf("unexpected discord call: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer discordSrv.Close()

	cipher := TokenCipher{Crypto: newTestCrypto(t)}
	s := &fakeStore{displayName: "Sem"}
	disc := discord.NewClient("cid", "csec",
		discord.WithBaseURL(discordSrv.URL),
		discord.WithRedirectURI("http://localhost/api/auth/discord/role-connection/callback"),
	)
	syncer := NewSyncer(SyncerConfig{
		Store: s, Discord: disc, ApplicationID: "APP", Cipher: cipher,
	})
	h := NewHandler(HandlerConfig{
		ClientID: "cid", ClientSecret: "csec",
		AppOrigin: "http://localhost", Syncer: syncer, DiscordAPI: disc,
	})

	req := httptest.NewRequest(http.MethodGet,
		"/api/auth/discord/role-connection/callback?code=ok&state=s1", nil)
	req.AddCookie(&http.Cookie{Name: "rl_rc_state", Value: "s1"})
	req.AddCookie(&http.Cookie{Name: "rl_rc_verifier", Value: "v1"})
	req.AddCookie(&http.Cookie{Name: "rl_rc_return", Value: "dashboard"})
	// Session for user d1.
	ctx := appauth.WithSession(req.Context(), &appauth.SessionUser{UserID: "u1", DiscordID: "d1"})
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	h.Callback(rec, req)
	if rec.Code != http.StatusFound {
		t.Errorf("status = %d, want 302", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "/dashboard?notice=discord-linked") {
		t.Errorf("loc = %s", loc)
	}
	if s.upsertCalled != 1 {
		t.Errorf("expected 1 upsert, got %d", s.upsertCalled)
	}
}

func TestHandler_Callback_DiscordIDMismatch(t *testing.T) {
	discordSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/oauth2/token":
			_, _ = w.Write([]byte(`{"access_token":"at","refresh_token":"rt","expires_in":604800,"token_type":"Bearer","scope":"identify role_connections.write"}`))
		case "/api/users/@me":
			_, _ = w.Write([]byte(`{"id":"OTHER","username":"x","global_name":"X","avatar":null}`))
		}
	}))
	defer discordSrv.Close()
	cipher := TokenCipher{Crypto: newTestCrypto(t)}
	s := &fakeStore{}
	disc := discord.NewClient("cid", "csec",
		discord.WithBaseURL(discordSrv.URL),
		discord.WithRedirectURI("http://localhost/api/auth/discord/role-connection/callback"),
	)
	syncer := NewSyncer(SyncerConfig{Store: s, Discord: disc, ApplicationID: "APP", Cipher: cipher})
	h := NewHandler(HandlerConfig{
		ClientID: "cid", ClientSecret: "csec", AppOrigin: "http://localhost",
		Syncer: syncer, DiscordAPI: disc,
	})
	req := httptest.NewRequest(http.MethodGet,
		"/api/auth/discord/role-connection/callback?code=ok&state=s1", nil)
	req.AddCookie(&http.Cookie{Name: "rl_rc_state", Value: "s1"})
	req.AddCookie(&http.Cookie{Name: "rl_rc_verifier", Value: "v1"})
	req.AddCookie(&http.Cookie{Name: "rl_rc_return", Value: "dashboard"})
	ctx := appauth.WithSession(req.Context(), &appauth.SessionUser{UserID: "u1", DiscordID: "d1"})
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	h.Callback(rec, req)
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "error=discord-mismatch") {
		t.Errorf("loc = %s", loc)
	}
	if s.upsertCalled != 0 {
		t.Errorf("upsert should not be called on mismatch, got %d", s.upsertCalled)
	}
}
```

- [ ] **Step 2: Run, verify fail**

Run: `cd apps/backend && go test ./internal/roleconnection/ -run TestHandler_Callback -v`
Expected: FAIL.

- [ ] **Step 3: Implement Callback**

Append to `handler.go`:

```go
// Callback handles GET /api/auth/discord/role-connection/callback.
// Validates the per-flow cookies, exchanges the code, confirms the linking
// Discord account matches the session user, stores the encrypted tokens,
// and synchronously pushes the current PRO metadata.
func (h *Handler) Callback(w http.ResponseWriter, r *http.Request) {
	defer func() {
		clearCookie(w, discord.CookieRCState)
		clearCookie(w, discord.CookieRCVerifier)
		clearCookie(w, discord.CookieRCReturn)
	}()

	session := appauth.SessionFromContext(r.Context())
	if session == nil {
		http.Redirect(w, r, h.frontendURL("/login?notice=session-required"), http.StatusFound)
		return
	}

	stateCookie, _ := r.Cookie(discord.CookieRCState)
	verifierCookie, _ := r.Cookie(discord.CookieRCVerifier)
	queryState := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")
	if stateCookie == nil || verifierCookie == nil ||
		queryState == "" || code == "" || queryState != stateCookie.Value {
		http.Redirect(w, r, h.frontendURL("/dashboard?error=discord-state"), http.StatusFound)
		return
	}

	ctx := r.Context()
	access, refresh, expiresIn, err := h.cfg.DiscordAPI.ExchangeCodeForTokens(ctx, code, verifierCookie.Value)
	if err != nil {
		http.Redirect(w, r, h.frontendURL("/dashboard?error=discord-token"), http.StatusFound)
		return
	}
	profile, err := h.cfg.DiscordAPI.FetchProfile(ctx, access)
	if err != nil {
		http.Redirect(w, r, h.frontendURL("/dashboard?error=discord-profile"), http.StatusFound)
		return
	}
	if profile.ID != session.DiscordID {
		http.Redirect(w, r, h.frontendURL("/dashboard?error=discord-mismatch"), http.StatusFound)
		return
	}

	accessCT, accessNonce, err := h.cfg.Syncer.cfg.Cipher.Encrypt(access)
	if err != nil {
		http.Redirect(w, r, h.frontendURL("/dashboard?error=discord-encrypt"), http.StatusFound)
		return
	}
	refreshCT, refreshNonce, err := h.cfg.Syncer.cfg.Cipher.Encrypt(refresh)
	if err != nil {
		http.Redirect(w, r, h.frontendURL("/dashboard?error=discord-encrypt"), http.StatusFound)
		return
	}
	if err := h.cfg.Syncer.cfg.Store.UpsertDiscordRoleConnection(ctx, store.DiscordRoleConnection{
		UserID:            session.UserID,
		DiscordID:         profile.ID,
		AccessTokenCT:     accessCT,
		AccessTokenNonce:  accessNonce,
		RefreshTokenCT:    refreshCT,
		RefreshTokenNonce: refreshNonce,
		AccessExpiresAt:   time.Now().Add(time.Duration(expiresIn) * time.Second),
		Scopes:            discord.RoleConnectionScopes,
	}); err != nil {
		http.Redirect(w, r, h.frontendURL("/dashboard?error=discord-persist"), http.StatusFound)
		return
	}

	// Immediate push so the role appears before the user's redirect lands.
	// Errors are logged but do not block the redirect — the reconciler will retry.
	if err := h.cfg.Syncer.Push(ctx, session.UserID); err != nil {
		slog.Warn("roleconnection initial push failed",
			slog.String("user_id", session.UserID), slog.Any("error", err))
	}

	returnTo := "dashboard"
	if c, err := r.Cookie(discord.CookieRCReturn); err == nil {
		returnTo = c.Value
	}
	if returnTo == "discord" {
		// Discord's linked-roles flow expects a redirect back to Discord, but
		// without a captured Discord redirect URL we fall back to dashboard.
		// (The linked-roles entry passes the destination via state.) For v1
		// we always send to dashboard with a success notice.
		http.Redirect(w, r, h.frontendURL("/dashboard?notice=discord-linked"), http.StatusFound)
		return
	}
	http.Redirect(w, r, h.frontendURL("/dashboard?notice=discord-linked"), http.StatusFound)
}
```

Add imports:
```go
	"log/slog"

	"rustlens/backend/internal/store"
```

Also rename the existing `ExchangeCode` to `ExchangeCodeForTokens` (returning all three values) — OR add a new function. Add a new function to keep existing callers untouched:

In `apps/backend/internal/oauth/discord/client.go`, append:

```go
// ExchangeCodeForTokens is like ExchangeCode but additionally returns the
// refresh_token and expires_in fields. Used by the role-connection flow which
// needs long-lived push access.
func (c *Client) ExchangeCodeForTokens(ctx context.Context, code, codeVerifier string) (access, refresh string, expiresIn int, err error) {
	if c.redirectURI == "" {
		return "", "", 0, errors.New("discord client: redirect_uri is required for code exchange")
	}
	form := url.Values{
		"client_id":     {c.clientID},
		"client_secret": {c.clientSecret},
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"code_verifier": {codeVerifier},
		"redirect_uri":  {c.redirectURI},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/api/oauth2/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", "", 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cache-Control", "no-store")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", "", 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", 0, fmt.Errorf("discord token exchange: %d %s", resp.StatusCode, string(body))
	}
	var out struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", "", 0, err
	}
	if out.AccessToken == "" {
		return "", "", 0, errors.New("discord token exchange: empty access_token")
	}
	return out.AccessToken, out.RefreshToken, out.ExpiresIn, nil
}
```

- [ ] **Step 4: Run, verify pass**

Run: `cd apps/backend && go test ./internal/roleconnection/ -run TestHandler_Callback -v`
Expected: PASS for both.

- [ ] **Step 5: Run the full backend test suite to make sure nothing else regressed**

Run: `cd apps/backend && go test ./...`
Expected: all PASS (some store tests will skip without TEST_POSTGRES_URL — that's fine).

- [ ] **Step 6: Commit and push**

```bash
git add apps/backend/internal/roleconnection/handler.go apps/backend/internal/roleconnection/handler_test.go apps/backend/internal/oauth/discord/client.go
git commit -m "feat(backend): roleconnection OAuth callback with mismatch guard"
git push
```

### Task 18: Disconnect handler

**Files:**
- Modify: `apps/backend/internal/roleconnection/handler.go`
- Modify: `apps/backend/internal/roleconnection/handler_test.go`

- [ ] **Step 1: Add failing test**

Append to `handler_test.go`:

```go
func TestHandler_Disconnect_DeletesRowAndPushesEmpty(t *testing.T) {
	var pushCalled int
	discordSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/users/@me/applications/APP/role-connection":
			pushCalled++
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer discordSrv.Close()
	cipher := TokenCipher{Crypto: newTestCrypto(t)}
	at, atNonce, _ := cipher.Encrypt("at")
	rt, rtNonce, _ := cipher.Encrypt("rt")
	s := &fakeStore{
		row: &store.DiscordRoleConnection{
			UserID: "u1", DiscordID: "d1",
			AccessTokenCT: at, AccessTokenNonce: atNonce,
			RefreshTokenCT: rt, RefreshTokenNonce: rtNonce,
			AccessExpiresAt: time.Now().Add(time.Hour),
			Scopes:          "identify role_connections.write",
		},
		displayName: "sem",
	}
	disc := discord.NewClient("cid", "csec", discord.WithBaseURL(discordSrv.URL))
	syncer := NewSyncer(SyncerConfig{Store: s, Discord: disc, ApplicationID: "APP", Cipher: cipher})
	h := NewHandler(HandlerConfig{Syncer: syncer, DiscordAPI: disc, AppOrigin: "http://localhost"})

	req := httptest.NewRequest(http.MethodDelete, "/api/app/me/discord-role-connection", nil)
	ctx := appauth.WithSession(req.Context(), &appauth.SessionUser{UserID: "u1", DiscordID: "d1"})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	h.Disconnect(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d", rec.Code)
	}
	if s.deleteCalled != 1 {
		t.Errorf("delete called %d times", s.deleteCalled)
	}
	if pushCalled != 1 {
		t.Errorf("expected best-effort empty push, got %d", pushCalled)
	}
}
```

- [ ] **Step 2: Run, verify fail**

Run: `cd apps/backend && go test ./internal/roleconnection/ -run TestHandler_Disconnect -v`
Expected: FAIL.

- [ ] **Step 3: Implement**

Append to `handler.go`:

```go
// Disconnect handles DELETE /api/app/me/discord-role-connection.
// Removes the stored tokens, then best-effort pushes empty metadata so
// Discord drops the linked role immediately rather than waiting for the
// user to disconnect from inside Discord.
func (h *Handler) Disconnect(w http.ResponseWriter, r *http.Request) {
	session := appauth.SessionFromContext(r.Context())
	if session == nil {
		http.Error(w, "unauthenticated", http.StatusUnauthorized)
		return
	}
	row, err := h.cfg.Syncer.cfg.Store.GetDiscordRoleConnection(r.Context(), session.UserID)
	if err != nil {
		http.Error(w, "store error", http.StatusInternalServerError)
		return
	}
	if row != nil && row.RevokedAt == nil {
		// Try to push empty metadata with the current access token. Best-effort:
		// failure here just means Discord will eventually drop the role on the
		// next reconcile or when the user disconnects from Discord's side.
		access, err := h.cfg.Syncer.cfg.Cipher.Decrypt(row.AccessTokenCT, row.AccessTokenNonce)
		if err == nil {
			body := discord.RoleConnectionBody{
				PlatformName: PlatformName, PlatformUsername: "",
				Metadata: map[string]string{"is_pro": "0"},
			}
			if perr := h.cfg.DiscordAPI.PutRoleConnection(r.Context(), access,
				h.cfg.Syncer.cfg.ApplicationID, body); perr != nil {
				slog.Warn("roleconnection disconnect push failed",
					slog.String("user_id", session.UserID), slog.Any("error", perr))
			}
		}
	}
	if err := h.cfg.Syncer.cfg.Store.DeleteDiscordRoleConnection(r.Context(), session.UserID); err != nil {
		http.Error(w, "delete failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
```

- [ ] **Step 4: Run, verify pass**

Run: `cd apps/backend && go test ./internal/roleconnection/ -run TestHandler_Disconnect -v`
Expected: PASS.

- [ ] **Step 5: Commit and push**

```bash
git add apps/backend/internal/roleconnection/handler.go apps/backend/internal/roleconnection/handler_test.go
git commit -m "feat(backend): roleconnection disconnect handler"
git push
```

### Task 19: Wire role-connection handlers into the chi router

**Files:**
- Modify: `apps/backend/internal/http/router.go`
- Modify: `apps/backend/cmd/server/main.go`

- [ ] **Step 1: Pass the Syncer into the router**

In `apps/backend/cmd/server/main.go`, after the schema-registration block (Task 15), construct the Syncer and pass it to `NewRouter`. Add:

```go
	var roleConnSyncer *roleconnection.Syncer
	if proxyCrypto != nil && cfg.DiscordClientID != "" && cfg.DiscordClientSecret != "" {
		discordClient := discord.NewClient(
			cfg.DiscordClientID, cfg.DiscordClientSecret,
			discord.WithRedirectURI(discord.RoleConnectionRedirectURI(cfg.AppOrigin)),
		)
		roleConnSyncer = roleconnection.NewSyncer(roleconnection.SyncerConfig{
			Store:         postgresStore,
			Discord:       discordClient,
			ApplicationID: cfg.DiscordClientID,
			Cipher:        roleconnection.TokenCipher{Crypto: proxyCrypto},
		})
	} else {
		log.Printf("[backend] role-connection sync disabled (need PROXY_ENCRYPTION_KEY + DISCORD_CLIENT_ID/SECRET)")
	}
```

Then change the `NewRouter(...)` call to pass `roleConnSyncer` as a new last argument.

- [ ] **Step 2: Extend `NewRouter` signature**

In `apps/backend/internal/http/router.go`:
- Add the parameter: `roleConnSyncer *roleconnection.Syncer` to `NewRouter`.
- Import `"rustlens/backend/internal/roleconnection"` and `"rustlens/backend/internal/oauth/discord"` if not already.
- After the existing `r.Route("/api/auth/discord", ...)` block, register the new sub-routes:

```go
	if roleConnSyncer != nil {
		rcDiscordClient := discord.NewClient(
			cfg.DiscordClientID, cfg.DiscordClientSecret,
			discord.WithRedirectURI(discord.RoleConnectionRedirectURI(cfg.AppOrigin)),
		)
		rcHandler := roleconnection.NewHandler(roleconnection.HandlerConfig{
			ClientID:     cfg.DiscordClientID,
			ClientSecret: cfg.DiscordClientSecret,
			AppOrigin:    cfg.AppOrigin,
			Syncer:       roleConnSyncer,
			DiscordAPI:   rcDiscordClient,
		})
		r.Route("/api/auth/discord/role-connection", func(r chi.Router) {
			r.Use(requireSession)
			r.Get("/", rcHandler.Initiate)
			r.Get("/callback", rcHandler.Callback)
		})
	}
```

And inside the `r.Route("/api/app", ...)` block, inside the existing session-required group, add:

```go
		r.Group(func(r chi.Router) {
			r.Use(requireSession)
			if roleConnSyncer != nil {
				rcDelete := roleconnection.NewHandler(roleconnection.HandlerConfig{
					AppOrigin: cfg.AppOrigin, Syncer: roleConnSyncer,
					DiscordAPI: discord.NewClient(cfg.DiscordClientID, cfg.DiscordClientSecret),
				})
				r.Delete("/me/discord-role-connection", rcDelete.Disconnect)
			}
		})
```

(If your router already has a session-required group containing `/me/connections` etc., add the `Delete` route inside it instead of creating a new group — this avoids redundant middleware. Search for `r.Get("/me/connections"` to find that group.)

- [ ] **Step 3: Build**

Run: `cd apps/backend && go build ./... && go vet ./...`
Expected: exit 0 for both.

- [ ] **Step 4: Commit and push**

```bash
git add apps/backend/internal/http/router.go apps/backend/cmd/server/main.go
git commit -m "feat(backend): wire roleconnection handlers into chi router"
git push
```

---

## Phase 7 — Hook syncer into admin grant/revoke actions

### Task 20: Add an optional Syncer dependency to users.roles.grant and revoke

**Files:**
- Modify: `apps/backend/internal/admin/actions/users_roles_grant.go`
- Modify: `apps/backend/internal/admin/actions/users_roles_revoke.go`
- Modify: `apps/backend/internal/admin/actions/dispatcher.go` (if it wires actions; check first)
- Modify: `apps/backend/internal/http/router.go` (action construction)

- [ ] **Step 1: Find where these actions are constructed**

Run: `rg -n "NewUsersRolesGrantAction|NewUsersRolesRevokeAction" apps/backend/`

- [ ] **Step 2: Define a hook interface in the actions package**

Create `apps/backend/internal/admin/actions/role_sync_hook.go`:

```go
package actions

// RoleSyncHook is invoked after a successful users.roles.grant or revoke for
// the "pro" role. Implementations should not block — they are called with the
// user_id and must fire-and-forget any side effects (e.g., Discord push).
type RoleSyncHook interface {
	OnProRoleChanged(userID string)
}

// NopRoleSyncHook is the default when no hook is wired.
type NopRoleSyncHook struct{}

func (NopRoleSyncHook) OnProRoleChanged(string) {}
```

- [ ] **Step 3: Inject the hook into the two actions**

In `users_roles_grant.go`:
- Add `hook RoleSyncHook` to the `usersRolesGrantAction` struct.
- Add a `hook` parameter to `NewUsersRolesGrantAction` (after `pub`).
- Default `nil` → use `NopRoleSyncHook`.
- After the successful insert and `a.pub.PublishWithFanout(...)`, add:

```go
	if p.RoleKey == rbac.RolePRO && a.hook != nil {
		a.hook.OnProRoleChanged(p.UserID)
	}
```

Do the same for `users_roles_revoke.go`.

- [ ] **Step 4: Update callers**

In `apps/backend/internal/http/router.go` (or wherever these actions are instantiated — likely `apps/backend/internal/admin/actions/dispatcher.go`), pass `roleConnSyncer` wrapped as a hook. Add an adapter near the syncer construction in `main.go` OR in the router (keep it next to the call site):

```go
type proRoleSyncAdapter struct{ s *roleconnection.Syncer }

func (a proRoleSyncAdapter) OnProRoleChanged(userID string) {
	if a.s != nil {
		a.s.PushAsync(userID)
	}
}
```

When constructing the actions, pass `proRoleSyncAdapter{s: roleConnSyncer}` for the hook parameter. When `roleConnSyncer` is nil, pass `actions.NopRoleSyncHook{}` instead.

- [ ] **Step 5: Update existing tests that construct these actions**

Run: `rg -n "NewUsersRolesGrantAction\(|NewUsersRolesRevokeAction\(" apps/backend/`

For each call site (likely in `users_roles_test.go`), add a `nil` or `actions.NopRoleSyncHook{}` argument so the tests still compile.

- [ ] **Step 6: Run tests**

Run: `cd apps/backend && go test ./internal/admin/...`
Expected: PASS.

- [ ] **Step 7: Commit and push**

```bash
git add apps/backend/internal/admin/actions/ apps/backend/internal/http/router.go apps/backend/cmd/server/main.go
git commit -m "feat(backend): trigger roleconnection push from PRO grant/revoke"
git push
```

---

## Phase 8 — Periodic reconciler

### Task 21: Reconciler with advisory lock

**Files:**
- Create: `apps/backend/internal/roleconnection/reconciler.go`
- Create: `apps/backend/internal/roleconnection/reconciler_test.go`

- [ ] **Step 1: Add failing test for the iterator behavior**

```go
package roleconnection

import (
	"context"
	"testing"
	"time"

	"rustlens/backend/internal/oauth/discord"
	"rustlens/backend/internal/store"
)

type recorderSyncer struct {
	pushed []string
	err    error
}

func (r *recorderSyncer) Push(_ context.Context, userID string) error {
	r.pushed = append(r.pushed, userID)
	return r.err
}

func TestReconcilerStep_PushesEachActiveRow(t *testing.T) {
	s := &fakeStore{
		listActive: []store.DiscordRoleConnection{
			{UserID: "a"}, {UserID: "b"}, {UserID: "c"},
		},
	}
	rec := &recorderSyncer{}
	r := NewReconciler(ReconcilerConfig{Store: s, Pusher: rec, Sleep: 0})
	if err := r.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if len(rec.pushed) != 3 {
		t.Errorf("pushed = %v, want 3", rec.pushed)
	}
}

func TestReconcilerStep_ContinuesAfterPerRowError(t *testing.T) {
	s := &fakeStore{
		listActive: []store.DiscordRoleConnection{
			{UserID: "a"}, {UserID: "b"},
		},
	}
	rec := &recorderSyncer{err: discord.ErrRefreshRevoked}
	r := NewReconciler(ReconcilerConfig{Store: s, Pusher: rec, Sleep: 0})
	if err := r.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if len(rec.pushed) != 2 {
		t.Errorf("pushed = %v", rec.pushed)
	}
}
```

- [ ] **Step 2: Run, verify fail**

Run: `cd apps/backend && go test ./internal/roleconnection/ -run TestReconciler -v`
Expected: FAIL.

- [ ] **Step 3: Implement**

Create `reconciler.go`:

```go
package roleconnection

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Pusher is the subset of Syncer the reconciler needs. Lets us test with
// a fake without standing up the full Syncer + Discord dependencies.
type Pusher interface {
	Push(ctx context.Context, userID string) error
}

type ReconcilerStore interface {
	SyncerStore // gives us ListActiveDiscordRoleConnections
}

type ReconcilerConfig struct {
	Store    ReconcilerStore
	Pusher   Pusher
	Sleep    time.Duration // between per-row pushes (be polite to Discord)
	Interval time.Duration // between full passes
	Pool     *pgxpool.Pool // for advisory lock; nil disables locking
}

type Reconciler struct {
	cfg ReconcilerConfig
}

// advisoryLockKey is a constant 64-bit integer for pg_try_advisory_lock so
// only one pod runs the reconciler at a time. Value chosen arbitrarily.
const advisoryLockKey int64 = 7427198631011

func NewReconciler(cfg ReconcilerConfig) *Reconciler {
	if cfg.Interval == 0 {
		cfg.Interval = 6 * time.Hour
	}
	if cfg.Sleep == 0 {
		cfg.Sleep = 200 * time.Millisecond
	}
	return &Reconciler{cfg: cfg}
}

// RunOnce iterates all active role-connection rows and calls Push for each.
// Per-row errors are logged; iteration continues. Holds the advisory lock
// for the duration of the pass when Pool is set.
func (r *Reconciler) RunOnce(ctx context.Context) error {
	if r.cfg.Pool != nil {
		conn, err := r.cfg.Pool.Acquire(ctx)
		if err != nil {
			return err
		}
		defer conn.Release()
		var got bool
		if err := conn.QueryRow(ctx,
			`SELECT pg_try_advisory_lock($1)`, advisoryLockKey).Scan(&got); err != nil {
			return err
		}
		if !got {
			slog.Info("roleconnection reconciler: another pod holds the lock, skipping")
			return nil
		}
		defer func() {
			_, _ = conn.Exec(ctx, `SELECT pg_advisory_unlock($1)`, advisoryLockKey)
		}()
	}
	rows, err := r.cfg.Store.ListActiveDiscordRoleConnections(ctx)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if err := r.cfg.Pusher.Push(ctx, row.UserID); err != nil {
			slog.Warn("roleconnection reconciler push failed",
				slog.String("user_id", row.UserID), slog.Any("error", err))
		}
		if r.cfg.Sleep > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(r.cfg.Sleep):
			}
		}
	}
	return nil
}

// Start runs RunOnce on an Interval ticker until ctx is done. Fires once
// immediately on start so a freshly-deployed pod doesn't wait Interval before
// catching up.
func (r *Reconciler) Start(ctx context.Context) {
	go func() {
		if err := r.RunOnce(ctx); err != nil {
			slog.Warn("roleconnection reconciler initial run failed", slog.Any("error", err))
		}
		t := time.NewTicker(r.cfg.Interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := r.RunOnce(ctx); err != nil {
					slog.Warn("roleconnection reconciler tick failed", slog.Any("error", err))
				}
			}
		}
	}()
}
```

- [ ] **Step 4: Verify pass**

Run: `cd apps/backend && go test ./internal/roleconnection/ -v`
Expected: PASS (including new reconciler tests).

- [ ] **Step 5: Commit and push**

```bash
git add apps/backend/internal/roleconnection/reconciler.go apps/backend/internal/roleconnection/reconciler_test.go
git commit -m "feat(backend): roleconnection reconciler with advisory lock"
git push
```

### Task 22: Start the reconciler from main.go

**Files:**
- Modify: `apps/backend/cmd/server/main.go`

- [ ] **Step 1: After constructing `roleConnSyncer`, also construct + start the reconciler**

Add immediately after the syncer construction block (Task 19's main.go change):

```go
	if roleConnSyncer != nil {
		reconciler := roleconnection.NewReconciler(roleconnection.ReconcilerConfig{
			Store:  postgresStore,
			Pusher: roleConnSyncer,
			Pool:   postgresStore.Pool(),
		})
		reconciler.Start(ctx)
	}
```

- [ ] **Step 2: Build + vet**

Run: `cd apps/backend && go build ./... && go vet ./...`
Expected: exit 0.

- [ ] **Step 3: Commit and push**

```bash
git add apps/backend/cmd/server/main.go
git commit -m "feat(backend): start linked-roles reconciler at boot"
git push
```

---

## Phase 9 — Surface state on /api/app/me

### Task 23: Extend CurrentUser with DiscordRoleConnection summary

**Files:**
- Modify: `apps/backend/internal/appauth/repo.go`
- Modify: `apps/backend/internal/store/role_connections.go` (a tiny summary query)

- [ ] **Step 1: Add a summary type + store query**

Append to `apps/backend/internal/store/role_connections.go`:

```go
// DiscordRoleConnectionSummary is the safe-to-expose subset of a row used by
// the frontend dashboard card. Tokens and ciphertext never leave the backend.
type DiscordRoleConnectionSummary struct {
	Linked         bool       `json:"linked"`
	DiscordID      *string    `json:"discordId"`
	LinkedAt       *time.Time `json:"linkedAt"`
	LastPushedAt   *time.Time `json:"lastPushedAt"`
	LastPushError  *string    `json:"lastPushError"`
	Revoked        bool       `json:"revoked"`
}

// GetDiscordRoleConnectionSummary returns the safe summary for the dashboard.
func (p *PostgresStore) GetDiscordRoleConnectionSummary(ctx context.Context, userID string) (DiscordRoleConnectionSummary, error) {
	row, err := p.GetDiscordRoleConnection(ctx, userID)
	if err != nil {
		return DiscordRoleConnectionSummary{}, err
	}
	if row == nil {
		return DiscordRoleConnectionSummary{Linked: false}, nil
	}
	id := row.DiscordID
	return DiscordRoleConnectionSummary{
		Linked:        row.RevokedAt == nil,
		DiscordID:     &id,
		LinkedAt:      &row.LinkedAt,
		LastPushedAt:  row.LastPushedAt,
		LastPushError: row.LastPushError,
		Revoked:       row.RevokedAt != nil,
	}, nil
}
```

- [ ] **Step 2: Add `DiscordRoleConnection` field to CurrentUser**

Modify `apps/backend/internal/appauth/repo.go`:

Add to the `CurrentUser` struct:
```go
	DiscordRoleConnection *store.DiscordRoleConnectionSummary `json:"discordRoleConnection,omitempty"`
```

Add `"rustlens/backend/internal/store"` to imports.

`Repo` currently doesn't depend on `store`. Two options: (a) expose a query method on Repo via a new dependency injection, or (b) load the summary in the handler.

Option (b) keeps Repo's surface minimal. Update `appauth.Handler.Me` to load the summary directly:

In `apps/backend/internal/appauth/handlers.go`:

```go
type Handler struct {
	repo *Repo
	rcStore RoleConnectionSummaryStore // new
}

// RoleConnectionSummaryStore is the subset of store needed by Me.
type RoleConnectionSummaryStore interface {
	GetDiscordRoleConnectionSummary(ctx context.Context, userID string) (store.DiscordRoleConnectionSummary, error)
}

func NewHandler(repo *Repo) *Handler { return &Handler{repo: repo} }

func NewHandlerWithRoleConnection(repo *Repo, rc RoleConnectionSummaryStore) *Handler {
	return &Handler{repo: repo, rcStore: rc}
}

// ... in Me():
	if h.rcStore != nil {
		summary, err := h.rcStore.GetDiscordRoleConnectionSummary(r.Context(), session.UserID)
		if err == nil {
			cu.DiscordRoleConnection = &summary
		}
	}
```

Update imports + the existing `NewHandler` call site to use `NewHandlerWithRoleConnection(appAuthRepo, postgresStore)` in router.go (the postgresStore satisfies the interface).

- [ ] **Step 3: Build**

Run: `cd apps/backend && go build ./... && go vet ./...`
Expected: exit 0.

- [ ] **Step 4: Add a sanity test against TEST_POSTGRES_URL**

Add to `apps/backend/internal/store/role_connections_test.go`:

```go
func TestGetDiscordRoleConnectionSummary(t *testing.T) {
	ctx := context.Background()
	s := newTestPostgres(t)
	suffix := time.Now().UnixNano() % 1_000_000_000
	userID := fmt.Sprintf("usr_summ_%d", suffix)
	insertTestAppUser(t, s, userID, fmt.Sprintf("9999000%010d", suffix))

	got, err := s.GetDiscordRoleConnectionSummary(ctx, userID)
	if err != nil {
		t.Fatalf("Summary(absent): %v", err)
	}
	if got.Linked {
		t.Error("expected Linked=false when row missing")
	}

	if err := s.UpsertDiscordRoleConnection(ctx, DiscordRoleConnection{
		UserID: userID, DiscordID: "d",
		AccessTokenCT: []byte("x"), AccessTokenNonce: []byte("y"),
		RefreshTokenCT: []byte("x"), RefreshTokenNonce: []byte("y"),
		AccessExpiresAt: time.Now().Add(time.Hour), Scopes: "identify",
	}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	got, err = s.GetDiscordRoleConnectionSummary(ctx, userID)
	if err != nil {
		t.Fatalf("Summary(present): %v", err)
	}
	if !got.Linked || got.DiscordID == nil || *got.DiscordID != "d" {
		t.Errorf("summary = %+v", got)
	}
}
```

Run: `cd apps/backend && TEST_POSTGRES_URL=... go test ./internal/store/ -run TestGetDiscordRoleConnectionSummary -v`
Expected: PASS.

- [ ] **Step 5: Commit and push**

```bash
git add apps/backend/internal/appauth/ apps/backend/internal/store/role_connections.go apps/backend/internal/store/role_connections_test.go apps/backend/internal/http/router.go
git commit -m "feat(backend): expose discordRoleConnection summary on /api/app/me"
git push
```

---

## Phase 10 — Frontend dashboard card

> **Reminder:** before editing any Next.js file, read the relevant guide in `apps/frontend/node_modules/next/dist/docs/` (server actions, redirect, cookies). Copy idioms from `apps/frontend/app/dashboard/page.tsx`, `apps/frontend/lib/csrf.ts`, `apps/frontend/lib/rustlens-api.ts`.

### Task 24: Extend the frontend rustlens-api types

**Files:**
- Modify: `apps/frontend/lib/rustlens-api.ts`

- [ ] **Step 1: Read the file to find the CurrentUser TS type**

Run: `rg -n "discordRoleConnection|adminRoles|export type CurrentUser|interface CurrentUser" apps/frontend/lib/rustlens-api.ts`

- [ ] **Step 2: Add the type**

Find the existing CurrentUser type (probably contains `adminRoles`, `permissions`). Add:

```ts
export interface DiscordRoleConnectionSummary {
  linked: boolean
  discordId: string | null
  linkedAt: string | null
  lastPushedAt: string | null
  lastPushError: string | null
  revoked: boolean
}
```

And in the existing CurrentUser interface/type, add (matching the optional shape):
```ts
  discordRoleConnection?: DiscordRoleConnectionSummary | null
```

- [ ] **Step 3: Lint**

Run: `cd apps/frontend && pnpm lint`
Expected: PASS.

- [ ] **Step 4: Commit and push**

```bash
git add apps/frontend/lib/rustlens-api.ts
git commit -m "feat(frontend): add DiscordRoleConnectionSummary type to currentUser"
git push
```

### Task 25: Dashboard card component

**Files:**
- Create: `apps/frontend/app/dashboard/_components/discord-role-connection-card.tsx`
- Create: `apps/frontend/app/dashboard/_components/discord-role-connection-actions.ts` (server actions)

- [ ] **Step 1: Find the existing card patterns**

Run: `ls apps/frontend/app/dashboard/_components/` and read one (e.g., `guild-list.tsx`) to mirror styling + imports.

Run: `rg -n "use server|server action" apps/frontend/lib/`
Read one existing server action file to mirror the conventions.

- [ ] **Step 2: Create the server actions**

Create `apps/frontend/app/dashboard/_components/discord-role-connection-actions.ts`:

```ts
"use server"

import { redirect } from "next/navigation"

import { rustlensFetch } from "@/lib/rustlens-api"
import { requireSession } from "@/lib/session"

export async function connectDiscordRoleAction(): Promise<void> {
  // Trigger the backend OAuth initiate. The backend reads our session cookie
  // and 302s to Discord; Next.js follows redirect() with a server-side 307.
  redirect("/api/auth/discord/role-connection?return=dashboard")
}

export async function disconnectDiscordRoleAction(): Promise<void> {
  await requireSession()
  const res = await rustlensFetch("/api/app/me/discord-role-connection", {
    method: "DELETE",
  })
  if (!res.ok && res.status !== 204) {
    throw new Error(`disconnect failed: ${res.status}`)
  }
  // Force the dashboard to re-fetch /api/app/me on next render.
  redirect("/dashboard?notice=discord-unlinked")
}
```

(Verify `rustlensFetch` is exported from `lib/rustlens-api.ts`. If the existing pattern is `apiClient.delete(...)` or similar, adapt to match.)

- [ ] **Step 3: Create the card component**

Create `apps/frontend/app/dashboard/_components/discord-role-connection-card.tsx`:

```tsx
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import type { DiscordRoleConnectionSummary } from "@/lib/rustlens-api"

import {
  connectDiscordRoleAction,
  disconnectDiscordRoleAction,
} from "./discord-role-connection-actions"

interface Props {
  summary: DiscordRoleConnectionSummary | null | undefined
  hasPro: boolean
}

export function DiscordRoleConnectionCard({ summary, hasPro }: Props) {
  const linked = Boolean(summary?.linked)
  const revoked = Boolean(summary?.revoked)

  return (
    <Card>
      <CardHeader>
        <CardTitle>Discord community role</CardTitle>
        <CardDescription>
          Link your Discord account to automatically receive the PRO role in
          our community server when you have PRO active on RustLens.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        {!linked && !revoked && (
          <form action={connectDiscordRoleAction}>
            <Button type="submit">Connect Discord</Button>
          </form>
        )}
        {linked && (
          <>
            <p className="text-sm text-muted-foreground">
              {hasPro
                ? `Connected. Your community role is in sync${
                    summary?.lastPushedAt
                      ? ` (last verified ${new Date(summary.lastPushedAt).toLocaleString()})`
                      : ""
                  }.`
                : "Connected. Your community role will appear automatically when you have PRO."}
            </p>
            {summary?.lastPushError && (
              <p className="text-sm text-amber-600">
                Last sync failed. The system will retry automatically.
              </p>
            )}
            <form action={disconnectDiscordRoleAction}>
              <Button type="submit" variant="outline">Disconnect</Button>
            </form>
          </>
        )}
        {revoked && (
          <>
            <p className="text-sm text-amber-600">
              Discord disconnected the app. Please reconnect to keep the
              community role in sync.
            </p>
            <form action={connectDiscordRoleAction}>
              <Button type="submit">Reconnect</Button>
            </form>
          </>
        )}
      </CardContent>
    </Card>
  )
}
```

- [ ] **Step 4: Lint**

Run: `cd apps/frontend && pnpm lint`
Expected: PASS.

- [ ] **Step 5: Commit and push**

```bash
git add apps/frontend/app/dashboard/_components/discord-role-connection-card.tsx apps/frontend/app/dashboard/_components/discord-role-connection-actions.ts
git commit -m "feat(frontend): add Discord role connection card with server actions"
git push
```

### Task 26: Render the card on the dashboard

**Files:**
- Modify: `apps/frontend/app/dashboard/page.tsx`

- [ ] **Step 1: Add the import**

```ts
import { DiscordRoleConnectionCard } from "./_components/discord-role-connection-card"
```

- [ ] **Step 2: Render it above the existing "Your guilds" section**

Inside the `<div className="flex flex-col gap-4 py-4 md:gap-6 md:py-6">` block, before the existing "Your guilds" subsection, add:

```tsx
              <div className="px-4 lg:px-6">
                <DiscordRoleConnectionCard
                  summary={currentUser?.discordRoleConnection ?? null}
                  hasPro={liveAdminRoles.includes("pro")}
                />
              </div>
```

- [ ] **Step 3: Lint + build**

Run: `cd apps/frontend && pnpm lint && pnpm build`
Expected: PASS.

- [ ] **Step 4: Manual smoke test**

In a separate terminal: `pnpm dev`
Browse to `http://localhost/dashboard`. The card should appear above your guild list. Without Discord OAuth credentials configured, clicking Connect will 302 to `/dashboard?error=discord-config` — that's expected behavior; it proves the wiring works.

- [ ] **Step 5: Commit and push**

```bash
git add apps/frontend/app/dashboard/page.tsx
git commit -m "feat(frontend): render Discord role connection card on dashboard"
git push
```

### Task 27: Notice toast handlers for the success/error redirects

**Files:**
- Modify: `apps/frontend/components/session-events.tsx` OR a notice-toast helper (search first)

- [ ] **Step 1: Find existing notice-handling code**

Run: `rg -n "notice=|NoticeToast|discord-state|discord-token" apps/frontend/`

- [ ] **Step 2: Add the new notice/error codes**

Wherever the existing notice mapping lives (likely `apps/frontend/lib/session-errors.ts` or a similar file), add entries for:
- `notice=discord-linked` → success toast "Discord connected — your community role will sync shortly."
- `notice=discord-unlinked` → info toast "Discord disconnected. You can reconnect anytime from your dashboard."
- `error=discord-mismatch` → error toast "That Discord account doesn't match your RustLens account. Sign in with the Discord account you used to register."
- `error=discord-state` / `error=discord-token` / `error=discord-config` → reuse existing generic Discord error mapping if present; otherwise add a generic "Discord sign-in failed, please try again."
- `error=discord-encrypt` / `error=discord-persist` → "Couldn't save your Discord link — please try again."

- [ ] **Step 3: Lint**

Run: `cd apps/frontend && pnpm lint`
Expected: PASS.

- [ ] **Step 4: Commit and push**

```bash
git add apps/frontend/
git commit -m "feat(frontend): add notice toasts for Discord role connection flow"
git push
```

---

## Phase 11 — Documentation

### Task 28: Update AGENTS.md with the new surfaces

**Files:**
- Modify: `apps/backend/AGENTS.md` (the symlink AGENTS.md at repo root → backend AGENTS.md per repo convention; if it's a single file, edit that). Confirm with `ls -la AGENTS.md`.

- [ ] **Step 1: Find the right file**

Run: `ls -la AGENTS.md apps/backend/AGENTS.md 2>/dev/null`
Edit whichever is the source of truth (not the symlink target).

- [ ] **Step 2: Add a section describing the linked-roles feature**

Under the Backend "Key patterns" bullets, add a new bullet:

```
- Discord Linked Roles for the `pro` admin role: `internal/roleconnection/` owns
  a separate OAuth flow at `/api/auth/discord/role-connection` (+ `/callback`)
  requesting `identify role_connections.write`. Tokens are stored AES-GCM-encrypted
  (PROXY_ENCRYPTION_KEY) in `discord_role_connections`. On grant/revoke of the
  `pro` role the `users.roles.grant`/`revoke` admin actions fire `Syncer.PushAsync`
  which PUTs is_pro + pro_since metadata to Discord's `/users/@me/applications/{id}/role-connection`.
  A 6h reconciler (advisory-locked via pg_try_advisory_lock) re-pushes for any
  drift. Schema registration (`PUT /applications/{id}/role-connections/metadata`)
  runs once at backend startup using `DISCORD_BOT_TOKEN`. Per-user disconnect:
  `DELETE /api/app/me/discord-role-connection`.
```

In the backend env var table, add `DISCORD_BOT_TOKEN` with value `(required for linked-roles schema registration; share with the bot's DISCORD_TOKEN)`.

In the routes section, add `/api/auth/discord/role-connection (+ /callback)` and `/api/app/me/discord-role-connection`.

In the frontend section, add `app/dashboard/_components/discord-role-connection-card.tsx` and the server actions file.

In the Discord Developer Portal setup notes (add a new short subsection if none exists), include:
> Linked Roles requires two configurations in the Discord Developer Portal under your application:
> 1. **OAuth2 → Redirects**: add `{APP_ORIGIN}/api/auth/discord/role-connection/callback`.
> 2. **App → Linked Roles Verification URL**: set to the same URL. This is what makes the "Link your account" button appear in your community Discord under the linked-role verification prompt.

- [ ] **Step 3: Lint check (no tool required — visual review)**

- [ ] **Step 4: Commit and push**

```bash
git add AGENTS.md apps/backend/AGENTS.md
git commit -m "docs: document discord linked-roles PRO sync"
git push
```

---

## Self-Review Checklist

After completing all tasks, verify:

1. **Spec coverage:** every section of the brainstorming conversation maps to a task:
   - Linked-roles OAuth flow → Tasks 16–17
   - Token storage (encrypted, reuse PROXY_ENCRYPTION_KEY) → Tasks 1, 10, 23
   - Auto-register schema at startup → Tasks 13–15
   - Event-driven + periodic reconcile → Tasks 20–22
   - Dashboard card for everyone → Tasks 25–26
   - Opt-in only (no backfill) → no code needed, it's the natural behavior of the OAuth requirement
   - Both Discord-side and dashboard-button entry points → Tasks 16, 25 (the same OAuth callback serves both)

2. **Placeholder scan:** no "TBD", "implement later", "add validation", or "similar to Task N" anywhere in the plan. Every code block is concrete.

3. **Type consistency:**
   - Table is `discord_role_connections` everywhere.
   - Package is `roleconnection` (lowercase, no underscore).
   - Method names: `Push`, `PushAsync`, `RunOnce`, `Start`.
   - Config struct: `SyncerConfig`, `ReconcilerConfig`, `HandlerConfig`.
   - Store methods: `UpsertDiscordRoleConnection`, `GetDiscordRoleConnection`, `DeleteDiscordRoleConnection`, `MarkDiscordRoleConnectionRevoked`, `RecordDiscordRoleConnectionPushed`, `RecordDiscordRoleConnectionPushError`, `ListActiveDiscordRoleConnections`, `GetUserProGrantedAt`, `GetAppUserDisplayName`, `GetDiscordRoleConnectionSummary`.
   - HTTP routes: `/api/auth/discord/role-connection`, `/api/auth/discord/role-connection/callback`, `DELETE /api/app/me/discord-role-connection`.
   - Cookie names: `rl_rc_state`, `rl_rc_verifier`, `rl_rc_return` (referenced via `discord.CookieRCState` etc.).
   - Metadata keys: `is_pro` (type 7), `pro_since` (type 6).
   - Frontend types: `DiscordRoleConnectionSummary` matches the Go JSON tags exactly.

If any drift is found during execution, fix the inconsistency in both the plan and the code rather than letting it ship.

---

**Plan complete.** Next: hand off to `superpowers:subagent-driven-development` for execution.
