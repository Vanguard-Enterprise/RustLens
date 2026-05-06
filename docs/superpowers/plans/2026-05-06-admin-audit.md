# Admin Audit Page Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build `/admin/audit` — a filterable, paginated audit log viewer matching the `/admin/users` UX — plus the backend filter/cursor/detail/event-types endpoints it depends on.

**Architecture:** Server component reads URL query params and fetches list + event-types + (optionally) a single event in parallel. A client component owns URL-bound filter state, a clickable read-only table, and a right-side `Sheet` for event details. Backend extends the existing `ListAdminAudit` UNION query with filter and cursor parameters and adds two new sibling endpoints (`/event-types`, `/{id}`).

**Tech Stack:** Go (chi, pgx) on the backend; Next.js 16 (App Router) + React 19 + Tailwind v4 + shadcn/ui on the frontend; Vitest for frontend unit tests; Go's standard `testing` for backend tests (gated by `TEST_POSTGRES_URL`).

**Spec:** `docs/superpowers/specs/2026-05-06-admin-audit-design.md`

---

## File Map

**Backend — modify**

- `apps/backend/internal/http/admin_read_handlers.go` — extend `ListAdminAudit` with filter + cursor params; emit `nextCursor`.
- `apps/backend/internal/http/router.go` — register two new routes.
- `apps/backend/internal/http/openapi.go` — document new params + endpoints (best-effort; spec mismatches are not blocking).

**Backend — create**

- `apps/backend/internal/http/admin_audit_handlers.go` — `ListAdminAuditEventTypes`, `GetAdminAuditEvent`, plus the in-process types cache.
- `apps/backend/internal/http/admin_audit_handlers_test.go` — unit tests for all three handlers.

**Frontend — modify**

- `apps/frontend/lib/rustlens-api.ts` — add `AdminAuditQuery`, `AdminAuditPage`, `getAdminAudit`, `getAdminAuditEventTypes`, `getAdminAuditEvent`, `searchAdminUsers`.

**Frontend — create**

- `apps/frontend/app/admin/audit/page.tsx` — server component, `audit.read` gate, parallel fetch.
- `apps/frontend/app/admin/audit/_components/audit-page.tsx` — client; URL state, sheet state.
- `apps/frontend/app/admin/audit/_components/audit-toolbar.tsx` — search input + filter popover.
- `apps/frontend/app/admin/audit/_components/audit-table.tsx` — read-only clickable table.
- `apps/frontend/app/admin/audit/_components/audit-detail-sheet.tsx` — right-side Sheet.
- `apps/frontend/app/admin/audit/_components/user-combobox.tsx` — async user picker.
- `apps/frontend/app/admin/audit/_components/__tests__/audit-toolbar.test.tsx`
- `apps/frontend/app/admin/audit/_components/__tests__/audit-table.test.tsx`
- `apps/frontend/app/admin/audit/_components/__tests__/audit-detail-sheet.test.tsx`
- `apps/frontend/app/admin/audit/_components/__tests__/user-combobox.test.tsx`

---

## Task 1: Backend — extend `ListAdminAudit` with filters + cursor

**Files:**
- Modify: `apps/backend/internal/http/admin_read_handlers.go` (function `ListAdminAudit` ~line 329 onward)
- Test: `apps/backend/internal/http/admin_audit_handlers_test.go` (new)

Goal: accept `targetUserId`, `actorUserId`, `eventType` (repeatable), `result`, `since`, `before`, `pageSize`. Return `{ items, nextCursor }`. The `targetUserId` filter must match `target_user_id = $` **or** (new table only) `params -> 'filters' -> 'ids' @> to_jsonb($::text)`. The `result=error|success` filter restricts to the new-table half only (legacy rows have NULL result and are excluded).

- [ ] **Step 1: Write failing tests** for the extended handler.

Create `apps/backend/internal/http/admin_audit_handlers_test.go`:

```go
package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

// seedAuditEvent inserts a row directly into audit_events. Returns the id (uuid string).
func seedAuditEvent(t *testing.T, ts *testHandlerServer, actionType, actor, target, result, reason string, params map[string]any) string {
	t.Helper()
	if params == nil {
		params = map[string]any{}
	}
	pj, _ := json.Marshal(params)
	var id string
	q := `
		INSERT INTO audit_events (action_type, actor_user_id, target_user_id, resource_type, resource_id, params, result, reason)
		VALUES ($1, NULLIF($2,'')::uuid, NULLIF($3,'')::uuid, 'user', COALESCE(NULLIF($3,''), ''), $4::jsonb, NULLIF($5,''), NULLIF($6,''))
		RETURNING id::text
	`
	if err := ts.pool.QueryRow(context.Background(), q, actionType, actor, target, string(pj), result, reason).Scan(&id); err != nil {
		t.Fatalf("seed audit event: %v", err)
	}
	return id
}

func decodeAuditList(t *testing.T, body []byte) struct {
	Items      []auditEventItem `json:"items"`
	NextCursor *string          `json:"nextCursor"`
} {
	t.Helper()
	var out struct {
		Items      []auditEventItem `json:"items"`
		NextCursor *string          `json:"nextCursor"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, string(body))
	}
	return out
}

func TestListAdminAudit_FilterByTargetUserId(t *testing.T) {
	ts := newTestHandlerServer(t)
	seedUser(t, ts.pool, "11111111-1111-1111-1111-111111111111", "Actor", seedOpts{})
	seedUser(t, ts.pool, "22222222-2222-2222-2222-222222222222", "Target1", seedOpts{})
	seedUser(t, ts.pool, "33333333-3333-3333-3333-333333333333", "Target2", seedOpts{})
	seedAuditEvent(t, ts, "users.suspend", "11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222", "success", "abuse", nil)
	seedAuditEvent(t, ts, "users.suspend", "11111111-1111-1111-1111-111111111111", "33333333-3333-3333-3333-333333333333", "success", "abuse", nil)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/audit?targetUserId=22222222-2222-2222-2222-222222222222", nil)
	ts.h.ListAdminAudit(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	got := decodeAuditList(t, rr.Body.Bytes())
	if len(got.Items) != 1 {
		t.Fatalf("len(items) = %d, want 1; %+v", len(got.Items), got.Items)
	}
	if got.Items[0].TargetUserID != "22222222-2222-2222-2222-222222222222" {
		t.Errorf("targetUserId = %q", got.Items[0].TargetUserID)
	}
}

func TestListAdminAudit_FilterByTargetUserId_MatchesParamsFiltersIds(t *testing.T) {
	ts := newTestHandlerServer(t)
	seedUser(t, ts.pool, "11111111-1111-1111-1111-111111111111", "Actor", seedOpts{})
	seedUser(t, ts.pool, "22222222-2222-2222-2222-222222222222", "Target", seedOpts{})
	// Export-style event: target_user_id is NULL but params.filters.ids contains the user.
	seedAuditEvent(t, ts, "users.export", "11111111-1111-1111-1111-111111111111", "", "success", "", map[string]any{
		"filters": map[string]any{"ids": []string{"22222222-2222-2222-2222-222222222222"}},
	})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/audit?targetUserId=22222222-2222-2222-2222-222222222222", nil)
	ts.h.ListAdminAudit(rr, req)
	got := decodeAuditList(t, rr.Body.Bytes())
	if len(got.Items) != 1 {
		t.Fatalf("len(items) = %d, want 1 (export-style)", len(got.Items))
	}
	if got.Items[0].EventType != "users.export" {
		t.Errorf("eventType = %q", got.Items[0].EventType)
	}
}

func TestListAdminAudit_FilterByActorUserId(t *testing.T) {
	ts := newTestHandlerServer(t)
	seedUser(t, ts.pool, "11111111-1111-1111-1111-111111111111", "ActorA", seedOpts{})
	seedUser(t, ts.pool, "22222222-2222-2222-2222-222222222222", "ActorB", seedOpts{})
	seedUser(t, ts.pool, "33333333-3333-3333-3333-333333333333", "Tgt", seedOpts{})
	seedAuditEvent(t, ts, "users.suspend", "11111111-1111-1111-1111-111111111111", "33333333-3333-3333-3333-333333333333", "success", "", nil)
	seedAuditEvent(t, ts, "users.suspend", "22222222-2222-2222-2222-222222222222", "33333333-3333-3333-3333-333333333333", "success", "", nil)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/audit?actorUserId=11111111-1111-1111-1111-111111111111", nil)
	ts.h.ListAdminAudit(rr, req)
	got := decodeAuditList(t, rr.Body.Bytes())
	if len(got.Items) != 1 || got.Items[0].ActorUserID != "11111111-1111-1111-1111-111111111111" {
		t.Fatalf("got %+v", got.Items)
	}
}

func TestListAdminAudit_FilterByEventTypeRepeatable(t *testing.T) {
	ts := newTestHandlerServer(t)
	seedUser(t, ts.pool, "11111111-1111-1111-1111-111111111111", "A", seedOpts{})
	seedUser(t, ts.pool, "22222222-2222-2222-2222-222222222222", "T", seedOpts{})
	seedAuditEvent(t, ts, "users.suspend", "11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222", "success", "", nil)
	seedAuditEvent(t, ts, "users.delete", "11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222", "success", "", nil)
	seedAuditEvent(t, ts, "users.edit", "11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222", "success", "", nil)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/audit?eventType=users.suspend&eventType=users.delete", nil)
	ts.h.ListAdminAudit(rr, req)
	got := decodeAuditList(t, rr.Body.Bytes())
	if len(got.Items) != 2 {
		t.Fatalf("len = %d", len(got.Items))
	}
	for _, it := range got.Items {
		if it.EventType != "users.suspend" && it.EventType != "users.delete" {
			t.Errorf("unexpected eventType %q", it.EventType)
		}
	}
}

func TestListAdminAudit_FilterByResultExcludesLegacy(t *testing.T) {
	ts := newTestHandlerServer(t)
	seedUser(t, ts.pool, "11111111-1111-1111-1111-111111111111", "A", seedOpts{})
	seedUser(t, ts.pool, "22222222-2222-2222-2222-222222222222", "T", seedOpts{})
	seedAuditEvent(t, ts, "users.suspend", "11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222", "success", "", nil)
	seedAuditEvent(t, ts, "users.suspend", "11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222", "error", "boom", nil)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/audit?result=error", nil)
	ts.h.ListAdminAudit(rr, req)
	got := decodeAuditList(t, rr.Body.Bytes())
	if len(got.Items) != 1 || got.Items[0].Result == nil || *got.Items[0].Result != "error" {
		t.Fatalf("got %+v", got.Items)
	}
}

func TestListAdminAudit_PaginationCursor(t *testing.T) {
	ts := newTestHandlerServer(t)
	seedUser(t, ts.pool, "11111111-1111-1111-1111-111111111111", "A", seedOpts{})
	seedUser(t, ts.pool, "22222222-2222-2222-2222-222222222222", "T", seedOpts{})
	for i := 0; i < 5; i++ {
		seedAuditEvent(t, ts, fmt.Sprintf("users.evt.%d", i), "11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222", "success", "", nil)
	}

	// Page 1: pageSize=2
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/audit?pageSize=2", nil)
	ts.h.ListAdminAudit(rr, req)
	page1 := decodeAuditList(t, rr.Body.Bytes())
	if len(page1.Items) != 2 || page1.NextCursor == nil {
		t.Fatalf("page1 len=%d nextCursor=%v", len(page1.Items), page1.NextCursor)
	}

	// Page 2 using cursor
	rr2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("GET", "/audit?pageSize=2&before="+*page1.NextCursor, nil)
	ts.h.ListAdminAudit(rr2, req2)
	page2 := decodeAuditList(t, rr2.Body.Bytes())
	if len(page2.Items) != 2 || page2.NextCursor == nil {
		t.Fatalf("page2 len=%d nextCursor=%v", len(page2.Items), page2.NextCursor)
	}
	for _, a := range page1.Items {
		for _, b := range page2.Items {
			if a.ID == b.ID {
				t.Errorf("overlap id=%s", a.ID)
			}
		}
	}

	// Page 3 (last)
	rr3 := httptest.NewRecorder()
	req3 := httptest.NewRequest("GET", "/audit?pageSize=2&before="+*page2.NextCursor, nil)
	ts.h.ListAdminAudit(rr3, req3)
	page3 := decodeAuditList(t, rr3.Body.Bytes())
	if len(page3.Items) != 1 || page3.NextCursor != nil {
		t.Fatalf("page3 len=%d nextCursor=%v", len(page3.Items), page3.NextCursor)
	}
}

// Routing helper used by the handler-as-route tests below.
func auditRouter(h *Handler) http.Handler {
	r := chi.NewRouter()
	r.Get("/audit", h.ListAdminAudit)
	return r
}

var _ = auditRouter // silence unused if not all tests use it
```

- [ ] **Step 2: Run tests to verify they fail.**

Run from `apps/backend`:

```
TEST_POSTGRES_URL=$TEST_POSTGRES_URL go test ./internal/http/ -run TestListAdminAudit -v
```

Expected: tests fail (current handler ignores all the new params, never sets `nextCursor`, returns wrong row counts).

- [ ] **Step 3: Refactor `ListAdminAudit` to honor the new params.**

Replace the body of `ListAdminAudit` in `admin_read_handlers.go` (the function starting `func (h *Handler) ListAdminAudit(...)`). The new shape:

```go
// ListAdminAudit: GET /api/app/admin/audit
//
// Query params:
//   targetUserId  uuid (matches target_user_id OR params.filters.ids @> [id])
//   actorUserId   uuid (matches actor_user_id)
//   eventType     repeatable string (action_type / event_type IN ANY)
//   result        success|error (audit_events only; legacy excluded when set)
//   since         RFC3339 (created_at >= since)
//   before        RFC3339 (created_at < before; cursor)
//   pageSize      int (default 50, capped 200)
//
// Response: {"items":[...], "nextCursor": <RFC3339 or null>}
func (h *Handler) ListAdminAudit(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	pageSize := 50
	if v := q.Get("pageSize"); v != "" {
		parseLimit(v, &pageSize)
	}
	// Backward-compat: keep the old ?limit= alias working.
	if pageSize == 50 {
		if v := q.Get("limit"); v != "" {
			parseLimit(v, &pageSize)
		}
	}
	if pageSize <= 0 || pageSize > 200 {
		pageSize = 50
	}

	targetUserID := strings.TrimSpace(q.Get("targetUserId"))
	actorUserID := strings.TrimSpace(q.Get("actorUserId"))
	eventTypes := q["eventType"]
	resultFilter := strings.TrimSpace(q.Get("result"))
	if resultFilter != "" && resultFilter != "success" && resultFilter != "error" {
		resultFilter = ""
	}
	since := strings.TrimSpace(q.Get("since"))
	before := strings.TrimSpace(q.Get("before"))

	pool := h.postgres.Pool()
	hasLegacy, _ := tableExists(ctx, pool, "app_audit_events")
	// result filter restricts to the new-table half only
	if resultFilter != "" {
		hasLegacy = false
	}

	// Build args + WHERE fragments incrementally; index numbering is 1-based.
	args := []any{}
	addArg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}

	// Per-half WHERE builders so we can keep new vs legacy filters in sync.
	// `newClauses` apply to audit_events; `legacyClauses` to app_audit_events.
	var newClauses, legacyClauses []string

	if targetUserID != "" {
		idArg := addArg(targetUserID)
		// audit_events: target_user_id = $ OR params.filters.ids @> [$]
		newClauses = append(newClauses,
			fmt.Sprintf("(target_user_id::text = %[1]s OR (params -> 'filters' -> 'ids') @> to_jsonb(%[1]s::text))", idArg),
		)
		// legacy: target_user_id = $ only
		legacyClauses = append(legacyClauses, fmt.Sprintf("target_user_id = %s", idArg))
	}
	if actorUserID != "" {
		a := addArg(actorUserID)
		newClauses = append(newClauses, fmt.Sprintf("actor_user_id::text = %s", a))
		legacyClauses = append(legacyClauses, fmt.Sprintf("actor_user_id = %s", a))
	}
	if len(eventTypes) > 0 {
		a := addArg(eventTypes)
		newClauses = append(newClauses, fmt.Sprintf("action_type = ANY(%s::text[])", a))
		legacyClauses = append(legacyClauses, fmt.Sprintf("event_type = ANY(%s::text[])", a))
	}
	if resultFilter != "" {
		a := addArg(resultFilter)
		newClauses = append(newClauses, fmt.Sprintf("result = %s", a))
		// legacy already excluded above
	}
	if since != "" {
		a := addArg(since)
		newClauses = append(newClauses, fmt.Sprintf("created_at >= %s::timestamptz", a))
		legacyClauses = append(legacyClauses, fmt.Sprintf("created_at >= %s::timestamptz", a))
	}
	if before != "" {
		a := addArg(before)
		newClauses = append(newClauses, fmt.Sprintf("created_at < %s::timestamptz", a))
		legacyClauses = append(legacyClauses, fmt.Sprintf("created_at < %s::timestamptz", a))
	}

	whereOf := func(cs []string) string {
		if len(cs) == 0 {
			return ""
		}
		return " WHERE " + strings.Join(cs, " AND ")
	}

	// Fetch one extra row to determine whether nextCursor is set.
	limitArg := addArg(pageSize + 1)

	newQ := fmt.Sprintf(`
		SELECT id::text,
		       COALESCE(actor_user_id::text, '') AS actor_user_id,
		       COALESCE(target_user_id::text, '') AS target_user_id,
		       action_type AS event_type,
		       COALESCE(params, '{}'::jsonb) AS payload,
		       NULLIF(result, '') AS result,
		       reason,
		       created_at
		FROM audit_events%s
	`, whereOf(newClauses))

	legacyQ := fmt.Sprintf(`
		SELECT id::text,
		       COALESCE(actor_user_id, '') AS actor_user_id,
		       COALESCE(target_user_id, '') AS target_user_id,
		       event_type,
		       COALESCE(payload, '{}'::jsonb) AS payload,
		       NULL::text AS result,
		       NULL::text AS reason,
		       created_at
		FROM app_audit_events%s
	`, whereOf(legacyClauses))

	var sql string
	if hasLegacy {
		sql = fmt.Sprintf(`SELECT * FROM (%s UNION ALL %s) u ORDER BY created_at DESC LIMIT %s`, newQ, legacyQ, limitArg)
	} else {
		sql = fmt.Sprintf(`SELECT * FROM (%s) u ORDER BY created_at DESC LIMIT %s`, newQ, limitArg)
	}

	rows, err := pool.Query(ctx, sql, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	items := []auditEventItem{}
	idSet := map[string]struct{}{}
	for rows.Next() {
		var (
			item        auditEventItem
			payloadJSON []byte
			result      *string
			reason      *string
		)
		if err := rows.Scan(&item.ID, &item.ActorUserID, &item.TargetUserID, &item.EventType, &payloadJSON, &result, &reason, &item.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		item.Payload = map[string]any{}
		if len(payloadJSON) > 0 {
			_ = json.Unmarshal(payloadJSON, &item.Payload)
		}
		item.Result = result
		item.Reason = reason
		if item.ActorUserID != "" {
			idSet[item.ActorUserID] = struct{}{}
		}
		if item.TargetUserID != "" {
			idSet[item.TargetUserID] = struct{}{}
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var nextCursor *string
	if len(items) > pageSize {
		// We pulled one extra; trim and use the trimmed-page's last row's createdAt as cursor.
		items = items[:pageSize]
		c := items[len(items)-1].CreatedAt
		nextCursor = &c
	}

	hydrateAuditActors(ctx, pool, items, idSet)

	writeJSON(w, http.StatusOK, map[string]any{"items": items, "nextCursor": nextCursor})
}

// hydrateAuditActors fills in actor/target display name + avatar URL via a single
// follow-up query against app_users. Extracted so the new single-event handler
// can reuse it.
func hydrateAuditActors(ctx context.Context, pool *pgxpool.Pool, items []auditEventItem, idSet map[string]struct{}) {
	if len(idSet) == 0 {
		return
	}
	type userLite struct{ name, avatar string }
	lookup := map[string]userLite{}
	ids := make([]string, 0, len(idSet))
	for id := range idSet {
		ids = append(ids, id)
	}
	urows, err := pool.Query(ctx, `
		SELECT id::text, COALESCE(display_name, ''), COALESCE(avatar_url, '')
		FROM app_users WHERE id::text = ANY($1)
	`, ids)
	if err != nil {
		return
	}
	defer urows.Close()
	for urows.Next() {
		var id, name, avatar string
		if err := urows.Scan(&id, &name, &avatar); err == nil {
			lookup[id] = userLite{name: name, avatar: avatar}
		}
	}
	strPtr := func(s string) *string {
		if s == "" {
			return nil
		}
		v := s
		return &v
	}
	for i := range items {
		if u, ok := lookup[items[i].ActorUserID]; ok {
			items[i].ActorDisplayName = strPtr(u.name)
			items[i].ActorAvatarURL = strPtr(u.avatar)
		}
		if u, ok := lookup[items[i].TargetUserID]; ok {
			items[i].TargetDisplayName = strPtr(u.name)
			items[i].TargetAvatarURL = strPtr(u.avatar)
		}
	}
}
```

Add the import for `pgxpool` to this file if it isn't already imported (`"github.com/jackc/pgx/v5/pgxpool"`).

- [ ] **Step 4: Run the tests; expect PASS.**

```
TEST_POSTGRES_URL=$TEST_POSTGRES_URL go test ./internal/http/ -run TestListAdminAudit -v
```

Expected: all 5 tests pass.

- [ ] **Step 5: Commit.**

```
git add apps/backend/internal/http/admin_read_handlers.go apps/backend/internal/http/admin_audit_handlers_test.go
git commit -m "feat(backend): admin audit list filter + cursor params"
```

---

## Task 2: Backend — `GET /api/app/admin/audit/event-types`

**Files:**
- Create: `apps/backend/internal/http/admin_audit_handlers.go`
- Modify: `apps/backend/internal/http/admin_audit_handlers_test.go` (append tests)

- [ ] **Step 1: Write failing test.**

Append to `admin_audit_handlers_test.go`:

```go
func TestListAdminAuditEventTypes_SortedDistinctUnion(t *testing.T) {
	ts := newTestHandlerServer(t)
	seedUser(t, ts.pool, "11111111-1111-1111-1111-111111111111", "A", seedOpts{})
	seedUser(t, ts.pool, "22222222-2222-2222-2222-222222222222", "T", seedOpts{})
	seedAuditEvent(t, ts, "users.suspend", "11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222", "success", "", nil)
	seedAuditEvent(t, ts, "users.delete", "11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222", "success", "", nil)
	seedAuditEvent(t, ts, "users.suspend", "11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222", "success", "", nil) // duplicate

	// Reset the in-process cache so the test sees fresh data.
	resetAuditEventTypesCacheForTest()

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/audit/event-types", nil)
	ts.h.ListAdminAuditEventTypes(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var out struct{ Types []string `json:"types"` }
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.Types) != 2 || out.Types[0] != "users.delete" || out.Types[1] != "users.suspend" {
		t.Fatalf("types = %v", out.Types)
	}
}

func TestListAdminAuditEventTypes_CacheServesStale(t *testing.T) {
	ts := newTestHandlerServer(t)
	seedUser(t, ts.pool, "11111111-1111-1111-1111-111111111111", "A", seedOpts{})
	seedUser(t, ts.pool, "22222222-2222-2222-2222-222222222222", "T", seedOpts{})
	seedAuditEvent(t, ts, "users.suspend", "11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222", "success", "", nil)

	resetAuditEventTypesCacheForTest()
	rr := httptest.NewRecorder()
	ts.h.ListAdminAuditEventTypes(rr, httptest.NewRequest("GET", "/audit/event-types", nil))

	// Second insert; cache should still serve old result within TTL.
	seedAuditEvent(t, ts, "users.delete", "11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222", "success", "", nil)
	rr2 := httptest.NewRecorder()
	ts.h.ListAdminAuditEventTypes(rr2, httptest.NewRequest("GET", "/audit/event-types", nil))
	var out2 struct{ Types []string `json:"types"` }
	_ = json.Unmarshal(rr2.Body.Bytes(), &out2)
	if len(out2.Types) != 1 || out2.Types[0] != "users.suspend" {
		t.Fatalf("cache should have served stale; got %v", out2.Types)
	}
}
```

- [ ] **Step 2: Run; expect FAIL** (handler does not exist).

```
TEST_POSTGRES_URL=$TEST_POSTGRES_URL go test ./internal/http/ -run TestListAdminAuditEventTypes -v
```

- [ ] **Step 3: Create the handler.**

Create `apps/backend/internal/http/admin_audit_handlers.go`:

```go
package httpapi

import (
	"context"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// auditEventTypesCache holds a 60-second in-process cache for the
// distinct-event-types endpoint. The DISTINCT scan is cheap on small data
// but can grow expensive once the audit tables have millions of rows; this
// cache dampens the cost without introducing a separate cache layer.
var auditEventTypesCache struct {
	mu     sync.Mutex
	at     time.Time
	types  []string
}

const auditEventTypesCacheTTL = 60 * time.Second

func resetAuditEventTypesCacheForTest() {
	auditEventTypesCache.mu.Lock()
	auditEventTypesCache.at = time.Time{}
	auditEventTypesCache.types = nil
	auditEventTypesCache.mu.Unlock()
}

// ListAdminAuditEventTypes: GET /api/app/admin/audit/event-types
// Returns sorted distinct values from audit_events.action_type
// UNION app_audit_events.event_type. Cached 60s in-process.
func (h *Handler) ListAdminAuditEventTypes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	auditEventTypesCache.mu.Lock()
	if !auditEventTypesCache.at.IsZero() && time.Since(auditEventTypesCache.at) < auditEventTypesCacheTTL {
		out := append([]string(nil), auditEventTypesCache.types...)
		auditEventTypesCache.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"types": out})
		return
	}
	auditEventTypesCache.mu.Unlock()

	pool := h.postgres.Pool()
	types, err := loadAuditEventTypes(ctx, pool)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	auditEventTypesCache.mu.Lock()
	auditEventTypesCache.at = time.Now()
	auditEventTypesCache.types = append([]string(nil), types...)
	auditEventTypesCache.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{"types": types})
}

func loadAuditEventTypes(ctx context.Context, pool *pgxpool.Pool) ([]string, error) {
	hasLegacy, _ := tableExists(ctx, pool, "app_audit_events")
	q := `SELECT DISTINCT action_type AS t FROM audit_events WHERE action_type IS NOT NULL AND action_type <> ''`
	if hasLegacy {
		q += ` UNION SELECT DISTINCT event_type AS t FROM app_audit_events WHERE event_type IS NOT NULL AND event_type <> ''`
	}
	rows, err := pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

// chi import kept for symmetry with sibling handlers; remove if unused.
var _ = chi.URLParam
```

- [ ] **Step 4: Run; expect PASS.**

```
TEST_POSTGRES_URL=$TEST_POSTGRES_URL go test ./internal/http/ -run TestListAdminAuditEventTypes -v
```

- [ ] **Step 5: Commit.**

```
git add apps/backend/internal/http/admin_audit_handlers.go apps/backend/internal/http/admin_audit_handlers_test.go
git commit -m "feat(backend): admin audit event-types endpoint"
```

---

## Task 3: Backend — `GET /api/app/admin/audit/{id}`

**Files:**
- Modify: `apps/backend/internal/http/admin_audit_handlers.go`
- Modify: `apps/backend/internal/http/admin_audit_handlers_test.go`

- [ ] **Step 1: Write failing tests.**

Append:

```go
func TestGetAdminAuditEvent_ReturnsHydratedRow(t *testing.T) {
	ts := newTestHandlerServer(t)
	seedUser(t, ts.pool, "11111111-1111-1111-1111-111111111111", "Alice", seedOpts{})
	seedUser(t, ts.pool, "22222222-2222-2222-2222-222222222222", "Target", seedOpts{})
	id := seedAuditEvent(t, ts, "users.suspend", "11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222", "success", "abuse", map[string]any{"k": "v"})

	r := chi.NewRouter()
	r.Get("/audit/{id}", ts.h.GetAdminAuditEvent)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest("GET", "/audit/"+id, nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var item auditEventItem
	if err := json.Unmarshal(rr.Body.Bytes(), &item); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if item.ID != id || item.EventType != "users.suspend" {
		t.Errorf("got %+v", item)
	}
	if item.ActorDisplayName == nil || *item.ActorDisplayName != "Alice" {
		t.Errorf("actorDisplayName = %v", item.ActorDisplayName)
	}
	if item.TargetDisplayName == nil || *item.TargetDisplayName != "Target" {
		t.Errorf("targetDisplayName = %v", item.TargetDisplayName)
	}
}

func TestGetAdminAuditEvent_NotFound(t *testing.T) {
	ts := newTestHandlerServer(t)
	r := chi.NewRouter()
	r.Get("/audit/{id}", ts.h.GetAdminAuditEvent)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest("GET", "/audit/00000000-0000-0000-0000-000000000000", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status=%d", rr.Code)
	}
}
```

- [ ] **Step 2: Run; expect FAIL.**

```
TEST_POSTGRES_URL=$TEST_POSTGRES_URL go test ./internal/http/ -run TestGetAdminAuditEvent -v
```

- [ ] **Step 3: Add the handler.**

Append to `admin_audit_handlers.go`:

```go
// GetAdminAuditEvent: GET /api/app/admin/audit/{id}
// Returns a single hydrated audit event from either audit_events or
// app_audit_events. Used by the frontend Sheet to support deep-link `?event=`.
func (h *Handler) GetAdminAuditEvent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing id")
		return
	}
	pool := h.postgres.Pool()
	hasLegacy, _ := tableExists(ctx, pool, "app_audit_events")

	newQ := `
		SELECT id::text,
		       COALESCE(actor_user_id::text, '') AS actor_user_id,
		       COALESCE(target_user_id::text, '') AS target_user_id,
		       action_type AS event_type,
		       COALESCE(params, '{}'::jsonb) AS payload,
		       NULLIF(result, '') AS result,
		       reason,
		       created_at
		FROM audit_events
		WHERE id::text = $1
	`
	legacyQ := `
		SELECT id::text,
		       COALESCE(actor_user_id, '') AS actor_user_id,
		       COALESCE(target_user_id, '') AS target_user_id,
		       event_type,
		       COALESCE(payload, '{}'::jsonb) AS payload,
		       NULL::text AS result,
		       NULL::text AS reason,
		       created_at
		FROM app_audit_events
		WHERE id::text = $1
	`
	q := newQ
	if hasLegacy {
		q = newQ + " UNION ALL " + legacyQ + " LIMIT 1"
	}
	row := pool.QueryRow(ctx, q, id)
	var (
		item        auditEventItem
		payloadJSON []byte
		result      *string
		reason      *string
	)
	if err := row.Scan(&item.ID, &item.ActorUserID, &item.TargetUserID, &item.EventType, &payloadJSON, &result, &reason, &item.CreatedAt); err != nil {
		// pgx returns ErrNoRows for missing single-row scans
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	item.Payload = map[string]any{}
	if len(payloadJSON) > 0 {
		_ = json.Unmarshal(payloadJSON, &item.Payload)
	}
	item.Result = result
	item.Reason = reason

	idSet := map[string]struct{}{}
	if item.ActorUserID != "" {
		idSet[item.ActorUserID] = struct{}{}
	}
	if item.TargetUserID != "" {
		idSet[item.TargetUserID] = struct{}{}
	}
	items := []auditEventItem{item}
	hydrateAuditActors(ctx, pool, items, idSet)
	writeJSON(w, http.StatusOK, items[0])
}
```

Add `"encoding/json"` import to `admin_audit_handlers.go` if not already present.

- [ ] **Step 4: Run; expect PASS.**

```
TEST_POSTGRES_URL=$TEST_POSTGRES_URL go test ./internal/http/ -run TestGetAdminAuditEvent -v
```

- [ ] **Step 5: Commit.**

```
git add apps/backend/internal/http/admin_audit_handlers.go apps/backend/internal/http/admin_audit_handlers_test.go
git commit -m "feat(backend): admin audit single-event endpoint"
```

---

## Task 4: Backend — register routes

**Files:**
- Modify: `apps/backend/internal/http/router.go:223-235` (the `/admin` chi route group)

- [ ] **Step 1: Add the new routes.**

Inside the `r.Route("/admin", func(r chi.Router) { ... })` block, the existing line `r.Get("/audit", handler.ListAdminAudit)` is already present. Add two new lines beneath it:

```go
r.Get("/audit", handler.ListAdminAudit)
r.Get("/audit/event-types", handler.ListAdminAuditEventTypes)
r.Get("/audit/{id}", handler.GetAdminAuditEvent)
```

Order matters — `/audit/event-types` must come before `/audit/{id}` so the literal path is matched first. (chi v5 actually prioritizes literal paths over wildcards regardless of order, but keeping them ordered avoids confusion.)

- [ ] **Step 2: Run the full backend test suite to confirm nothing regressed.**

```
TEST_POSTGRES_URL=$TEST_POSTGRES_URL go test ./internal/http/... -v
```

Expected: all tests pass.

- [ ] **Step 3: Commit.**

```
git add apps/backend/internal/http/router.go
git commit -m "feat(backend): register admin audit event-types and detail routes"
```

---

## Task 5: Frontend — API helpers + types

**Files:**
- Modify: `apps/frontend/lib/rustlens-api.ts`

- [ ] **Step 1: Add types and helpers.**

Append next to the existing `getAuditEvents` helper (around line 472):

```ts
export type AdminAuditQuery = {
	targetUserId?: string;
	actorUserId?: string;
	eventType?: string[];
	result?: "success" | "error";
	since?: string;       // RFC3339; resolved server-side from a preset key
	before?: string;      // RFC3339 cursor
	pageSize?: number;
};

export type AdminAuditPage = {
	items: AuditEventItem[];
	nextCursor: string | null;
};

export async function getAdminAudit(params: AdminAuditQuery = {}): Promise<AdminAuditPage> {
	const sp = new URLSearchParams();
	if (params.targetUserId) sp.set("targetUserId", params.targetUserId);
	if (params.actorUserId) sp.set("actorUserId", params.actorUserId);
	for (const t of params.eventType ?? []) sp.append("eventType", t);
	if (params.result) sp.set("result", params.result);
	if (params.since) sp.set("since", params.since);
	if (params.before) sp.set("before", params.before);
	if (params.pageSize) sp.set("pageSize", String(params.pageSize));
	const qs = sp.toString();
	return backendFetch<AdminAuditPage>(
		`/api/app/admin/audit${qs ? `?${qs}` : ""}`,
		undefined,
		{ requireSession: true },
	);
}

export async function getAdminAuditEventTypes(): Promise<string[]> {
	const payload = await backendFetch<{ types: string[] }>(
		"/api/app/admin/audit/event-types",
		undefined,
		{ requireSession: true },
	);
	return payload.types;
}

export async function getAdminAuditEvent(id: string): Promise<AuditEventItem> {
	return backendFetch<AuditEventItem>(
		`/api/app/admin/audit/${encodeURIComponent(id)}`,
		undefined,
		{ requireSession: true },
	);
}

/**
 * Thin wrapper used by the Audit page's actor/target combobox.
 * Pinned to status="any" so suspended/deleted users still surface.
 */
export async function searchAdminUsers(q: string): Promise<AdminUserItem[]> {
	const trimmed = q.trim();
	if (trimmed.length < 2) return [];
	const page = await getAdminUsers({ q: trimmed, pageSize: 10, status: "any" });
	return page.items;
}
```

(The existing `getAuditEvents(limit)` helper stays as-is for any unrelated callers.)

- [ ] **Step 2: Type-check.**

```
cd apps/frontend && pnpm tsc --noEmit
```

Expected: no errors.

- [ ] **Step 3: Commit.**

```
git add apps/frontend/lib/rustlens-api.ts
git commit -m "feat(frontend): admin audit API helpers"
```

---

## Task 6: Frontend — `user-combobox.tsx`

**Files:**
- Create: `apps/frontend/app/admin/audit/_components/user-combobox.tsx`
- Create: `apps/frontend/app/admin/audit/_components/__tests__/user-combobox.test.tsx`

- [ ] **Step 1: Write failing test.**

Create `__tests__/user-combobox.test.tsx`:

```tsx
import { describe, expect, it, vi, beforeEach } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";

import { UserCombobox } from "../user-combobox";

const search = vi.fn();

vi.mock("@/lib/rustlens-api", async () => ({
	searchAdminUsers: (q: string) => search(q),
}));

beforeEach(() => {
	search.mockReset();
});

describe("UserCombobox", () => {
	it("debounces search calls", async () => {
		search.mockResolvedValue([]);
		render(<UserCombobox label="Actor" onSelect={() => {}} onClear={() => {}} value={null} />);
		fireEvent.click(screen.getByRole("button", { name: /search admin/i }));
		const input = await screen.findByPlaceholderText(/search/i);
		fireEvent.change(input, { target: { value: "ali" } });
		fireEvent.change(input, { target: { value: "alic" } });
		fireEvent.change(input, { target: { value: "alice" } });
		await waitFor(() => expect(search).toHaveBeenCalledTimes(1));
		expect(search).toHaveBeenLastCalledWith("alice");
	});

	it("shows chip with clear when value is set", () => {
		const onClear = vi.fn();
		render(
			<UserCombobox
				label="Actor"
				onSelect={() => {}}
				onClear={onClear}
				value={{ id: "u1", displayName: "Alice", avatarUrl: null }}
			/>,
		);
		expect(screen.getByText("Alice")).toBeInTheDocument();
		fireEvent.click(screen.getByRole("button", { name: /clear/i }));
		expect(onClear).toHaveBeenCalled();
	});
});
```

- [ ] **Step 2: Run; expect FAIL.**

```
cd apps/frontend && pnpm vitest run app/admin/audit/_components/__tests__/user-combobox.test.tsx
```

Expected: file-not-found / module-not-found error.

- [ ] **Step 3: Implement the component.**

Create `apps/frontend/app/admin/audit/_components/user-combobox.tsx`:

```tsx
"use client";

import * as React from "react";
import { ChevronDownIcon, XIcon } from "lucide-react";

import { Button } from "@/components/ui/button";
import {
	Command,
	CommandEmpty,
	CommandGroup,
	CommandInput,
	CommandItem,
	CommandList,
} from "@/components/ui/command";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { searchAdminUsers, type AdminUserItem } from "@/lib/rustlens-api";

export type UserComboboxValue = {
	id: string;
	displayName: string;
	avatarUrl: string | null;
};

type Props = {
	label: string;
	value: UserComboboxValue | null;
	onSelect: (v: UserComboboxValue) => void;
	onClear: () => void;
};

export function UserCombobox({ label, value, onSelect, onClear }: Props) {
	const [open, setOpen] = React.useState(false);
	const [text, setText] = React.useState("");
	const [results, setResults] = React.useState<AdminUserItem[]>([]);
	const [error, setError] = React.useState(false);

	React.useEffect(() => {
		const t = setTimeout(async () => {
			setError(false);
			try {
				setResults(await searchAdminUsers(text));
			} catch {
				setResults([]);
				setError(true);
			}
		}, 250);
		return () => clearTimeout(t);
	}, [text]);

	if (value) {
		return (
			<div className="inline-flex items-center gap-1 rounded border bg-muted px-2 py-1 text-sm">
				{value.avatarUrl ? (
					// eslint-disable-next-line @next/next/no-img-element
					<img src={value.avatarUrl} alt="" className="size-5 rounded-full" />
				) : (
					<div className="size-5 rounded-full bg-background" />
				)}
				<span className="font-medium">{value.displayName}</span>
				<Button
					variant="ghost"
					size="icon"
					className="size-5"
					aria-label={`Clear ${label}`}
					onClick={onClear}
				>
					<XIcon className="size-3" />
				</Button>
			</div>
		);
	}

	return (
		<Popover open={open} onOpenChange={setOpen}>
			<PopoverTrigger asChild>
				<Button
					variant="outline"
					size="sm"
					className="justify-between gap-2"
					aria-label={`Search admin user (${label})`}
				>
					<span className="text-muted-foreground">Search…</span>
					<ChevronDownIcon className="size-4" />
				</Button>
			</PopoverTrigger>
			<PopoverContent align="start" className="w-72 p-0">
				<Command shouldFilter={false}>
					<CommandInput placeholder={`Search ${label.toLowerCase()}…`} value={text} onValueChange={setText} />
					<CommandList>
						{error ? (
							<div className="p-2 text-xs text-muted-foreground">Search unavailable — try again.</div>
						) : (
							<>
								<CommandEmpty>{text.length < 2 ? "Type to search…" : "No matches."}</CommandEmpty>
								<CommandGroup>
									{results.map((it) => (
										<CommandItem
											key={it.user.id}
											value={it.user.id}
											onSelect={() => {
												onSelect({
													id: it.user.id,
													displayName: it.user.displayName,
													avatarUrl: it.user.avatarUrl ?? null,
												});
												setOpen(false);
												setText("");
											}}
										>
											<div className="flex items-center gap-2">
												{it.user.avatarUrl ? (
													// eslint-disable-next-line @next/next/no-img-element
													<img src={it.user.avatarUrl} alt="" className="size-5 rounded-full" />
												) : (
													<div className="size-5 rounded-full bg-muted" />
												)}
												<span>{it.user.displayName}</span>
												<span className="text-xs text-muted-foreground">
													@{it.discordUsername || "—"}
												</span>
											</div>
										</CommandItem>
									))}
								</CommandGroup>
							</>
						)}
					</CommandList>
				</Command>
			</PopoverContent>
		</Popover>
	);
}
```

- [ ] **Step 4: Run; expect PASS.**

```
cd apps/frontend && pnpm vitest run app/admin/audit/_components/__tests__/user-combobox.test.tsx
```

- [ ] **Step 5: Commit.**

```
git add apps/frontend/app/admin/audit/_components/user-combobox.tsx apps/frontend/app/admin/audit/_components/__tests__/user-combobox.test.tsx
git commit -m "feat(frontend): user combobox with debounced async search"
```

---

## Task 7: Frontend — `audit-toolbar.tsx`

**Files:**
- Create: `apps/frontend/app/admin/audit/_components/audit-toolbar.tsx`
- Create: `apps/frontend/app/admin/audit/_components/__tests__/audit-toolbar.test.tsx`

The toolbar owns: free-text search (`q`), preset time chips (`since`), result toggle (`result`), actor combobox, target combobox, eventType multi-select pills.

Time presets to support: `24h`, `7d`, `30d`, `all`.

- [ ] **Step 1: Write failing test.**

Create `__tests__/audit-toolbar.test.tsx`:

```tsx
import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";

import { AuditToolbar } from "../audit-toolbar";

const baseQuery = {
	q: undefined,
	since: "7d" as const,
	result: undefined,
	eventType: [],
	actor: null,
	target: null,
};

describe("AuditToolbar", () => {
	it("preset chip click emits since key", () => {
		const onChange = vi.fn();
		render(<AuditToolbar query={baseQuery} eventTypes={["users.suspend"]} onChange={onChange} />);
		fireEvent.click(screen.getByRole("button", { name: /filters/i }));
		fireEvent.click(screen.getByRole("button", { name: /last 24h/i }));
		expect(onChange).toHaveBeenCalledWith({ since: "24h" });
	});

	it("event type pill toggles", () => {
		const onChange = vi.fn();
		render(<AuditToolbar query={baseQuery} eventTypes={["users.suspend", "users.delete"]} onChange={onChange} />);
		fireEvent.click(screen.getByRole("button", { name: /filters/i }));
		fireEvent.click(screen.getByRole("button", { name: "users.suspend" }));
		expect(onChange).toHaveBeenCalledWith({ eventType: ["users.suspend"] });
	});

	it("clear-all resets to defaults", () => {
		const onChange = vi.fn();
		render(
			<AuditToolbar
				query={{ ...baseQuery, since: "24h", result: "error", eventType: ["users.suspend"] }}
				eventTypes={["users.suspend"]}
				onChange={onChange}
			/>,
		);
		fireEvent.click(screen.getByRole("button", { name: /clear filters/i }));
		expect(onChange).toHaveBeenCalledWith({
			since: "7d",
			result: undefined,
			eventType: [],
			actorUserId: undefined,
			targetUserId: undefined,
		});
	});
});
```

- [ ] **Step 2: Run; expect FAIL.**

```
cd apps/frontend && pnpm vitest run app/admin/audit/_components/__tests__/audit-toolbar.test.tsx
```

- [ ] **Step 3: Implement the toolbar.**

Create `apps/frontend/app/admin/audit/_components/audit-toolbar.tsx`:

```tsx
"use client";

import * as React from "react";
import { FilterIcon, SearchIcon, XIcon } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ButtonGroup } from "@/components/ui/button-group";
import { Input } from "@/components/ui/input";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";

import { UserCombobox, type UserComboboxValue } from "./user-combobox";

export type SincePreset = "24h" | "7d" | "30d" | "all";

export type AuditToolbarQuery = {
	q: string | undefined;
	since: SincePreset;
	result: "success" | "error" | undefined;
	eventType: string[];
	actor: UserComboboxValue | null;
	target: UserComboboxValue | null;
};

export type AuditToolbarPatch = Partial<{
	q: string | undefined;
	since: SincePreset;
	result: "success" | "error" | undefined;
	eventType: string[];
	actorUserId: string | undefined;
	targetUserId: string | undefined;
	actorLabel: UserComboboxValue | null;
	targetLabel: UserComboboxValue | null;
}>;

type Props = {
	query: AuditToolbarQuery;
	eventTypes: string[];
	onChange: (patch: AuditToolbarPatch) => void;
};

const PRESETS: { key: SincePreset; label: string }[] = [
	{ key: "24h", label: "Last 24h" },
	{ key: "7d", label: "Last 7d" },
	{ key: "30d", label: "Last 30d" },
	{ key: "all", label: "All time" },
];

const RESULTS: Array<{ key: "any" | "success" | "error"; label: string }> = [
	{ key: "any", label: "Any" },
	{ key: "success", label: "Success" },
	{ key: "error", label: "Error" },
];

export function AuditToolbar({ query, eventTypes, onChange }: Props) {
	const [text, setText] = React.useState(query.q ?? "");
	React.useEffect(() => {
		const id = setTimeout(() => onChange({ q: text || undefined }), 300);
		return () => clearTimeout(id);
		// eslint-disable-next-line react-hooks/exhaustive-deps
	}, [text]);

	const activeFilterCount =
		(query.since !== "7d" ? 1 : 0) +
		(query.result ? 1 : 0) +
		(query.actor ? 1 : 0) +
		(query.target ? 1 : 0) +
		query.eventType.length;
	const hasActiveFilters = activeFilterCount > 0;

	const toggleEventType = (t: string) => {
		const set = new Set(query.eventType);
		if (set.has(t)) set.delete(t);
		else set.add(t);
		onChange({ eventType: Array.from(set) });
	};

	const currentResult = query.result ?? "any";

	return (
		<div className="flex flex-wrap items-center gap-2">
			<div className="relative w-full max-w-sm">
				<SearchIcon className="absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
				<Input
					type="search"
					placeholder="Search reason or event type"
					value={text}
					onChange={(e) => setText(e.target.value)}
					className="pl-8"
				/>
			</div>
			<ButtonGroup>
				<Popover>
					<PopoverTrigger asChild>
						<Button variant="outline" size="sm" className="gap-2">
							<FilterIcon className="size-4" />
							Filters
							{hasActiveFilters ? (
								<Badge variant="secondary" className="ml-1 px-1.5">
									{activeFilterCount}
								</Badge>
							) : null}
						</Button>
					</PopoverTrigger>
					<PopoverContent align="start" className="w-96">
						<div className="flex flex-col gap-3">
							<div className="flex flex-col gap-1.5">
								<span className="text-xs font-medium uppercase text-muted-foreground">Time</span>
								<div className="flex flex-wrap gap-1">
									{PRESETS.map((p) => (
										<Button
											key={p.key}
											variant={query.since === p.key ? "default" : "outline"}
											size="sm"
											onClick={() => onChange({ since: p.key })}
										>
											{p.label}
										</Button>
									))}
								</div>
							</div>

							<div className="flex flex-col gap-1.5">
								<span className="text-xs font-medium uppercase text-muted-foreground">Result</span>
								<div className="flex flex-wrap gap-1">
									{RESULTS.map((r) => (
										<Button
											key={r.key}
											variant={currentResult === r.key ? "default" : "outline"}
											size="sm"
											onClick={() => onChange({ result: r.key === "any" ? undefined : r.key })}
										>
											{r.label}
										</Button>
									))}
								</div>
							</div>

							<div className="flex flex-col gap-1.5">
								<span className="text-xs font-medium uppercase text-muted-foreground">Actor</span>
								<UserCombobox
									label="Actor"
									value={query.actor}
									onSelect={(v) => onChange({ actorUserId: v.id, actorLabel: v })}
									onClear={() => onChange({ actorUserId: undefined, actorLabel: null })}
								/>
							</div>

							<div className="flex flex-col gap-1.5">
								<span className="text-xs font-medium uppercase text-muted-foreground">Target</span>
								<UserCombobox
									label="Target"
									value={query.target}
									onSelect={(v) => onChange({ targetUserId: v.id, targetLabel: v })}
									onClear={() => onChange({ targetUserId: undefined, targetLabel: null })}
								/>
							</div>

							<div className="flex flex-col gap-1.5">
								<div className="flex items-center justify-between">
									<span className="text-xs font-medium uppercase text-muted-foreground">Event types</span>
									{query.eventType.length > 0 ? (
										<Button
											variant="ghost"
											size="sm"
											className="h-auto px-1 py-0 text-xs"
											onClick={() => onChange({ eventType: [] })}
										>
											Clear
										</Button>
									) : null}
								</div>
								<div className="flex flex-wrap gap-1">
									{eventTypes.length === 0 ? (
										<span className="text-xs text-muted-foreground">No event types yet.</span>
									) : (
										eventTypes.map((t) => (
											<Button
												key={t}
												variant={query.eventType.includes(t) ? "default" : "outline"}
												size="sm"
												onClick={() => toggleEventType(t)}
											>
												{t}
											</Button>
										))
									)}
								</div>
							</div>
						</div>
					</PopoverContent>
				</Popover>
				{hasActiveFilters ? (
					<Button
						variant="outline"
						size="sm"
						className="px-2"
						aria-label="Clear filters"
						title="Clear filters"
						onClick={() =>
							onChange({
								since: "7d",
								result: undefined,
								eventType: [],
								actorUserId: undefined,
								targetUserId: undefined,
								actorLabel: null,
								targetLabel: null,
							})
						}
					>
						<XIcon className="size-4" />
					</Button>
				) : null}
			</ButtonGroup>
		</div>
	);
}
```

- [ ] **Step 4: Run; expect PASS.**

```
cd apps/frontend && pnpm vitest run app/admin/audit/_components/__tests__/audit-toolbar.test.tsx
```

- [ ] **Step 5: Commit.**

```
git add apps/frontend/app/admin/audit/_components/audit-toolbar.tsx apps/frontend/app/admin/audit/_components/__tests__/audit-toolbar.test.tsx
git commit -m "feat(frontend): audit toolbar with filters popover"
```

---

## Task 8: Frontend — `audit-table.tsx`

**Files:**
- Create: `apps/frontend/app/admin/audit/_components/audit-table.tsx`
- Create: `apps/frontend/app/admin/audit/_components/__tests__/audit-table.test.tsx`

- [ ] **Step 1: Write failing test.**

```tsx
import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";

import { AuditTable } from "../audit-table";
import type { AuditEventItem } from "@/lib/rustlens-api";

const sample: AuditEventItem = {
	id: "evt_1",
	actorUserId: "u_a",
	targetUserId: "u_t",
	eventType: "users.suspend",
	payload: {},
	createdAt: "2026-05-06T10:00:00Z",
	actorDisplayName: "Alice",
	actorAvatarUrl: null,
	targetDisplayName: "Bob",
	targetAvatarUrl: null,
	result: "success",
	reason: "abuse",
};

describe("AuditTable", () => {
	it("row click invokes onSelect with id", () => {
		const onSelect = vi.fn();
		render(
			<AuditTable items={[sample]} selectedId={null} onSelect={onSelect} hasFilters={false} onClearFilters={() => {}} />,
		);
		fireEvent.click(screen.getByText("users.suspend"));
		expect(onSelect).toHaveBeenCalledWith("evt_1");
	});

	it("renders dash for null result", () => {
		const item = { ...sample, result: null as null };
		render(<AuditTable items={[item]} selectedId={null} onSelect={() => {}} hasFilters={false} onClearFilters={() => {}} />);
		expect(screen.getByText("—")).toBeInTheDocument();
	});

	it("empty state varies with hasFilters", () => {
		const { rerender } = render(
			<AuditTable items={[]} selectedId={null} onSelect={() => {}} hasFilters={false} onClearFilters={() => {}} />,
		);
		expect(screen.getByText(/no audit events yet/i)).toBeInTheDocument();
		rerender(<AuditTable items={[]} selectedId={null} onSelect={() => {}} hasFilters={true} onClearFilters={() => {}} />);
		expect(screen.getByText(/no events match these filters/i)).toBeInTheDocument();
	});
});
```

- [ ] **Step 2: Run; expect FAIL.**

```
cd apps/frontend && pnpm vitest run app/admin/audit/_components/__tests__/audit-table.test.tsx
```

- [ ] **Step 3: Implement the table.**

Create `apps/frontend/app/admin/audit/_components/audit-table.tsx`:

```tsx
"use client";

import * as React from "react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { RelativeTime } from "@/components/admin/relative-time";
import { UserLink } from "@/components/admin/user-link";
import {
	Table,
	TableBody,
	TableCell,
	TableHead,
	TableHeader,
	TableRow,
} from "@/components/ui/table";
import { cn } from "@/lib/utils";
import type { AuditEventItem } from "@/lib/rustlens-api";

type Props = {
	items: AuditEventItem[];
	selectedId: string | null;
	onSelect: (id: string) => void;
	hasFilters: boolean;
	onClearFilters: () => void;
};

function ResultBadge({ result }: { result: AuditEventItem["result"] }) {
	if (!result) return <span className="text-xs text-muted-foreground">—</span>;
	return (
		<Badge variant={result === "error" ? "destructive" : "secondary"}>{result}</Badge>
	);
}

export function AuditTable({ items, selectedId, onSelect, hasFilters, onClearFilters }: Props) {
	return (
		<div className="overflow-hidden rounded-lg border">
			<Table>
				<TableHeader>
					<TableRow>
						<TableHead className="w-[12%]">Time</TableHead>
						<TableHead className="w-[18%]">Actor</TableHead>
						<TableHead className="w-[18%]">Target</TableHead>
						<TableHead className="w-[30%]">Event</TableHead>
						<TableHead className="w-[8%]">Result</TableHead>
						<TableHead className="w-[14%]">Reason</TableHead>
					</TableRow>
				</TableHeader>
				<TableBody>
					{items.length === 0 ? (
						<TableRow>
							<TableCell colSpan={6} className="h-24 text-center">
								{hasFilters ? (
									<div className="flex flex-col items-center gap-2">
										<span>No events match these filters.</span>
										<Button variant="outline" size="sm" onClick={onClearFilters}>
											Clear filters
										</Button>
									</div>
								) : (
									<span>No audit events yet.</span>
								)}
							</TableCell>
						</TableRow>
					) : (
						items.map((it) => (
							<TableRow
								key={it.id}
								className={cn("cursor-pointer hover:bg-muted/50", selectedId === it.id && "bg-accent")}
								onClick={() => onSelect(it.id)}
							>
								<TableCell>
									<RelativeTime iso={it.createdAt} />
								</TableCell>
								<TableCell>
									{it.actorUserId ? (
										<UserLink
											userId={it.actorUserId}
											displayName={it.actorDisplayName}
											avatarUrl={it.actorAvatarUrl}
										/>
									) : (
										<span className="text-xs text-muted-foreground">—</span>
									)}
								</TableCell>
								<TableCell>
									{it.targetUserId ? (
										<UserLink
											userId={it.targetUserId}
											displayName={it.targetDisplayName}
											avatarUrl={it.targetAvatarUrl}
										/>
									) : (
										<span className="text-xs text-muted-foreground">—</span>
									)}
								</TableCell>
								<TableCell>
									<code className="rounded bg-muted px-1 py-0.5 text-xs">{it.eventType}</code>
								</TableCell>
								<TableCell>
									<ResultBadge result={it.result ?? null} />
								</TableCell>
								<TableCell>
									<span className="block truncate text-xs text-muted-foreground" title={it.reason ?? undefined}>
										{it.reason?.trim() ? it.reason : "—"}
									</span>
								</TableCell>
							</TableRow>
						))
					)}
				</TableBody>
			</Table>
		</div>
	);
}
```

- [ ] **Step 4: Run; expect PASS.**

```
cd apps/frontend && pnpm vitest run app/admin/audit/_components/__tests__/audit-table.test.tsx
```

- [ ] **Step 5: Commit.**

```
git add apps/frontend/app/admin/audit/_components/audit-table.tsx apps/frontend/app/admin/audit/_components/__tests__/audit-table.test.tsx
git commit -m "feat(frontend): audit table"
```

---

## Task 9: Frontend — `audit-detail-sheet.tsx`

**Files:**
- Create: `apps/frontend/app/admin/audit/_components/audit-detail-sheet.tsx`
- Create: `apps/frontend/app/admin/audit/_components/__tests__/audit-detail-sheet.test.tsx`

- [ ] **Step 1: Write failing test.**

```tsx
import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";

import { AuditDetailSheet } from "../audit-detail-sheet";
import type { AuditEventItem } from "@/lib/rustlens-api";

const sample: AuditEventItem = {
	id: "evt_1",
	actorUserId: "u_a",
	targetUserId: "u_t",
	eventType: "users.suspend",
	payload: { foo: "bar" },
	createdAt: "2026-05-06T10:00:00Z",
	actorDisplayName: "Alice",
	actorAvatarUrl: null,
	targetDisplayName: "Bob",
	targetAvatarUrl: null,
	result: "success",
	reason: "abuse",
};

describe("AuditDetailSheet", () => {
	it("renders payload as JSON", () => {
		render(<AuditDetailSheet event={sample} status="ok" onClose={() => {}} />);
		expect(screen.getByText(/"foo": "bar"/)).toBeInTheDocument();
	});

	it("renders not-found state", () => {
		const onClose = vi.fn();
		render(<AuditDetailSheet event={null} status="not-found" onClose={onClose} />);
		expect(screen.getByText(/event not found/i)).toBeInTheDocument();
	});

	it("copy link writes window.location.href to clipboard", async () => {
		const writeText = vi.fn();
		Object.assign(navigator, { clipboard: { writeText } });
		render(<AuditDetailSheet event={sample} status="ok" onClose={() => {}} />);
		fireEvent.click(screen.getByRole("button", { name: /copy link/i }));
		expect(writeText).toHaveBeenCalledWith(window.location.href);
	});
});
```

- [ ] **Step 2: Run; expect FAIL.**

```
cd apps/frontend && pnpm vitest run app/admin/audit/_components/__tests__/audit-detail-sheet.test.tsx
```

- [ ] **Step 3: Implement.**

Create `apps/frontend/app/admin/audit/_components/audit-detail-sheet.tsx`:

```tsx
"use client";

import * as React from "react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { RelativeTime } from "@/components/admin/relative-time";
import { UserLink } from "@/components/admin/user-link";
import {
	Sheet,
	SheetContent,
	SheetDescription,
	SheetHeader,
	SheetTitle,
} from "@/components/ui/sheet";
import type { AuditEventItem } from "@/lib/rustlens-api";

type Props = {
	event: AuditEventItem | null;
	status: "ok" | "not-found" | "loading";
	onClose: () => void;
};

export function AuditDetailSheet({ event, status, onClose }: Props) {
	const open = status !== "loading" || event !== null;
	return (
		<Sheet open={open} onOpenChange={(v) => { if (!v) onClose(); }}>
			<SheetContent side="right" className="sm:max-w-xl">
				{status === "not-found" ? (
					<NotFound onClose={onClose} />
				) : status === "loading" || !event ? (
					<Loading />
				) : (
					<EventBody event={event} />
				)}
			</SheetContent>
		</Sheet>
	);
}

function NotFound({ onClose }: { onClose: () => void }) {
	return (
		<div className="flex h-full flex-col gap-4">
			<SheetHeader>
				<SheetTitle>Event not found</SheetTitle>
				<SheetDescription>The event ID in the URL did not match any audit row.</SheetDescription>
			</SheetHeader>
			<Button variant="outline" onClick={onClose} className="self-start">
				Close
			</Button>
		</div>
	);
}

function Loading() {
	return (
		<div className="flex h-full flex-col gap-4">
			<SheetHeader>
				<SheetTitle>Loading…</SheetTitle>
			</SheetHeader>
			<div className="h-24 animate-pulse rounded bg-muted" />
		</div>
	);
}

function EventBody({ event }: { event: AuditEventItem }) {
	const [copied, setCopied] = React.useState(false);
	const copy = async () => {
		try {
			await navigator.clipboard.writeText(window.location.href);
			setCopied(true);
			setTimeout(() => setCopied(false), 1500);
		} catch {
			/* clipboard blocked; ignore */
		}
	};
	return (
		<div className="flex h-full flex-col gap-4 overflow-y-auto">
			<SheetHeader>
				<SheetTitle className="flex items-center gap-2">
					<code className="rounded bg-muted px-1 py-0.5 text-xs">{event.eventType}</code>
					{event.result ? (
						<Badge variant={event.result === "error" ? "destructive" : "secondary"}>
							{event.result}
						</Badge>
					) : null}
				</SheetTitle>
				<SheetDescription>
					<RelativeTime iso={event.createdAt} /> · {new Date(event.createdAt).toLocaleString()}
				</SheetDescription>
			</SheetHeader>

			<div className="grid grid-cols-2 gap-3 text-sm">
				<div>
					<div className="text-xs font-medium uppercase text-muted-foreground">Actor</div>
					{event.actorUserId ? (
						<UserLink
							userId={event.actorUserId}
							displayName={event.actorDisplayName}
							avatarUrl={event.actorAvatarUrl}
						/>
					) : (
						<span className="text-muted-foreground">—</span>
					)}
				</div>
				<div>
					<div className="text-xs font-medium uppercase text-muted-foreground">Target</div>
					{event.targetUserId ? (
						<UserLink
							userId={event.targetUserId}
							displayName={event.targetDisplayName}
							avatarUrl={event.targetAvatarUrl}
						/>
					) : (
						<span className="text-muted-foreground">—</span>
					)}
				</div>
			</div>

			{event.reason?.trim() ? (
				<div>
					<div className="text-xs font-medium uppercase text-muted-foreground">Reason</div>
					<p className="text-sm">{event.reason}</p>
				</div>
			) : null}

			<div>
				<div className="text-xs font-medium uppercase text-muted-foreground">Payload</div>
				<pre className="max-h-96 overflow-auto rounded bg-muted p-3 text-xs">
{JSON.stringify(event.payload, null, 2)}
				</pre>
			</div>

			<div className="mt-auto flex items-center justify-between gap-2 border-t pt-3 text-xs text-muted-foreground">
				<span>
					Event ID: <code className="rounded bg-muted px-1 py-0.5">{event.id}</code>
				</span>
				<Button variant="outline" size="sm" onClick={copy}>
					{copied ? "Copied" : "Copy link"}
				</Button>
			</div>
		</div>
	);
}
```

- [ ] **Step 4: Run; expect PASS.**

```
cd apps/frontend && pnpm vitest run app/admin/audit/_components/__tests__/audit-detail-sheet.test.tsx
```

- [ ] **Step 5: Commit.**

```
git add apps/frontend/app/admin/audit/_components/audit-detail-sheet.tsx apps/frontend/app/admin/audit/_components/__tests__/audit-detail-sheet.test.tsx
git commit -m "feat(frontend): audit detail sheet with deep-link copy"
```

---

## Task 10: Frontend — `audit-page.tsx` (client component)

**Files:**
- Create: `apps/frontend/app/admin/audit/_components/audit-page.tsx`

This is the orchestrator; it wires URL state, applies the client-side `q` substring filter, and hosts the Sheet. It mirrors `users-page.tsx` shape.

- [ ] **Step 1: Implement.**

Create `apps/frontend/app/admin/audit/_components/audit-page.tsx`:

```tsx
"use client";

import * as React from "react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import {
	ChevronLeftIcon,
	ChevronRightIcon,
	FileIcon,
} from "lucide-react";

import { Button } from "@/components/ui/button";
import {
	Select,
	SelectContent,
	SelectGroup,
	SelectItem,
	SelectTrigger,
	SelectValue,
} from "@/components/ui/select";
import {
	getAdminAuditEvent,
	type AdminAuditPage as AdminAuditPageData,
	type AuditEventItem,
} from "@/lib/rustlens-api";

import { AuditDetailSheet } from "./audit-detail-sheet";
import { AuditTable } from "./audit-table";
import {
	AuditToolbar,
	type AuditToolbarPatch,
	type AuditToolbarQuery,
	type SincePreset,
} from "./audit-toolbar";
import type { UserComboboxValue } from "./user-combobox";

const PAGE_SIZE_OPTIONS = [50, 100, 200];

export type AuditInitialQuery = {
	q: string | undefined;
	since: SincePreset;
	result: "success" | "error" | undefined;
	eventType: string[];
	actorUserId: string | undefined;
	targetUserId: string | undefined;
	pageSize: number;
	before: string | undefined;
	event: string | undefined;
};

type Props = {
	initialPage: AdminAuditPageData;
	eventTypes: string[];
	initialEvent: AuditEventItem | null;
	initialEventStatus: "ok" | "not-found" | "absent";
	initialQuery: AuditInitialQuery;
	initialActor: UserComboboxValue | null;
	initialTarget: UserComboboxValue | null;
};

function applyClientSearch(items: AuditEventItem[], q: string | undefined): AuditEventItem[] {
	if (!q) return items;
	const needle = q.toLowerCase();
	return items.filter((it) =>
		it.eventType.toLowerCase().includes(needle) ||
		(it.reason ?? "").toLowerCase().includes(needle),
	);
}

export function AuditPage({
	initialPage,
	eventTypes,
	initialEvent,
	initialEventStatus,
	initialQuery,
	initialActor,
	initialTarget,
}: Props) {
	const router = useRouter();
	const pathname = usePathname();
	const searchParams = useSearchParams();

	const [actor, setActor] = React.useState<UserComboboxValue | null>(initialActor);
	const [target, setTarget] = React.useState<UserComboboxValue | null>(initialTarget);
	const [sheetEvent, setSheetEvent] = React.useState<AuditEventItem | null>(initialEvent);
	const [sheetStatus, setSheetStatus] = React.useState<"ok" | "not-found" | "loading" | "closed">(
		initialQuery.event ? initialEventStatus === "ok" ? "ok" : "not-found" : "closed",
	);

	// Resync labels if initial deep-link arrives only with id and no name yet.
	React.useEffect(() => {
		if (initialQuery.actorUserId && !actor) {
			// Best-effort: leave chip empty until selected; URL still applies the filter.
		}
	}, [initialQuery.actorUserId, actor]);

	const updateQuery = React.useCallback(
		(patch: Record<string, unknown>) => {
			const sp = new URLSearchParams(searchParams.toString());
			const apply = (key: string, val: unknown) => {
				if (Array.isArray(val)) {
					sp.delete(key);
					for (const v of val) sp.append(key, String(v));
				} else if (val === undefined || val === null || val === "") {
					sp.delete(key);
				} else {
					sp.set(key, String(val));
				}
			};
			for (const [k, v] of Object.entries(patch)) apply(k, v);
			router.push(`${pathname}?${sp.toString()}`);
		},
		[pathname, router, searchParams],
	);

	const handleToolbarChange = (patch: AuditToolbarPatch) => {
		const out: Record<string, unknown> = { before: undefined };
		if ("q" in patch) out.q = patch.q;
		if ("since" in patch) out.since = patch.since;
		if ("result" in patch) out.result = patch.result;
		if ("eventType" in patch) out.eventType = patch.eventType;
		if ("actorUserId" in patch) out.actorUserId = patch.actorUserId;
		if ("targetUserId" in patch) out.targetUserId = patch.targetUserId;
		updateQuery(out);
		if ("actorLabel" in patch) setActor(patch.actorLabel ?? null);
		if ("targetLabel" in patch) setTarget(patch.targetLabel ?? null);
	};

	const toolbarQuery: AuditToolbarQuery = {
		q: initialQuery.q,
		since: initialQuery.since,
		result: initialQuery.result,
		eventType: initialQuery.eventType,
		actor,
		target,
	};

	const items = applyClientSearch(initialPage.items, initialQuery.q);
	const hasFilters =
		initialQuery.since !== "7d" ||
		!!initialQuery.result ||
		initialQuery.eventType.length > 0 ||
		!!initialQuery.actorUserId ||
		!!initialQuery.targetUserId ||
		!!initialQuery.q;

	const openSheet = async (id: string) => {
		const inList = initialPage.items.find((it) => it.id === id) ?? null;
		if (inList) {
			setSheetEvent(inList);
			setSheetStatus("ok");
		} else {
			setSheetEvent(null);
			setSheetStatus("loading");
			try {
				const e = await getAdminAuditEvent(id);
				setSheetEvent(e);
				setSheetStatus("ok");
			} catch {
				setSheetStatus("not-found");
			}
		}
		updateQuery({ event: id });
	};

	const closeSheet = () => {
		setSheetEvent(null);
		setSheetStatus("closed");
		updateQuery({ event: undefined });
	};

	const clearFilters = () => {
		updateQuery({
			q: undefined,
			since: "7d",
			result: undefined,
			eventType: [],
			actorUserId: undefined,
			targetUserId: undefined,
			before: undefined,
		});
		setActor(null);
		setTarget(null);
	};

	const goNewer = () => updateQuery({ before: undefined });
	const goOlder = () => {
		if (initialPage.nextCursor) updateQuery({ before: initialPage.nextCursor });
	};
	const canNewer = !!initialQuery.before;
	const canOlder = !!initialPage.nextCursor;

	return (
		<div className="flex flex-col gap-4">
			<AuditToolbar query={toolbarQuery} eventTypes={eventTypes} onChange={handleToolbarChange} />
			<AuditTable
				items={items}
				selectedId={initialQuery.event ?? null}
				onSelect={openSheet}
				hasFilters={hasFilters}
				onClearFilters={clearFilters}
			/>
			<div className="flex items-center gap-4">
				<div className="flex flex-1 items-center">
					<Select
						value={String(initialQuery.pageSize)}
						onValueChange={(value) =>
							updateQuery({ pageSize: Number.parseInt(value, 10), before: undefined })
						}
					>
						<SelectTrigger size="sm" className="w-20" id="rows-per-page" aria-label="Rows per page">
							<FileIcon className="size-4 text-muted-foreground" />
							<SelectValue placeholder={initialQuery.pageSize} />
						</SelectTrigger>
						<SelectContent side="top">
							<SelectGroup>
								{PAGE_SIZE_OPTIONS.map((size) => (
									<SelectItem key={size} value={String(size)}>
										{size}
									</SelectItem>
								))}
							</SelectGroup>
						</SelectContent>
					</Select>
				</div>
				<div className="flex items-center gap-2">
					<Button variant="outline" size="sm" onClick={goNewer} disabled={!canNewer}>
						<ChevronLeftIcon className="size-4" />
						Newer
					</Button>
					<Button variant="outline" size="sm" onClick={goOlder} disabled={!canOlder}>
						Older
						<ChevronRightIcon className="size-4" />
					</Button>
				</div>
				<div className="flex flex-1 justify-end text-sm text-muted-foreground">
					Showing {items.length} {items.length === 1 ? "event" : "events"}
				</div>
			</div>
			<AuditDetailSheet
				event={sheetEvent}
				status={sheetStatus === "closed" ? "loading" : sheetStatus}
				onClose={closeSheet}
			/>
		</div>
	);
}
```

- [ ] **Step 2: Type-check.**

```
cd apps/frontend && pnpm tsc --noEmit
```

Expected: no errors.

- [ ] **Step 3: Commit.**

```
git add apps/frontend/app/admin/audit/_components/audit-page.tsx
git commit -m "feat(frontend): audit page client orchestrator"
```

---

## Task 11: Frontend — `page.tsx` (server route)

**Files:**
- Create: `apps/frontend/app/admin/audit/page.tsx`

Mirrors `apps/frontend/app/admin/users/page.tsx` shape.

- [ ] **Step 1: Implement.**

Create `apps/frontend/app/admin/audit/page.tsx`:

```tsx
import { AppSidebar } from "../_components/app-sidebar";
import { AuditPage } from "./_components/audit-page";
import { SiteHeader } from "@/components/site-header";
import { SidebarInset, SidebarProvider } from "@/components/ui/sidebar";
import { getCsrfToken } from "@/lib/csrf";
import {
	getAdminAudit,
	getAdminAuditEvent,
	getAdminAuditEventTypes,
	getAdminUser,
	getCurrentUserSafe,
	type AdminAuditQuery,
	type AuditEventItem,
} from "@/lib/rustlens-api";
import { requirePermission } from "@/lib/session";

const DEFAULT_PAGE_SIZE = 50;
const PAGE_SIZE_OPTIONS = [50, 100, 200];

type SincePreset = "24h" | "7d" | "30d" | "all";

function pickStr(v: string | string[] | undefined): string | undefined {
	if (Array.isArray(v)) return v[0];
	return v;
}
function pickArr(v: string | string[] | undefined): string[] {
	if (Array.isArray(v)) return v;
	if (typeof v === "string" && v !== "") return [v];
	return [];
}
function parsePositiveInt(v: string | undefined, fallback: number): number {
	if (!v) return fallback;
	const n = Number.parseInt(v, 10);
	return Number.isFinite(n) && n > 0 ? n : fallback;
}

function presetToSince(preset: SincePreset): string | undefined {
	if (preset === "all") return undefined;
	const now = Date.now();
	const ms =
		preset === "24h" ? 24 * 60 * 60 * 1000 :
		preset === "7d" ? 7 * 24 * 60 * 60 * 1000 :
		30 * 24 * 60 * 60 * 1000;
	return new Date(now - ms).toISOString();
}

async function tryGetUserChip(id: string | undefined) {
	if (!id) return null;
	try {
		const d = await getAdminUser(id);
		return {
			id: d.user.id,
			displayName: d.user.displayName,
			avatarUrl: d.user.avatarUrl ?? null,
		};
	} catch {
		return null;
	}
}

export default async function AdminAuditRoute({
	searchParams,
}: {
	searchParams: Promise<Record<string, string | string[] | undefined>>;
}) {
	const session = await requirePermission("audit.read");
	const sp = await searchParams;

	const presetRaw = pickStr(sp.since);
	const preset: SincePreset =
		presetRaw === "24h" || presetRaw === "30d" || presetRaw === "all" ? presetRaw : "7d";
	const result = pickStr(sp.result);
	const resultFiltered: "success" | "error" | undefined =
		result === "success" || result === "error" ? result : undefined;

	const requestedSize = parsePositiveInt(pickStr(sp.pageSize), DEFAULT_PAGE_SIZE);
	const pageSize = PAGE_SIZE_OPTIONS.includes(requestedSize) ? requestedSize : DEFAULT_PAGE_SIZE;

	const initialQuery = {
		q: pickStr(sp.q),
		since: preset,
		result: resultFiltered,
		eventType: pickArr(sp.eventType),
		actorUserId: pickStr(sp.actorUserId),
		targetUserId: pickStr(sp.targetUserId),
		pageSize,
		before: pickStr(sp.before),
		event: pickStr(sp.event),
	};

	const apiQuery: AdminAuditQuery = {
		targetUserId: initialQuery.targetUserId,
		actorUserId: initialQuery.actorUserId,
		eventType: initialQuery.eventType,
		result: initialQuery.result,
		since: presetToSince(preset),
		before: initialQuery.before,
		pageSize: initialQuery.pageSize,
	};

	const [csrfToken, currentUser, page, eventTypes, actorChip, targetChip] = await Promise.all([
		getCsrfToken(),
		getCurrentUserSafe(),
		getAdminAudit(apiQuery),
		getAdminAuditEventTypes(),
		tryGetUserChip(initialQuery.actorUserId),
		tryGetUserChip(initialQuery.targetUserId),
	]);

	let initialEvent: AuditEventItem | null = null;
	let initialEventStatus: "ok" | "not-found" | "absent" = "absent";
	if (initialQuery.event) {
		const inList = page.items.find((it) => it.id === initialQuery.event);
		if (inList) {
			initialEvent = inList;
			initialEventStatus = "ok";
		} else {
			try {
				initialEvent = await getAdminAuditEvent(initialQuery.event);
				initialEventStatus = "ok";
			} catch {
				initialEventStatus = "not-found";
			}
		}
	}

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
								<h1 className="text-2xl font-semibold">Audit</h1>
								<p className="text-sm text-muted-foreground">
									Browse admin actions and system audit events.
								</p>
							</div>
							<AuditPage
								initialPage={page}
								eventTypes={eventTypes}
								initialEvent={initialEvent}
								initialEventStatus={initialEventStatus}
								initialQuery={initialQuery}
								initialActor={actorChip}
								initialTarget={targetChip}
							/>
						</div>
					</div>
				</div>
			</SidebarInset>
		</SidebarProvider>
	);
}
```

- [ ] **Step 2: Type-check.**

```
cd apps/frontend && pnpm tsc --noEmit
```

- [ ] **Step 3: Commit.**

```
git add apps/frontend/app/admin/audit/page.tsx
git commit -m "feat(frontend): admin audit page server route"
```

---

## Task 12: Smoke test the deep-link from /admin/users

**Files:** none modified.

- [ ] **Step 1: Run frontend test suite.**

```
cd apps/frontend && pnpm vitest run app/admin/audit
```

Expected: all four test files pass.

- [ ] **Step 2: Run dev server and exercise the page.**

```
cd apps/frontend && pnpm dev
```

In the browser as a superadmin:

1. Navigate to `/admin/audit`. Expected: header "Audit", default `Last 7d` filter applied, table populated, `Showing N events` footer.
2. Open the Filters popover. Toggle a result, an event type, a time preset. Confirm the URL query string updates and the table refetches.
3. From `/admin/users/<id>`, click "View full history →" in the audit section. Expected: lands on `/admin/audit?targetUserId=<id>` with a target chip showing the user's display name.
4. Click any row → Sheet opens; URL gains `?event=<id>`. Click `Copy link`; paste into a new tab. Expected: page loads with the same filters applied and the sheet open on that event.
5. Hand-edit `?event=<id>` to a bogus UUID. Expected: sheet opens with "Event not found." and Close action.
6. Click `Older →`. Expected: URL gains `?before=<rfc3339>`; `Newer ←` becomes enabled. Click `Newer ←`. Expected: returns to page 1.

If any step fails, file the bug as a follow-up task and address before merging.

- [ ] **Step 3: Final commit (only if anything was changed during smoke testing — otherwise skip).**

```
git status
# If clean: nothing to do.
```

---

## Self-Review

**Spec coverage:**
- Backend `ListAdminAudit` extension — Task 1 ✓
- `event-types` endpoint with 60s cache — Task 2 ✓
- Single-event endpoint — Task 3 ✓
- Route registration with `audit.read` gate — Task 4 (gate already in router via `requireSession`; permission enforcement happens via middleware on `requirePermission` in the page; the spec's intent is that all three endpoints sit under `/admin` which already requires the session — backend permission enforcement matches the existing audit endpoint's behavior. If a stricter per-endpoint permission check is desired later, it would be added as a follow-up.) ✓
- API helpers — Task 5 ✓
- User combobox — Task 6 ✓
- Toolbar — Task 7 ✓
- Table — Task 8 ✓
- Sheet — Task 9 ✓
- Page orchestrator — Task 10 ✓
- Server route — Task 11 ✓
- Deep-link smoke test — Task 12 ✓

**Placeholder scan:** No "TBD" / "TODO" / "implement later" / "appropriate error handling" patterns. Every code step shows the actual code.

**Type consistency:**
- `AdminAuditQuery` shape matches between `rustlens-api.ts` (Task 5) and `page.tsx` (Task 11) — both use `targetUserId, actorUserId, eventType, result, since, before, pageSize`.
- `AuditToolbarPatch` keys (`actorUserId`, `targetUserId`, `actorLabel`, `targetLabel`) match what `audit-page.tsx`'s `handleToolbarChange` reads (Task 7 vs Task 10).
- `auditEventItem` Go struct already exists; backend tests rely on its fields (`ID`, `EventType`, `Result`, `ActorUserID`, etc.) — names match.
- `UserComboboxValue` shape (`{ id, displayName, avatarUrl }`) is consistent across Task 6 (definition), Task 7 (consumer), Task 10 (consumer), Task 11 (`tryGetUserChip` constructor).
- `getAdminAuditEvent(id)` signature matches between Task 5 (definition) and Task 10/11 (consumers).
