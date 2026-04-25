# Discord OAuth Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build backend-mediated Discord OAuth 2.0 with PKCE for sign-in (auto-create or auto-link by verified email) and account linking (attach Discord to current session user).

**Architecture:** All OAuth work moves to the Go backend. Three new HTTP routes under `/api/auth/discord/*` (initiate sign-in, initiate link, shared callback) handle PKCE, code exchange against Discord, profile fetch, and `app_users` upsert. The backend mints the same `rustlens_session` HMAC-signed cookie the frontend uses (via `appauth.SerializeSession`), so no frontend code is needed in the auth path. The existing scaffolded Next.js Discord routes and the `completeDiscordAuth` helper are deleted.

**Tech Stack:** Go 1.24, chi v5, pgx v5 (Postgres 16), `crypto/rand`, `crypto/sha256`, standard `net/http` + `net/http/httptest`. Frontend: Next.js 16 / React 19 (UI surface only).

**Spec:** `docs/superpowers/specs/2026-04-24-discord-oauth-design.md`.

---

## Conventions used by this plan

- Run all backend tests with `cd apps/backend && go test ./...`. Repo-level tests skip when `TEST_POSTGRES_URL` is unset, matching the existing pattern in `internal/appauth/session_test.go`.
- New user IDs follow the existing `usr_<24-hex>` shape (see `apps/backend/internal/admin/actions/users_invite.go:61`).
- Browser-redirect handlers use `http.SetCookie(w, ...)` followed by `http.Redirect(w, r, target, http.StatusFound)`.
- Cookies cleared on the callback are set with `MaxAge: -1` and the same `Path: "/"`.
- All git commits use the project format (`feat:`, `fix:`, `refactor:`, `docs:`).

---

## Task 1: Add `AdminRoles` to backend `SessionUser`

The frontend's `SessionUser` payload includes `adminRoles`; the Go struct currently doesn't. We need it so the backend-minted token after sign-in carries the same shape the frontend expects.

**Files:**
- Modify: `apps/backend/internal/appauth/session.go`
- Test: `apps/backend/internal/appauth/session_test.go`

- [ ] **Step 1: Write the failing test**

Append to `apps/backend/internal/appauth/session_test.go`:

```go
func TestSerializeSession_RoundTripsAdminRoles(t *testing.T) {
	secret := "test-secret"
	in := appauth.SessionUser{
		UserID:      "usr_admin_round_trip",
		Roles:       []string{"user"},
		AdminRoles:  []string{"superadmin"},
		Permissions: []string{"users.read"},
		Epoch:       0,
		ExpiresAt:   time.Now().Add(time.Hour).UnixMilli(),
	}
	tok, err := appauth.SerializeSession(in, secret)
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}
	got, err := appauth.ParseSession(tok, secret)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(got.AdminRoles) != 1 || got.AdminRoles[0] != "superadmin" {
		t.Errorf("AdminRoles = %v, want [superadmin]", got.AdminRoles)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```
cd apps/backend && go test ./internal/appauth/ -run TestSerializeSession_RoundTripsAdminRoles -v
```
Expected: compile error — `AdminRoles` field does not exist on `appauth.SessionUser`.

- [ ] **Step 3: Add the field**

In `apps/backend/internal/appauth/session.go`, modify the `SessionUser` struct:

```go
type SessionUser struct {
	UserID      string   `json:"userId"`
	DisplayName string   `json:"displayName"`
	AvatarURL   string   `json:"avatarUrl"`
	Roles       []string `json:"roles"`
	AdminRoles  []string `json:"adminRoles,omitempty"`
	Permissions []string `json:"permissions,omitempty"`
	DiscordID   string   `json:"discordId,omitempty"`
	Email       string   `json:"email,omitempty"`
	Epoch       int      `json:"epoch"`
	ExpiresAt   int64    `json:"expiresAt"`
}
```

- [ ] **Step 4: Run tests to verify pass**

```
cd apps/backend && go test ./internal/appauth/ -v
```
Expected: PASS.

- [ ] **Step 5: Commit**

```
git add apps/backend/internal/appauth/session.go apps/backend/internal/appauth/session_test.go
git commit -m "feat(backend): add AdminRoles to SessionUser"
```

---

## Task 2: Backend config — `DISCORD_CLIENT_ID`, `DISCORD_CLIENT_SECRET`, `APP_ORIGIN`

`APP_ORIGIN` is required for building the OAuth redirect URI and the post-callback frontend redirects.

**Files:**
- Modify: `apps/backend/internal/config/config.go`
- Test: `apps/backend/internal/config/config_test.go`
- Modify: `apps/backend/.env.example`
- Modify: `apps/backend/.env`

- [ ] **Step 1: Write failing test**

Append to `apps/backend/internal/config/config_test.go`:

```go
func TestLoad_DiscordOAuth(t *testing.T) {
	t.Setenv("DISCORD_CLIENT_ID", "id-123")
	t.Setenv("DISCORD_CLIENT_SECRET", "secret-456")
	t.Setenv("APP_ORIGIN", "http://localhost")
	cfg := config.Load()
	if cfg.DiscordClientID != "id-123" {
		t.Errorf("DiscordClientID = %q, want id-123", cfg.DiscordClientID)
	}
	if cfg.DiscordClientSecret != "secret-456" {
		t.Errorf("DiscordClientSecret = %q, want secret-456", cfg.DiscordClientSecret)
	}
	if cfg.AppOrigin != "http://localhost" {
		t.Errorf("AppOrigin = %q, want http://localhost", cfg.AppOrigin)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```
cd apps/backend && go test ./internal/config/ -run TestLoad_DiscordOAuth -v
```
Expected: compile error — fields don't exist.

- [ ] **Step 3: Add fields and load values**

In `apps/backend/internal/config/config.go`, extend the struct:

```go
type Config struct {
	// ... existing fields ...
	DiscordClientID     string
	DiscordClientSecret string
	AppOrigin           string
}
```

In `Load()`, append before `return Config{...}`'s closing brace:

```go
		DiscordClientID:     getEnv("DISCORD_CLIENT_ID", ""),
		DiscordClientSecret: getEnv("DISCORD_CLIENT_SECRET", ""),
		AppOrigin:           getEnv("APP_ORIGIN", "http://localhost"),
```

- [ ] **Step 4: Run test to verify it passes**

```
cd apps/backend && go test ./internal/config/ -v
```
Expected: PASS.

- [ ] **Step 5: Update `.env.example` and `.env`**

Append to `apps/backend/.env.example`:

```
DISCORD_CLIENT_ID=
DISCORD_CLIENT_SECRET=
APP_ORIGIN=http://localhost
```

Append the same three lines to `apps/backend/.env` (with empty values for client id/secret — user will fill in from Discord developer portal).

- [ ] **Step 6: Commit**

```
git add apps/backend/internal/config/ apps/backend/.env.example apps/backend/.env
git commit -m "feat(backend): add Discord OAuth and APP_ORIGIN config"
```

---

## Task 3: Repo method `FindUserByDiscordID`

Returns the `app_users.id` mapped to a Discord account ID, or `ErrUserNotFound`.

**Files:**
- Modify: `apps/backend/internal/appauth/repo.go`
- Test: `apps/backend/internal/appauth/repo_discord_test.go` (new)

- [ ] **Step 1: Write the failing test**

Create `apps/backend/internal/appauth/repo_discord_test.go`:

```go
package appauth_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"rustlens/backend/internal/appauth"
	"rustlens/backend/internal/store"
)

func discordTestStore(t *testing.T) *store.PostgresStore {
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
		t.Fatalf("schema: %v", err)
	}
	return s
}

func TestRepo_FindUserByDiscordID(t *testing.T) {
	s := discordTestStore(t)
	ctx := context.Background()
	repo := appauth.NewRepo(s.Pool())

	const userID = "usr_disc_find_1"
	if _, err := s.Pool().Exec(ctx,
		`INSERT INTO app_users (id, display_name) VALUES ($1, 'D') ON CONFLICT (id) DO NOTHING`, userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := s.Pool().Exec(ctx,
		`INSERT INTO user_connected_accounts (user_id, provider, provider_account_id, username)
		 VALUES ($1, 'discord', '999111', 'd_user')
		 ON CONFLICT (provider, provider_account_id) DO NOTHING`, userID); err != nil {
		t.Fatalf("seed conn: %v", err)
	}

	got, err := repo.FindUserByDiscordID(ctx, "999111")
	if err != nil {
		t.Fatalf("FindUserByDiscordID: %v", err)
	}
	if got != userID {
		t.Errorf("got %q, want %q", got, userID)
	}

	_, err = repo.FindUserByDiscordID(ctx, "no-such-discord-id")
	if !errors.Is(err, appauth.ErrUserNotFound) {
		t.Errorf("missing id: got %v, want ErrUserNotFound", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```
cd apps/backend && go test ./internal/appauth/ -run TestRepo_FindUserByDiscordID -v
```
Expected: compile error — `FindUserByDiscordID` does not exist.

- [ ] **Step 3: Implement the method**

Append to `apps/backend/internal/appauth/repo.go` (anywhere after `loadDiscordAccount`):

```go
// FindUserByDiscordID returns the app_users.id linked to the given Discord
// account ID, or ErrUserNotFound when no row exists.
func (r *Repo) FindUserByDiscordID(ctx context.Context, discordID string) (string, error) {
	var userID string
	err := r.pool.QueryRow(ctx, `
		SELECT user_id FROM user_connected_accounts
		WHERE provider = 'discord' AND provider_account_id = $1
	`, discordID).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrUserNotFound
	}
	return userID, err
}
```

- [ ] **Step 4: Run test to verify it passes**

```
cd apps/backend && go test ./internal/appauth/ -run TestRepo_FindUserByDiscordID -v
```
Expected: PASS (or SKIP if `TEST_POSTGRES_URL` unset).

- [ ] **Step 5: Commit**

```
git add apps/backend/internal/appauth/repo.go apps/backend/internal/appauth/repo_discord_test.go
git commit -m "feat(backend): add FindUserByDiscordID repo method"
```

---

## Task 4: Repo method `RefreshDiscordConnection`

Updates the existing `user_connected_accounts` row for a Discord profile (called on every sign-in to keep username/avatar fresh).

**Files:**
- Modify: `apps/backend/internal/appauth/repo.go`
- Test: `apps/backend/internal/appauth/repo_discord_test.go`

- [ ] **Step 1: Write the failing test**

Append to `apps/backend/internal/appauth/repo_discord_test.go`:

```go
func TestRepo_RefreshDiscordConnection(t *testing.T) {
	s := discordTestStore(t)
	ctx := context.Background()
	repo := appauth.NewRepo(s.Pool())

	const userID = "usr_disc_refresh_1"
	if _, err := s.Pool().Exec(ctx,
		`INSERT INTO app_users (id, display_name) VALUES ($1, 'D') ON CONFLICT (id) DO NOTHING`, userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := s.Pool().Exec(ctx,
		`INSERT INTO user_connected_accounts (user_id, provider, provider_account_id, username, display_name, avatar_url, profile_email)
		 VALUES ($1, 'discord', '111222', 'old_name', 'Old Display', 'http://old/avatar', 'old@example.com')
		 ON CONFLICT (provider, provider_account_id) DO UPDATE
		 SET username='old_name', display_name='Old Display', avatar_url='http://old/avatar', profile_email='old@example.com'`, userID); err != nil {
		t.Fatalf("seed conn: %v", err)
	}

	err := repo.RefreshDiscordConnection(ctx, userID, appauth.DiscordProfile{
		ID:          "111222",
		Username:    "new_name",
		DisplayName: "New Display",
		AvatarURL:   "http://new/avatar",
		Email:       "new@example.com",
	})
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	var u, d, a, e string
	if err := s.Pool().QueryRow(ctx,
		`SELECT username, display_name, avatar_url, profile_email
		 FROM user_connected_accounts WHERE provider='discord' AND provider_account_id='111222'`).
		Scan(&u, &d, &a, &e); err != nil {
		t.Fatalf("read: %v", err)
	}
	if u != "new_name" || d != "New Display" || a != "http://new/avatar" || e != "new@example.com" {
		t.Errorf("row not refreshed: %q %q %q %q", u, d, a, e)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```
cd apps/backend && go test ./internal/appauth/ -run TestRepo_RefreshDiscordConnection -v
```
Expected: compile error — `DiscordProfile` and `RefreshDiscordConnection` don't exist.

- [ ] **Step 3: Implement**

Append to `apps/backend/internal/appauth/repo.go`:

```go
// DiscordProfile is the subset of Discord OAuth profile data the appauth repo
// needs to upsert and refresh user_connected_accounts rows.
type DiscordProfile struct {
	ID          string
	Username    string
	DisplayName string
	AvatarURL   string
	Email       string
}

// RefreshDiscordConnection updates the connection row for a known Discord
// account. The user_id must already match; this is called on repeat sign-ins
// to keep username/display name/avatar/profile_email fresh.
func (r *Repo) RefreshDiscordConnection(ctx context.Context, userID string, p DiscordProfile) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE user_connected_accounts
		   SET username       = $3,
		       display_name   = $4,
		       avatar_url     = $5,
		       profile_email  = $6,
		       updated_at     = NOW()
		 WHERE user_id = $1
		   AND provider = 'discord'
		   AND provider_account_id = $2
	`, userID, p.ID, p.Username, p.DisplayName, p.AvatarURL, p.Email)
	return err
}
```

- [ ] **Step 4: Run test to verify it passes**

```
cd apps/backend && go test ./internal/appauth/ -run TestRepo_RefreshDiscordConnection -v
```
Expected: PASS (or SKIP).

- [ ] **Step 5: Commit**

```
git add apps/backend/internal/appauth/repo.go apps/backend/internal/appauth/repo_discord_test.go
git commit -m "feat(backend): add RefreshDiscordConnection repo method"
```

---

## Task 5: Repo method `FindUserByEmail`

Returns the `user_id` and `is_verified` flag for a `user_emails` row (case-insensitive email match), plus an `exists` boolean. Drives the auto-link policy.

**Files:**
- Modify: `apps/backend/internal/appauth/repo.go`
- Test: `apps/backend/internal/appauth/repo_discord_test.go`

- [ ] **Step 1: Write the failing test**

Append to `apps/backend/internal/appauth/repo_discord_test.go`:

```go
func TestRepo_FindUserByEmail(t *testing.T) {
	s := discordTestStore(t)
	ctx := context.Background()
	repo := appauth.NewRepo(s.Pool())

	const verifiedID = "usr_email_v"
	const unverifiedID = "usr_email_u"
	for _, id := range []string{verifiedID, unverifiedID} {
		if _, err := s.Pool().Exec(ctx,
			`INSERT INTO app_users (id, display_name) VALUES ($1, 'E') ON CONFLICT (id) DO NOTHING`, id); err != nil {
			t.Fatalf("seed user %s: %v", id, err)
		}
	}
	if _, err := s.Pool().Exec(ctx,
		`INSERT INTO user_emails (email, user_id, is_primary, is_verified)
		 VALUES ('Verified@Example.com', $1, true, true)
		 ON CONFLICT (email) DO UPDATE SET user_id=$1, is_verified=true`, verifiedID); err != nil {
		t.Fatalf("seed verified: %v", err)
	}
	if _, err := s.Pool().Exec(ctx,
		`INSERT INTO user_emails (email, user_id, is_primary, is_verified)
		 VALUES ('unverified@example.com', $1, true, false)
		 ON CONFLICT (email) DO UPDATE SET user_id=$1, is_verified=false`, unverifiedID); err != nil {
		t.Fatalf("seed unverified: %v", err)
	}

	id, verified, exists, err := repo.FindUserByEmail(ctx, "verified@example.com")
	if err != nil {
		t.Fatalf("FindUserByEmail verified: %v", err)
	}
	if !exists || !verified || id != verifiedID {
		t.Errorf("verified case: id=%q verified=%v exists=%v", id, verified, exists)
	}

	id, verified, exists, err = repo.FindUserByEmail(ctx, "UNVERIFIED@example.com")
	if err != nil {
		t.Fatalf("FindUserByEmail unverified: %v", err)
	}
	if !exists || verified || id != unverifiedID {
		t.Errorf("unverified case: id=%q verified=%v exists=%v", id, verified, exists)
	}

	id, verified, exists, err = repo.FindUserByEmail(ctx, "missing@example.com")
	if err != nil {
		t.Fatalf("FindUserByEmail missing: %v", err)
	}
	if exists || verified || id != "" {
		t.Errorf("missing case: id=%q verified=%v exists=%v", id, verified, exists)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```
cd apps/backend && go test ./internal/appauth/ -run TestRepo_FindUserByEmail -v
```
Expected: compile error — `FindUserByEmail` does not exist.

- [ ] **Step 3: Implement**

Append to `apps/backend/internal/appauth/repo.go`:

```go
// FindUserByEmail looks up a user_emails row case-insensitively. Returns
// (userID, isVerified, exists, err). When no row exists, returns
// ("", false, false, nil) — *not* an error.
func (r *Repo) FindUserByEmail(ctx context.Context, email string) (string, bool, bool, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return "", false, false, nil
	}
	var userID string
	var verified bool
	err := r.pool.QueryRow(ctx, `
		SELECT user_id, is_verified
		FROM user_emails
		WHERE lower(email) = $1
		LIMIT 1
	`, email).Scan(&userID, &verified)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, false, nil
	}
	if err != nil {
		return "", false, false, err
	}
	return userID, verified, true, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

```
cd apps/backend && go test ./internal/appauth/ -run TestRepo_FindUserByEmail -v
```
Expected: PASS (or SKIP).

- [ ] **Step 5: Commit**

```
git add apps/backend/internal/appauth/repo.go apps/backend/internal/appauth/repo_discord_test.go
git commit -m "feat(backend): add FindUserByEmail repo method"
```

---

## Task 6: Repo method `CreateDiscordUser`

Creates a new `app_users` row, primary verified email, and connected Discord account in one transaction. Returns the new user ID.

**Files:**
- Modify: `apps/backend/internal/appauth/repo.go`
- Test: `apps/backend/internal/appauth/repo_discord_test.go`

- [ ] **Step 1: Write the failing test**

Append to `apps/backend/internal/appauth/repo_discord_test.go`:

```go
func TestRepo_CreateDiscordUser(t *testing.T) {
	s := discordTestStore(t)
	ctx := context.Background()
	repo := appauth.NewRepo(s.Pool())

	id, err := repo.CreateDiscordUser(ctx, appauth.DiscordProfile{
		ID:          "777888999",
		Username:    "create_test",
		DisplayName: "Create Test",
		AvatarURL:   "http://avatar/c",
		Email:       "create_test@example.com",
	})
	if err != nil {
		t.Fatalf("CreateDiscordUser: %v", err)
	}
	if id == "" {
		t.Fatalf("expected non-empty id")
	}

	var dn string
	if err := s.Pool().QueryRow(ctx,
		`SELECT display_name FROM app_users WHERE id=$1`, id).Scan(&dn); err != nil {
		t.Fatalf("read user: %v", err)
	}
	if dn != "Create Test" {
		t.Errorf("display_name = %q", dn)
	}

	var verified bool
	if err := s.Pool().QueryRow(ctx,
		`SELECT is_verified FROM user_emails WHERE user_id=$1 AND is_primary=true`, id).Scan(&verified); err != nil {
		t.Fatalf("read email: %v", err)
	}
	if !verified {
		t.Errorf("primary email should be verified")
	}

	var connID string
	if err := s.Pool().QueryRow(ctx,
		`SELECT provider_account_id FROM user_connected_accounts WHERE user_id=$1 AND provider='discord'`, id).Scan(&connID); err != nil {
		t.Fatalf("read conn: %v", err)
	}
	if connID != "777888999" {
		t.Errorf("provider_account_id = %q", connID)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```
cd apps/backend && go test ./internal/appauth/ -run TestRepo_CreateDiscordUser -v
```
Expected: compile error.

- [ ] **Step 3: Implement**

Add imports `"crypto/rand"` and `"encoding/hex"` to `apps/backend/internal/appauth/repo.go` if not present, plus `"github.com/jackc/pgx/v5/pgxpool"` is already imported. Append:

```go
// generateDiscordUserID returns a fresh "usr_<24-hex>" ID. Mirrors the format
// used by admin/actions/users_invite.go so all app_users IDs share a shape.
func generateDiscordUserID() (string, error) {
	var buf [12]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return "usr_" + hex.EncodeToString(buf[:]), nil
}

// CreateDiscordUser provisions a new user from a Discord profile in a single
// transaction: app_users + verified primary user_emails + user_connected_accounts.
// Trusts the caller to have already confirmed Discord reported the email as
// verified; this method does not re-check.
func (r *Repo) CreateDiscordUser(ctx context.Context, p DiscordProfile) (string, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var userID string
	for attempt := 0; attempt < 2; attempt++ {
		userID, err = generateDiscordUserID()
		if err != nil {
			return "", err
		}
		_, ierr := tx.Exec(ctx, `
			INSERT INTO app_users (id, display_name, avatar_url, roles)
			VALUES ($1, $2, $3, '["user"]'::jsonb)
		`, userID, p.DisplayName, p.AvatarURL)
		if ierr == nil {
			break
		}
		var pgErr *pgconn.PgError
		if errors.As(ierr, &pgErr) && pgErr.Code == "23505" && attempt == 0 {
			continue
		}
		return "", ierr
	}

	email := strings.ToLower(strings.TrimSpace(p.Email))
	if _, err := tx.Exec(ctx, `
		INSERT INTO user_emails (email, user_id, is_primary, is_verified)
		VALUES ($1, $2, true, true)
	`, email, userID); err != nil {
		return "", err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO user_connected_accounts
			(user_id, provider, provider_account_id, username, display_name, avatar_url, profile_email)
		VALUES ($1, 'discord', $2, $3, $4, $5, $6)
	`, userID, p.ID, p.Username, p.DisplayName, p.AvatarURL, p.Email); err != nil {
		return "", err
	}

	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return userID, nil
}
```

Add the `"github.com/jackc/pgx/v5/pgconn"` import to `repo.go` if not already there (used here for the duplicate-key 23505 check).

- [ ] **Step 4: Run test to verify it passes**

```
cd apps/backend && go test ./internal/appauth/ -run TestRepo_CreateDiscordUser -v
```
Expected: PASS (or SKIP).

- [ ] **Step 5: Commit**

```
git add apps/backend/internal/appauth/repo.go apps/backend/internal/appauth/repo_discord_test.go
git commit -m "feat(backend): add CreateDiscordUser repo method"
```

---

## Task 7: Repo method `LinkDiscordToUser` with sentinel errors

Inserts a `user_connected_accounts` row for an existing user; returns sentinel errors when the row collides on either constraint.

**Files:**
- Modify: `apps/backend/internal/appauth/repo.go`
- Test: `apps/backend/internal/appauth/repo_discord_test.go`

- [ ] **Step 1: Write the failing test**

Append to `apps/backend/internal/appauth/repo_discord_test.go`:

```go
func TestRepo_LinkDiscordToUser(t *testing.T) {
	s := discordTestStore(t)
	ctx := context.Background()
	repo := appauth.NewRepo(s.Pool())

	const userA = "usr_link_a"
	const userB = "usr_link_b"
	for _, id := range []string{userA, userB} {
		if _, err := s.Pool().Exec(ctx,
			`INSERT INTO app_users (id, display_name) VALUES ($1, 'L') ON CONFLICT (id) DO NOTHING`, id); err != nil {
			t.Fatalf("seed user %s: %v", id, err)
		}
		// Clean up any prior link for this user so we start from a known state.
		if _, err := s.Pool().Exec(ctx,
			`DELETE FROM user_connected_accounts WHERE user_id=$1 AND provider='discord'`, id); err != nil {
			t.Fatalf("clean conn %s: %v", id, err)
		}
	}
	// Also clean any prior row for this Discord ID.
	if _, err := s.Pool().Exec(ctx,
		`DELETE FROM user_connected_accounts WHERE provider='discord' AND provider_account_id='link_target_discord'`); err != nil {
		t.Fatalf("clean discord id: %v", err)
	}

	prof := appauth.DiscordProfile{
		ID:          "link_target_discord",
		Username:    "link_user",
		DisplayName: "Link User",
		AvatarURL:   "http://avatar/l",
		Email:       "link@example.com",
	}

	// Happy path: link to userA.
	if err := repo.LinkDiscordToUser(ctx, userA, prof); err != nil {
		t.Fatalf("first link: %v", err)
	}

	// userA tries to add a *second* Discord account → ErrDiscordAlreadyLinked.
	otherProf := prof
	otherProf.ID = "second_discord"
	err := repo.LinkDiscordToUser(ctx, userA, otherProf)
	if !errors.Is(err, appauth.ErrDiscordAlreadyLinked) {
		t.Errorf("second link by same user: got %v, want ErrDiscordAlreadyLinked", err)
	}

	// userB tries to claim the same Discord ID userA already has → ErrDiscordLinkedToOther.
	err = repo.LinkDiscordToUser(ctx, userB, prof)
	if !errors.Is(err, appauth.ErrDiscordLinkedToOther) {
		t.Errorf("conflict by other user: got %v, want ErrDiscordLinkedToOther", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```
cd apps/backend && go test ./internal/appauth/ -run TestRepo_LinkDiscordToUser -v
```
Expected: compile error.

- [ ] **Step 3: Add sentinel errors and implement**

Modify the `var (...)` error block near the top of `apps/backend/internal/appauth/repo.go` to add:

```go
var (
	ErrUserNotFound          = errors.New("user not found")
	ErrInvalidCredentials    = errors.New("invalid credentials")
	ErrDiscordAlreadyLinked  = errors.New("user already has a discord connection")
	ErrDiscordLinkedToOther  = errors.New("discord account is linked to another user")
)
```

Append the implementation:

```go
// LinkDiscordToUser inserts a user_connected_accounts row attaching the given
// Discord profile to userID. Returns:
//   - ErrDiscordAlreadyLinked  when the user already has a discord row
//     (UNIQUE(user_id, provider) violation).
//   - ErrDiscordLinkedToOther  when the Discord account is already linked to
//     a different user (PK(provider, provider_account_id) violation).
func (r *Repo) LinkDiscordToUser(ctx context.Context, userID string, p DiscordProfile) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO user_connected_accounts
			(user_id, provider, provider_account_id, username, display_name, avatar_url, profile_email)
		VALUES ($1, 'discord', $2, $3, $4, $5, $6)
	`, userID, p.ID, p.Username, p.DisplayName, p.AvatarURL, p.Email)
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		// Postgres returns 23505 (unique_violation) for both PK and UNIQUE.
		// Disambiguate by constraint name.
		switch {
		case strings.Contains(pgErr.ConstraintName, "user_connected_accounts_pkey"):
			return ErrDiscordLinkedToOther
		case strings.Contains(pgErr.ConstraintName, "user_connected_accounts_user_id_provider_key"):
			return ErrDiscordAlreadyLinked
		}
		// Fallback when the constraint name doesn't match either expected
		// shape (e.g. Postgres renames in a future schema change). Treat as
		// the more conservative "linked to other".
		return ErrDiscordLinkedToOther
	}
	return err
}
```

- [ ] **Step 4: Run test to verify it passes**

```
cd apps/backend && go test ./internal/appauth/ -run TestRepo_LinkDiscordToUser -v
```
Expected: PASS (or SKIP).

- [ ] **Step 5: Commit**

```
git add apps/backend/internal/appauth/repo.go apps/backend/internal/appauth/repo_discord_test.go
git commit -m "feat(backend): add LinkDiscordToUser repo method"
```

---

## Task 8: OAuth helpers — PKCE, state, intent cookies

Pure helpers (no HTTP I/O) that the handlers will use.

**Files:**
- Create: `apps/backend/internal/oauth/discord/oauth.go`
- Create: `apps/backend/internal/oauth/discord/oauth_test.go`

- [ ] **Step 1: Write the failing tests**

Create `apps/backend/internal/oauth/discord/oauth_test.go`:

```go
package discord_test

import (
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"

	"rustlens/backend/internal/oauth/discord"
)

func TestNewPkceVerifier_LengthAndCharset(t *testing.T) {
	v, err := discord.NewPkceVerifier()
	if err != nil {
		t.Fatalf("NewPkceVerifier: %v", err)
	}
	if len(v) < 43 || len(v) > 128 {
		t.Errorf("verifier length %d outside 43..128", len(v))
	}
	for _, r := range v {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_') {
			t.Errorf("verifier char %q not base64url-safe", r)
		}
	}
}

func TestPkceChallenge_MatchesS256(t *testing.T) {
	v := "test-verifier-stable-input-1234567890abcdef"
	want := base64.RawURLEncoding.EncodeToString(func() []byte {
		h := sha256.Sum256([]byte(v))
		return h[:]
	}())
	got := discord.PkceChallenge(v)
	if got != want {
		t.Errorf("PkceChallenge mismatch:\n got %s\nwant %s", got, want)
	}
}

func TestNewState_NonEmpty_Unique(t *testing.T) {
	a, err := discord.NewState()
	if err != nil {
		t.Fatalf("NewState a: %v", err)
	}
	b, err := discord.NewState()
	if err != nil {
		t.Fatalf("NewState b: %v", err)
	}
	if a == "" || b == "" || a == b {
		t.Errorf("expected non-empty distinct values, got %q %q", a, b)
	}
}

func TestRedirectURI_BuildsCallback(t *testing.T) {
	got := discord.RedirectURI("http://localhost")
	want := "http://localhost/api/auth/discord/callback"
	if got != want {
		t.Errorf("RedirectURI = %q, want %q", got, want)
	}
	if discord.RedirectURI("http://localhost/") != want {
		t.Errorf("trailing slash should be normalized")
	}
}

func TestIntentCookie_Constants(t *testing.T) {
	if discord.IntentSignin != "signin" || discord.IntentLink != "link" {
		t.Errorf("intent constants drifted: signin=%q link=%q", discord.IntentSignin, discord.IntentLink)
	}
	for _, name := range []string{discord.CookieState, discord.CookieVerifier, discord.CookieIntent} {
		if !strings.HasPrefix(name, "rustlens_discord_oauth_") {
			t.Errorf("cookie name %q lacks expected prefix", name)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```
cd apps/backend && go test ./internal/oauth/discord/ -v
```
Expected: package not found.

- [ ] **Step 3: Implement**

Create `apps/backend/internal/oauth/discord/oauth.go`:

```go
package discord

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"strings"
)

// Cookie names. All set with HttpOnly, SameSite=Lax, 10-minute MaxAge during
// initiate; cleared (MaxAge: -1) on callback regardless of outcome.
const (
	CookieState    = "rustlens_discord_oauth_state"
	CookieVerifier = "rustlens_discord_oauth_verifier"
	CookieIntent   = "rustlens_discord_oauth_intent"
)

// Intent cookie values.
const (
	IntentSignin = "signin"
	IntentLink   = "link"
)

// CookieMaxAgeSeconds is the lifetime of the per-flow cookies set on initiate.
const CookieMaxAgeSeconds = 60 * 10

// NewPkceVerifier returns a fresh base64url-encoded random string suitable as
// a PKCE code_verifier (RFC 7636). 32 random bytes encode to 43 chars.
func NewPkceVerifier() (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf[:]), nil
}

// PkceChallenge returns the S256 challenge for a verifier.
func PkceChallenge(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

// NewState returns a fresh random state value. 16 bytes encoded base64url.
func NewState() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf[:]), nil
}

// RedirectURI builds the OAuth callback URL given an APP_ORIGIN.
// Normalizes a single trailing slash on origin.
func RedirectURI(appOrigin string) string {
	return strings.TrimRight(appOrigin, "/") + "/api/auth/discord/callback"
}
```

- [ ] **Step 4: Run tests to verify they pass**

```
cd apps/backend && go test ./internal/oauth/discord/ -v
```
Expected: PASS.

- [ ] **Step 5: Commit**

```
git add apps/backend/internal/oauth/discord/
git commit -m "feat(backend): add Discord OAuth PKCE and state helpers"
```

---

## Task 9: Discord HTTP client — `ExchangeCode` and `FetchProfile`

Wraps the two Discord endpoints. Tests use `httptest.NewServer` to fake Discord; no real network calls.

**Files:**
- Create: `apps/backend/internal/oauth/discord/client.go`
- Create: `apps/backend/internal/oauth/discord/client_test.go`

- [ ] **Step 1: Write the failing tests**

Create `apps/backend/internal/oauth/discord/client_test.go`:

```go
package discord_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"rustlens/backend/internal/oauth/discord"
)

func TestClient_ExchangeCode_Success(t *testing.T) {
	var gotForm string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/oauth2/token" || r.Method != http.MethodPost {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		_ = r.ParseForm()
		gotForm = r.Form.Encode()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"tok-abc","token_type":"Bearer"}`))
	}))
	defer srv.Close()

	c := discord.NewClient("client-id", "client-secret", discord.WithBaseURL(srv.URL), discord.WithRedirectURI("http://localhost/api/auth/discord/callback"))
	tok, err := c.ExchangeCode(context.Background(), "the-code", "the-verifier")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if tok != "tok-abc" {
		t.Errorf("access_token = %q", tok)
	}
	for _, want := range []string{"client_id=client-id", "client_secret=client-secret", "grant_type=authorization_code", "code=the-code", "code_verifier=the-verifier", "redirect_uri="} {
		if !strings.Contains(gotForm, want) {
			t.Errorf("body missing %q; got %q", want, gotForm)
		}
	}
}

func TestClient_ExchangeCode_NonOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer srv.Close()
	c := discord.NewClient("id", "secret", discord.WithBaseURL(srv.URL), discord.WithRedirectURI("x"))
	if _, err := c.ExchangeCode(context.Background(), "code", "verifier"); err == nil {
		t.Fatalf("expected error on 400")
	}
}

func TestClient_FetchProfile_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/users/@me" || r.Method != http.MethodGet {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tok-abc" {
			t.Errorf("Authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "777",
			"username": "alice",
			"global_name": "Alice",
			"avatar": "abcd",
			"email": "alice@example.com",
			"verified": true
		}`))
	}))
	defer srv.Close()
	c := discord.NewClient("id", "secret", discord.WithBaseURL(srv.URL), discord.WithRedirectURI("x"))
	p, err := c.FetchProfile(context.Background(), "tok-abc")
	if err != nil {
		t.Fatalf("FetchProfile: %v", err)
	}
	if p.ID != "777" || p.Username != "alice" || p.GlobalName != "Alice" || p.Email != "alice@example.com" || !p.Verified {
		t.Errorf("profile mismatch: %+v", p)
	}
	wantAvatar := "https://cdn.discordapp.com/avatars/777/abcd.png?size=256"
	if p.AvatarURL != wantAvatar {
		t.Errorf("AvatarURL = %q, want %q", p.AvatarURL, wantAvatar)
	}
}

func TestClient_FetchProfile_NoAvatar(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"1","username":"x","avatar":null,"verified":false}`))
	}))
	defer srv.Close()
	c := discord.NewClient("id", "secret", discord.WithBaseURL(srv.URL), discord.WithRedirectURI("x"))
	p, err := c.FetchProfile(context.Background(), "tok")
	if err != nil {
		t.Fatalf("FetchProfile: %v", err)
	}
	if p.AvatarURL != "" {
		t.Errorf("AvatarURL with null avatar = %q, want empty", p.AvatarURL)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```
cd apps/backend && go test ./internal/oauth/discord/ -v
```
Expected: compile errors — `Client`, `NewClient`, `WithBaseURL`, `WithRedirectURI`, `Profile`, `ExchangeCode`, `FetchProfile` don't exist.

- [ ] **Step 3: Implement**

Create `apps/backend/internal/oauth/discord/client.go`:

```go
package discord

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Profile is the subset of Discord user fields we use.
type Profile struct {
	ID          string
	Username    string
	GlobalName  string
	AvatarURL   string
	Email       string
	Verified    bool
}

// DisplayName returns global_name when set, falling back to username.
func (p Profile) DisplayName() string {
	if p.GlobalName != "" {
		return p.GlobalName
	}
	return p.Username
}

// Client wraps the two Discord OAuth endpoints we depend on.
type Client struct {
	clientID     string
	clientSecret string
	baseURL      string
	redirectURI  string
	httpClient   *http.Client
	timeout      time.Duration
}

// Option configures a Client.
type Option func(*Client)

// WithBaseURL overrides the Discord API base URL (default https://discord.com).
// Used in tests with httptest servers.
func WithBaseURL(u string) Option { return func(c *Client) { c.baseURL = u } }

// WithRedirectURI sets the redirect_uri sent during the token exchange. Must
// match what was used in the authorize redirect.
func WithRedirectURI(u string) Option { return func(c *Client) { c.redirectURI = u } }

// WithHTTPClient overrides the http.Client used for outbound calls.
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.httpClient = h } }

func NewClient(clientID, clientSecret string, opts ...Option) *Client {
	c := &Client{
		clientID:     clientID,
		clientSecret: clientSecret,
		baseURL:      "https://discord.com",
		httpClient:   &http.Client{Timeout: 10 * time.Second},
		timeout:      10 * time.Second,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// ExchangeCode trades an authorization code (with PKCE verifier) for an
// access token. Returns the access_token string.
func (c *Client) ExchangeCode(ctx context.Context, code, codeVerifier string) (string, error) {
	if c.redirectURI == "" {
		return "", errors.New("discord client: redirect_uri is required for code exchange")
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
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cache-Control", "no-store")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("discord token exchange: %d %s", resp.StatusCode, string(body))
	}
	var out struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", err
	}
	if out.AccessToken == "" {
		return "", errors.New("discord token exchange: empty access_token")
	}
	return out.AccessToken, nil
}

// FetchProfile retrieves the authenticated user's profile from /users/@me.
func (c *Client) FetchProfile(ctx context.Context, accessToken string) (Profile, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/users/@me", nil)
	if err != nil {
		return Profile{}, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Cache-Control", "no-store")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return Profile{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Profile{}, fmt.Errorf("discord profile fetch: %d %s", resp.StatusCode, string(body))
	}
	var raw struct {
		ID         string  `json:"id"`
		Username   string  `json:"username"`
		GlobalName *string `json:"global_name"`
		Avatar     *string `json:"avatar"`
		Email      string  `json:"email"`
		Verified   bool    `json:"verified"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return Profile{}, err
	}
	p := Profile{
		ID:       raw.ID,
		Username: raw.Username,
		Email:    raw.Email,
		Verified: raw.Verified,
	}
	if raw.GlobalName != nil {
		p.GlobalName = *raw.GlobalName
	}
	if raw.Avatar != nil && *raw.Avatar != "" {
		p.AvatarURL = fmt.Sprintf("https://cdn.discordapp.com/avatars/%s/%s.png?size=256", raw.ID, *raw.Avatar)
	}
	return p, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

```
cd apps/backend && go test ./internal/oauth/discord/ -v
```
Expected: PASS.

- [ ] **Step 5: Commit**

```
git add apps/backend/internal/oauth/discord/
git commit -m "feat(backend): add Discord OAuth HTTP client"
```

---

## Task 10: Cookie session reader for browser-driven routes

`appauth.RequireSession` middleware reads the session token from `X-RustLens-Session` (header). The OAuth callback is browser-driven and only sees the cookie. Add a helper that reads from the `rustlens_session` cookie.

**Files:**
- Modify: `apps/backend/internal/appauth/middleware.go`
- Test: `apps/backend/internal/appauth/middleware_cookie_test.go` (new)

- [ ] **Step 1: Write the failing test**

Create `apps/backend/internal/appauth/middleware_cookie_test.go`:

```go
package appauth_test

import (
	"net/http"
	"testing"
	"time"

	"rustlens/backend/internal/appauth"
)

func TestSessionFromCookie_ValidToken(t *testing.T) {
	secret := "test-secret"
	in := appauth.SessionUser{
		UserID:    "usr_cookie_1",
		Roles:     []string{"user"},
		Epoch:     0,
		ExpiresAt: time.Now().Add(time.Hour).UnixMilli(),
	}
	tok, err := appauth.SerializeSession(in, secret)
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}
	r, _ := http.NewRequest("GET", "/", nil)
	r.AddCookie(&http.Cookie{Name: appauth.SessionCookieName, Value: tok})
	got, err := appauth.SessionFromCookie(r, secret)
	if err != nil {
		t.Fatalf("SessionFromCookie: %v", err)
	}
	if got == nil || got.UserID != "usr_cookie_1" {
		t.Errorf("got %+v", got)
	}
}

func TestSessionFromCookie_Missing(t *testing.T) {
	r, _ := http.NewRequest("GET", "/", nil)
	if _, err := appauth.SessionFromCookie(r, "secret"); err == nil {
		t.Fatalf("expected error on missing cookie")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```
cd apps/backend && go test ./internal/appauth/ -run TestSessionFromCookie -v
```
Expected: compile error — `SessionCookieName` and `SessionFromCookie` don't exist.

- [ ] **Step 3: Implement**

Append to `apps/backend/internal/appauth/middleware.go`:

```go
// SessionCookieName is the cookie set by the frontend's setSession() and by
// the backend after a successful Discord OAuth sign-in. Mirrors
// SESSION_COOKIE in apps/frontend/lib/session.ts.
const SessionCookieName = "rustlens_session"

// SessionFromCookie reads and verifies the rustlens_session cookie on the
// given request. Returns ErrMalformedToken / ErrBadSignature / ErrExpired on
// invalid tokens, or http.ErrNoCookie when the cookie isn't present.
func SessionFromCookie(r *http.Request, secret string) (*SessionUser, error) {
	c, err := r.Cookie(SessionCookieName)
	if err != nil {
		return nil, err
	}
	return ParseSession(c.Value, secret)
}
```

Add `"net/http"` to the imports if not already present (it is).

- [ ] **Step 4: Run tests to verify pass**

```
cd apps/backend && go test ./internal/appauth/ -run TestSessionFromCookie -v
```
Expected: PASS.

- [ ] **Step 5: Commit**

```
git add apps/backend/internal/appauth/middleware.go apps/backend/internal/appauth/middleware_cookie_test.go
git commit -m "feat(backend): add SessionFromCookie helper for browser routes"
```

---

## Task 11: OAuth handlers — `Initiate` (sign-in)

Sets PKCE / state / intent=`signin` cookies and 302s to Discord. Refuses with `?error=discord-already-signed-in` when a session is present.

**Files:**
- Create: `apps/backend/internal/oauth/discord/handlers.go`
- Create: `apps/backend/internal/oauth/discord/handlers_test.go`

- [ ] **Step 1: Write the failing tests**

Create `apps/backend/internal/oauth/discord/handlers_test.go`:

```go
package discord_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"rustlens/backend/internal/oauth/discord"
)

func TestInitiate_RedirectsToDiscord_SetsCookies(t *testing.T) {
	h := discord.NewHandler(discord.HandlerConfig{
		ClientID:      "the-client-id",
		AppOrigin:     "http://localhost",
		SessionSecret: "session-secret",
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/auth/discord", nil)
	h.Initiate(w, r)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", w.Code)
	}
	loc, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}
	if loc.Host != "discord.com" || loc.Path != "/oauth2/authorize" {
		t.Errorf("Location host/path: %s%s", loc.Host, loc.Path)
	}
	q := loc.Query()
	for _, want := range []string{"client_id", "response_type", "redirect_uri", "scope", "state", "code_challenge", "code_challenge_method"} {
		if q.Get(want) == "" {
			t.Errorf("missing query param %q", want)
		}
	}
	if q.Get("client_id") != "the-client-id" {
		t.Errorf("client_id = %q", q.Get("client_id"))
	}
	if q.Get("redirect_uri") != "http://localhost/api/auth/discord/callback" {
		t.Errorf("redirect_uri = %q", q.Get("redirect_uri"))
	}
	if q.Get("scope") != "identify email" {
		t.Errorf("scope = %q", q.Get("scope"))
	}
	if q.Get("code_challenge_method") != "S256" {
		t.Errorf("code_challenge_method = %q", q.Get("code_challenge_method"))
	}

	cookies := w.Result().Cookies()
	names := map[string]string{}
	for _, c := range cookies {
		names[c.Name] = c.Value
	}
	for _, want := range []string{discord.CookieState, discord.CookieVerifier, discord.CookieIntent} {
		if names[want] == "" {
			t.Errorf("missing cookie %q", want)
		}
	}
	if names[discord.CookieIntent] != discord.IntentSignin {
		t.Errorf("intent cookie = %q", names[discord.CookieIntent])
	}
}

func TestInitiate_RefusesWhenSessionPresent(t *testing.T) {
	// Build a valid session cookie so the handler sees the user as logged in.
	tok := mustSerializeSessionForTest(t, "session-secret", "usr_already")
	h := discord.NewHandler(discord.HandlerConfig{
		ClientID:      "id",
		AppOrigin:     "http://localhost",
		SessionSecret: "session-secret",
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/auth/discord", nil)
	r.AddCookie(&http.Cookie{Name: "rustlens_session", Value: tok})
	h.Initiate(w, r)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "/login?error=discord-already-signed-in") {
		t.Errorf("Location = %q, want ?error=discord-already-signed-in", loc)
	}
}
```

Add a tiny test helper file `apps/backend/internal/oauth/discord/helpers_for_test.go`:

```go
package discord_test

import (
	"testing"
	"time"

	"rustlens/backend/internal/appauth"
)

func mustSerializeSessionForTest(t *testing.T, secret, userID string) string {
	t.Helper()
	tok, err := appauth.SerializeSession(appauth.SessionUser{
		UserID:    userID,
		Roles:     []string{"user"},
		Epoch:     0,
		ExpiresAt: time.Now().Add(time.Hour).UnixMilli(),
	}, secret)
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}
	return tok
}
```

- [ ] **Step 2: Run tests to verify they fail**

```
cd apps/backend && go test ./internal/oauth/discord/ -run TestInitiate -v
```
Expected: compile errors — `Handler`, `NewHandler`, `HandlerConfig`, `Initiate` don't exist.

- [ ] **Step 3: Implement**

Create `apps/backend/internal/oauth/discord/handlers.go`:

```go
package discord

import (
	"net/http"
	"net/url"

	"github.com/jackc/pgx/v5/pgxpool"

	"rustlens/backend/internal/appauth"
)

// HandlerConfig wires runtime dependencies into the OAuth handlers.
type HandlerConfig struct {
	ClientID      string
	ClientSecret  string
	AppOrigin     string
	SessionSecret string
	Repo          *appauth.Repo
	Pool          *pgxpool.Pool
	Client        *Client // optional — when nil, NewHandler builds one from ClientID/ClientSecret
}

type Handler struct {
	cfg    HandlerConfig
	client *Client
}

func NewHandler(cfg HandlerConfig) *Handler {
	c := cfg.Client
	if c == nil {
		c = NewClient(cfg.ClientID, cfg.ClientSecret, WithRedirectURI(RedirectURI(cfg.AppOrigin)))
	}
	return &Handler{cfg: cfg, client: c}
}

func (h *Handler) frontendURL(path string) string {
	return h.cfg.AppOrigin + path
}

// Initiate handles GET /api/auth/discord (sign-in).
func (h *Handler) Initiate(w http.ResponseWriter, r *http.Request) {
	// Refuse if a valid session cookie is already present.
	if _, err := appauth.SessionFromCookie(r, h.cfg.SessionSecret); err == nil {
		http.Redirect(w, r, h.frontendURL("/login?error=discord-already-signed-in"), http.StatusFound)
		return
	}
	h.beginFlow(w, r, IntentSignin)
}

func (h *Handler) beginFlow(w http.ResponseWriter, r *http.Request, intent string) {
	if h.cfg.ClientID == "" {
		http.Redirect(w, r, h.frontendURL("/login?error=discord-config"), http.StatusFound)
		return
	}
	state, err := NewState()
	if err != nil {
		http.Redirect(w, r, h.frontendURL("/login?error=discord-config"), http.StatusFound)
		return
	}
	verifier, err := NewPkceVerifier()
	if err != nil {
		http.Redirect(w, r, h.frontendURL("/login?error=discord-config"), http.StatusFound)
		return
	}

	setShortCookie(w, CookieState, state)
	setShortCookie(w, CookieVerifier, verifier)
	setShortCookie(w, CookieIntent, intent)

	authorize, _ := url.Parse("https://discord.com/oauth2/authorize")
	q := authorize.Query()
	q.Set("client_id", h.cfg.ClientID)
	q.Set("response_type", "code")
	q.Set("redirect_uri", RedirectURI(h.cfg.AppOrigin))
	q.Set("scope", "identify email")
	q.Set("prompt", "consent")
	q.Set("state", state)
	q.Set("code_challenge", PkceChallenge(verifier))
	q.Set("code_challenge_method", "S256")
	authorize.RawQuery = q.Encode()

	http.Redirect(w, r, authorize.String(), http.StatusFound)
}

func setShortCookie(w http.ResponseWriter, name, value string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		MaxAge:   CookieMaxAgeSeconds,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func clearCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}
```

- [ ] **Step 4: Run tests to verify pass**

```
cd apps/backend && go test ./internal/oauth/discord/ -run TestInitiate -v
```
Expected: PASS.

- [ ] **Step 5: Commit**

```
git add apps/backend/internal/oauth/discord/handlers.go apps/backend/internal/oauth/discord/handlers_test.go apps/backend/internal/oauth/discord/helpers_for_test.go
git commit -m "feat(backend): add Discord OAuth Initiate handler"
```

---

## Task 12: OAuth handler — `InitiateLink` (logged-in linking)

Same flow as `Initiate` but requires a session and stores `intent=link`.

**Files:**
- Modify: `apps/backend/internal/oauth/discord/handlers.go`
- Modify: `apps/backend/internal/oauth/discord/handlers_test.go`

- [ ] **Step 1: Write the failing test**

Append to `apps/backend/internal/oauth/discord/handlers_test.go`:

```go
func TestInitiateLink_RequiresSession(t *testing.T) {
	h := discord.NewHandler(discord.HandlerConfig{
		ClientID:      "id",
		AppOrigin:     "http://localhost",
		SessionSecret: "session-secret",
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/auth/discord/link", nil)
	h.InitiateLink(w, r)
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d", w.Code)
	}
	if got := w.Header().Get("Location"); !strings.Contains(got, "/login?error=session") {
		t.Errorf("Location = %q", got)
	}
}

func TestInitiateLink_SetsIntentLink(t *testing.T) {
	tok := mustSerializeSessionForTest(t, "session-secret", "usr_link")
	h := discord.NewHandler(discord.HandlerConfig{
		ClientID:      "id",
		AppOrigin:     "http://localhost",
		SessionSecret: "session-secret",
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/auth/discord/link", nil)
	r.AddCookie(&http.Cookie{Name: "rustlens_session", Value: tok})
	h.InitiateLink(w, r)
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d", w.Code)
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == discord.CookieIntent && c.Value == discord.IntentLink {
			return
		}
	}
	t.Errorf("expected intent=link cookie")
}
```

- [ ] **Step 2: Run tests to verify they fail**

```
cd apps/backend && go test ./internal/oauth/discord/ -run TestInitiateLink -v
```
Expected: compile error — `InitiateLink` doesn't exist.

- [ ] **Step 3: Implement**

Append to `apps/backend/internal/oauth/discord/handlers.go`:

```go
// InitiateLink handles GET /api/auth/discord/link. Requires an active session
// (refuses with /login?error=session otherwise).
func (h *Handler) InitiateLink(w http.ResponseWriter, r *http.Request) {
	if _, err := appauth.SessionFromCookie(r, h.cfg.SessionSecret); err != nil {
		http.Redirect(w, r, h.frontendURL("/login?error=session"), http.StatusFound)
		return
	}
	h.beginFlow(w, r, IntentLink)
}
```

- [ ] **Step 4: Run tests to verify they pass**

```
cd apps/backend && go test ./internal/oauth/discord/ -run TestInitiateLink -v
```
Expected: PASS.

- [ ] **Step 5: Commit**

```
git add apps/backend/internal/oauth/discord/handlers.go apps/backend/internal/oauth/discord/handlers_test.go
git commit -m "feat(backend): add Discord OAuth InitiateLink handler"
```

---

## Task 13: OAuth handler — `Callback` sign-in branch

Validates state, exchanges code, fetches profile, branches on the policy decisions, mints a session cookie, and 302s.

**Files:**
- Modify: `apps/backend/internal/oauth/discord/handlers.go`
- Modify: `apps/backend/internal/oauth/discord/handlers_test.go`

- [ ] **Step 1: Write failing tests**

Append to `apps/backend/internal/oauth/discord/handlers_test.go`. These cover the five sign-in branches + state mismatch. They use a real Postgres pool when `TEST_POSTGRES_URL` is set:

```go
func discordTestPool(t *testing.T) (*pgxpool.Pool, func()) {
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
	if err := s.EnsureSchema(ctx); err != nil {
		s.Close()
		t.Fatalf("schema: %v", err)
	}
	return s.Pool(), s.Close
}

// fakeDiscord returns an httptest server that pretends to be Discord. The
// returned profile is whatever the caller wires in.
func fakeDiscord(t *testing.T, profileJSON string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/oauth2/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"tok","token_type":"Bearer"}`))
	})
	mux.HandleFunc("/api/users/@me", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(profileJSON))
	})
	return httptest.NewServer(mux)
}

func newCallbackRequest(state, verifier, intent, code string) *http.Request {
	r := httptest.NewRequest("GET", "/api/auth/discord/callback?code="+code+"&state="+state, nil)
	r.AddCookie(&http.Cookie{Name: discord.CookieState, Value: state})
	r.AddCookie(&http.Cookie{Name: discord.CookieVerifier, Value: verifier})
	r.AddCookie(&http.Cookie{Name: discord.CookieIntent, Value: intent})
	return r
}

func newCallbackHandler(t *testing.T, pool *pgxpool.Pool, baseURL string) *discord.Handler {
	t.Helper()
	repo := appauth.NewRepo(pool)
	c := discord.NewClient("id", "secret",
		discord.WithBaseURL(baseURL),
		discord.WithRedirectURI("http://localhost/api/auth/discord/callback"))
	return discord.NewHandler(discord.HandlerConfig{
		ClientID:      "id",
		ClientSecret:  "secret",
		AppOrigin:     "http://localhost",
		SessionSecret: "session-secret",
		Repo:          repo,
		Pool:          pool,
		Client:        c,
	})
}

func TestCallback_Signin_StateMismatch(t *testing.T) {
	pool, cleanup := discordTestPool(t)
	defer cleanup()
	srv := fakeDiscord(t, `{}`)
	defer srv.Close()
	h := newCallbackHandler(t, pool, srv.URL)

	w := httptest.NewRecorder()
	r := newCallbackRequest("client-state", "v", discord.IntentSignin, "code")
	r.URL.RawQuery = "code=code&state=other-state"
	h.Callback(w, r)
	if !strings.Contains(w.Header().Get("Location"), "discord-state") {
		t.Errorf("expected discord-state error, got %q", w.Header().Get("Location"))
	}
}

func TestCallback_Signin_NoEmail(t *testing.T) {
	pool, cleanup := discordTestPool(t)
	defer cleanup()
	srv := fakeDiscord(t, `{"id":"d_no_email_1","username":"u","verified":false}`)
	defer srv.Close()
	h := newCallbackHandler(t, pool, srv.URL)

	w := httptest.NewRecorder()
	r := newCallbackRequest("s", "v", discord.IntentSignin, "code")
	h.Callback(w, r)
	if !strings.Contains(w.Header().Get("Location"), "discord-no-email") {
		t.Errorf("expected discord-no-email, got %q", w.Header().Get("Location"))
	}
}

func TestCallback_Signin_ExistingDiscordRefreshesAndMintsSession(t *testing.T) {
	pool, cleanup := discordTestPool(t)
	defer cleanup()
	ctx := context.Background()
	const userID = "usr_callback_existing"
	if _, err := pool.Exec(ctx,
		`INSERT INTO app_users (id, display_name) VALUES ($1, 'Existing') ON CONFLICT (id) DO NOTHING`, userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO user_connected_accounts (user_id, provider, provider_account_id, username)
		 VALUES ($1, 'discord', 'd_callback_existing', 'old')
		 ON CONFLICT (provider, provider_account_id) DO UPDATE SET username='old'`, userID); err != nil {
		t.Fatalf("seed conn: %v", err)
	}
	srv := fakeDiscord(t, `{"id":"d_callback_existing","username":"new_name","global_name":"New","email":"e@x.com","verified":true,"avatar":"abc"}`)
	defer srv.Close()
	h := newCallbackHandler(t, pool, srv.URL)

	w := httptest.NewRecorder()
	r := newCallbackRequest("s", "v", discord.IntentSignin, "code")
	h.Callback(w, r)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d", w.Code)
	}
	if got := w.Header().Get("Location"); !strings.HasSuffix(got, "/dashboard") {
		t.Errorf("Location = %q, want suffix /dashboard", got)
	}
	var sessionFound bool
	for _, c := range w.Result().Cookies() {
		if c.Name == "rustlens_session" && c.Value != "" {
			sessionFound = true
		}
	}
	if !sessionFound {
		t.Errorf("expected rustlens_session cookie set")
	}
	var u string
	if err := pool.QueryRow(ctx,
		`SELECT username FROM user_connected_accounts WHERE provider='discord' AND provider_account_id='d_callback_existing'`).Scan(&u); err != nil {
		t.Fatalf("read: %v", err)
	}
	if u != "new_name" {
		t.Errorf("username not refreshed: %q", u)
	}
}

func TestCallback_Signin_AutoLinkVerifiedEmail(t *testing.T) {
	pool, cleanup := discordTestPool(t)
	defer cleanup()
	ctx := context.Background()
	const userID = "usr_callback_autolink"
	if _, err := pool.Exec(ctx,
		`INSERT INTO app_users (id, display_name) VALUES ($1, 'AutoLink') ON CONFLICT (id) DO NOTHING`, userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO user_emails (email, user_id, is_primary, is_verified)
		 VALUES ('autolink@example.com', $1, true, true)
		 ON CONFLICT (email) DO UPDATE SET user_id=$1, is_verified=true`, userID); err != nil {
		t.Fatalf("seed email: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`DELETE FROM user_connected_accounts WHERE provider='discord' AND provider_account_id='d_autolink'`); err != nil {
		t.Fatalf("clean: %v", err)
	}
	srv := fakeDiscord(t, `{"id":"d_autolink","username":"al","email":"autolink@example.com","verified":true}`)
	defer srv.Close()
	h := newCallbackHandler(t, pool, srv.URL)

	w := httptest.NewRecorder()
	r := newCallbackRequest("s", "v", discord.IntentSignin, "code")
	h.Callback(w, r)
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d", w.Code)
	}
	if !strings.HasSuffix(w.Header().Get("Location"), "/dashboard") {
		t.Errorf("Location = %q", w.Header().Get("Location"))
	}
	var linkedUser string
	if err := pool.QueryRow(ctx,
		`SELECT user_id FROM user_connected_accounts WHERE provider='discord' AND provider_account_id='d_autolink'`).Scan(&linkedUser); err != nil {
		t.Fatalf("read: %v", err)
	}
	if linkedUser != userID {
		t.Errorf("auto-link target = %q, want %q", linkedUser, userID)
	}
}

func TestCallback_Signin_EmailInUse_Unverified(t *testing.T) {
	pool, cleanup := discordTestPool(t)
	defer cleanup()
	ctx := context.Background()
	const userID = "usr_callback_unverified"
	if _, err := pool.Exec(ctx,
		`INSERT INTO app_users (id, display_name) VALUES ($1, 'U') ON CONFLICT (id) DO NOTHING`, userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO user_emails (email, user_id, is_primary, is_verified)
		 VALUES ('inuse@example.com', $1, true, false)
		 ON CONFLICT (email) DO UPDATE SET user_id=$1, is_verified=false`, userID); err != nil {
		t.Fatalf("seed email: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`DELETE FROM user_connected_accounts WHERE provider='discord' AND provider_account_id='d_inuse'`); err != nil {
		t.Fatalf("clean: %v", err)
	}
	srv := fakeDiscord(t, `{"id":"d_inuse","username":"u","email":"inuse@example.com","verified":true}`)
	defer srv.Close()
	h := newCallbackHandler(t, pool, srv.URL)

	w := httptest.NewRecorder()
	r := newCallbackRequest("s", "v", discord.IntentSignin, "code")
	h.Callback(w, r)
	if !strings.Contains(w.Header().Get("Location"), "discord-email-in-use") {
		t.Errorf("Location = %q, want discord-email-in-use", w.Header().Get("Location"))
	}
}

func TestCallback_Signin_NewUserCreated(t *testing.T) {
	pool, cleanup := discordTestPool(t)
	defer cleanup()
	ctx := context.Background()
	if _, err := pool.Exec(ctx,
		`DELETE FROM user_emails WHERE email='brand_new@example.com'`); err != nil {
		t.Fatalf("clean email: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`DELETE FROM user_connected_accounts WHERE provider='discord' AND provider_account_id='d_new'`); err != nil {
		t.Fatalf("clean conn: %v", err)
	}
	srv := fakeDiscord(t, `{"id":"d_new","username":"newbie","email":"brand_new@example.com","verified":true}`)
	defer srv.Close()
	h := newCallbackHandler(t, pool, srv.URL)

	w := httptest.NewRecorder()
	r := newCallbackRequest("s", "v", discord.IntentSignin, "code")
	h.Callback(w, r)
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d", w.Code)
	}
	if !strings.HasSuffix(w.Header().Get("Location"), "/dashboard") {
		t.Errorf("Location = %q", w.Header().Get("Location"))
	}
	var newID string
	if err := pool.QueryRow(ctx,
		`SELECT user_id FROM user_connected_accounts WHERE provider='discord' AND provider_account_id='d_new'`).Scan(&newID); err != nil {
		t.Fatalf("read conn: %v", err)
	}
	if newID == "" {
		t.Errorf("expected new user")
	}
}
```

Add the new imports needed in `handlers_test.go`: `"context"`, `"os"`, `"github.com/jackc/pgx/v5/pgxpool"`, `"rustlens/backend/internal/appauth"`, `"rustlens/backend/internal/store"`.

- [ ] **Step 2: Run tests to verify they fail**

```
cd apps/backend && go test ./internal/oauth/discord/ -run TestCallback_Signin -v
```
Expected: compile errors — `Callback` doesn't exist.

- [ ] **Step 3: Implement**

Append to `apps/backend/internal/oauth/discord/handlers.go`:

```go
// Callback handles GET /api/auth/discord/callback. Validates the state cookie,
// exchanges the code with Discord, fetches the profile, then branches on the
// intent cookie (sign-in vs link).
func (h *Handler) Callback(w http.ResponseWriter, r *http.Request) {
	// Always clear the per-flow cookies on the way out, regardless of outcome.
	defer func() {
		clearCookie(w, CookieState)
		clearCookie(w, CookieVerifier)
		clearCookie(w, CookieIntent)
	}()

	stateCookie, _ := r.Cookie(CookieState)
	verifierCookie, _ := r.Cookie(CookieVerifier)
	intentCookie, _ := r.Cookie(CookieIntent)
	queryState := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")

	if stateCookie == nil || verifierCookie == nil || intentCookie == nil ||
		queryState == "" || code == "" || queryState != stateCookie.Value {
		http.Redirect(w, r, h.frontendURL("/login?error=discord-state"), http.StatusFound)
		return
	}

	ctx := r.Context()
	accessToken, err := h.client.ExchangeCode(ctx, code, verifierCookie.Value)
	if err != nil {
		http.Redirect(w, r, h.frontendURL("/login?error=discord-token"), http.StatusFound)
		return
	}
	profile, err := h.client.FetchProfile(ctx, accessToken)
	if err != nil {
		http.Redirect(w, r, h.frontendURL("/login?error=discord-profile"), http.StatusFound)
		return
	}

	switch intentCookie.Value {
	case IntentLink:
		h.callbackLink(w, r, profile)
	default:
		h.callbackSignin(w, r, profile)
	}
}

func (h *Handler) callbackSignin(w http.ResponseWriter, r *http.Request, profile Profile) {
	ctx := r.Context()
	repo := h.cfg.Repo

	dp := appauth.DiscordProfile{
		ID:          profile.ID,
		Username:    profile.Username,
		DisplayName: profile.DisplayName(),
		AvatarURL:   profile.AvatarURL,
		Email:       profile.Email,
	}

	// Step 1: existing Discord ID? Refresh + sign in.
	if userID, err := repo.FindUserByDiscordID(ctx, profile.ID); err == nil {
		if err := repo.RefreshDiscordConnection(ctx, userID, dp); err != nil {
			http.Redirect(w, r, h.frontendURL("/login?error=discord-auth"), http.StatusFound)
			return
		}
		_ = repo.TouchLastLogin(ctx, userID)
		h.mintAndRedirect(w, r, userID, "/dashboard")
		return
	}

	// Step 2: no email or unverified email from Discord → reject.
	if profile.Email == "" || !profile.Verified {
		http.Redirect(w, r, h.frontendURL("/login?error=discord-no-email"), http.StatusFound)
		return
	}

	// Step 3: existing user_emails row?
	existingUserID, isVerified, exists, err := repo.FindUserByEmail(ctx, profile.Email)
	if err != nil {
		http.Redirect(w, r, h.frontendURL("/login?error=discord-auth"), http.StatusFound)
		return
	}
	switch {
	case exists && isVerified:
		if err := repo.LinkDiscordToUser(ctx, existingUserID, dp); err != nil {
			http.Redirect(w, r, h.frontendURL("/login?error=discord-auth"), http.StatusFound)
			return
		}
		_ = repo.TouchLastLogin(ctx, existingUserID)
		h.mintAndRedirect(w, r, existingUserID, "/dashboard")
	case exists && !isVerified:
		http.Redirect(w, r, h.frontendURL("/login?error=discord-email-in-use"), http.StatusFound)
	default:
		newID, err := repo.CreateDiscordUser(ctx, dp)
		if err != nil {
			http.Redirect(w, r, h.frontendURL("/login?error=discord-auth"), http.StatusFound)
			return
		}
		h.mintAndRedirect(w, r, newID, "/dashboard")
	}
}

func (h *Handler) mintAndRedirect(w http.ResponseWriter, r *http.Request, userID string, target string) {
	cu, err := h.cfg.Repo.LoadCurrentUser(r.Context(), userID)
	if err != nil {
		http.Redirect(w, r, h.frontendURL("/login?error=discord-auth"), http.StatusFound)
		return
	}
	session := appauth.SessionUser{
		UserID:      cu.User.ID,
		DisplayName: cu.User.DisplayName,
		AvatarURL:   cu.User.AvatarURL,
		Roles:       cu.User.Roles,
		AdminRoles:  cu.AdminRoles,
		Permissions: cu.Permissions,
		Epoch:       cu.User.SessionEpoch,
		ExpiresAt:   time.Now().Add(14 * 24 * time.Hour).UnixMilli(),
	}
	if cu.DiscordAccount != nil {
		session.DiscordID = cu.DiscordAccount.ProviderAccountID
	}
	if cu.PrimaryEmail != nil {
		session.Email = cu.PrimaryEmail.Email
	}
	tok, err := appauth.SerializeSession(session, h.cfg.SessionSecret)
	if err != nil {
		http.Redirect(w, r, h.frontendURL("/login?error=discord-auth"), http.StatusFound)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     appauth.SessionCookieName,
		Value:    tok,
		Path:     "/",
		MaxAge:   60 * 60 * 24 * 14,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, h.frontendURL(target), http.StatusFound)
}
```

Add the import `"time"` to `handlers.go`.

The `callbackLink` method is implemented in Task 14 — for now define a temporary stub so the file compiles:

```go
func (h *Handler) callbackLink(w http.ResponseWriter, r *http.Request, profile Profile) {
	// Implemented in Task 14.
	http.Redirect(w, r, h.frontendURL("/dashboard/settings"), http.StatusFound)
}
```

- [ ] **Step 4: Run tests to verify they pass**

```
cd apps/backend && go test ./internal/oauth/discord/ -run TestCallback_Signin -v
```
Expected: PASS (or SKIP).

- [ ] **Step 5: Commit**

```
git add apps/backend/internal/oauth/discord/handlers.go apps/backend/internal/oauth/discord/handlers_test.go
git commit -m "feat(backend): add Discord OAuth Callback sign-in branch"
```

---

## Task 14: OAuth handler — `Callback` link branch

Implements `callbackLink` properly: idempotent re-link, error redirects on either constraint violation.

**Files:**
- Modify: `apps/backend/internal/oauth/discord/handlers.go`
- Modify: `apps/backend/internal/oauth/discord/handlers_test.go`

- [ ] **Step 1: Write failing tests**

Append to `apps/backend/internal/oauth/discord/handlers_test.go`:

```go
func newCallbackRequestWithSession(state, verifier, intent, code, sessionTok string) *http.Request {
	r := newCallbackRequest(state, verifier, intent, code)
	r.AddCookie(&http.Cookie{Name: "rustlens_session", Value: sessionTok})
	return r
}

func TestCallback_Link_Success(t *testing.T) {
	pool, cleanup := discordTestPool(t)
	defer cleanup()
	ctx := context.Background()
	const userID = "usr_link_success"
	if _, err := pool.Exec(ctx,
		`INSERT INTO app_users (id, display_name) VALUES ($1, 'L') ON CONFLICT (id) DO NOTHING`, userID); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`DELETE FROM user_connected_accounts WHERE user_id=$1`, userID); err != nil {
		t.Fatalf("clean self: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`DELETE FROM user_connected_accounts WHERE provider='discord' AND provider_account_id='d_link_success'`); err != nil {
		t.Fatalf("clean target: %v", err)
	}
	srv := fakeDiscord(t, `{"id":"d_link_success","username":"linker","email":"l@x.com","verified":true}`)
	defer srv.Close()
	h := newCallbackHandler(t, pool, srv.URL)
	tok := mustSerializeSessionForTest(t, "session-secret", userID)

	w := httptest.NewRecorder()
	r := newCallbackRequestWithSession("s", "v", discord.IntentLink, "code", tok)
	h.Callback(w, r)

	if !strings.Contains(w.Header().Get("Location"), "/dashboard/settings?notice=discord-linked") {
		t.Errorf("Location = %q", w.Header().Get("Location"))
	}
}

func TestCallback_Link_AlreadyLinkedToOther(t *testing.T) {
	pool, cleanup := discordTestPool(t)
	defer cleanup()
	ctx := context.Background()
	const ownerID = "usr_link_owner"
	const otherID = "usr_link_other"
	for _, id := range []string{ownerID, otherID} {
		if _, err := pool.Exec(ctx,
			`INSERT INTO app_users (id, display_name) VALUES ($1, 'L') ON CONFLICT (id) DO NOTHING`, id); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
		if _, err := pool.Exec(ctx,
			`DELETE FROM user_connected_accounts WHERE user_id=$1`, id); err != nil {
			t.Fatalf("clean %s: %v", id, err)
		}
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO user_connected_accounts (user_id, provider, provider_account_id, username)
		 VALUES ($1, 'discord', 'd_collide', 'owner')
		 ON CONFLICT (provider, provider_account_id) DO UPDATE SET user_id=$1`, ownerID); err != nil {
		t.Fatalf("seed conn: %v", err)
	}

	srv := fakeDiscord(t, `{"id":"d_collide","username":"x","email":"x@x.com","verified":true}`)
	defer srv.Close()
	h := newCallbackHandler(t, pool, srv.URL)
	tok := mustSerializeSessionForTest(t, "session-secret", otherID)

	w := httptest.NewRecorder()
	r := newCallbackRequestWithSession("s", "v", discord.IntentLink, "code", tok)
	h.Callback(w, r)

	if !strings.Contains(w.Header().Get("Location"), "discord-linked-to-other") {
		t.Errorf("Location = %q", w.Header().Get("Location"))
	}
}

func TestCallback_Link_AlreadyLinkedSameUser_Idempotent(t *testing.T) {
	pool, cleanup := discordTestPool(t)
	defer cleanup()
	ctx := context.Background()
	const userID = "usr_link_idem"
	if _, err := pool.Exec(ctx,
		`INSERT INTO app_users (id, display_name) VALUES ($1, 'I') ON CONFLICT (id) DO NOTHING`, userID); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`DELETE FROM user_connected_accounts WHERE user_id=$1`, userID); err != nil {
		t.Fatalf("clean: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO user_connected_accounts (user_id, provider, provider_account_id, username)
		 VALUES ($1, 'discord', 'd_idem', 'old')
		 ON CONFLICT (provider, provider_account_id) DO UPDATE SET user_id=$1, username='old'`, userID); err != nil {
		t.Fatalf("seed conn: %v", err)
	}
	srv := fakeDiscord(t, `{"id":"d_idem","username":"new_username","email":"i@x.com","verified":true}`)
	defer srv.Close()
	h := newCallbackHandler(t, pool, srv.URL)
	tok := mustSerializeSessionForTest(t, "session-secret", userID)

	w := httptest.NewRecorder()
	r := newCallbackRequestWithSession("s", "v", discord.IntentLink, "code", tok)
	h.Callback(w, r)

	if !strings.Contains(w.Header().Get("Location"), "/dashboard/settings?notice=discord-linked") {
		t.Errorf("Location = %q", w.Header().Get("Location"))
	}
	var u string
	if err := pool.QueryRow(ctx,
		`SELECT username FROM user_connected_accounts WHERE provider='discord' AND provider_account_id='d_idem'`).Scan(&u); err != nil {
		t.Fatalf("read: %v", err)
	}
	if u != "new_username" {
		t.Errorf("username not refreshed: %q", u)
	}
}

func TestCallback_Link_AlreadyLinked_DifferentDiscordOnSameUser(t *testing.T) {
	pool, cleanup := discordTestPool(t)
	defer cleanup()
	ctx := context.Background()
	const userID = "usr_link_dup"
	if _, err := pool.Exec(ctx,
		`INSERT INTO app_users (id, display_name) VALUES ($1, 'D') ON CONFLICT (id) DO NOTHING`, userID); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`DELETE FROM user_connected_accounts WHERE user_id=$1`, userID); err != nil {
		t.Fatalf("clean: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO user_connected_accounts (user_id, provider, provider_account_id, username)
		 VALUES ($1, 'discord', 'd_already_owned', 'owned')
		 ON CONFLICT (provider, provider_account_id) DO UPDATE SET user_id=$1`, userID); err != nil {
		t.Fatalf("seed conn: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`DELETE FROM user_connected_accounts WHERE provider='discord' AND provider_account_id='d_brand_new'`); err != nil {
		t.Fatalf("clean target: %v", err)
	}

	srv := fakeDiscord(t, `{"id":"d_brand_new","username":"x","email":"x@x.com","verified":true}`)
	defer srv.Close()
	h := newCallbackHandler(t, pool, srv.URL)
	tok := mustSerializeSessionForTest(t, "session-secret", userID)

	w := httptest.NewRecorder()
	r := newCallbackRequestWithSession("s", "v", discord.IntentLink, "code", tok)
	h.Callback(w, r)

	if !strings.Contains(w.Header().Get("Location"), "discord-already-linked") {
		t.Errorf("Location = %q", w.Header().Get("Location"))
	}
}

func TestCallback_Link_NoSession(t *testing.T) {
	pool, cleanup := discordTestPool(t)
	defer cleanup()
	srv := fakeDiscord(t, `{"id":"x","username":"x","email":"x@x.com","verified":true}`)
	defer srv.Close()
	h := newCallbackHandler(t, pool, srv.URL)

	w := httptest.NewRecorder()
	r := newCallbackRequest("s", "v", discord.IntentLink, "code")
	h.Callback(w, r)
	if !strings.Contains(w.Header().Get("Location"), "/login?error=session") {
		t.Errorf("Location = %q", w.Header().Get("Location"))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```
cd apps/backend && go test ./internal/oauth/discord/ -run TestCallback_Link -v
```
Expected: FAIL (the stub redirects to `/dashboard/settings` without the expected error/notice query strings).

- [ ] **Step 3: Replace the stub with the real `callbackLink`**

Replace the stub `callbackLink` in `apps/backend/internal/oauth/discord/handlers.go` with:

```go
func (h *Handler) callbackLink(w http.ResponseWriter, r *http.Request, profile Profile) {
	ctx := r.Context()
	session, err := appauth.SessionFromCookie(r, h.cfg.SessionSecret)
	if err != nil || session == nil {
		http.Redirect(w, r, h.frontendURL("/login?error=session"), http.StatusFound)
		return
	}

	dp := appauth.DiscordProfile{
		ID:          profile.ID,
		Username:    profile.Username,
		DisplayName: profile.DisplayName(),
		AvatarURL:   profile.AvatarURL,
		Email:       profile.Email,
	}

	err = h.cfg.Repo.LinkDiscordToUser(ctx, session.UserID, dp)
	switch {
	case err == nil:
		http.Redirect(w, r, h.frontendURL("/dashboard/settings?notice=discord-linked"), http.StatusFound)
	case errors.Is(err, appauth.ErrDiscordLinkedToOther):
		// Could still be the *same* user that the row belongs to (idempotent case);
		// LinkDiscordToUser only returns ErrDiscordLinkedToOther on a PK violation,
		// which means a row already exists for this provider_account_id.
		ownerID, lookupErr := h.cfg.Repo.FindUserByDiscordID(ctx, profile.ID)
		if lookupErr == nil && ownerID == session.UserID {
			if err := h.cfg.Repo.RefreshDiscordConnection(ctx, session.UserID, dp); err != nil {
				http.Redirect(w, r, h.frontendURL("/dashboard/settings?error=discord-linked-to-other"), http.StatusFound)
				return
			}
			http.Redirect(w, r, h.frontendURL("/dashboard/settings?notice=discord-linked"), http.StatusFound)
			return
		}
		http.Redirect(w, r, h.frontendURL("/dashboard/settings?error=discord-linked-to-other"), http.StatusFound)
	case errors.Is(err, appauth.ErrDiscordAlreadyLinked):
		http.Redirect(w, r, h.frontendURL("/dashboard/settings?error=discord-already-linked"), http.StatusFound)
	default:
		http.Redirect(w, r, h.frontendURL("/dashboard/settings?error=discord-already-linked"), http.StatusFound)
	}
}
```

Add `"errors"` to the imports of `handlers.go` if not already present.

- [ ] **Step 4: Run tests to verify they pass**

```
cd apps/backend && go test ./internal/oauth/discord/ -v
```
Expected: PASS (or SKIP).

- [ ] **Step 5: Commit**

```
git add apps/backend/internal/oauth/discord/handlers.go apps/backend/internal/oauth/discord/handlers_test.go
git commit -m "feat(backend): add Discord OAuth Callback link branch"
```

---

## Task 15: Wire OAuth handlers into the router

**Files:**
- Modify: `apps/backend/internal/http/router.go`

- [ ] **Step 1: Edit `router.go`**

Add import at the top of `apps/backend/internal/http/router.go`:

```go
	"rustlens/backend/internal/oauth/discord"
```

After the existing `appAuthRepo := appauth.NewRepo(postgres.Pool())` line, add:

```go
	discordHandler := discord.NewHandler(discord.HandlerConfig{
		ClientID:      cfg.DiscordClientID,
		ClientSecret:  cfg.DiscordClientSecret,
		AppOrigin:     cfg.AppOrigin,
		SessionSecret: cfg.AppSessionSecret,
		Repo:          appAuthRepo,
		Pool:          postgres.Pool(),
	})
```

Above the `r.Route("/api/app", ...)` block, add:

```go
	r.Route("/api/auth/discord", func(r chi.Router) {
		r.Get("/", discordHandler.Initiate)
		r.Get("/link", discordHandler.InitiateLink)
		r.Get("/callback", discordHandler.Callback)
	})
```

- [ ] **Step 2: Build to verify compile**

```
cd apps/backend && go build ./cmd/server
```
Expected: success, no output.

- [ ] **Step 3: Run all backend tests**

```
cd apps/backend && go test ./...
```
Expected: PASS (or SKIP for tests requiring `TEST_POSTGRES_URL`).

- [ ] **Step 4: Commit**

```
git add apps/backend/internal/http/router.go
git commit -m "feat(backend): wire Discord OAuth routes"
```

---

## Task 16: Update OpenAPI doc with Discord OAuth routes

**Files:**
- Modify: `apps/backend/internal/http/openapi.go`

- [ ] **Step 1: Locate the path map and add three entries**

Open `apps/backend/internal/http/openapi.go`. Find the existing `"paths": map[string]any{...}` map. Add the three new entries inside it (placement: at the end of the map, before the closing brace of `"paths"`):

```go
		"/api/auth/discord": map[string]any{
			"get": map[string]any{
				"summary":     "Begin Discord OAuth sign-in",
				"description": "Sets PKCE/state cookies and 302-redirects to Discord. Refuses with /login?error=discord-already-signed-in if the user already has a session cookie.",
				"responses": map[string]any{
					"302": map[string]any{
						"description": "Redirect to Discord authorize endpoint or back to /login on policy refusal.",
					},
				},
			},
		},
		"/api/auth/discord/link": map[string]any{
			"get": map[string]any{
				"summary":     "Begin Discord OAuth account-link flow",
				"description": "Requires an active session (rustlens_session cookie). 302s to /login?error=session when missing.",
				"responses": map[string]any{
					"302": map[string]any{"description": "Redirect to Discord authorize endpoint or /login on missing session."},
				},
			},
		},
		"/api/auth/discord/callback": map[string]any{
			"get": map[string]any{
				"summary":     "Discord OAuth callback (sign-in or link)",
				"description": "Validates state, exchanges code, fetches profile, branches on intent cookie. Sign-in path mints rustlens_session cookie and redirects to /dashboard. Link path redirects to /dashboard/settings with notice or error query string.",
				"parameters": []any{
					map[string]any{"name": "code", "in": "query", "required": true, "schema": map[string]any{"type": "string"}},
					map[string]any{"name": "state", "in": "query", "required": true, "schema": map[string]any{"type": "string"}},
				},
				"responses": map[string]any{
					"302": map[string]any{"description": "Redirect to /dashboard, /dashboard/settings, or /login depending on outcome."},
				},
			},
		},
```

- [ ] **Step 2: Run OpenAPI tests**

```
cd apps/backend && go test ./internal/http/ -run OpenAPI -v
```
Expected: PASS.

- [ ] **Step 3: Commit**

```
git add apps/backend/internal/http/openapi.go
git commit -m "docs(backend): document Discord OAuth routes in OpenAPI"
```

---

## Task 17: Frontend — delete scaffolded Discord routes and helper

**Files:**
- Delete: `apps/frontend/app/auth/discord/route.ts`
- Delete: `apps/frontend/app/auth/callback/discord/route.ts`
- Modify: `apps/frontend/lib/rustlens-api.ts`

- [ ] **Step 1: Delete the two route files**

```
rm apps/frontend/app/auth/discord/route.ts
rm apps/frontend/app/auth/callback/discord/route.ts
```

If the parent `apps/frontend/app/auth/discord/` and `apps/frontend/app/auth/callback/discord/` directories are now empty, also remove them with `rmdir`.

- [ ] **Step 2: Remove the API helper**

Open `apps/frontend/lib/rustlens-api.ts`, locate this block (currently around lines 278–290):

```ts
export function completeDiscordAuth(payload: {
	discordId: string;
	username: string;
	displayName: string;
	avatarUrl: string;
	email: string;
	emailVerified: boolean;
}) {
	return backendFetch<CurrentUser>("/api/app/auth/discord/complete", {
		method: "POST",
		body: JSON.stringify(payload),
	});
}
```

Delete the entire `completeDiscordAuth` function (and its trailing blank line if one separates it from the next export).

- [ ] **Step 3: Verify nothing else imports it**

```
grep -r "completeDiscordAuth" apps/frontend
```
Expected: no matches.

- [ ] **Step 4: Type-check frontend**

```
cd apps/frontend && pnpm lint
```
Expected: PASS (the lint script runs `tsc --noEmit && eslint .`).

- [ ] **Step 5: Commit**

```
git add apps/frontend
git commit -m "refactor(frontend): remove frontend-mediated Discord OAuth scaffolding"
```

---

## Task 18: Frontend — update auth-page Discord button + error codes

**Files:**
- Modify: `apps/frontend/components/auth-page.tsx`

- [ ] **Step 1: Locate the Discord button**

Find the JSX rendering the Discord button in `auth-page.tsx`. It currently uses a Next.js `Link` (or anchor) pointing at `/auth/discord`. Change the `href` to `/api/auth/discord`. (If the button is rendered as a `<Link href="/auth/discord">` change to `<a href="/api/auth/discord">` so Next.js treats it as a full navigation rather than client-side route. The `<Link>` component would attempt client-side routing into a non-existent Next.js route.)

- [ ] **Step 2: Update `getErrorMessage`**

Replace the existing Discord cases in `getErrorMessage` (currently around lines 33–40) with:

```ts
		case "discord-config":
			return "Discord OAuth is not configured yet.";
		case "discord-state":
			return "The Discord sign-in state was invalid or expired.";
		case "discord-token":
		case "discord-profile":
		case "discord-auth":
			return "Discord sign-in failed. Try again.";
		case "discord-no-email":
			return "Discord didn't share an email address. Grant the email scope when prompted, or sign up with a password.";
		case "discord-email-in-use":
			return "That Discord email matches an existing unverified account. Verify that account's email first, or sign up with a different one.";
		case "discord-already-signed-in":
			return "You're already signed in. Use Settings to manage Discord.";
```

(`discord-linked-to-other` and `discord-already-linked` are settings-page errors and don't belong in the login error mapper.)

- [ ] **Step 3: Add or update tests**

Find any existing tests for `getErrorMessage` (likely in `apps/frontend/components/auth-page.test.tsx` if it exists, otherwise create). Add a test:

```tsx
import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { AuthPage } from "@/components/auth-page";

describe("AuthPage error messages", () => {
	it.each([
		"discord-config",
		"discord-state",
		"discord-no-email",
		"discord-email-in-use",
		"discord-already-signed-in",
	])("renders a non-empty message for %s", (code) => {
		render(<AuthPage csrfToken="x" error={code} />);
		expect(screen.queryByText("")).toBeNull();
	});
});
```

- [ ] **Step 4: Run tests + lint**

```
cd apps/frontend && pnpm lint && pnpm test
```
Expected: PASS.

- [ ] **Step 5: Commit**

```
git add apps/frontend
git commit -m "feat(frontend): point Discord button at backend OAuth route"
```

---

## Task 19: Frontend — settings page Discord connection panel

The settings page needs a "Connect Discord" CTA when no Discord is linked, and an "Already connected" indicator when one is. It also needs to surface the new `notice=discord-linked` and `error=discord-linked-to-other` / `error=discord-already-linked` query params.

**Files:**
- Modify: `apps/frontend/app/dashboard/settings/page.tsx`

- [ ] **Step 1: Read the existing settings page**

```
cat apps/frontend/app/dashboard/settings/page.tsx
```

Note where the page reads `searchParams` for existing `error`/`notice` codes — extend that switch with the three new codes. Note where existing connection panels (e.g. Steam) live — place the Discord panel next to them, matching the same component patterns.

- [ ] **Step 2: Add Discord panel and message handling**

Add to the settings page (placement matches existing connection-panel layout):

```tsx
function DiscordPanel({ account }: { account: { username: string; avatarUrl: string } | null }) {
	if (!account) {
		return (
			<section className="rounded-lg border p-4">
				<h2 className="text-lg font-semibold">Discord</h2>
				<p className="text-sm text-muted-foreground">Connect your Discord account to sign in faster and link your identity across RustLens.</p>
				<a
					href="/api/auth/discord/link"
					className="mt-3 inline-flex items-center gap-2 rounded-md bg-[#5865F2] px-3 py-2 text-sm font-medium text-white hover:bg-[#4752c4]"
				>
					Connect Discord
				</a>
			</section>
		);
	}
	return (
		<section className="rounded-lg border p-4">
			<h2 className="text-lg font-semibold">Discord</h2>
			<p className="text-sm">Connected as <span className="font-medium">@{account.username}</span></p>
		</section>
	);
}
```

In the page component itself, wherever `currentUser` is loaded:

```tsx
<DiscordPanel
	account={currentUser.discordAccount ? {
		username: currentUser.discordAccount.username,
		avatarUrl: currentUser.discordAccount.avatarUrl,
	} : null}
/>
```

Extend the existing settings-page error/notice mapping (or add one if absent) with:

```ts
function getSettingsErrorMessage(error?: string) {
	switch (error) {
		case "discord-linked-to-other":
			return "That Discord account is linked to a different RustLens user.";
		case "discord-already-linked":
			return "You already have a Discord account linked.";
		default:
			return "";
	}
}

function getSettingsNoticeMessage(notice?: string) {
	switch (notice) {
		case "discord-linked":
			return "Discord account connected.";
		default:
			return "";
	}
}
```

Render those messages in whatever notice/error region the settings page already uses.

- [ ] **Step 3: Lint and run tests**

```
cd apps/frontend && pnpm lint && pnpm test
```
Expected: PASS.

- [ ] **Step 4: Commit**

```
git add apps/frontend/app/dashboard/settings/page.tsx
git commit -m "feat(frontend): add Discord connection panel to settings"
```

---

## Task 20: Frontend env cleanup

The frontend no longer reads `DISCORD_CLIENT_ID`/`DISCORD_CLIENT_SECRET`. Remove them from any frontend env files and add a comment to the backend `.env.example` linking to the developer portal.

**Files:**
- Modify: `apps/frontend/.env.example`
- Modify: `apps/frontend/.env.local` (only if they were added)
- Modify: `apps/backend/.env.example`

- [ ] **Step 1: Remove Discord env from frontend**

```
grep -n "DISCORD_CLIENT" apps/frontend/.env.example apps/frontend/.env.local 2>/dev/null
```

If any matches, delete those lines from each file.

- [ ] **Step 2: Add Discord setup comment to backend `.env.example`**

Edit `apps/backend/.env.example`. Above the `DISCORD_CLIENT_ID=` line added in Task 2, insert:

```
# Discord OAuth — register an app at https://discord.com/developers/applications
# OAuth2 → Redirects must include ${APP_ORIGIN}/api/auth/discord/callback
```

- [ ] **Step 3: Commit**

```
git add apps/frontend/.env.example apps/frontend/.env.local apps/backend/.env.example
git commit -m "docs: clean up Discord OAuth env files"
```

---

## Task 21: Manual smoke test

This task isn't TDD; it's verifying the integrated flow before declaring the feature done. Each step has an explicit pass/fail expectation.

- [ ] **Step 1: Register a Discord application**

1. Go to https://discord.com/developers/applications and create a new application.
2. OAuth2 → Redirects → add `http://localhost/api/auth/discord/callback`.
3. Copy Client ID and Client Secret into `apps/backend/.env`:
   ```
   DISCORD_CLIENT_ID=<your-id>
   DISCORD_CLIENT_SECRET=<your-secret>
   ```

- [ ] **Step 2: Restart the dev stack**

```
pnpm dev:app
```

Wait for the backend to log `listening on :8080` and the frontend on port 3000.

- [ ] **Step 3: Smoke test 1 — config error path**

1. Temporarily blank `DISCORD_CLIENT_ID` in `apps/backend/.env` and restart the backend.
2. Visit http://localhost/login and click the Discord button.
3. Verify URL becomes `http://localhost/login?error=discord-config` with the matching error message rendered.
4. Restore the env value and restart.

- [ ] **Step 4: Smoke test 2 — fresh sign-in creates account**

1. Sign out (or use a private window).
2. Visit http://localhost/login → click Discord button.
3. Authorize on Discord.
4. Verify URL ends at `http://localhost/dashboard`, the dashboard renders, and DevTools shows a `rustlens_session` cookie.
5. Run `psql $POSTGRES_URL -c "SELECT id, display_name FROM app_users ORDER BY created_at DESC LIMIT 1;"` — verify a new row exists.

- [ ] **Step 5: Smoke test 3 — repeat sign-in is idempotent and refreshes profile**

1. Change your Discord avatar.
2. Sign out, then sign in via Discord again.
3. Run `psql $POSTGRES_URL -c "SELECT username, display_name, avatar_url, updated_at FROM user_connected_accounts WHERE provider='discord' ORDER BY updated_at DESC LIMIT 1;"` — verify the row was updated and the new avatar URL is stored.

- [ ] **Step 6: Smoke test 4 — already-signed-in refuses sign-in flow**

1. While signed in, go to http://localhost/api/auth/discord directly.
2. Verify URL becomes `http://localhost/login?error=discord-already-signed-in`.

- [ ] **Step 7: Smoke test 5 — link from settings**

1. Create a fresh password account (via `/sign-up`) and sign in.
2. Go to `/dashboard/settings`.
3. Click "Connect Discord", authorize.
4. Verify return URL is `/dashboard/settings?notice=discord-linked` and the panel now shows "Connected as @<username>".

- [ ] **Step 8: Smoke test 6 — link target already linked to another user**

1. Sign out. Sign in with password as a *different* account that has no Discord linked.
2. Go to settings → Connect Discord.
3. Authorize using the SAME Discord account from step 7.
4. Verify URL is `/dashboard/settings?error=discord-linked-to-other` with the matching message.

- [ ] **Step 9: Smoke test 7 — re-link same Discord on same user is idempotent**

1. Sign back in as the user from step 7 (whose Discord is already linked).
2. Settings → Connect Discord → authorize.
3. Verify success notice appears (idempotent path).

If any smoke test fails, halt the merge — diagnose before continuing.

- [ ] **Step 10: Final commit if any docs changed**

If you adjusted error messages or other surface during smoke testing, commit those tweaks. Otherwise this task has nothing to commit; record a note in the PR description.

---

## Self-Review

**Spec coverage:** Each spec section maps to tasks:
- Goal/Architecture → Tasks 1–15 (backend) + 17–20 (frontend) cover the architecture switch.
- Routes table → Tasks 11, 12, 13, 14, 15.
- Cookies → Task 8 (constants), Task 11 (set on initiate), Task 13 (cleared on callback).
- Backend implementation → Tasks 8, 9, 11, 12, 13, 14, 15.
- New repo methods → Tasks 3, 4, 5, 6, 7.
- Sign-in callback policy branches → Task 13 covers all five branches.
- Link callback policy branches → Task 14 covers success / collision / idempotent.
- Session minting → Task 13 (`mintAndRedirect`) + Task 1 (`AdminRoles` field).
- OpenAPI → Task 16.
- Frontend deletions and edits → Tasks 17, 18, 19.
- Config / env → Task 2 (backend), Task 20 (frontend cleanup, backend example comment).
- Error/notice codes → Task 18 (auth-page) + Task 19 (settings page).
- Tests → tests embedded in every code task; smoke test in Task 21.

**Placeholder scan:** No "TBD"/"add error handling"/etc. Every step has the actual code.

**Type/name consistency check:** `DiscordProfile` (Tasks 4, 6, 7), `Profile` (Tasks 9, 11–14), `IntentSignin/IntentLink` (Tasks 8, 11, 12, 13), `CookieState/CookieVerifier/CookieIntent` (Tasks 8, 11, 13), `SessionCookieName` (Task 10, 13), `LoadCurrentUser` (already exists, used in 13), `ErrDiscordAlreadyLinked/ErrDiscordLinkedToOther` (Tasks 7, 14), `mintAndRedirect` (Task 13), `RedirectURI(appOrigin)` (Tasks 8, 11). All consistent across tasks.
