# Discord-Only Auth Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Migrate the app from email/password auth (with Discord as a connected account) to Discord-only auth, deleting the entire email/password surface and storing Discord identity directly on `app_users`.

**Architecture:** Discord OAuth callback finds-or-creates an `app_users` row keyed on a new `discord_id` column. No email is captured. The email/password backend handlers, frontend routes, duplicate auth pages, admin actions, and DB tables are deleted. A small CLI promotes/demotes admin roles by Discord ID.

**Tech Stack:** Go (backend, `apps/backend`), Next.js / React (frontend, `apps/frontend`), Postgres (schema bootstrapped in `apps/backend/internal/store/postgres.go`), pgx, chi.

**Spec:** `docs/superpowers/specs/2026-04-25-discord-only-auth-design.md`

---

## File Structure Overview

**Modified:**
- `apps/backend/internal/store/postgres.go` — schema bootstrap: alter `app_users`, drop email/password tables.
- `apps/backend/internal/appauth/repo.go` — replace email/link methods with `FindOrCreateByDiscordID`; trim `CurrentUser`.
- `apps/backend/internal/appauth/handlers.go` — delete email login/register/verify/reset handlers; trim `CurrentUser` build.
- `apps/backend/internal/appauth/middleware.go` — drop email/password middleware paths if any.
- `apps/backend/internal/oauth/discord/handlers.go` — collapse callback to single sign-in branch (no link branch).
- `apps/backend/internal/admin/actions/dispatcher.go` (or registry init wherever it lives) — remove deleted action registrations.
- `apps/backend/cmd/server/main.go` — remove route registrations for deleted email handlers.
- `apps/frontend/app/login/page.tsx` — already exists; verify it renders Discord-only.
- `apps/frontend/components/auth-page.tsx` — strip email/password modes; Discord button only.

**Deleted:**
- Backend: `apps/backend/internal/appauth/repo.go` methods `FindUserByEmail`, `LinkDiscordToUser`, `RefreshDiscordConnection` (replaced or absorbed).
- Backend admin actions (under `apps/backend/internal/admin/actions/`):
  - `users_email_add.go` (+ test)
  - `users_email_remove.go` (+ test)
  - `users_email_set_primary.go` (+ test)
  - `users_email_resend_verification.go` (+ test)
  - `users_password_force_reset.go` (+ test)
  - `users_invite.go` (+ test)
  - `users_connections_unlink.go` (+ test)
- Frontend route handlers:
  - `apps/frontend/app/auth/login/route.ts`
  - `apps/frontend/app/auth/register/route.ts`
  - `apps/frontend/app/auth/email/verify/route.ts`
  - `apps/frontend/app/auth/email/verify/request/route.ts`
  - `apps/frontend/app/auth/password/reset/consume/route.ts`
  - `apps/frontend/app/auth/password/reset/request/route.ts`
- Frontend pages (duplicates / email-only):
  - `apps/frontend/app/sign-in/page.tsx`
  - `apps/frontend/app/sign-up/page.tsx`
  - `apps/frontend/app/signin/page.tsx`
  - `apps/frontend/app/signup/page.tsx`
  - `apps/frontend/app/forgot-password/page.tsx`
  - `apps/frontend/app/reset-password/page.tsx`

**Created:**
- `scripts/admin-role.mjs` — Node CLI for `admin:promote` / `admin:demote` (or wire as `apps/backend/cmd/adminrole/main.go` if Go is the existing ops convention).
- `package.json` scripts: `admin:promote`, `admin:demote`.

---

## Pre-Flight

- [ ] **Verify clean working tree**

Run:
```bash
git status
```
Expected: only the spec edits committed earlier on `development/web-administration`. Stash or commit anything else first.

- [ ] **Confirm `user_connected_accounts` has no non-Discord consumers**

Run:
```bash
grep -rn "user_connected_accounts" apps/backend/internal/ apps/frontend/lib/
```
Expected: every hit is in code paths we're deleting (Discord linking, admin connections-unlink) or in `appauth/repo.go`. If a non-Discord provider appears, STOP and revise the plan to keep the table.

- [ ] **Snapshot the dev DB row counts (sanity)**

Run:
```bash
docker exec infra-postgres-1 psql -U rustlens -d rustlens -c "SELECT count(*) FROM app_users; SELECT count(*) FROM user_emails;"
```
Expected: any value — we're confirming the tables exist and we're about to drop them. Per the spec, no real data is at risk.

---

## Task 1: Wipe dev DB user data (clean slate)

**Files:**
- None — runtime DB only.

- [ ] **Step 1: Drop relevant rows so the schema-bootstrap path can re-create the new shape on next start**

Run:
```bash
docker exec infra-postgres-1 psql -U rustlens -d rustlens -c "
TRUNCATE app_users, user_emails, user_password_credentials,
         email_sign_in_tokens, email_verification_tokens,
         password_reset_tokens, user_connected_accounts CASCADE;"
```
Expected: `TRUNCATE TABLE` for each (or `relation does not exist` if the table is already absent — fine). No errors that abort the transaction.

- [ ] **Step 2: Drop the now-defunct tables so `IF NOT EXISTS` re-create logic doesn't re-make them**

Run:
```bash
docker exec infra-postgres-1 psql -U rustlens -d rustlens -c "
DROP TABLE IF EXISTS user_emails              CASCADE;
DROP TABLE IF EXISTS user_password_credentials CASCADE;
DROP TABLE IF EXISTS email_sign_in_tokens     CASCADE;
DROP TABLE IF EXISTS email_verification_tokens CASCADE;
DROP TABLE IF EXISTS password_reset_tokens    CASCADE;
DROP TABLE IF EXISTS user_connected_accounts  CASCADE;"
```
Expected: `DROP TABLE` for each.

- [ ] **Step 3: No commit yet** — schema code is the source of truth and will be edited in Task 2; the dropped tables would otherwise be re-created on next boot.

---

## Task 2: Update schema bootstrap

**Files:**
- Modify: `apps/backend/internal/store/postgres.go` (around lines 451–620)

- [ ] **Step 1: Read the current schema block**

Open `apps/backend/internal/store/postgres.go` and locate the `app_users` `CREATE TABLE IF NOT EXISTS` (around line 456) and the email/password tables (around lines 465, 474, 587–618).

- [ ] **Step 2: Replace the `app_users` + email/connected-accounts block**

Replace the block starting at the comment `// app_users and the login/connected-account/email tables...` and the `CREATE TABLE IF NOT EXISTS app_users (...)` through the closing ``); err != nil { return fmt.Errorf("create app_users and related tables: %w", err) }` with:

```go
// app_users — Discord-only identity. discord_id is the sole login key.
// user_emails / user_connected_accounts / user_password_credentials and
// the email-token tables are intentionally absent; the email/password
// auth surface was removed in the Discord-only migration.
if _, err := p.pool.Exec(ctx, `
    CREATE TABLE IF NOT EXISTS app_users (
        id                  TEXT PRIMARY KEY,
        discord_id          TEXT UNIQUE NOT NULL,
        discord_username    TEXT NOT NULL,
        discord_global_name TEXT NULL,
        discord_avatar      TEXT NULL,
        display_name        TEXT NOT NULL DEFAULT '',
        avatar_url          TEXT NOT NULL DEFAULT '',
        roles               JSONB NOT NULL DEFAULT '["user"]'::jsonb,
        created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
        updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
        last_login_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
    );
    CREATE INDEX IF NOT EXISTS app_users_discord_id_idx ON app_users (discord_id);
`); err != nil {
    return fmt.Errorf("create app_users: %w", err)
}
```

- [ ] **Step 3: Delete the email/password auth tables block**

Delete the block starting at the comment `// Auth support tables — the full schemas live in the frontend's idempotent migration module.` through the matching error-return that creates `user_password_credentials`, `password_reset_tokens`, `email_verification_tokens`, `email_sign_in_tokens` (lines ~582 to ~622). Remove the entire `p.pool.Exec(ctx, ...)` call and its `if err := ...` wrapper.

- [ ] **Step 4: Add idempotent migration step for already-existing `app_users` rows in dev/test DBs**

Right after the `CREATE TABLE IF NOT EXISTS app_users` block, add a one-shot `ALTER TABLE` that brings older `app_users` rows up to the new shape if the columns are missing. This makes the bootstrap re-runnable in environments that did not get truncated:

```go
if _, err := p.pool.Exec(ctx, `
    ALTER TABLE app_users
        ADD COLUMN IF NOT EXISTS discord_id          TEXT,
        ADD COLUMN IF NOT EXISTS discord_username    TEXT,
        ADD COLUMN IF NOT EXISTS discord_global_name TEXT,
        ADD COLUMN IF NOT EXISTS discord_avatar      TEXT;
`); err != nil {
    return fmt.Errorf("alter app_users add discord columns: %w", err)
}
```

(Note: the `NOT NULL` and `UNIQUE` constraints are only enforced for fresh tables; on re-runs the columns are added nullable so existing dev rows don't fail. After Task 1 truncated the table this is a no-op, but keep the idempotency for CI/test DBs.)

- [ ] **Step 5: Build the backend to confirm compilation**

Run:
```bash
cd apps/backend && go build ./...
```
Expected: success. If any package now fails to compile because it referenced a deleted column or method, stop and note the file — Task 3 covers `appauth/repo.go` rewrites.

- [ ] **Step 6: Commit**

```bash
git add apps/backend/internal/store/postgres.go
git commit -m "refactor(backend): drop email/password tables, add discord identity columns to app_users"
```

---

## Task 3: Rewrite `appauth.Repo` for Discord-only identity

**Files:**
- Modify: `apps/backend/internal/appauth/repo.go`
- Modify: `apps/backend/internal/appauth/repo_discord_test.go`

- [ ] **Step 1: Write the failing test for `FindOrCreateByDiscordID` (new + existing paths)**

Open `apps/backend/internal/appauth/repo_discord_test.go` and replace its body with:

```go
package appauth_test

import (
    "context"
    "testing"

    "rustlens/backend/internal/appauth"
)

func TestFindOrCreateByDiscordID_CreatesNewUser(t *testing.T) {
    ctx := context.Background()
    pool := testPool(t) // existing test helper in helpers_for_test.go
    repo := appauth.NewRepo(pool)

    profile := appauth.DiscordProfile{
        ID:         "111111111111111111",
        Username:   "alice",
        GlobalName: "Alice A.",
        Avatar:     "abc123",
    }
    userID, created, err := repo.FindOrCreateByDiscordID(ctx, profile)
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if !created {
        t.Fatalf("expected created=true on first call")
    }
    if userID == "" {
        t.Fatalf("expected non-empty user id")
    }
}

func TestFindOrCreateByDiscordID_ReturnsExistingUser(t *testing.T) {
    ctx := context.Background()
    pool := testPool(t)
    repo := appauth.NewRepo(pool)

    profile := appauth.DiscordProfile{ID: "222", Username: "bob"}
    first, _, err := repo.FindOrCreateByDiscordID(ctx, profile)
    if err != nil {
        t.Fatalf("first call: %v", err)
    }
    second, created, err := repo.FindOrCreateByDiscordID(ctx, profile)
    if err != nil {
        t.Fatalf("second call: %v", err)
    }
    if created {
        t.Fatalf("expected created=false on returning user")
    }
    if second != first {
        t.Fatalf("expected same user id; got %q vs %q", first, second)
    }
}

func TestFindOrCreateByDiscordID_RefreshesProfileFields(t *testing.T) {
    ctx := context.Background()
    pool := testPool(t)
    repo := appauth.NewRepo(pool)

    initial := appauth.DiscordProfile{ID: "333", Username: "old_name"}
    userID, _, err := repo.FindOrCreateByDiscordID(ctx, initial)
    if err != nil {
        t.Fatalf("initial: %v", err)
    }

    updated := appauth.DiscordProfile{ID: "333", Username: "new_name", GlobalName: "New Name", Avatar: "newhash"}
    if _, _, err := repo.FindOrCreateByDiscordID(ctx, updated); err != nil {
        t.Fatalf("refresh: %v", err)
    }

    var got string
    if err := pool.QueryRow(ctx, `SELECT discord_username FROM app_users WHERE id=$1`, userID).Scan(&got); err != nil {
        t.Fatalf("query: %v", err)
    }
    if got != "new_name" {
        t.Fatalf("expected profile refresh to update discord_username to %q, got %q", "new_name", got)
    }
}
```

(If `testPool` does not already exist as a helper, define it in `helpers_for_test.go` to read `TEST_DATABASE_URL` and call `store.EnsureSchema`.)

- [ ] **Step 2: Run the tests to verify they fail**

Run:
```bash
cd apps/backend && go test ./internal/appauth/ -run FindOrCreateByDiscordID -v
```
Expected: FAIL — `repo.FindOrCreateByDiscordID undefined`.

- [ ] **Step 3: Replace the Discord-related repo methods**

In `apps/backend/internal/appauth/repo.go`, delete:
- `FindUserByDiscordID`
- `RefreshDiscordConnection`
- `FindUserByEmail`
- `CreateDiscordUser`
- `LinkDiscordToUser`

…and add a single replacement:

```go
// FindOrCreateByDiscordID looks up an app_users row by Discord ID. If found,
// it refreshes the cached profile fields and returns (id, false, nil).
// If not found, it inserts a new row with roles=["user"] and the supplied
// profile and returns (id, true, nil).
func (r *Repo) FindOrCreateByDiscordID(ctx context.Context, p DiscordProfile) (string, bool, error) {
    const lookupSQL = `SELECT id FROM app_users WHERE discord_id = $1`
    const refreshSQL = `
        UPDATE app_users
        SET discord_username    = $2,
            discord_global_name = NULLIF($3, ''),
            discord_avatar      = NULLIF($4, ''),
            updated_at          = NOW(),
            last_login_at       = NOW()
        WHERE id = $1`
    const insertSQL = `
        INSERT INTO app_users (id, discord_id, discord_username, discord_global_name, discord_avatar, roles)
        VALUES ($1, $2, $3, NULLIF($4, ''), NULLIF($5, ''), '["user"]'::jsonb)`

    var id string
    err := r.pool.QueryRow(ctx, lookupSQL, p.ID).Scan(&id)
    switch {
    case err == nil:
        if _, err := r.pool.Exec(ctx, refreshSQL, id, p.Username, p.GlobalName, p.Avatar); err != nil {
            return "", false, fmt.Errorf("refresh profile: %w", err)
        }
        return id, false, nil
    case errors.Is(err, pgx.ErrNoRows):
        newID, err := newUserID()
        if err != nil {
            return "", false, fmt.Errorf("new id: %w", err)
        }
        if _, err := r.pool.Exec(ctx, insertSQL, newID, p.ID, p.Username, p.GlobalName, p.Avatar); err != nil {
            return "", false, fmt.Errorf("insert app_users: %w", err)
        }
        return newID, true, nil
    default:
        return "", false, fmt.Errorf("lookup: %w", err)
    }
}

// newUserID returns a "usr_<24 hex>" identifier.
func newUserID() (string, error) {
    var b [12]byte
    if _, err := rand.Read(b[:]); err != nil {
        return "", err
    }
    return "usr_" + hex.EncodeToString(b[:]), nil
}
```

(If `newUserID` already exists in the file, reuse it instead of redefining.)

- [ ] **Step 4: Run the tests to verify they pass**

Run:
```bash
cd apps/backend && go test ./internal/appauth/ -run FindOrCreateByDiscordID -v
```
Expected: 3 PASSes.

- [ ] **Step 5: Build full backend; expect compile errors in callers**

Run:
```bash
cd apps/backend && go build ./...
```
Expected: compile errors in `oauth/discord/handlers.go` (uses `FindUserByDiscordID`, `CreateDiscordUser`, `LinkDiscordToUser`, `RefreshDiscordConnection`, `FindUserByEmail`). Those are fixed in Task 4.

- [ ] **Step 6: Commit (tests first, then impl is fine — but commit together since they ship as a unit)**

```bash
git add apps/backend/internal/appauth/repo.go apps/backend/internal/appauth/repo_discord_test.go
git commit -m "refactor(backend): collapse Discord repo methods to FindOrCreateByDiscordID"
```

---

## Task 4: Collapse Discord OAuth callback to a single sign-in branch

**Files:**
- Modify: `apps/backend/internal/oauth/discord/handlers.go`
- Modify: `apps/backend/internal/oauth/discord/handlers_test.go`
- Modify: `apps/backend/cmd/server/main.go` (route registrations)

- [ ] **Step 1: Read the current handler file end-to-end**

Run:
```bash
wc -l apps/backend/internal/oauth/discord/handlers.go
```
Then `cat` (or `Read`) the file. Identify: `Initiate`, `InitiateLink`, `Callback`, and any helper that branches on `IntentSignin` vs `IntentLink`.

- [ ] **Step 2: Delete `InitiateLink` and the link branch in `Callback`**

Remove:
- The entire `InitiateLink` method.
- Any `IntentLink` constant.
- The `case IntentLink:` (or equivalent) block inside `Callback` / its helpers.
- All references to `repo.LinkDiscordToUser`, `repo.FindUserByEmail`, `repo.CreateDiscordUser`, `repo.RefreshDiscordConnection`, `repo.FindUserByDiscordID`.

Replace the post-token-exchange logic with a single call:

```go
userID, _, err := h.cfg.Repo.FindOrCreateByDiscordID(r.Context(), profile)
if err != nil {
    http.Redirect(w, r, h.frontendURL("/login?error=discord-auth"), http.StatusFound)
    return
}
// existing session-mint logic uses userID
```

- [ ] **Step 3: Update / trim `handlers_test.go`**

Delete tests that exercise the link branch or email-conflict behavior (anything testing `discord-no-email`, `discord-email-in-use`, `IntentLink`). Update the sign-in tests to call the new repo method via the existing test fake. If the existing tests use a repo interface, make sure the fake exposes `FindOrCreateByDiscordID` and remove the old methods from it.

Add (or keep one) sign-in test:

```go
func TestCallback_SignsInExistingUser(t *testing.T) {
    // existing helpers — adapt as needed
    h, cleanup := newTestHandler(t)
    defer cleanup()
    // …seed an app_users row with discord_id="999"…
    rr := callCallback(t, h, fakeCodeForDiscordID("999"))
    if rr.Code != http.StatusFound {
        t.Fatalf("expected 302; got %d", rr.Code)
    }
    if !strings.HasSuffix(rr.Header().Get("Location"), "/dashboard") {
        t.Fatalf("expected redirect to /dashboard; got %q", rr.Header().Get("Location"))
    }
    // assert session cookie set
    if cookie := rr.Header().Get("Set-Cookie"); !strings.Contains(cookie, "session=") {
        t.Fatalf("expected session cookie to be set")
    }
}
```

- [ ] **Step 4: Remove the `/api/auth/discord/link` route registration**

Open `apps/backend/cmd/server/main.go` and delete the route line for `InitiateLink` (search for `discord/link`). Leave `/api/auth/discord` (Initiate) and the callback route in place.

- [ ] **Step 5: Build + run Discord package tests**

Run:
```bash
cd apps/backend && go build ./... && go test ./internal/oauth/discord/ -v
```
Expected: build succeeds; tests pass.

- [ ] **Step 6: Commit**

```bash
git add apps/backend/internal/oauth/discord/ apps/backend/cmd/server/main.go
git commit -m "refactor(backend): collapse Discord OAuth callback to single sign-in branch"
```

---

## Task 5: Trim `CurrentUser` response shape

**Files:**
- Modify: `apps/backend/internal/appauth/repo.go` (struct definitions near top)
- Modify: `apps/backend/internal/appauth/handlers.go` (whatever assembles `CurrentUser`)
- Modify: `apps/frontend/lib/rustlens-api.ts`, `apps/frontend/lib/session.ts` (consumer types)

- [ ] **Step 1: Find every consumer of the trimmed fields**

Run:
```bash
grep -rn "PrimaryEmail\|primaryEmail\|\.Emails\|UserEmail\|ConnectedAccount\|connectedAccount\|DiscordAccount\|discordAccount" apps/backend/internal/ apps/frontend/lib/ apps/frontend/components/ apps/frontend/app/
```
Note every hit. These all need updates or deletions.

- [ ] **Step 2: Edit the Go types in `repo.go`**

In `apps/backend/internal/appauth/repo.go`:

- Delete `UserEmail` struct.
- Delete `ConnectedAccount` struct (Discord identity now lives on `AppUser` directly).
- Update `AppUser` to expose Discord fields:

```go
type AppUser struct {
    ID                string    `json:"id"`
    DiscordID         string    `json:"discordId"`
    DiscordUsername   string    `json:"discordUsername"`
    DiscordGlobalName string    `json:"discordGlobalName,omitempty"`
    DiscordAvatar     string    `json:"discordAvatar,omitempty"`
    DisplayName       string    `json:"displayName"`
    AvatarURL         string    `json:"avatarUrl"`
    Roles             []string  `json:"roles"`
    CreatedAt         time.Time `json:"createdAt"`
    UpdatedAt         time.Time `json:"updatedAt"`
    LastLoginAt       time.Time `json:"lastLoginAt"`
    SessionEpoch      int       `json:"sessionEpoch"`
}
```

- Update `CurrentUser`:

```go
type CurrentUser struct {
    User          AppUser               `json:"user"`
    SteamAccounts []LinkedSteamAccount  `json:"steamAccounts"`
    Guilds        []UserGuildMembership `json:"guilds"`
    AdminRoles    []string              `json:"adminRoles"`
}
```

- [ ] **Step 3: Update the handler/repo code that builds `CurrentUser`**

Wherever `PrimaryEmail`, `Emails`, `DiscordAccount` are populated (likely in `handlers.go` or `repo.go` `LoadCurrentUser`-style method), delete those queries/assignments. Replace with the Discord fields read directly from `app_users`.

- [ ] **Step 4: Update the frontend session/api types**

In `apps/frontend/lib/rustlens-api.ts` and `apps/frontend/lib/session.ts`, find the matching TypeScript types for `AppUser` / `CurrentUser` / `UserEmail` / `ConnectedAccount`. Mirror the backend changes:
- Delete `UserEmail`, `ConnectedAccount` types.
- Update `AppUser` to add `discordId`, `discordUsername`, `discordGlobalName?`, `discordAvatar?`.
- Update `CurrentUser` to remove `primaryEmail`, `emails`, `discordAccount`.

- [ ] **Step 5: Fix component-level consumers**

For each hit from Step 1 in `apps/frontend/components/` or `apps/frontend/app/`:
- If the file is being deleted in Tasks 9–11 anyway, skip it.
- Otherwise replace `currentUser.discordAccount?.username` with `currentUser.user.discordUsername`, etc.

- [ ] **Step 6: Build and typecheck**

Run:
```bash
cd apps/backend && go build ./...
cd ../frontend && pnpm typecheck
```
Expected: both pass. Resolve any remaining type errors before continuing.

- [ ] **Step 7: Commit**

```bash
git add apps/backend/internal/appauth/ apps/frontend/lib/ apps/frontend/components/ apps/frontend/app/
git commit -m "refactor: trim CurrentUser to Discord-only identity shape"
```

---

## Task 6: Delete email/password backend handlers and routes

**Files:**
- Modify: `apps/backend/internal/appauth/handlers.go`
- Modify: `apps/backend/internal/appauth/middleware.go`
- Modify: `apps/backend/cmd/server/main.go`
- Delete (if dedicated): any `appauth/email_*.go`, `appauth/password_*.go`, `appauth/token_*.go` files.

- [ ] **Step 1: Locate all email/password handler functions**

Run:
```bash
grep -n "func.*Login\|func.*Register\|func.*VerifyEmail\|func.*ResetPassword\|func.*ConsumePasswordReset\|func.*RequestPasswordReset\|func.*RequestEmailVerify\|func.*ConsumeEmailVerify\|bcrypt" apps/backend/internal/appauth/
```
List every match. These are the deletion targets.

- [ ] **Step 2: Delete them**

For each handler function listed, delete the function and any helpers used only by it (token issuance, bcrypt wrapper, email-token consume helpers).

- [ ] **Step 3: Remove the route registrations in `cmd/server/main.go`**

Open `apps/backend/cmd/server/main.go` and delete every chi `r.Post`, `r.Get`, etc. that pointed at the deleted handlers. Search for `auth.login`, `auth.register`, `auth.email`, `auth.password` to find them.

- [ ] **Step 4: Drop the `golang.org/x/crypto/bcrypt` import if it has no remaining users**

Run:
```bash
grep -rn "bcrypt" apps/backend/internal/
```
If the only remaining hit was inside the now-deleted code, run `goimports -w` on touched files (or remove the import manually).

- [ ] **Step 5: Build**

Run:
```bash
cd apps/backend && go build ./...
```
Expected: success.

- [ ] **Step 6: Run all backend tests**

Run:
```bash
cd apps/backend && go test ./...
```
Expected: PASS, OR fail only in `internal/admin/actions/users_email_*` and `users_password_force_reset_*` (those tests will be deleted with their actions in Task 7) and `internal/appauth/middleware_cookie_test.go` if it asserts an email path. Note the failures and proceed.

- [ ] **Step 7: Commit**

```bash
git add apps/backend/
git commit -m "refactor(backend): delete email/password auth handlers and routes"
```

---

## Task 7: Delete email/password admin actions

**Files:**
- Delete:
  - `apps/backend/internal/admin/actions/users_email_add.go` + `_test.go`
  - `apps/backend/internal/admin/actions/users_email_remove.go` + `_test.go`
  - `apps/backend/internal/admin/actions/users_email_set_primary.go` + `_test.go`
  - `apps/backend/internal/admin/actions/users_email_resend_verification.go` + `_test.go`
  - `apps/backend/internal/admin/actions/users_password_force_reset.go` + `_test.go`
  - `apps/backend/internal/admin/actions/users_invite.go` + `_test.go`
  - `apps/backend/internal/admin/actions/users_connections_unlink.go` + `_test.go`
- Modify: the registry that registers these actions (search for the file).

- [ ] **Step 1: Locate the action registry**

Run:
```bash
grep -rn "users_email_add\|users.email.add\|UsersEmailAdd\|RegisterAction" apps/backend/internal/admin/
```
Find the file that registers actions into the dispatcher's `Registry`. (It is likely `apps/backend/internal/admin/actions/registry.go` or wired up in `cmd/server/main.go`.)

- [ ] **Step 2: Delete the seven action files and their tests**

Run:
```bash
rm apps/backend/internal/admin/actions/users_email_add.go apps/backend/internal/admin/actions/users_email_add_test.go
rm apps/backend/internal/admin/actions/users_email_remove.go apps/backend/internal/admin/actions/users_email_remove_test.go
rm apps/backend/internal/admin/actions/users_email_set_primary.go apps/backend/internal/admin/actions/users_email_set_primary_test.go
rm apps/backend/internal/admin/actions/users_email_resend_verification.go apps/backend/internal/admin/actions/users_email_resend_verification_test.go
rm apps/backend/internal/admin/actions/users_password_force_reset.go apps/backend/internal/admin/actions/users_password_force_reset_test.go
rm apps/backend/internal/admin/actions/users_invite.go apps/backend/internal/admin/actions/users_invite_test.go
rm apps/backend/internal/admin/actions/users_connections_unlink.go apps/backend/internal/admin/actions/users_connections_unlink_test.go
```

- [ ] **Step 3: Remove the registry entries**

In the registry file from Step 1, delete every line that registers any of: `users.email.add`, `users.email.remove`, `users.email.set_primary`, `users.email.resend_verification`, `users.password.force_reset`, `users.invite`, `users.connections.unlink`. Also delete any `import` lines that become unused.

- [ ] **Step 4: Update `users_bulk` and any other actions that referenced the deleted ones**

Run:
```bash
grep -rn "users.email\|users.password.force_reset\|users.invite\|users.connections.unlink" apps/backend/internal/admin/
```
For each remaining hit (likely in `users_bulk.go` or the dispatcher's action-type allow-list), remove the reference.

- [ ] **Step 5: Build + test**

Run:
```bash
cd apps/backend && go build ./... && go test ./internal/admin/...
```
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add apps/backend/
git commit -m "refactor(backend): delete email/password admin actions"
```

---

## Task 8: Replace login UI (Discord-only)

**Files:**
- Modify: `apps/frontend/components/auth-page.tsx`
- Modify: `apps/frontend/components/auth-page.test.tsx`
- Modify: `apps/frontend/app/login/page.tsx` (verify only)

- [ ] **Step 1: Update the auth-page test for the new behavior**

Open `apps/frontend/components/auth-page.test.tsx`. Replace its body with tests asserting:
- Renders a "Sign in with Discord" button.
- Renders the error banner for known error codes (`cancelled`, `unavailable`, `disabled`).
- Does NOT render any email or password input.

```tsx
import { render, screen } from "@testing-library/react";
import { AuthPage } from "./auth-page";

describe("AuthPage", () => {
  it("renders a Discord sign-in button", () => {
    render(<AuthPage csrfToken="t" />);
    expect(screen.getByRole("link", { name: /sign in with discord/i })).toBeInTheDocument();
  });

  it("does not render email or password inputs", () => {
    render(<AuthPage csrfToken="t" />);
    expect(screen.queryByLabelText(/email/i)).toBeNull();
    expect(screen.queryByLabelText(/password/i)).toBeNull();
  });

  it("shows the cancelled-error message when error=cancelled", () => {
    render(<AuthPage csrfToken="t" error="cancelled" />);
    expect(screen.getByText(/sign-in cancelled/i)).toBeInTheDocument();
  });

  it("shows the unavailable-error message when error=unavailable", () => {
    render(<AuthPage csrfToken="t" error="unavailable" />);
    expect(screen.getByText(/temporarily unavailable/i)).toBeInTheDocument();
  });

  it("shows the disabled-error message when error=disabled", () => {
    render(<AuthPage csrfToken="t" error="disabled" />);
    expect(screen.getByText(/account disabled/i)).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: Run the tests to verify they fail**

Run:
```bash
cd apps/frontend && pnpm test auth-page
```
Expected: FAIL (existing component still has email/password modes).

- [ ] **Step 3: Replace `auth-page.tsx` with a Discord-only version**

Replace the full file with:

```tsx
import { IconBrandDiscordFilled } from "@tabler/icons-react";
import { Button } from "@/components/ui/button";

type AuthPageProps = {
  error?: string;
  notice?: string;
  csrfToken: string;
};

function getErrorMessage(error?: string) {
  switch (error) {
    case "cancelled":
      return "Sign-in cancelled.";
    case "unavailable":
      return "Discord sign-in is temporarily unavailable, try again.";
    case "disabled":
      return "Account disabled.";
    case "discord-config":
      return "Discord OAuth is not configured yet.";
    case "discord-state":
      return "The Discord sign-in state was invalid or expired.";
    case "discord-token":
    case "discord-profile":
    case "discord-auth":
      return "Discord sign-in failed. Try again.";
    case "discord-already-signed-in":
      return "You're already signed in.";
    default:
      return error ? "Sign-in failed. Try again." : null;
  }
}

export function AuthPage({ error, notice, csrfToken }: AuthPageProps) {
  const errorMessage = getErrorMessage(error);
  return (
    <main className="flex min-h-screen flex-col items-center justify-center gap-6 p-6">
      <h1 className="text-2xl font-semibold">Sign in</h1>
      {errorMessage ? (
        <div role="alert" className="rounded-md bg-destructive/10 px-4 py-2 text-sm text-destructive">
          {errorMessage}
        </div>
      ) : null}
      {notice ? (
        <div role="status" className="rounded-md bg-muted px-4 py-2 text-sm">
          {notice}
        </div>
      ) : null}
      <Button asChild size="lg">
        <a href={`/api/auth/discord?csrf=${encodeURIComponent(csrfToken)}`}>
          <IconBrandDiscordFilled className="mr-2 size-5" />
          Sign in with Discord
        </a>
      </Button>
    </main>
  );
}
```

- [ ] **Step 4: Update the login page if it passes a `showDiscord` prop**

In `apps/frontend/app/login/page.tsx`, remove the `showDiscord` prop (now implicit). If `getCsrfToken` is no longer needed because the Discord initiate route handles its own CSRF via state cookie, simplify accordingly — but if existing tests/middleware rely on the CSRF token, keep passing it.

```tsx
import { AuthPage } from "@/components/auth-page";
import { getCsrfToken } from "@/lib/csrf";

export default async function LoginPage({
  searchParams,
}: {
  searchParams?: Promise<Record<string, string | string[] | undefined>>;
}) {
  const resolvedSearchParams = (await searchParams) ?? {};
  const error = Array.isArray(resolvedSearchParams.error) ? resolvedSearchParams.error[0] : resolvedSearchParams.error;
  const notice = Array.isArray(resolvedSearchParams.notice) ? resolvedSearchParams.notice[0] : resolvedSearchParams.notice;
  const csrfToken = await getCsrfToken();
  return <AuthPage csrfToken={csrfToken} error={error} notice={notice} />;
}
```

- [ ] **Step 5: Run the tests**

Run:
```bash
cd apps/frontend && pnpm test auth-page
```
Expected: PASS (5 tests).

- [ ] **Step 6: Commit**

```bash
git add apps/frontend/components/auth-page.tsx apps/frontend/components/auth-page.test.tsx apps/frontend/app/login/page.tsx
git commit -m "refactor(frontend): replace AuthPage with Discord-only sign-in"
```

---

## Task 9: Delete frontend email/password route handlers

**Files:**
- Delete:
  - `apps/frontend/app/auth/login/route.ts`
  - `apps/frontend/app/auth/register/route.ts`
  - `apps/frontend/app/auth/email/verify/route.ts`
  - `apps/frontend/app/auth/email/verify/request/route.ts`
  - `apps/frontend/app/auth/password/reset/consume/route.ts`
  - `apps/frontend/app/auth/password/reset/request/route.ts`
  - The empty parent dirs (`apps/frontend/app/auth/email`, `apps/frontend/app/auth/password`, `apps/frontend/app/auth/login`, `apps/frontend/app/auth/register`).

- [ ] **Step 1: Delete the route files**

Run:
```bash
rm apps/frontend/app/auth/login/route.ts
rm apps/frontend/app/auth/register/route.ts
rm apps/frontend/app/auth/email/verify/route.ts
rm apps/frontend/app/auth/email/verify/request/route.ts
rm apps/frontend/app/auth/password/reset/consume/route.ts
rm apps/frontend/app/auth/password/reset/request/route.ts
```

- [ ] **Step 2: Remove now-empty parent dirs**

Run:
```bash
rmdir apps/frontend/app/auth/login apps/frontend/app/auth/register \
      apps/frontend/app/auth/email/verify/request apps/frontend/app/auth/email/verify apps/frontend/app/auth/email \
      apps/frontend/app/auth/password/reset/consume apps/frontend/app/auth/password/reset apps/frontend/app/auth/password \
      apps/frontend/app/auth 2>&1 || true
```
(`|| true` because if anything else lives under `apps/frontend/app/auth/` we leave it alone.)

- [ ] **Step 3: Build + typecheck**

Run:
```bash
cd apps/frontend && pnpm typecheck && pnpm build
```
Expected: success.

- [ ] **Step 4: Commit**

```bash
git add -A apps/frontend/app/auth/
git commit -m "refactor(frontend): delete email/password auth route handlers"
```

---

## Task 10: Delete duplicate / email-only frontend pages

**Files:**
- Delete:
  - `apps/frontend/app/sign-in/page.tsx` + dir
  - `apps/frontend/app/sign-up/page.tsx` + dir
  - `apps/frontend/app/signin/page.tsx` + dir
  - `apps/frontend/app/signup/page.tsx` + dir
  - `apps/frontend/app/forgot-password/page.tsx` + dir
  - `apps/frontend/app/reset-password/page.tsx` + dir

- [ ] **Step 1: Confirm none of the duplicates are linked from kept code**

Run:
```bash
grep -rn "sign-in\|/sign-up\|/signin\|/signup\|forgot-password\|reset-password" apps/frontend/app apps/frontend/components apps/frontend/lib
```
Note any links. Update them to point at `/login` (the single retained sign-in page) before deleting the targets.

- [ ] **Step 2: Delete the directories**

Run:
```bash
rm -rf apps/frontend/app/sign-in apps/frontend/app/sign-up apps/frontend/app/signin apps/frontend/app/signup apps/frontend/app/forgot-password apps/frontend/app/reset-password
```

- [ ] **Step 3: Build + typecheck**

Run:
```bash
cd apps/frontend && pnpm typecheck && pnpm build
```
Expected: success.

- [ ] **Step 4: Commit**

```bash
git add -A apps/frontend/app/
git commit -m "refactor(frontend): delete duplicate / email-only auth pages"
```

---

## Task 11: Replace Discord settings panel

**Files:**
- Modify (or replace): the settings page that renders the Discord connection panel (commit `3051191` added it; locate via `grep`).

- [ ] **Step 1: Find the panel**

Run:
```bash
grep -rn "Discord" apps/frontend/app/dashboard apps/frontend/components | grep -iE "settings|connection|panel"
```

- [ ] **Step 2: Replace its content with a read-only block**

The panel should show:
- Discord avatar (`currentUser.user.discordAvatar`, fall back to default)
- Display: `currentUser.user.discordGlobalName ?? currentUser.user.discordUsername`
- Subtitle: `@${currentUser.user.discordUsername}`
- A "Sign out" button that links to the existing `/logout` route handler.

There is no "Disconnect" or "Re-link" button — Discord IS the account.

- [ ] **Step 3: Build + typecheck**

Run:
```bash
cd apps/frontend && pnpm typecheck
```

- [ ] **Step 4: Commit**

```bash
git add apps/frontend/
git commit -m "refactor(frontend): replace Discord connection panel with read-only profile block"
```

---

## Task 12: Add `pnpm admin:promote` / `admin:demote` CLI

**Files:**
- Create: `scripts/admin-role.mjs`
- Modify: root `package.json` (add scripts)

- [ ] **Step 1: Create the script**

Write `scripts/admin-role.mjs`:

```js
#!/usr/bin/env node
// Promote or demote an app_users role by Discord ID.
// Usage:
//   pnpm admin:promote <discord_id> [role]   (default role: internal_admin)
//   pnpm admin:demote  <discord_id> <role>
//
// Reads DATABASE_URL from the environment (same env the backend uses).

import { createRequire } from "node:module";
const require = createRequire(import.meta.url);
const { Client } = require("pg");

const action = process.argv[2]; // "promote" | "demote"
const discordId = process.argv[3];
const role = process.argv[4] ?? "internal_admin";

if (!action || !["promote", "demote"].includes(action) || !discordId) {
  console.error("Usage: admin-role.mjs <promote|demote> <discord_id> [role]");
  process.exit(1);
}

const url = process.env.DATABASE_URL;
if (!url) {
  console.error("DATABASE_URL must be set");
  process.exit(1);
}

const client = new Client({ connectionString: url });
await client.connect();

const { rows } = await client.query(
  "SELECT id, roles FROM app_users WHERE discord_id = $1",
  [discordId],
);
if (rows.length === 0) {
  console.error(`No app_users row with discord_id=${discordId}. Sign in via Discord first.`);
  await client.end();
  process.exit(2);
}
const { id, roles: current } = rows[0];
const before = Array.isArray(current) ? current : JSON.parse(current);
let next;
if (action === "promote") {
  next = before.includes(role) ? before : [...before, role];
} else {
  next = before.filter((r) => r !== role);
}
await client.query(
  "UPDATE app_users SET roles = $1::jsonb, updated_at = NOW() WHERE id = $2",
  [JSON.stringify(next), id],
);
console.log(`user=${id} discord_id=${discordId}`);
console.log(`  before: ${JSON.stringify(before)}`);
console.log(`  after:  ${JSON.stringify(next)}`);
await client.end();
```

- [ ] **Step 2: Add scripts to root `package.json`**

Open `package.json` (root) and add inside `"scripts"`:

```json
"admin:promote": "node scripts/admin-role.mjs promote",
"admin:demote":  "node scripts/admin-role.mjs demote"
```

- [ ] **Step 3: Verify `pg` is available**

Run:
```bash
pnpm list pg --depth=-1
```
Expected: a version is listed somewhere (root or workspace). If not:

```bash
pnpm add -w -D pg
```

- [ ] **Step 4: Smoke-test against the dev DB**

Sign in via Discord first to seed an `app_users` row, then run:

```bash
DATABASE_URL=postgres://rustlens:rustlens@localhost:5432/rustlens pnpm admin:promote <your_discord_id>
DATABASE_URL=postgres://rustlens:rustlens@localhost:5432/rustlens pnpm admin:demote <your_discord_id> internal_admin
```
Expected: prints `before` and `after` arrays. The `before` of the demote should match the `after` of the promote.

- [ ] **Step 5: Commit**

```bash
git add scripts/admin-role.mjs package.json
git commit -m "feat(scripts): add admin:promote and admin:demote CLI"
```

---

## Task 13: End-to-end smoke

**Files:**
- None — runtime verification only.

- [ ] **Step 1: Restart the stack**

Bring the backend and frontend up against the freshly-truncated DB.

- [ ] **Step 2: Visit `/login`**

Expected: only the Discord button renders. No email/password fields. Error/notice banners render correctly when query params are set.

- [ ] **Step 3: Sign in via Discord (new account path)**

Expected: redirected to dashboard. Browser session cookie set. `psql` confirms a single new `app_users` row with `discord_id`, `discord_username`, `roles=["user"]` populated.

- [ ] **Step 4: Sign out, sign in again (returning account path)**

Expected: same `app_users.id` as Step 3. `last_login_at` updated. `discord_username` matches the current Discord state.

- [ ] **Step 5: Promote yourself with the CLI, then verify admin panel access**

Run:
```bash
DATABASE_URL=... pnpm admin:promote <your_discord_id>
```
Then refresh the dashboard. Expected: admin links/panels appear (per the existing RBAC dispatcher).

- [ ] **Step 6: Verify deleted routes return 404**

Run:
```bash
curl -i http://localhost:3000/auth/login
curl -i http://localhost:3000/sign-in
curl -i http://localhost:3000/forgot-password
curl -i http://localhost:3000/api/auth/discord/link
```
Expected: each returns 404 (frontend) or 404 / route-not-found (backend).

- [ ] **Step 7: Run full test suites once**

Run:
```bash
cd apps/backend && go test ./...
cd ../frontend && pnpm test && pnpm typecheck && pnpm build
```
Expected: all pass.

- [ ] **Step 8: Final commit (if any cleanup edits needed)**

If Steps 1–7 surfaced any small fix (typo, missed reference), commit:

```bash
git add -A
git commit -m "fix: post-migration cleanup from smoke test"
```

---

## Out of Scope (per spec)

- In-app role editor UI — CLI / direct SQL is sufficient.
- Account deletion / data export.
- Re-introducing email capture (schema change reversible if needed).
- Migrating real user data — there is none.
