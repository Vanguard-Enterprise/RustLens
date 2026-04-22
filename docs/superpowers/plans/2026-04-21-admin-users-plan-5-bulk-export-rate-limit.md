# Admin Users — Plan 5: Bulk, Export, Rate Limits

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans.

**Goal:** Add bulk mutations (suspend/unsuspend/roles.grant/roles.revoke), CSV export streaming, dispatcher rate-limit buckets, and idempotency-key caching.

**Prerequisite:** Plans 1–4 merged.

---

## File Structure

**Backend**
- Create: `apps/backend/internal/admin/actions/users_bulk.go` — bulk wrapper action.
- Modify: `apps/backend/internal/admin/actions/dispatcher.go` — add rate-limit middleware and idempotency cache.
- Create: `apps/backend/internal/http/admin_export_handlers.go` — `ExportAdminUsersCSV`.
- Modify: `apps/backend/internal/http/router.go` — mount `/users/export.csv`.
- Test: `users_bulk_test.go`, `dispatcher_ratelimit_test.go`, `admin_export_handlers_test.go`.

**Frontend**
- Modify: `apps/frontend/app/admin/users/page.tsx` — pass selection state down.
- Modify: `apps/frontend/components/admin/admin-data-table.tsx` (or fork) — row-checkbox + selection state exposed via URL or React state.
- Create: `apps/frontend/app/admin/users/_components/users-bulk-toolbar.tsx`.
- Modify: `apps/frontend/lib/rustlens-api.ts` — `adminUsersExportUrl(query)` helper (returns URL with auth; use document-form POST or window.open with cookies).

---

## Task 1: Dispatcher — rate-limit buckets

**Files:**
- Modify: `apps/backend/internal/admin/actions/dispatcher.go` (or its package-mates)

- [ ] **Step 1:** Add a per-actor per-action token bucket using an in-memory map: `map[string]*bucket` keyed on `"{actorID}:{actionType}"`, with `sync.Mutex` guarding the map. Bucket = classic token bucket: `capacity`, `refillPerSec`, `tokens float64`, `lastRefill time.Time`.

- [ ] **Step 2:** Default limits: `30` tokens + `0.5/sec` for mutating actions; `10` tokens + `0.1/sec` for token-generating actions (`users.invite`, `users.password.forceReset`, `users.email.resendVerification`). Read defaults from constants; allow override via a per-action `RateLimit() (capacity int, refillPerSec float64)` interface method (optional — implement if the action exposes it, else fall back to mutating vs token-generating default).

- [ ] **Step 3:** On dispatch, before permission check: take a token; if none, return `429` with `Retry-After` header.

- [ ] **Step 4:** Idempotency cache: LRU with TTL 24h, keyed on `{actorID}:{actionType}:{idempotencyKey}`; value is the response bytes + status code. On hit, return cached response.

- [ ] **Step 5:** Tests:
  - Rapid-fire N+1 calls → last one gets 429.
  - Same idempotency key → second call returns cached body byte-identical, does not execute action again (assert DB state unchanged vs a non-cached second call).
  - Different idempotency keys → both execute.
- [ ] Commit: `feat(backend): dispatcher rate limits and idempotency cache`.

---

## Task 2: Bulk wrapper action — `users.bulk`

**Files:**
- Create: `apps/backend/internal/admin/actions/users_bulk.go`

- [ ] Type `users.bulk`. `Permission()` returns an empty string — the per-row action's permission is checked inside. Dangerous: `true` (prompts confirmation).

- [ ] Validate: `{action: "suspend"|"unsuspend"|"roles.grant"|"roles.revoke", userIds: string[], params?: object}`. `userIds` length 1..200. If `action` is roles.grant/roles.revoke, `params.roleKey` required.

- [ ] Execute: iterate `userIds`; for each, look up the underlying action from the registry and invoke it via the same dispatcher path (including permission check + per-row audit). Wrap each call in its own transaction (the sub-action already does this); collect `{userId, ok, error?}`. Do **not** wrap all rows in one tx — partial failures must not roll back siblings.

- [ ] Write a top-level `audit_events` row with `action_type = "users.bulk_{action}"` and `params = {action, count: N, succeeded: K, failed: M}`; the sub-actions each write their own rows with their normal `action_type`.

- [ ] Return `{results: [{userId, ok, error?}], succeeded: K, failed: M}`.

- [ ] Tests:
  - All-success bulk suspend on 3 users → 3 sub-audit rows + 1 summary row.
  - Mixed: one user already suspended (sub-action returns 409) → partial success, summary audit row reflects counts.
  - Unauthorized caller for the sub-action permission → all sub-actions 403; top-level is still recorded.
- [ ] Commit: `feat(backend): users.bulk wrapper with per-row results`.

---

## Task 3: CSV export endpoint

**Files:**
- Create: `apps/backend/internal/http/admin_export_handlers.go`
- Modify: `apps/backend/internal/http/router.go` — `r.Get("/users/export.csv", handler.ExportAdminUsersCSV)`.

- [ ] **Step 1:** Reuse the filter-building code from `ListAdminUsers` by extracting a shared `buildUsersQuery(q url.Values) (whereSQL, args, orderBy, err)` helper; refactor `ListAdminUsers` to call it.

- [ ] **Step 2:** `ExportAdminUsersCSV`:
  - Permission: require `users.export` via the session.
  - Headers: `Content-Type: text/csv; charset=utf-8`, `Content-Disposition: attachment; filename="users-<date>.csv"`, `Cache-Control: no-store`.
  - Row cap: 100_000. Run a `SELECT COUNT(*)` first; if over cap, return 413.
  - Stream rows using pgx cursor-style (`pool.Query` + iterate) and `encoding/csv.Writer` directly into `w`. Flush per row or per 1000 rows.
  - Columns: `id, display_name, primary_email, email_verified, status, admin_roles, discord_id, discord_username, steam_count, guild_count, created_at, last_login_at`.
  - Timeout: 60s via `context.WithTimeout`.

- [ ] **Step 3:** Tests:
  - Happy path with 3 seeded users → 3 data rows + 1 header row.
  - Filters compose identically to list.
  - 413 when count > cap (simulate by temporarily lowering the cap in a test hook).
- [ ] Commit: `feat(backend): streaming CSV export of admin users`.

---

## Task 4: Frontend — multi-select in data table

**Files:**
- Modify: `apps/frontend/components/admin/admin-data-table.tsx` (or wrap it) and `apps/frontend/app/admin/users/page.tsx`.

- [ ] Add a `selectable` prop to `AdminDataTable` (or create `UsersTable` dedicated). Each row has a checkbox; header has select-all (current page). Selection state lives in parent client component via `useState`.

- [ ] Lift selection state up to the page. Since `page.tsx` is RSC, create a thin client wrapper `UsersTableView` that takes the initial data and renders the table + bulk toolbar. The toolbar is visible only when `selected.length > 0`.

- [ ] Commit: `feat(frontend): multi-select in users table`.

---

## Task 5: Frontend — bulk toolbar

**Files:**
- Create: `apps/frontend/app/admin/users/_components/users-bulk-toolbar.tsx`

- [ ] Sticky toolbar displayed above the table when selection > 0:
  ```
  N selected  [Grant role ▾] [Revoke role ▾] [Suspend] [Unsuspend] [Export CSV]  [Clear]
  ```
- [ ] Suspend / Unsuspend: confirmation dialog ("Suspend N users?") → POST `users.bulk` with `{action: "suspend"|"unsuspend", userIds: selection}`. On response, show a results toast "K succeeded, M failed" and expand-to-list failures.
- [ ] Grant role / Revoke role: dropdown of roles → POST `users.bulk` with `{action: "roles.grant"|"roles.revoke", userIds, params: {roleKey}}`.
- [ ] Export CSV: opens `/api/app/admin/users/export.csv?<current filter params>&ids=<selected>` — backend accepts an optional `ids=<csv>` filter; update the endpoint accordingly. If unselected, exports the currently filtered list.
- [ ] Clear selection button.
- [ ] Commit: `feat(frontend): users bulk toolbar`.

---

## Task 6: Frontend — tests

- [ ] Bulk toolbar: hides when selection empty; correctly posts bulk payloads; renders partial-failure summary.
- [ ] Export CSV link: correctly composes URL with selected ids and current filters.
- [ ] Commit: `test(frontend): bulk toolbar and export URL`.

---

## Task 7: E2E smoke (extend existing admin e2e)

**Files:**
- Modify: `apps/backend/internal/admin/e2e_test.go`

- [ ] Add a scenario: admin with `user_admin` role logs in → invites two users → bulk-suspends them → downloads CSV with `status=suspended` → assert CSV contains both rows.
- [ ] Commit: `test(backend): admin users e2e for invite + bulk + export`.

---

## Done when

- [ ] `go test ./...` green.
- [ ] Frontend test + typecheck green.
- [ ] Hitting `POST /api/app/admin/actions/users.invite` 11× in under 60s from the same actor returns 429 on the 11th.
- [ ] Posting `users.bulk` with 3 users where one is already suspended returns a 200 with `{succeeded: 2, failed: 1}` and exactly 3 sub-audit rows + 1 summary row are written.
- [ ] CSV export downloads a correctly-formatted file with filters honored.
