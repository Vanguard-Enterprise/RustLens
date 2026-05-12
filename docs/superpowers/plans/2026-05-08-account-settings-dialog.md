# Account Settings Dialog Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The dashboard sidebar's bottom-left **Settings** entry opens a modal with a Notifications section containing two persisted toggles, backed by a new Postgres table and two authenticated REST endpoints.

**Architecture:** Storage in `user_notification_preferences` keyed by `app_users.id`, lazily defaulted on read. Two routes under `/api/app/me/preferences` (GET / PATCH) live in the existing `appauth` package. The frontend exposes a server-only client + Server Actions, called by a new client-side `<AccountSettingsDialog>` rendered from the dashboard sidebar.

**Tech Stack:** Go 1.24 / chi v5 / pgx v5 / Postgres 16 (backend); Next.js 16 / React 19 / shadcn / Tailwind v4 (frontend).

**Spec:** `docs/superpowers/specs/2026-05-08-account-settings-dialog-design.md`.

---

## File Structure

| File | Status | Responsibility |
|---|---|---|
| `apps/backend/internal/store/postgres.go` | modify | Add `user_notification_preferences` to `EnsureSchema()` |
| `apps/backend/internal/store/postgres_schema_test.go` | modify | Idempotency assertion for the new table |
| `apps/backend/internal/appauth/preferences.go` | create | Repo methods (`LoadNotificationPreferences`, `UpsertNotificationPreferences`) and HTTP handlers (`Preferences`, `UpdatePreferences`) |
| `apps/backend/internal/appauth/preferences_test.go` | create | TDD coverage for repo + handlers |
| `apps/backend/internal/http/router.go` | modify | Wire `GET` / `PATCH /api/app/me/preferences` |
| `apps/backend/internal/http/openapi.go` | modify | Add path entries + `NotificationPreferences` schema |
| `apps/backend/AGENTS.md` | modify | Document the new table |
| `apps/frontend/lib/rustlens-api.ts` | modify | `NotificationPreferences` type + `getNotificationPreferences` / `updateNotificationPreferences` |
| `apps/frontend/app/dashboard/_actions/preferences.ts` | create | Server Actions wrapping the API helpers |
| `apps/frontend/components/account-settings-dialog.tsx` | create | Client dialog with the Notifications section |
| `apps/frontend/components/nav-secondary.tsx` | modify | Item type accepts optional `onSelect`; render `<button>` when present |
| `apps/frontend/components/dashboard-app-sidebar.tsx` | modify | Local `settingsOpen` state, Settings entry uses `onSelect`, dialog rendered inside sidebar |

---

## Task 1: Add `user_notification_preferences` table to schema

**Files:**
- Modify: `apps/backend/internal/store/postgres.go` (inside `EnsureSchema`'s SQL string)
- Modify: `apps/backend/internal/store/postgres_schema_test.go`

- [ ] **Step 1: Add an idempotency assertion to the schema test**

Append the following test to `apps/backend/internal/store/postgres_schema_test.go`:

```go
func TestEnsureSchema_UserNotificationPreferences(t *testing.T) {
	ctx := context.Background()
	s := newTestPostgres(t)
	if err := s.EnsureSchema(ctx); err != nil {
		t.Fatalf("first EnsureSchema: %v", err)
	}
	if err := s.EnsureSchema(ctx); err != nil {
		t.Fatalf("second EnsureSchema: %v", err)
	}
	var count int
	err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM information_schema.columns
		WHERE table_name = 'user_notification_preferences'
		  AND column_name IN ('user_id','notify_in_app','notify_discord_announcements','updated_at')
	`).Scan(&count)
	if err != nil {
		t.Fatalf("count columns: %v", err)
	}
	if count != 4 {
		t.Fatalf("expected 4 columns on user_notification_preferences, got %d", count)
	}
}
```

- [ ] **Step 2: Run the test to confirm it fails**

```bash
cd apps/backend && TEST_POSTGRES_URL="$TEST_POSTGRES_URL" go test ./internal/store/ -run TestEnsureSchema_UserNotificationPreferences -v
```

Expected: FAIL — `expected 4 columns on user_notification_preferences, got 0`. (If `TEST_POSTGRES_URL` is unset the test is skipped — set it before continuing; check `apps/backend/.env` or ask the operator.)

- [ ] **Step 3: Add the `CREATE TABLE` statement to `EnsureSchema`**

In `apps/backend/internal/store/postgres.go`, locate the multi-statement SQL string inside `EnsureSchema`. Append a new `CREATE TABLE IF NOT EXISTS` statement (anywhere after the `app_users` table definition — order is irrelevant since `IF NOT EXISTS` is used). The statement:

```sql
CREATE TABLE IF NOT EXISTS user_notification_preferences (
    user_id                       TEXT PRIMARY KEY REFERENCES app_users(id) ON DELETE CASCADE,
    notify_in_app                 BOOLEAN     NOT NULL DEFAULT TRUE,
    notify_discord_announcements  BOOLEAN     NOT NULL DEFAULT TRUE,
    updated_at                    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
-- Flags are storage-only at this point; no delivery system reads them yet.
```

- [ ] **Step 4: Run the test to confirm it passes**

```bash
cd apps/backend && TEST_POSTGRES_URL="$TEST_POSTGRES_URL" go test ./internal/store/ -run TestEnsureSchema -v
```

Expected: PASS for both `TestEnsureSchema_Idempotent` and `TestEnsureSchema_UserNotificationPreferences`.

- [ ] **Step 5: Commit**

```bash
git add apps/backend/internal/store/postgres.go apps/backend/internal/store/postgres_schema_test.go
git commit -m "feat(backend): add user_notification_preferences table"
```

---

## Task 2: Repo methods for loading + upserting preferences

**Files:**
- Create: `apps/backend/internal/appauth/preferences.go`
- Create: `apps/backend/internal/appauth/preferences_test.go`

- [ ] **Step 1: Write failing tests for the repo methods**

Create `apps/backend/internal/appauth/preferences_test.go`:

```go
package appauth_test

import (
	"context"
	"testing"

	"rustlens/backend/internal/appauth"
)

const prefsTestUserID = "usr_prefs_user_1"

func TestRepo_LoadNotificationPreferences_LazyDefault(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx,
		`INSERT INTO app_users (id, discord_id, discord_username, display_name)
		 VALUES ($1, 'discord-prefs-1', 'prefs_tester', 'Prefs')`, prefsTestUserID); err != nil {
		t.Fatalf("seed: %v", err)
	}

	repo := appauth.NewRepo(pool)
	prefs, err := repo.LoadNotificationPreferences(ctx, prefsTestUserID)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !prefs.NotifyInApp || !prefs.NotifyDiscordAnnouncements {
		t.Fatalf("expected both defaults true, got %+v", prefs)
	}

	// Row should now exist.
	var n int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM user_notification_preferences WHERE user_id = $1`,
		prefsTestUserID).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 prefs row after lazy default, got %d", n)
	}
}

func TestRepo_UpsertNotificationPreferences_PartialPatch(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx,
		`INSERT INTO app_users (id, discord_id, discord_username, display_name)
		 VALUES ($1, 'discord-prefs-2', 'prefs_tester2', 'Prefs2')`, prefsTestUserID); err != nil {
		t.Fatalf("seed: %v", err)
	}

	repo := appauth.NewRepo(pool)
	// Patch only the in-app flag; the announcements flag must stay at default true.
	falseVal := false
	got, err := repo.UpsertNotificationPreferences(ctx, prefsTestUserID, appauth.NotificationPreferencesPatch{
		NotifyInApp: &falseVal,
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if got.NotifyInApp != false {
		t.Errorf("NotifyInApp: got %v, want false", got.NotifyInApp)
	}
	if got.NotifyDiscordAnnouncements != true {
		t.Errorf("NotifyDiscordAnnouncements: got %v, want true (default preserved)", got.NotifyDiscordAnnouncements)
	}

	// Now patch the announcements flag too; in-app must remain false.
	trueVal := true
	got, err = repo.UpsertNotificationPreferences(ctx, prefsTestUserID, appauth.NotificationPreferencesPatch{
		NotifyDiscordAnnouncements: &trueVal,
	})
	if err != nil {
		t.Fatalf("upsert 2: %v", err)
	}
	if got.NotifyInApp != false {
		t.Errorf("after second patch NotifyInApp: got %v, want false", got.NotifyInApp)
	}
	if got.NotifyDiscordAnnouncements != true {
		t.Errorf("after second patch NotifyDiscordAnnouncements: got %v, want true", got.NotifyDiscordAnnouncements)
	}
}

func TestRepo_UpsertNotificationPreferences_EmptyPatchReadsCurrent(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx,
		`INSERT INTO app_users (id, discord_id, discord_username, display_name)
		 VALUES ($1, 'discord-prefs-3', 'prefs_tester3', 'Prefs3')`, prefsTestUserID); err != nil {
		t.Fatalf("seed: %v", err)
	}
	repo := appauth.NewRepo(pool)
	got, err := repo.UpsertNotificationPreferences(ctx, prefsTestUserID, appauth.NotificationPreferencesPatch{})
	if err != nil {
		t.Fatalf("upsert empty: %v", err)
	}
	if !got.NotifyInApp || !got.NotifyDiscordAnnouncements {
		t.Errorf("empty patch must leave defaults intact, got %+v", got)
	}
}
```

- [ ] **Step 2: Run the tests to confirm they fail with "undefined" symbols**

```bash
cd apps/backend && TEST_POSTGRES_URL="$TEST_POSTGRES_URL" go test ./internal/appauth/ -run TestRepo_LoadNotificationPreferences -v
```

Expected: FAIL — `undefined: appauth.NotificationPreferencesPatch` (or similar build error).

- [ ] **Step 3: Implement the repo types and methods**

Create `apps/backend/internal/appauth/preferences.go`:

```go
package appauth

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// NotificationPreferences is the per-user notification toggle state.
// Both flags default to TRUE (opt-out model). They are storage-only at
// this point — no delivery system reads them yet.
type NotificationPreferences struct {
	NotifyInApp                bool `json:"notifyInApp"`
	NotifyDiscordAnnouncements bool `json:"notifyDiscordAnnouncements"`
}

// NotificationPreferencesPatch is a partial update. Nil pointers leave
// the corresponding column unchanged.
type NotificationPreferencesPatch struct {
	NotifyInApp                *bool
	NotifyDiscordAnnouncements *bool
}

// LoadNotificationPreferences returns the user's notification preferences,
// creating a default row if none exists. The lazy-default behaviour means
// callers never have to handle a "missing row" case.
func (r *Repo) LoadNotificationPreferences(ctx context.Context, userID string) (NotificationPreferences, error) {
	var prefs NotificationPreferences
	err := r.pool.QueryRow(ctx, `
		INSERT INTO user_notification_preferences (user_id)
		VALUES ($1)
		ON CONFLICT (user_id) DO UPDATE SET user_id = EXCLUDED.user_id
		RETURNING notify_in_app, notify_discord_announcements
	`, userID).Scan(&prefs.NotifyInApp, &prefs.NotifyDiscordAnnouncements)
	if errors.Is(err, pgx.ErrNoRows) {
		return NotificationPreferences{}, ErrUserNotFound
	}
	if err != nil {
		return NotificationPreferences{}, err
	}
	return prefs, nil
}

// UpsertNotificationPreferences applies a partial update and returns the
// resulting row. Nil pointers in the patch leave that column unchanged.
// An empty patch is a no-op that returns current values (creating the
// default row if absent, just like LoadNotificationPreferences).
func (r *Repo) UpsertNotificationPreferences(ctx context.Context, userID string, patch NotificationPreferencesPatch) (NotificationPreferences, error) {
	var prefs NotificationPreferences
	// COALESCE($N::boolean, column) keeps the existing column value when the
	// patch field is nil; the boolean cast is required because pgx encodes a
	// nil *bool as a typeless NULL otherwise.
	err := r.pool.QueryRow(ctx, `
		INSERT INTO user_notification_preferences (user_id, notify_in_app, notify_discord_announcements, updated_at)
		VALUES ($1, COALESCE($2::boolean, TRUE), COALESCE($3::boolean, TRUE), NOW())
		ON CONFLICT (user_id) DO UPDATE SET
			notify_in_app                = COALESCE($2::boolean, user_notification_preferences.notify_in_app),
			notify_discord_announcements = COALESCE($3::boolean, user_notification_preferences.notify_discord_announcements),
			updated_at                   = NOW()
		RETURNING notify_in_app, notify_discord_announcements
	`, userID, patch.NotifyInApp, patch.NotifyDiscordAnnouncements).
		Scan(&prefs.NotifyInApp, &prefs.NotifyDiscordAnnouncements)
	if err != nil {
		return NotificationPreferences{}, err
	}
	return prefs, nil
}
```

- [ ] **Step 4: Run tests to confirm they pass**

```bash
cd apps/backend && TEST_POSTGRES_URL="$TEST_POSTGRES_URL" go test ./internal/appauth/ -run TestRepo_ -v
```

Expected: all three `TestRepo_*` tests PASS.

- [ ] **Step 5: Run vet to confirm no issues**

```bash
cd apps/backend && go vet ./internal/appauth/...
```

Expected: no output (clean).

- [ ] **Step 6: Commit**

```bash
git add apps/backend/internal/appauth/preferences.go apps/backend/internal/appauth/preferences_test.go
git commit -m "feat(backend): add notification preferences repo"
```

---

## Task 3: HTTP handlers for `GET` / `PATCH /api/app/me/preferences`

**Files:**
- Modify: `apps/backend/internal/appauth/preferences.go` (append handler methods)
- Modify: `apps/backend/internal/appauth/preferences_test.go` (append handler tests)

- [ ] **Step 1: Write failing handler tests**

In `apps/backend/internal/appauth/preferences_test.go`, replace the existing import block with the union of old and new imports:

```go
import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"rustlens/backend/internal/appauth"
)
```

Then append the following functions to the file:

```go
// withSession wraps a handler so the appauth.SessionUser is attached the same
// way the RequireSession middleware would. Mirrors the pattern used elsewhere
// in this package for tests that bypass the middleware.
func withSession(userID string, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := appauth.ContextWithSession(r.Context(), &appauth.SessionUser{UserID: userID})
		h.ServeHTTP(w, r.WithContext(ctx))
	}
}

func TestHandler_Preferences_Get_LazyDefault(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx,
		`INSERT INTO app_users (id, discord_id, discord_username, display_name)
		 VALUES ($1, 'discord-prefs-h1', 'prefs_h1', 'P')`, prefsTestUserID); err != nil {
		t.Fatalf("seed: %v", err)
	}
	h := appauth.NewHandler(appauth.NewRepo(pool))

	req := httptest.NewRequest(http.MethodGet, "/api/app/me/preferences", nil)
	rec := httptest.NewRecorder()
	withSession(prefsTestUserID, h.Preferences)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var got appauth.NotificationPreferences
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.NotifyInApp || !got.NotifyDiscordAnnouncements {
		t.Errorf("expected both defaults true, got %+v", got)
	}
}

func TestHandler_Preferences_Patch_PartialUpdate(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx,
		`INSERT INTO app_users (id, discord_id, discord_username, display_name)
		 VALUES ($1, 'discord-prefs-h2', 'prefs_h2', 'P2')`, prefsTestUserID); err != nil {
		t.Fatalf("seed: %v", err)
	}
	h := appauth.NewHandler(appauth.NewRepo(pool))

	body := bytes.NewBufferString(`{"notifyInApp": false}`)
	req := httptest.NewRequest(http.MethodPatch, "/api/app/me/preferences", body)
	rec := httptest.NewRecorder()
	withSession(prefsTestUserID, h.UpdatePreferences)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var got appauth.NotificationPreferences
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.NotifyInApp != false {
		t.Errorf("NotifyInApp: got %v, want false", got.NotifyInApp)
	}
	if got.NotifyDiscordAnnouncements != true {
		t.Errorf("NotifyDiscordAnnouncements: got %v, want true (untouched)", got.NotifyDiscordAnnouncements)
	}
}

func TestHandler_Preferences_Patch_RejectsUnknownField(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx,
		`INSERT INTO app_users (id, discord_id, discord_username, display_name)
		 VALUES ($1, 'discord-prefs-h3', 'prefs_h3', 'P3')`, prefsTestUserID); err != nil {
		t.Fatalf("seed: %v", err)
	}
	h := appauth.NewHandler(appauth.NewRepo(pool))

	body := bytes.NewBufferString(`{"emailMarketing": true}`)
	req := httptest.NewRequest(http.MethodPatch, "/api/app/me/preferences", body)
	rec := httptest.NewRecorder()
	withSession(prefsTestUserID, h.UpdatePreferences)(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandler_Preferences_Patch_RejectsNonBoolean(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx,
		`INSERT INTO app_users (id, discord_id, discord_username, display_name)
		 VALUES ($1, 'discord-prefs-h4', 'prefs_h4', 'P4')`, prefsTestUserID); err != nil {
		t.Fatalf("seed: %v", err)
	}
	h := appauth.NewHandler(appauth.NewRepo(pool))

	body := bytes.NewBufferString(`{"notifyInApp": "yes"}`)
	req := httptest.NewRequest(http.MethodPatch, "/api/app/me/preferences", body)
	rec := httptest.NewRecorder()
	withSession(prefsTestUserID, h.UpdatePreferences)(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandler_Preferences_NoSession_401(t *testing.T) {
	pool := testPool(t)
	h := appauth.NewHandler(appauth.NewRepo(pool))

	req := httptest.NewRequest(http.MethodGet, "/api/app/me/preferences", nil)
	rec := httptest.NewRecorder()
	h.Preferences(rec, req) // no withSession wrapper

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want 401", rec.Code)
	}
}
```

- [ ] **Step 2: Confirm `ContextWithSession` exists; if not, expose it**

The tests call `appauth.ContextWithSession`. Check `apps/backend/internal/appauth/middleware.go` — if the package only stores sessions via the unexported `sessionCtxKey` and exposes `SessionFromContext` but no setter, add this exported helper to `middleware.go`:

```go
// ContextWithSession attaches a session to ctx. Test-only; production code
// uses RequireSession which sets the same key.
func ContextWithSession(ctx context.Context, s *SessionUser) context.Context {
	return context.WithValue(ctx, sessionCtxKey, s)
}
```

If a similar helper already exists, use that name in the tests instead. Run a quick grep before adding:

```bash
grep -n "ContextWithSession\|WithValue.*sessionCtxKey" apps/backend/internal/appauth/*.go
```

- [ ] **Step 3: Run the tests to confirm they fail**

```bash
cd apps/backend && TEST_POSTGRES_URL="$TEST_POSTGRES_URL" go test ./internal/appauth/ -run TestHandler_Preferences -v
```

Expected: FAIL — `h.Preferences undefined` and `h.UpdatePreferences undefined`.

- [ ] **Step 4: Implement the handlers**

Append to `apps/backend/internal/appauth/preferences.go`:

```go
// Preferences handles GET /api/app/me/preferences. Requires RequireSession.
// Lazily creates a default row if none exists.
func (h *Handler) Preferences(w http.ResponseWriter, r *http.Request) {
	session := SessionFromContext(r.Context())
	if session == nil {
		writeJSONError(w, http.StatusUnauthorized, "session_required")
		return
	}
	prefs, err := h.repo.LoadNotificationPreferences(r.Context(), session.UserID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	writeJSON(w, http.StatusOK, prefs)
}

// UpdatePreferences handles PATCH /api/app/me/preferences. Requires
// RequireSession. Body is a strict-decoded subset of NotificationPreferences;
// absent fields are unchanged. Unknown fields and non-boolean values → 400.
func (h *Handler) UpdatePreferences(w http.ResponseWriter, r *http.Request) {
	session := SessionFromContext(r.Context())
	if session == nil {
		writeJSONError(w, http.StatusUnauthorized, "session_required")
		return
	}

	var raw struct {
		NotifyInApp                *bool `json:"notifyInApp"`
		NotifyDiscordAnnouncements *bool `json:"notifyDiscordAnnouncements"`
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&raw); err != nil && !errors.Is(err, io.EOF) {
		writeJSONError(w, http.StatusBadRequest, "bad_request")
		return
	}

	prefs, err := h.repo.UpsertNotificationPreferences(r.Context(), session.UserID, NotificationPreferencesPatch{
		NotifyInApp:                raw.NotifyInApp,
		NotifyDiscordAnnouncements: raw.NotifyDiscordAnnouncements,
	})
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	writeJSON(w, http.StatusOK, prefs)
}
```

Add imports `encoding/json`, `errors`, `io`, `net/http` at the top of `preferences.go` if not already present.

- [ ] **Step 5: Run tests to confirm they pass**

```bash
cd apps/backend && TEST_POSTGRES_URL="$TEST_POSTGRES_URL" go test ./internal/appauth/ -v
```

Expected: all `TestRepo_*` and `TestHandler_Preferences_*` tests PASS.

- [ ] **Step 6: Run vet**

```bash
cd apps/backend && go vet ./internal/appauth/...
```

Expected: no output.

- [ ] **Step 7: Commit**

```bash
git add apps/backend/internal/appauth/preferences.go apps/backend/internal/appauth/preferences_test.go apps/backend/internal/appauth/middleware.go
git commit -m "feat(backend): add notification preferences handlers"
```

---

## Task 4: Wire routes in `router.go`

**Files:**
- Modify: `apps/backend/internal/http/router.go`

- [ ] **Step 1: Add the two routes inside the existing session-gated group**

In `apps/backend/internal/http/router.go`, locate the block:

```go
r.Group(func(r chi.Router) {
    r.Use(requireSession)
    r.Get("/me", appAuthHandler.Me)
})
```

Replace it with:

```go
r.Group(func(r chi.Router) {
    r.Use(requireSession)
    r.Get("/me", appAuthHandler.Me)
    r.Get("/me/preferences", appAuthHandler.Preferences)
    r.Patch("/me/preferences", appAuthHandler.UpdatePreferences)
})
```

- [ ] **Step 2: Build to confirm everything compiles**

```bash
cd apps/backend && go build ./cmd/server
```

Expected: clean build, no output.

- [ ] **Step 3: Re-run the appauth tests as a smoke check**

```bash
cd apps/backend && TEST_POSTGRES_URL="$TEST_POSTGRES_URL" go test ./internal/appauth/ -v
```

Expected: all PASS.

- [ ] **Step 4: Commit**

```bash
git add apps/backend/internal/http/router.go
git commit -m "feat(backend): wire /api/app/me/preferences routes"
```

---

## Task 5: Update OpenAPI spec and AGENTS.md

**Files:**
- Modify: `apps/backend/internal/http/openapi.go`
- Modify: `apps/backend/AGENTS.md`

- [ ] **Step 1: Add the path entry**

In `apps/backend/internal/http/openapi.go`, find the `/api/app/me` entry (around line 2044). Immediately after the `/api/app/me` map (after its closing `}` and before `/api/app/me/connections`), insert:

```go
"/api/app/me/preferences": map[string]any{
    "get": map[string]any{
        "summary":     "Get the current user's notification preferences",
        "description": "Returns the signed-in user's notification preferences. Lazily creates a default row (both flags true) if none exists.",
        "security":    []map[string][]string{{"appSecretAuth": {}, "appSessionAuth": {}}},
        "responses": map[string]any{
            "200": map[string]any{
                "description": "Preferences returned",
                "content": map[string]any{
                    "application/json": map[string]any{
                        "schema": map[string]any{"$ref": "#/components/schemas/NotificationPreferences"},
                    },
                },
            },
            "401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
        },
    },
    "patch": map[string]any{
        "summary":     "Update the current user's notification preferences",
        "description": "Applies a partial update. Absent fields are unchanged. Unknown fields and non-boolean values return 400.",
        "security":    []map[string][]string{{"appSecretAuth": {}, "appSessionAuth": {}}},
        "requestBody": map[string]any{
            "required": true,
            "content": map[string]any{
                "application/json": map[string]any{
                    "schema": map[string]any{"$ref": "#/components/schemas/NotificationPreferencesPatch"},
                },
            },
        },
        "responses": map[string]any{
            "200": map[string]any{
                "description": "Updated preferences returned",
                "content": map[string]any{
                    "application/json": map[string]any{
                        "schema": map[string]any{"$ref": "#/components/schemas/NotificationPreferences"},
                    },
                },
            },
            "400": map[string]any{"$ref": "#/components/responses/BadRequest"},
            "401": map[string]any{"$ref": "#/components/responses/Unauthorized"},
        },
    },
},
```

- [ ] **Step 2: Add the schemas**

Inside the `"schemas": map[string]any{ ... }` block (around line 2344), add two new schema entries (alphabetical position is fine — append near similar `*Response`/request schemas):

```go
"NotificationPreferences": map[string]any{
    "type": "object",
    "properties": map[string]any{
        "notifyInApp":                map[string]any{"type": "boolean"},
        "notifyDiscordAnnouncements": map[string]any{"type": "boolean"},
    },
    "required": []string{"notifyInApp", "notifyDiscordAnnouncements"},
},
"NotificationPreferencesPatch": map[string]any{
    "type": "object",
    "properties": map[string]any{
        "notifyInApp":                map[string]any{"type": "boolean"},
        "notifyDiscordAnnouncements": map[string]any{"type": "boolean"},
    },
    "additionalProperties": false,
},
```

- [ ] **Step 3: Confirm `BadRequest` response component exists**

```bash
grep -n "\"BadRequest\":" apps/backend/internal/http/openapi.go
```

If no match, add a `"BadRequest"` entry to the `responses` block alongside `Unauthorized` / `NotFound`:

```go
"BadRequest": map[string]any{
    "description": "Bad request",
    "content": map[string]any{
        "application/json": map[string]any{
            "schema": map[string]any{"$ref": "#/components/schemas/ErrorResponse"},
        },
    },
},
```

- [ ] **Step 4: Document the new table in AGENTS.md**

In `apps/backend/AGENTS.md`, under the "Database Schema" section, add a new entry alphabetically near other `user_*` tables (or at the end of the section):

```markdown
### `user_notification_preferences`

Per-user notification toggle state. Both flags default to `TRUE` (opt-out). Storage-only at this point — no delivery system reads them yet.

| Column | Type | Notes |
|---|---|---|
| `user_id` | `TEXT` | PK, FK to `app_users.id` ON DELETE CASCADE |
| `notify_in_app` | `BOOLEAN NOT NULL DEFAULT TRUE` | Future in-app notification centre |
| `notify_discord_announcements` | `BOOLEAN NOT NULL DEFAULT TRUE` | Future bot DM announcements |
| `updated_at` | `TIMESTAMPTZ NOT NULL DEFAULT NOW()` | |

Lazily upserted on first `GET /api/app/me/preferences` so handlers never see a missing-row case.
```

- [ ] **Step 5: Confirm OpenAPI still parses**

The spec is built programmatically and only fails at runtime. Build and run a quick fetch:

```bash
cd apps/backend && go build ./cmd/server
```

Expected: clean build.

- [ ] **Step 6: Commit**

```bash
git add apps/backend/internal/http/openapi.go apps/backend/AGENTS.md
git commit -m "docs(backend): document notification preferences endpoints"
```

---

## Task 6: Frontend API helpers

**Files:**
- Modify: `apps/frontend/lib/rustlens-api.ts`

- [ ] **Step 1: Add the type and two helper functions**

In `apps/frontend/lib/rustlens-api.ts`, add the type near other small response types (e.g. just below `TokenResponse` around line 207):

```ts
export type NotificationPreferences = {
	notifyInApp: boolean;
	notifyDiscordAnnouncements: boolean;
};

export type NotificationPreferencesPatch = Partial<NotificationPreferences>;
```

Then add two helper functions near the other `/api/app/me/*` helpers (around line 352, after `getCurrentUserSafe`):

```ts
export function getNotificationPreferences() {
	return backendFetch<NotificationPreferences>(
		"/api/app/me/preferences",
		undefined,
		{ requireSession: true },
	);
}

export function updateNotificationPreferences(patch: NotificationPreferencesPatch) {
	return backendFetch<NotificationPreferences>(
		"/api/app/me/preferences",
		{ method: "PATCH", body: JSON.stringify(patch) },
		{ requireSession: true },
	);
}
```

- [ ] **Step 2: Run typecheck**

```bash
cd apps/frontend && pnpm lint
```

Expected: clean (or pre-existing errors unrelated to this change). If the lint script fails for unrelated reasons, run only the typecheck:

```bash
cd apps/frontend && npx tsc --noEmit
```

- [ ] **Step 3: Commit**

```bash
git add apps/frontend/lib/rustlens-api.ts
git commit -m "feat(frontend): add notification preferences API helpers"
```

---

## Task 7: Server Actions for preferences

**Files:**
- Create: `apps/frontend/app/dashboard/_actions/preferences.ts`

- [ ] **Step 1: Create the actions file**

Create `apps/frontend/app/dashboard/_actions/preferences.ts`:

```ts
"use server";

import {
	getNotificationPreferences,
	updateNotificationPreferences,
	type NotificationPreferences,
	type NotificationPreferencesPatch,
} from "@/lib/rustlens-api";
import { requireSession } from "@/lib/session";

export type LoadPreferencesResult =
	| { ok: true; preferences: NotificationPreferences }
	| { ok: false; error: string };

export type SavePreferencesResult =
	| { ok: true; preferences: NotificationPreferences }
	| { ok: false; error: string };

export async function loadNotificationPreferencesAction(): Promise<LoadPreferencesResult> {
	await requireSession();
	try {
		const preferences = await getNotificationPreferences();
		return { ok: true, preferences };
	} catch (err) {
		return { ok: false, error: (err as Error).message };
	}
}

export async function saveNotificationPreferencesAction(
	patch: NotificationPreferencesPatch,
): Promise<SavePreferencesResult> {
	await requireSession();
	try {
		const preferences = await updateNotificationPreferences(patch);
		return { ok: true, preferences };
	} catch (err) {
		return { ok: false, error: (err as Error).message };
	}
}
```

- [ ] **Step 2: Run typecheck**

```bash
cd apps/frontend && npx tsc --noEmit
```

Expected: clean.

- [ ] **Step 3: Commit**

```bash
git add apps/frontend/app/dashboard/_actions/preferences.ts
git commit -m "feat(frontend): add notification preferences server actions"
```

---

## Task 8: Build the `<AccountSettingsDialog>` component

**Files:**
- Create: `apps/frontend/components/account-settings-dialog.tsx`

- [ ] **Step 1: Create the dialog component**

Create `apps/frontend/components/account-settings-dialog.tsx`:

```tsx
"use client";

import * as React from "react";

import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Switch } from "@/components/ui/switch";
import { Label } from "@/components/ui/label";
import { Alert, AlertDescription } from "@/components/ui/alert";

import {
	loadNotificationPreferencesAction,
	saveNotificationPreferencesAction,
} from "@/app/dashboard/_actions/preferences";
import type { NotificationPreferences } from "@/lib/rustlens-api";

type LoadState =
	| { status: "idle" }
	| { status: "loading" }
	| { status: "loaded"; loaded: NotificationPreferences; draft: NotificationPreferences }
	| { status: "error"; message: string };

type SaveState =
	| { status: "idle" }
	| { status: "saving" }
	| { status: "error"; message: string };

export function AccountSettingsDialog({
	open,
	onOpenChange,
}: {
	open: boolean;
	onOpenChange: (open: boolean) => void;
}) {
	const [loadState, setLoadState] = React.useState<LoadState>({ status: "idle" });
	const [saveState, setSaveState] = React.useState<SaveState>({ status: "idle" });

	const load = React.useCallback(async () => {
		setLoadState({ status: "loading" });
		const result = await loadNotificationPreferencesAction();
		if (!result.ok) {
			setLoadState({ status: "error", message: result.error });
			return;
		}
		setLoadState({
			status: "loaded",
			loaded: result.preferences,
			draft: result.preferences,
		});
	}, []);

	// Load preferences each time the dialog opens; reset state when it closes.
	React.useEffect(() => {
		if (open) {
			void load();
			setSaveState({ status: "idle" });
		} else {
			setLoadState({ status: "idle" });
		}
	}, [open, load]);

	const setDraftField = (field: keyof NotificationPreferences, value: boolean) => {
		setLoadState((prev) =>
			prev.status === "loaded"
				? { ...prev, draft: { ...prev.draft, [field]: value } }
				: prev,
		);
	};

	const dirty =
		loadState.status === "loaded" &&
		(loadState.draft.notifyInApp !== loadState.loaded.notifyInApp ||
			loadState.draft.notifyDiscordAnnouncements !==
				loadState.loaded.notifyDiscordAnnouncements);

	const handleSave = async () => {
		if (loadState.status !== "loaded" || !dirty) return;
		setSaveState({ status: "saving" });
		const patch: Partial<NotificationPreferences> = {};
		if (loadState.draft.notifyInApp !== loadState.loaded.notifyInApp) {
			patch.notifyInApp = loadState.draft.notifyInApp;
		}
		if (
			loadState.draft.notifyDiscordAnnouncements !==
			loadState.loaded.notifyDiscordAnnouncements
		) {
			patch.notifyDiscordAnnouncements = loadState.draft.notifyDiscordAnnouncements;
		}
		const result = await saveNotificationPreferencesAction(patch);
		if (!result.ok) {
			setSaveState({ status: "error", message: result.error });
			return;
		}
		setSaveState({ status: "idle" });
		onOpenChange(false);
	};

	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent className="sm:max-w-md">
				<DialogHeader>
					<DialogTitle>Account settings</DialogTitle>
					<DialogDescription>
						Manage how RustLens contacts you.
					</DialogDescription>
				</DialogHeader>

				<section aria-labelledby="notifications-heading" className="space-y-4">
					<div>
						<h3 id="notifications-heading" className="text-sm font-medium">
							Notifications
						</h3>
					</div>

					{loadState.status === "loading" || loadState.status === "idle" ? (
						<div className="text-sm text-muted-foreground">Loading…</div>
					) : loadState.status === "error" ? (
						<Alert variant="destructive">
							<AlertDescription className="flex items-center justify-between gap-3">
								<span>Couldn&apos;t load preferences: {loadState.message}</span>
								<Button size="sm" variant="outline" onClick={() => void load()}>
									Retry
								</Button>
							</AlertDescription>
						</Alert>
					) : (
						<div className="space-y-4">
							<div className="flex items-start justify-between gap-4">
								<div className="space-y-1">
									<Label htmlFor="notify-in-app">In-app notifications</Label>
									<p className="text-xs text-muted-foreground">
										Show alerts in the dashboard about activity in your guilds.
									</p>
								</div>
								<Switch
									id="notify-in-app"
									checked={loadState.draft.notifyInApp}
									onCheckedChange={(v) => setDraftField("notifyInApp", v)}
								/>
							</div>

							<div className="flex items-start justify-between gap-4">
								<div className="space-y-1">
									<Label htmlFor="notify-discord">Discord product updates</Label>
									<p className="text-xs text-muted-foreground">
										Allow the bot to DM you about RustLens product updates and
										announcements. Guild-event DMs are unaffected.
									</p>
								</div>
								<Switch
									id="notify-discord"
									checked={loadState.draft.notifyDiscordAnnouncements}
									onCheckedChange={(v) =>
										setDraftField("notifyDiscordAnnouncements", v)
									}
								/>
							</div>
						</div>
					)}
				</section>

				{saveState.status === "error" ? (
					<Alert variant="destructive">
						<AlertDescription>
							Couldn&apos;t save: {saveState.message}
						</AlertDescription>
					</Alert>
				) : null}

				<DialogFooter>
					<Button
						variant="outline"
						onClick={() => onOpenChange(false)}
						disabled={saveState.status === "saving"}
					>
						Cancel
					</Button>
					<Button
						onClick={() => void handleSave()}
						disabled={
							loadState.status !== "loaded" ||
							!dirty ||
							saveState.status === "saving"
						}
					>
						{saveState.status === "saving" ? "Saving…" : "Save changes"}
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
```

- [ ] **Step 2: Run typecheck**

```bash
cd apps/frontend && npx tsc --noEmit
```

Expected: clean.

- [ ] **Step 3: Commit**

```bash
git add apps/frontend/components/account-settings-dialog.tsx
git commit -m "feat(frontend): add account settings dialog component"
```

---

## Task 9: Extend `NavSecondary` to support `onSelect` items

**Files:**
- Modify: `apps/frontend/components/nav-secondary.tsx`

- [ ] **Step 1: Widen the item type and render a button when `onSelect` is set**

Replace the entire body of `apps/frontend/components/nav-secondary.tsx` with:

```tsx
"use client"

import * as React from "react"

import {
  SidebarGroup,
  SidebarGroupContent,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
} from "@/components/ui/sidebar"

export type NavSecondaryItem = {
  title: string
  icon: React.ReactNode
  url?: string
  onSelect?: () => void
}

export function NavSecondary({
  items,
  ...props
}: {
  items: NavSecondaryItem[]
} & React.ComponentPropsWithoutRef<typeof SidebarGroup>) {
  return (
    <SidebarGroup {...props}>
      <SidebarGroupContent>
        <SidebarMenu>
          {items.map((item) => (
            <SidebarMenuItem key={item.title}>
              {item.onSelect ? (
                <SidebarMenuButton asChild>
                  <button type="button" onClick={item.onSelect}>
                    {item.icon}
                    <span>{item.title}</span>
                  </button>
                </SidebarMenuButton>
              ) : (
                <SidebarMenuButton asChild>
                  <a href={item.url ?? "#"}>
                    {item.icon}
                    <span>{item.title}</span>
                  </a>
                </SidebarMenuButton>
              )}
            </SidebarMenuItem>
          ))}
        </SidebarMenu>
      </SidebarGroupContent>
    </SidebarGroup>
  )
}
```

- [ ] **Step 2: Run typecheck**

```bash
cd apps/frontend && npx tsc --noEmit
```

Expected: clean. (The existing call sites in `dashboard-app-sidebar.tsx` still pass `{ title, url, icon }` shapes — `onSelect` is optional, so they remain valid.)

- [ ] **Step 3: Commit**

```bash
git add apps/frontend/components/nav-secondary.tsx
git commit -m "refactor(frontend): allow NavSecondary items to use onSelect"
```

---

## Task 10: Wire the dialog into the dashboard sidebar

**Files:**
- Modify: `apps/frontend/components/dashboard-app-sidebar.tsx`

- [ ] **Step 1: Convert the Settings entry to use `onSelect` and render the dialog**

In `apps/frontend/components/dashboard-app-sidebar.tsx`:

**(a)** Remove the module-level `NAV_SECONDARY` constant (around lines 94–98). The Settings entry now closes over component state, so it must be built inside the component.

**(b)** Add the dialog import at the top of the file:

```tsx
import { AccountSettingsDialog } from "@/components/account-settings-dialog";
```

**(c)** Inside the `DashboardAppSidebar` function body (above the `return`), add:

```tsx
const [settingsOpen, setSettingsOpen] = React.useState(false);

const navSecondary = React.useMemo(
    () => [
        {
            title: "Settings",
            icon: <Settings2Icon />,
            onSelect: () => setSettingsOpen(true),
        },
        { title: "Get Help", icon: <CircleHelpIcon />, url: "#" },
        { title: "Search", icon: <SearchIcon />, url: "#" },
    ],
    [],
);
```

**(d)** Replace the `<NavSecondary items={NAV_SECONDARY} className="mt-auto" />` line (around line 205) with:

```tsx
<NavSecondary items={navSecondary} className="mt-auto" />
```

**(e)** Inside the `return`, change the outer JSX from a single `<Sidebar>` element to a fragment that also renders the dialog. Find the outer return (`return ( <Sidebar ...>` around line 132–212) and wrap it:

```tsx
return (
    <>
        <Sidebar collapsible="icon" variant={sidebarVariant} {...props}>
            {/* unchanged children */}
        </Sidebar>
        <AccountSettingsDialog open={settingsOpen} onOpenChange={setSettingsOpen} />
    </>
);
```

- [ ] **Step 2: Run typecheck**

```bash
cd apps/frontend && npx tsc --noEmit
```

Expected: clean.

- [ ] **Step 3: Manual verification**

Start the dev stack and verify the feature in a browser. The frontend dev server runs from `apps/frontend`:

```bash
cd apps/frontend && pnpm dev
```

(The backend must also be running locally — see `apps/backend/AGENTS.md`.)

Manual test cases — with a Discord-OAuth-signed-in account on `/dashboard`:

1. **Open dialog:** Click the **Settings** item at the bottom of the sidebar. Dialog opens with title "Account settings" and the Notifications section visible. Both switches load as ON (defaults).
2. **Toggle and save:** Flip "In-app notifications" to OFF and click **Save changes**. The dialog closes. Re-open it: the switch is still OFF.
3. **Cancel discards:** Flip a switch, click **Cancel**. Re-open the dialog — the change was not persisted.
4. **No-op disabled:** Open the dialog without changing anything. **Save changes** is disabled.
5. **Reload persistence:** Hard-refresh the page and re-open the dialog. The previously-saved value still shows.
6. **Cross-device check (optional):** Sign in to the same account from another browser and confirm the saved value carries over.
7. **Network error surfaces:** Stop the backend, open the dialog. The Notifications section should show an inline alert with a **Retry** button. Restart the backend, click Retry — the section recovers.

If any test case fails, fix the issue inline before committing.

- [ ] **Step 4: Commit**

```bash
git add apps/frontend/components/dashboard-app-sidebar.tsx
git commit -m "feat(frontend): wire account settings dialog into sidebar"
```

---

## Self-Review Notes

- **Spec coverage:**
  - Sidebar wiring → Tasks 9, 10.
  - DB table → Task 1.
  - GET / PATCH endpoints + lazy default → Tasks 2, 3, 4.
  - OpenAPI doc → Task 5.
  - Dialog with two toggles, dirty save, cancel, error handling → Task 8.
  - Backend test cases listed in spec → all covered in Tasks 2 + 3.
  - Out-of-scope items (delivery wiring, email, tabs) stay out — no tasks added.
- **Type consistency:** field names `notifyInApp` / `notifyDiscordAnnouncements` are identical across the Go struct, the TS type, the OpenAPI schema, the dialog state, and the toggle handlers. Method names `LoadNotificationPreferences` / `UpsertNotificationPreferences` match between repo and tests; handler names `Preferences` / `UpdatePreferences` match between handler file, tests, and router.
- **No placeholders:** every step contains the actual change.

---

**Plan complete and saved to `docs/superpowers/plans/2026-05-08-account-settings-dialog.md`. Two execution options:**

1. **Subagent-Driven (recommended)** — fresh subagent per task, review between tasks, fast iteration.
2. **Inline Execution** — execute tasks in this session using `executing-plans`, batch execution with checkpoints.

Which approach?
