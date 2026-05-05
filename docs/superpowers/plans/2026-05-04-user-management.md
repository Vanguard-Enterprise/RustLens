# User Management Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the admin user management surface at `/admin/users` (list with filtering, bulk actions, CSV export) and `/admin/users/[userId]` (detail with role assignment, profile editing, suspend/delete/restore/purge), and add heavy auditing to the existing CSV export endpoint.

**Architecture:** Two new App Router pages, each a server component that calls existing backend endpoints (`/api/app/admin/users`, `/api/app/admin/users/{id}`, `/api/app/admin/users/{id}/audit`, `/api/app/admin/roles`). Mutations flow through Next.js Server Actions in `app/admin/users/actions.ts` that call thin wrappers around the existing `postAdminAction` helper, mirroring the established pattern from `app/admin/roles/`. Backend changes are scoped to `admin_export_handlers.go` (paired audit events) and the `fetchUserAudit` helper (JSONB OR-match for `params.filters.ids`).

**Tech Stack:** Next.js 16 App Router, React 19, TypeScript, shadcn/ui, Tailwind v4, vitest + @testing-library/react. Go 1.24 (chi, pgx) for backend changes. Tests run via `pnpm --filter frontend test` and `cd apps/backend && go test ./internal/http/...`.

**Spec:** `docs/superpowers/specs/2026-05-04-user-management-design.md`

---

## File map

### New frontend files

| Path | Responsibility |
|---|---|
| `apps/frontend/app/admin/users/page.tsx` | Server component: fetch initial users page + roles, render `<UsersPage>` |
| `apps/frontend/app/admin/users/[userId]/page.tsx` | Server component: fetch user detail + audit + roles, render `<UserDetailPage>` |
| `apps/frontend/app/admin/users/actions.ts` | Server actions: `assignRoleAction`, `revokeRoleAction`, `editUserAction`, `suspendUserAction`, `unsuspendUserAction`, `deleteUserAction`, `restoreUserAction`, `purgeUserAction`, `bulkUserAction` |
| `apps/frontend/app/admin/users/_components/users-page.tsx` | Client orchestrator: URL ↔ filter state, owns selection set |
| `apps/frontend/app/admin/users/_components/users-toolbar.tsx` | Search input, status pills, role multi-select, sort dropdown, "Export filtered" |
| `apps/frontend/app/admin/users/_components/users-table.tsx` | Data table with checkbox column |
| `apps/frontend/app/admin/users/_components/users-bulk-toolbar.tsx` | Sticky toolbar shown when ≥1 row selected |
| `apps/frontend/app/admin/users/_components/bulk-suspend-dialog.tsx` | Reason dialog → `bulkUserAction("suspend"\|"unsuspend", …)` |
| `apps/frontend/app/admin/users/_components/bulk-role-dialog.tsx` | Role picker + reason → `bulkUserAction("roles.grant"\|"roles.revoke", …)` |
| `apps/frontend/app/admin/users/_components/export-button.tsx` | Triggers CSV download via `<a download>`, shows "Logged to audit" microcopy |
| `apps/frontend/app/admin/users/_components/user-detail-page.tsx` | Client orchestrator for the detail view |
| `apps/frontend/app/admin/users/_components/identity-header.tsx` | Avatar, names, IDs, status badges |
| `apps/frontend/app/admin/users/_components/roles-section.tsx` | Chips + "Add role…" combobox |
| `apps/frontend/app/admin/users/_components/grant-role-dialog.tsx` | Reason dialog → `assignRoleAction` |
| `apps/frontend/app/admin/users/_components/revoke-role-dialog.tsx` | Reason dialog → `revokeRoleAction` |
| `apps/frontend/app/admin/users/_components/profile-section.tsx` | Read/edit profile fields |
| `apps/frontend/app/admin/users/_components/audit-section.tsx` | Last 30 audit rows + link to `/admin/audit?targetUserId=` |
| `apps/frontend/app/admin/users/_components/danger-zone.tsx` | Suspend / Unsuspend / Delete / Restore / Purge entries |
| `apps/frontend/app/admin/users/_components/suspend-dialog.tsx` | Single-user suspend (reason) |
| `apps/frontend/app/admin/users/_components/delete-dialog.tsx` | Single-user delete (reason + typed display-name) |
| `apps/frontend/app/admin/users/_components/restore-dialog.tsx` | Single-user restore (confirm) |
| `apps/frontend/app/admin/users/_components/purge-dialog.tsx` | Single-user purge (reason + typed user-id) |
| `apps/frontend/app/admin/users/_components/__tests__/users-page.test.tsx` | URL ↔ state, selection behavior |
| `apps/frontend/app/admin/users/_components/__tests__/grant-role-dialog.test.tsx` | Verifies assignRoleAction payload |
| `apps/frontend/app/admin/users/_components/__tests__/bulk-role-dialog.test.tsx` | Verifies bulkUserAction payload |
| `apps/frontend/app/admin/users/_components/__tests__/export-button.test.tsx` | Verifies download URL build |

### Modified frontend files

| Path | Change |
|---|---|
| `apps/frontend/lib/rustlens-api.ts` | Add typed wrappers: `assignAdminRole`, `revokeAdminRole`, `bulkUserAction`, `editAdminUser`, `suspendUser`, `unsuspendUser`, `deleteUser`, `restoreUser`, `purgeUser`, `getAdminUsersExportUrl` |
| `apps/frontend/components/app-shared.tsx` | Change `/admin/users` permissions from `["users.suspend", "users.force_unlink_steam"]` to `["users.read"]` |

### Modified backend files

| Path | Change |
|---|---|
| `apps/backend/internal/http/admin_export_handlers.go` | Add `users.export.started` and `users.export.completed` audit events |
| `apps/backend/internal/http/admin_export_handlers_test.go` | Add tests asserting paired audit rows |
| `apps/backend/internal/http/admin_read_handlers.go` | Extend `fetchUserAudit` to OR-match `params -> 'filters' -> 'ids' ? $userId` |
| `apps/backend/internal/http/admin_user_detail_test.go` | Add test asserting export-with-ids surfaces on the per-user audit |

---

## Phase 1: Backend — paired audit events on CSV export

The export endpoint currently writes zero audit rows. Tasks 1–3 add the paired `users.export.started` / `users.export.completed` events and extend the per-user audit query so exports targeting a user via `ids=` show up on that user's audit feed.

### Task 1: Write failing tests for export audit pairs

**Files:**
- Modify: `apps/backend/internal/http/admin_export_handlers_test.go`

- [ ] **Step 1: Add helper to count audit rows by action_type and exportId**

Add this to `admin_export_handlers_test.go` (top of file, after imports, beneath the existing `newExportHandlerServer` helper):

```go
func countAuditByAction(t *testing.T, pool *pgxpool.Pool, actionType string) int {
	t.Helper()
	var n int
	err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM audit_events WHERE action_type = $1`, actionType,
	).Scan(&n)
	if err != nil {
		t.Fatalf("count %s: %v", actionType, err)
	}
	return n
}

func fetchAuditRow(t *testing.T, pool *pgxpool.Pool, actionType string) struct {
	ResourceID    string
	TargetUserID  *string
	Params        []byte
	Result        string
	IPAddress     *string
	ErrorMessage  *string
} {
	t.Helper()
	var row struct {
		ResourceID    string
		TargetUserID  *string
		Params        []byte
		Result        string
		IPAddress     *string
		ErrorMessage  *string
	}
	err := pool.QueryRow(context.Background(),
		`SELECT COALESCE(resource_id,''), target_user_id::text, params,
		        result, ip_address::text, error_message
		 FROM audit_events WHERE action_type = $1
		 ORDER BY created_at DESC LIMIT 1`,
		actionType,
	).Scan(&row.ResourceID, &row.TargetUserID, &row.Params,
		&row.Result, &row.IPAddress, &row.ErrorMessage)
	if err != nil {
		t.Fatalf("fetch %s: %v", actionType, err)
	}
	return row
}
```

You also need `"github.com/jackc/pgx/v5/pgxpool"` and `"context"` already imported (they are).

- [ ] **Step 2: Add the happy-path audit assertions test**

Append to `admin_export_handlers_test.go`:

```go
func TestExportAdminUsersCSV_WritesPairedAuditEvents(t *testing.T) {
	h, s := newExportHandlerServer(t)
	seedUser(t, s.Pool(), "usr_a", "Alice", seedOpts{Discord: "alice", DiscordID: "d_alice"})

	// Pre-conditions: zero rows of each event type yet.
	if got := countAuditByAction(t, s.Pool(), "users.export.started"); got != 0 {
		t.Fatalf("expected 0 started rows pre-export, got %d", got)
	}
	if got := countAuditByAction(t, s.Pool(), "users.export.completed"); got != 0 {
		t.Fatalf("expected 0 completed rows pre-export, got %d", got)
	}

	req := httptest.NewRequest("GET", "/api/app/admin/users/export.csv?status=any", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	req.Header.Set("User-Agent", "test-agent")
	req = req.WithContext(appauth.InjectSessionForTest(req.Context(),
		&appauth.SessionUser{UserID: "usr_exporter", Roles: []string{"superadmin"}}))
	rr := httptest.NewRecorder()
	h.ExportAdminUsersCSV(rr, req)

	if rr.Code != 200 {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}

	if got := countAuditByAction(t, s.Pool(), "users.export.started"); got != 1 {
		t.Fatalf("expected 1 started row, got %d", got)
	}
	if got := countAuditByAction(t, s.Pool(), "users.export.completed"); got != 1 {
		t.Fatalf("expected 1 completed row, got %d", got)
	}

	started := fetchAuditRow(t, s.Pool(), "users.export.started")
	completed := fetchAuditRow(t, s.Pool(), "users.export.completed")

	if started.ResourceID == "" {
		t.Fatal("started.resource_id should be a non-empty exportId")
	}
	if started.ResourceID != completed.ResourceID {
		t.Errorf("export pair must share resource_id, got started=%q completed=%q",
			started.ResourceID, completed.ResourceID)
	}
	if started.Result != "success" {
		t.Errorf("started.result=%q want success", started.Result)
	}
	if completed.Result != "success" {
		t.Errorf("completed.result=%q want success", completed.Result)
	}

	var startedParams map[string]any
	if err := json.Unmarshal(started.Params, &startedParams); err != nil {
		t.Fatalf("unmarshal started params: %v", err)
	}
	if _, ok := startedParams["expectedRowCount"]; !ok {
		t.Errorf("started params missing expectedRowCount: %s", started.Params)
	}
	filters, _ := startedParams["filters"].(map[string]any)
	if filters == nil {
		t.Fatalf("started params missing filters: %s", started.Params)
	}
	if filters["status"] != "any" {
		t.Errorf("filters.status=%v want \"any\"", filters["status"])
	}

	var completedParams map[string]any
	if err := json.Unmarshal(completed.Params, &completedParams); err != nil {
		t.Fatalf("unmarshal completed params: %v", err)
	}
	if _, ok := completedParams["rowsWritten"]; !ok {
		t.Errorf("completed params missing rowsWritten: %s", completed.Params)
	}
	if _, ok := completedParams["durationMs"]; !ok {
		t.Errorf("completed params missing durationMs: %s", completed.Params)
	}
}
```

You need `"encoding/json"` imported (the existing file does not yet import it — add to imports).

- [ ] **Step 3: Add ids-filter target_user_id and JSONB-ids tests**

Append:

```go
func TestExportAdminUsersCSV_SingleIDsTargetsUser(t *testing.T) {
	h, s := newExportHandlerServer(t)
	seedUser(t, s.Pool(), "usr_a", "Alice", seedOpts{Discord: "alice", DiscordID: "d_alice"})

	req := httptest.NewRequest("GET", "/api/app/admin/users/export.csv?ids=usr_a", nil)
	req = req.WithContext(appauth.InjectSessionForTest(req.Context(),
		&appauth.SessionUser{UserID: "usr_exporter", Roles: []string{"superadmin"}}))
	rr := httptest.NewRecorder()
	h.ExportAdminUsersCSV(rr, req)
	if rr.Code != 200 {
		t.Fatalf("status=%d", rr.Code)
	}

	started := fetchAuditRow(t, s.Pool(), "users.export.started")
	if started.TargetUserID == nil || *started.TargetUserID != "usr_a" {
		t.Errorf("target_user_id=%v want usr_a", started.TargetUserID)
	}
}

func TestExportAdminUsersCSV_MultipleIDsKeepsTargetNullButRecordsIDs(t *testing.T) {
	h, s := newExportHandlerServer(t)
	seedUser(t, s.Pool(), "usr_a", "Alice", seedOpts{Discord: "alice", DiscordID: "d_alice"})
	seedUser(t, s.Pool(), "usr_b", "Bob", seedOpts{Discord: "bob", DiscordID: "d_bob"})

	req := httptest.NewRequest("GET", "/api/app/admin/users/export.csv?ids=usr_a,usr_b", nil)
	req = req.WithContext(appauth.InjectSessionForTest(req.Context(),
		&appauth.SessionUser{UserID: "usr_exporter", Roles: []string{"superadmin"}}))
	rr := httptest.NewRecorder()
	h.ExportAdminUsersCSV(rr, req)
	if rr.Code != 200 {
		t.Fatalf("status=%d", rr.Code)
	}

	started := fetchAuditRow(t, s.Pool(), "users.export.started")
	if started.TargetUserID != nil {
		t.Errorf("target_user_id=%v want nil for multi-id export", *started.TargetUserID)
	}
	var p map[string]any
	if err := json.Unmarshal(started.Params, &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	filters, _ := p["filters"].(map[string]any)
	ids, _ := filters["ids"].([]any)
	if len(ids) != 2 {
		t.Fatalf("filters.ids len=%d want 2: %v", len(ids), ids)
	}
}
```

- [ ] **Step 4: Run tests to verify they fail**

Run:

```
cd apps/backend && go test ./internal/http/ -run TestExportAdminUsersCSV_WritesPairedAuditEvents -v
```

Expected: FAIL with assertion `expected 1 started row, got 0`. Confirms the implementation is missing.

- [ ] **Step 5: Commit failing tests**

```
git add apps/backend/internal/http/admin_export_handlers_test.go
git commit -m "test(backend): export audit-pair assertions (failing)"
```

### Task 2: Implement paired audit events in export handler

**Files:**
- Modify: `apps/backend/internal/http/admin_export_handlers.go`

- [ ] **Step 1: Add imports and an `exportFilters` builder**

At the top of `admin_export_handlers.go`, ensure these imports exist:

```go
import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"rustlens/backend/internal/admin/audit"
	"rustlens/backend/internal/admin/rbac"
	"rustlens/backend/internal/appauth"
)
```

Add this helper at the bottom of the file:

```go
// extractExportFilters captures the filter querystring as a structured map for
// audit logging. Only includes fields that were actually supplied; this keeps
// audit rows readable and makes JSONB queries (e.g. params -> 'filters' ->
// 'ids' ? $userId) reliable.
func extractExportFilters(q url.Values) map[string]any {
	out := map[string]any{}
	if v := strings.TrimSpace(q.Get("q")); v != "" {
		out["q"] = v
	}
	if roles, ok := q["role"]; ok && len(roles) > 0 {
		out["role"] = roles
	}
	if v := q.Get("status"); v != "" {
		out["status"] = v
	}
	if v := q.Get("discord"); v != "" {
		out["discord"] = v
	}
	if v := q.Get("steam"); v != "" {
		out["steam"] = v
	}
	if v := q.Get("sort"); v != "" {
		out["sort"] = v
	}
	if rawIDs := strings.TrimSpace(q.Get("ids")); rawIDs != "" {
		parts := strings.Split(rawIDs, ",")
		cleaned := make([]string, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				cleaned = append(cleaned, p)
			}
		}
		out["ids"] = cleaned
	}
	return out
}
```

Add `"net/url"` to the import list as well (needed by `extractExportFilters`).

- [ ] **Step 2: Wire export handler to write the started event**

Replace the existing `ExportAdminUsersCSV` body. The new flow:

1. Auth + permission as before.
2. Compute `whereSQL`, `args`, `orderBy`, `total` as before.
3. **Before** writing CSV headers, build `exportID := uuid.NewString()` and write `users.export.started`.
4. Set Content-Type/Disposition headers.
5. Stream rows, tracking `rowsWritten`.
6. After streaming completes (success or error), write `users.export.completed` with the right `result`.

The full replacement function (replace lines 38–163 of `admin_export_handlers.go`):

```go
// ExportAdminUsersCSV streams a CSV snapshot of admin users matching the
// provided filters. Requires the users.export permission. Writes paired
// users.export.started + users.export.completed audit events so every export
// attempt is permanently logged with full filter context, even if streaming
// fails or the client disconnects mid-flight.
func (h *Handler) ExportAdminUsersCSV(w http.ResponseWriter, r *http.Request) {
	session := appauth.SessionFromContext(r.Context())
	if session == nil || session.UserID == "" {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), adminUsersExportTimeout)
	defer cancel()

	allowed, err := h.rbacResolver.HasPermission(ctx, session.UserID, rbac.PermUsersExport)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !allowed {
		writeError(w, http.StatusForbidden, "permission_denied")
		return
	}

	whereSQL, args, orderBy, err := buildUsersQuery(r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	pool := h.postgres.Pool()

	var total int
	countSQL := fmt.Sprintf(`SELECT COUNT(*) FROM app_users u %s`, whereSQL)
	if err := pool.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if total > adminUsersExportRowCap {
		writeError(w, http.StatusRequestEntityTooLarge,
			fmt.Sprintf("row count %d exceeds export cap %d", total, adminUsersExportRowCap))
		return
	}

	// Build the started audit event before any bytes leave the server. If
	// streaming fails halfway through, we still know an export was attempted.
	exportID := uuid.NewString()
	filters := extractExportFilters(r.URL.Query())
	var targetUserID *string
	if ids, ok := filters["ids"].([]string); ok && len(ids) == 1 {
		t := ids[0]
		targetUserID = &t
	}
	actorID := session.UserID
	ip := clientIPForAudit(r)
	ua := r.Header.Get("User-Agent")

	startedParams, _ := json.Marshal(map[string]any{
		"exportId":          exportID,
		"filters":           filters,
		"expectedRowCount":  total,
	})
	if err := h.auditStore.Record(ctx, audit.Entry{
		ActionType:        "users.export.started",
		ActorUserID:       &actorID,
		ActorRoleSnapshot: session.Roles,
		ResourceType:      "user_export",
		ResourceID:        exportID,
		TargetUserID:      targetUserID,
		Params:            startedParams,
		Result:            "success",
		IPAddress:         &ip,
		UserAgent:         &ua,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// completionResult is captured for the paired completion row written below.
	completionResult := "success"
	var completionErr *string
	rowsWritten := 0
	bytesWritten := 0
	startedAt := time.Now()

	// Defer the completion row so timeouts, panics during streaming, or
	// client disconnects still produce a paired event.
	defer func() {
		duration := time.Since(startedAt).Milliseconds()
		// Use a fresh context so we still write completion even if the request
		// context was cancelled or timed out.
		bgCtx, bgCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer bgCancel()
		completedParams, _ := json.Marshal(map[string]any{
			"exportId":     exportID,
			"rowsWritten":  rowsWritten,
			"bytesWritten": bytesWritten,
			"durationMs":   duration,
		})
		_ = h.auditStore.Record(bgCtx, audit.Entry{
			ActionType:        "users.export.completed",
			ActorUserID:       &actorID,
			ActorRoleSnapshot: session.Roles,
			ResourceType:      "user_export",
			ResourceID:        exportID,
			TargetUserID:      targetUserID,
			Params:            completedParams,
			Result:            completionResult,
			ErrorMessage:      completionErr,
			IPAddress:         &ip,
			UserAgent:         &ua,
		})
	}()

	// Streaming headers.
	filename := fmt.Sprintf("users-%s.csv", time.Now().UTC().Format("2006-01-02"))
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.Header().Set("Cache-Control", "no-store")

	listSQL := fmt.Sprintf(`
		SELECT u.id, u.display_name,
		       COALESCE(u.discord_id, '') AS discord_id,
		       COALESCE(u.discord_username, '') AS discord_username,
		       COALESCE(u.discord_global_name, '') AS discord_global_name,
		       CASE
		         WHEN u.deleted_at IS NOT NULL THEN 'deleted'
		         WHEN u.suspended_at IS NOT NULL THEN 'suspended'
		         ELSE 'active'
		       END AS status,
		       COALESCE(array_agg(DISTINCT r.key) FILTER (WHERE r.key IS NOT NULL), ARRAY[]::text[]) AS admin_roles,
		       COALESCE((SELECT COUNT(*) FROM user_guilds g WHERE g.discord_id = u.discord_id AND g.left_at IS NULL), 0) AS guild_count,
		       u.created_at,
		       u.last_login_at
		FROM app_users u
		LEFT JOIN admin_user_roles aur ON aur.user_id = u.id
		LEFT JOIN admin_roles r ON r.id = aur.role_id
		%s
		GROUP BY u.id
		ORDER BY %s
	`, whereSQL, orderBy)

	rows, err := pool.Query(ctx, listSQL, args...)
	if err != nil {
		completionResult = "error"
		msg := err.Error()
		completionErr = &msg
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	cw := &countingWriter{w: w}
	writer := csv.NewWriter(cw)
	flusher, _ := w.(http.Flusher)

	if err := writer.Write(exportRowColumns); err != nil {
		completionResult = "error"
		msg := err.Error()
		completionErr = &msg
		return
	}

	for rows.Next() {
		var (
			id, name, status                        string
			discordID, discordName, discordGlobal   string
			adminRoles                              []string
			guildCount                              int64
			createdAt, lastLoginAt                  time.Time
		)
		if err := rows.Scan(
			&id, &name, &discordID, &discordName, &discordGlobal, &status,
			&adminRoles, &guildCount, &createdAt, &lastLoginAt,
		); err != nil {
			completionResult = "error"
			msg := err.Error()
			completionErr = &msg
			return
		}
		record := []string{
			id,
			name,
			discordID,
			discordName,
			discordGlobal,
			status,
			strings.Join(adminRoles, "|"),
			strconv.FormatInt(guildCount, 10),
			createdAt.UTC().Format(time.RFC3339),
			lastLoginAt.UTC().Format(time.RFC3339),
		}
		if err := writer.Write(record); err != nil {
			completionResult = "error"
			msg := err.Error()
			completionErr = &msg
			return
		}
		rowsWritten++
		if rowsWritten%csvFlushInterval == 0 {
			writer.Flush()
			if flusher != nil {
				flusher.Flush()
			}
		}
	}
	writer.Flush()
	if flusher != nil {
		flusher.Flush()
	}
	if err := rows.Err(); err != nil {
		completionResult = "error"
		msg := err.Error()
		completionErr = &msg
		return
	}

	bytesWritten = cw.n
}

// countingWriter wraps http.ResponseWriter to count bytes written, used for
// the bytesWritten audit field. It only forwards Write — Header/Flush still go
// through the underlying ResponseWriter via the original handle.
type countingWriter struct {
	w http.ResponseWriter
	n int
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += n
	return n, err
}

// clientIPForAudit returns the request's remote IP suitable for audit
// logging. RealIP middleware sets r.RemoteAddr to a bare IP when an upstream
// proxy header is configured; otherwise it's "host:port". Fall back to the
// raw RemoteAddr value rather than failing the export.
func clientIPForAudit(r *http.Request) string {
	addr := r.RemoteAddr
	if i := strings.LastIndex(addr, ":"); i > 0 && strings.Count(addr, ":") == 1 {
		return addr[:i]
	}
	return addr
}
```

- [ ] **Step 2 (verify imports): Run `go build` to check imports**

Run:

```
cd apps/backend && go build ./internal/http/...
```

Expected: build succeeds. If it complains about unused imports, remove them; if it complains about missing imports, add them. Specifically `"net/url"` and `"github.com/google/uuid"` should be present.

- [ ] **Step 3: Run the audit tests to verify they now pass**

Run:

```
cd apps/backend && go test ./internal/http/ -run TestExportAdminUsersCSV -v
```

Expected: All `TestExportAdminUsersCSV_*` tests PASS, including the three new audit-related tests and the two existing ones (`HappyPath`, `IdsFilter`).

- [ ] **Step 4: Commit**

```
git add apps/backend/internal/http/admin_export_handlers.go apps/backend/internal/http/admin_export_handlers_test.go
git commit -m "feat(backend): write paired audit events on user CSV export"
```

### Task 3: Surface ids-export events on per-user audit feed

**Files:**
- Modify: `apps/backend/internal/http/admin_read_handlers.go`
- Modify: `apps/backend/internal/http/admin_user_detail_test.go`

- [ ] **Step 1: Write failing test for JSONB ids match on per-user audit**

Append to `admin_user_detail_test.go`:

```go
func TestUserAudit_SurfacesExportWithIDsFilter(t *testing.T) {
	h, s := newAdminDetailHandlerServer(t)
	seedUser(t, s.Pool(), "usr_target", "Target", seedOpts{Discord: "tgt", DiscordID: "d_tgt"})

	// Insert a users.export.started row whose target_user_id is NULL but
	// whose params.filters.ids contains usr_target. The per-user audit feed
	// should surface this row via JSONB containment.
	_, err := s.Pool().Exec(context.Background(), `
		INSERT INTO audit_events (action_type, actor_user_id, resource_type, resource_id, target_user_id, params, result)
		VALUES ('users.export.started', $1, 'user_export', 'export-1', NULL,
		        $2::jsonb, 'success')
	`, "usr_actor", `{"exportId":"export-1","filters":{"ids":["usr_target","usr_other"]}}`)
	if err != nil {
		t.Fatalf("seed export started: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/app/admin/users/usr_target/audit?limit=10", nil)
	req = req.WithContext(appauth.InjectSessionForTest(req.Context(),
		&appauth.SessionUser{UserID: "usr_caller", Roles: []string{"superadmin"}}))
	req = withRouteParam(req, "id", "usr_target")
	rr := httptest.NewRecorder()
	h.ListAdminUserAudit(rr, req)
	if rr.Code != 200 {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	found := false
	for _, it := range resp.Items {
		if it["eventType"] == "users.export.started" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected users.export.started in per-user audit, got %v", resp.Items)
	}
}
```

The helpers `newAdminDetailHandlerServer` and `withRouteParam` already exist in `admin_user_detail_test.go`; reuse them. Confirm with `grep withRouteParam admin_user_detail_test.go`. If they don't (file is older than this task suggests), search the rest of `internal/http/*_test.go` for them and import accordingly.

- [ ] **Step 2: Run the test to verify it fails**

Run:

```
cd apps/backend && go test ./internal/http/ -run TestUserAudit_SurfacesExportWithIDsFilter -v
```

Expected: FAIL — `expected users.export.started in per-user audit, got []`. The current `fetchUserAudit` only matches `target_user_id = $1`.

- [ ] **Step 3: Extend `fetchUserAudit` to OR-match `params.filters.ids`**

In `apps/backend/internal/http/admin_read_handlers.go`, modify the `fetchUserAudit` function. Replace the body with:

```go
func fetchUserAudit(ctx context.Context, pool *pgxpool.Pool, userID string, limit int, before string) []auditEventItem {
	args := []any{userID}
	cursorClause := ""
	if before != "" {
		if t, err := time.Parse(time.RFC3339, before); err == nil {
			args = append(args, t)
			cursorClause = fmt.Sprintf(" AND created_at < $%d", len(args))
		}
	}
	args = append(args, limit)
	limitIdx := len(args)

	hasLegacy, _ := tableExists(ctx, pool, "app_audit_events")

	// New audit_events: match by target_user_id OR by params.filters.ids
	// containing the userId. The latter surfaces export events that targeted
	// this user via ?ids=<list>.
	newQ := fmt.Sprintf(`
		SELECT id::text, COALESCE(actor_user_id::text,''), COALESCE(target_user_id::text,''),
		       action_type, COALESCE(params,'{}'::jsonb), created_at
		FROM audit_events
		WHERE (
			target_user_id::text = $1
			OR (params -> 'filters' -> 'ids') @> to_jsonb($1::text)
		)%s
	`, cursorClause)

	var q string
	if hasLegacy {
		q = fmt.Sprintf(`%s
			UNION ALL
			SELECT id::text, COALESCE(actor_user_id,''), COALESCE(target_user_id,''),
			       event_type, COALESCE(payload,'{}'::jsonb), created_at
			FROM app_audit_events
			WHERE target_user_id = $1%s
			ORDER BY created_at DESC
			LIMIT $%d
		`, newQ, cursorClause, limitIdx)
	} else {
		q = fmt.Sprintf(`%s
			ORDER BY created_at DESC
			LIMIT $%d
		`, newQ, limitIdx)
	}

	rows, err := pool.Query(ctx, q, args...)
	if err != nil {
		return []auditEventItem{}
	}
	defer rows.Close()
	out := []auditEventItem{}
	for rows.Next() {
		var it auditEventItem
		var payload []byte
		if err := rows.Scan(&it.ID, &it.ActorUserID, &it.TargetUserID, &it.EventType, &payload, &it.CreatedAt); err != nil {
			continue
		}
		it.Payload = map[string]any{}
		if len(payload) > 0 {
			_ = json.Unmarshal(payload, &it.Payload)
		}
		out = append(out, it)
	}
	return out
}
```

The only change is the `WHERE` clause: it now uses `OR (params -> 'filters' -> 'ids') @> to_jsonb($1::text)` to match exports that include this user via the `ids` filter array.

- [ ] **Step 4: Run the test to verify it passes**

Run:

```
cd apps/backend && go test ./internal/http/ -run TestUserAudit_SurfacesExportWithIDsFilter -v
```

Expected: PASS.

- [ ] **Step 5: Run the full http test suite to confirm no regressions**

Run:

```
cd apps/backend && go test ./internal/http/...
```

Expected: PASS across all admin tests.

- [ ] **Step 6: Commit**

```
git add apps/backend/internal/http/admin_read_handlers.go apps/backend/internal/http/admin_user_detail_test.go
git commit -m "feat(backend): surface ids-targeted exports on per-user audit feed"
```

---

## Phase 2: Frontend API library + server actions

### Task 4: Add typed API wrappers in `lib/rustlens-api.ts`

**Files:**
- Modify: `apps/frontend/lib/rustlens-api.ts`

- [ ] **Step 1: Add the wrappers at the end of the file**

Append after the last `deleteAdminRole` definition (around line 510):

```ts
// --- User actions (single) ---

export type AssignRoleResult = { userId: string; roleKey: string };

export async function assignAdminRole(
	userId: string,
	roleKey: string,
	reason: string,
): Promise<AssignRoleResult> {
	return postAdminAction<AssignRoleResult>("users.roles.grant", { userId, roleKey }, reason);
}

export async function revokeAdminRole(
	userId: string,
	roleKey: string,
	reason: string,
): Promise<AssignRoleResult> {
	return postAdminAction<AssignRoleResult>("users.roles.revoke", { userId, roleKey }, reason);
}

export type EditUserPatch = {
	displayName?: string;
	avatarUrl?: string;
};

export async function editAdminUser(
	userId: string,
	patch: EditUserPatch,
	reason: string,
) {
	return postAdminAction<{ userId: string }>("users.edit", { userId, ...patch }, reason);
}

export async function suspendAdminUser(userId: string, reason: string) {
	return postAdminAction<{ userId: string }>("users.suspend", { userId }, reason);
}

export async function unsuspendAdminUser(userId: string, reason: string) {
	return postAdminAction<{ userId: string }>("users.unsuspend", { userId }, reason);
}

export async function deleteAdminUser(
	userId: string,
	reason: string,
	typedConfirmation: string,
) {
	return postAdminAction<{ userId: string }>(
		"users.delete",
		{ userId, typedConfirmation },
		reason,
	);
}

export async function restoreAdminUser(userId: string, reason: string) {
	return postAdminAction<{ userId: string }>("users.restore", { userId }, reason);
}

export async function purgeAdminUser(
	userId: string,
	reason: string,
	typedConfirmation: string,
) {
	return postAdminAction<{ userId: string }>(
		"users.purge",
		{ userId, typedConfirmation },
		reason,
	);
}

// --- Bulk action ---

export type BulkUserSubAction = "suspend" | "unsuspend" | "roles.grant" | "roles.revoke";

export type BulkUserResult = {
	action: BulkUserSubAction;
	succeeded: number;
	failed: number;
	results: Array<{ userId: string; ok: boolean; error?: string }>;
};

export async function bulkAdminUserAction(
	action: BulkUserSubAction,
	userIds: string[],
	params: Record<string, unknown>,
	reason: string,
): Promise<BulkUserResult> {
	return postAdminAction<BulkUserResult>(
		"users.bulk",
		{ action, userIds, params },
		reason,
	);
}

// --- Export URL builder ---

/**
 * Builds the URL for the streaming CSV export. Browser hits this directly via
 * an anchor download — Next.js will inject the app secret via the same
 * proxy/server-component path used elsewhere if this is server-rendered, but
 * since the export endpoint is on the backend, the toolbar wraps this in a
 * server action that returns the URL after auth. See app/admin/users/_components/export-button.tsx.
 */
export function buildAdminUsersExportUrl(opts: {
	q?: string;
	role?: string[];
	status?: AdminUsersQuery["status"];
	sort?: string;
	ids?: string[];
}): string {
	const sp = new URLSearchParams();
	if (opts.q) sp.set("q", opts.q);
	for (const r of opts.role ?? []) sp.append("role", r);
	if (opts.status) sp.set("status", opts.status);
	if (opts.sort) sp.set("sort", opts.sort);
	if (opts.ids && opts.ids.length > 0) sp.set("ids", opts.ids.join(","));
	const qs = sp.toString();
	return `/api/app/admin/users/export.csv${qs ? `?${qs}` : ""}`;
}
```

- [ ] **Step 2: Run typecheck**

Run:

```
pnpm --filter frontend exec tsc --noEmit
```

Expected: no type errors.

- [ ] **Step 3: Commit**

```
git add apps/frontend/lib/rustlens-api.ts
git commit -m "feat(frontend): typed wrappers for user admin actions and export URL"
```

### Task 5: Add server actions in `app/admin/users/actions.ts`

**Files:**
- Create: `apps/frontend/app/admin/users/actions.ts`

- [ ] **Step 1: Write the actions file**

Create `apps/frontend/app/admin/users/actions.ts`:

```ts
"use server";

import { revalidatePath } from "next/cache";

import {
	assignAdminRole,
	bulkAdminUserAction,
	deleteAdminUser,
	editAdminUser,
	purgeAdminUser,
	restoreAdminUser,
	revokeAdminRole,
	suspendAdminUser,
	unsuspendAdminUser,
	type BulkUserResult,
	type BulkUserSubAction,
	type EditUserPatch,
} from "@/lib/rustlens-api";
import { requirePermission } from "@/lib/session";

const USERS_PATH = "/admin/users";

export type ActionResult<T = unknown> =
	| { ok: true; result: T }
	| { ok: false; error: string };

function userPath(userId: string) {
	return `${USERS_PATH}/${userId}`;
}

async function withReason<T>(
	reason: string,
	fn: () => Promise<T>,
): Promise<ActionResult<T>> {
	if (!reason.trim()) return { ok: false, error: "reason_required" };
	try {
		const result = await fn();
		return { ok: true, result };
	} catch (err) {
		return { ok: false, error: (err as Error).message };
	}
}

export async function assignRoleAction(
	userId: string,
	roleKey: string,
	reason: string,
): Promise<ActionResult> {
	await requirePermission("users.roles.assign");
	const res = await withReason(reason, () =>
		assignAdminRole(userId, roleKey, reason.trim()),
	);
	if (res.ok) {
		revalidatePath(USERS_PATH);
		revalidatePath(userPath(userId));
	}
	return res;
}

export async function revokeRoleAction(
	userId: string,
	roleKey: string,
	reason: string,
): Promise<ActionResult> {
	await requirePermission("users.roles.assign");
	const res = await withReason(reason, () =>
		revokeAdminRole(userId, roleKey, reason.trim()),
	);
	if (res.ok) {
		revalidatePath(USERS_PATH);
		revalidatePath(userPath(userId));
	}
	return res;
}

export async function editUserAction(
	userId: string,
	patch: EditUserPatch,
	reason: string,
): Promise<ActionResult> {
	await requirePermission("users.edit");
	// users.edit doesn't require a reason; pass empty string through.
	try {
		await editAdminUser(userId, patch, reason.trim());
		revalidatePath(USERS_PATH);
		revalidatePath(userPath(userId));
		return { ok: true, result: { userId } };
	} catch (err) {
		return { ok: false, error: (err as Error).message };
	}
}

export async function suspendUserAction(
	userId: string,
	reason: string,
): Promise<ActionResult> {
	await requirePermission("users.suspend");
	const res = await withReason(reason, () =>
		suspendAdminUser(userId, reason.trim()),
	);
	if (res.ok) {
		revalidatePath(USERS_PATH);
		revalidatePath(userPath(userId));
	}
	return res;
}

export async function unsuspendUserAction(
	userId: string,
	reason: string,
): Promise<ActionResult> {
	await requirePermission("users.suspend");
	// users.unsuspend doesn't require a reason on the backend.
	try {
		await unsuspendAdminUser(userId, reason.trim());
		revalidatePath(USERS_PATH);
		revalidatePath(userPath(userId));
		return { ok: true, result: { userId } };
	} catch (err) {
		return { ok: false, error: (err as Error).message };
	}
}

export async function deleteUserAction(
	userId: string,
	reason: string,
	typedConfirmation: string,
): Promise<ActionResult> {
	await requirePermission("users.delete");
	const res = await withReason(reason, () =>
		deleteAdminUser(userId, reason.trim(), typedConfirmation),
	);
	if (res.ok) {
		revalidatePath(USERS_PATH);
		revalidatePath(userPath(userId));
	}
	return res;
}

export async function restoreUserAction(
	userId: string,
	reason: string,
): Promise<ActionResult> {
	await requirePermission("users.delete");
	try {
		await restoreAdminUser(userId, reason.trim());
		revalidatePath(USERS_PATH);
		revalidatePath(userPath(userId));
		return { ok: true, result: { userId } };
	} catch (err) {
		return { ok: false, error: (err as Error).message };
	}
}

export async function purgeUserAction(
	userId: string,
	reason: string,
	typedConfirmation: string,
): Promise<ActionResult> {
	await requirePermission("admins.grant");
	const res = await withReason(reason, () =>
		purgeAdminUser(userId, reason.trim(), typedConfirmation),
	);
	if (res.ok) {
		revalidatePath(USERS_PATH);
	}
	return res;
}

export async function bulkUserAction(
	action: BulkUserSubAction,
	userIds: string[],
	params: Record<string, unknown>,
	reason: string,
): Promise<ActionResult<BulkUserResult>> {
	if (action === "suspend" || action === "roles.grant" || action === "roles.revoke") {
		// All three require a reason at the sub-action level.
		if (!reason.trim()) return { ok: false, error: "reason_required" };
	}
	if (action === "suspend" || action === "unsuspend") {
		await requirePermission("users.suspend");
	} else {
		await requirePermission("users.roles.assign");
	}
	if (userIds.length < 1 || userIds.length > 200) {
		return { ok: false, error: "userIds must be 1..200" };
	}
	try {
		const result = await bulkAdminUserAction(action, userIds, params, reason.trim());
		revalidatePath(USERS_PATH);
		return { ok: true, result };
	} catch (err) {
		return { ok: false, error: (err as Error).message };
	}
}
```

- [ ] **Step 2: Run typecheck**

Run:

```
pnpm --filter frontend exec tsc --noEmit
```

Expected: no type errors.

- [ ] **Step 3: Commit**

```
git add apps/frontend/app/admin/users/actions.ts
git commit -m "feat(frontend): server actions for /admin/users mutations"
```

---

## Phase 3: Detail page (build first because the list page links to it)

The detail page is built before the list page so the `<UserLink>` and row-click navigation in the list page lead somewhere real.

### Task 6: Detail page route + identity header

**Files:**
- Create: `apps/frontend/app/admin/users/[userId]/page.tsx`
- Create: `apps/frontend/app/admin/users/_components/identity-header.tsx`
- Create: `apps/frontend/app/admin/users/_components/user-detail-page.tsx`

- [ ] **Step 1: Write the server-component route**

Create `apps/frontend/app/admin/users/[userId]/page.tsx`:

```tsx
import { notFound } from "next/navigation";

import { AppSidebar } from "../../_components/app-sidebar";
import { UserDetailPage } from "../_components/user-detail-page";
import { SiteHeader } from "@/components/site-header";
import { SidebarInset, SidebarProvider } from "@/components/ui/sidebar";
import { getCsrfToken } from "@/lib/csrf";
import {
	getAdminRoles,
	getAdminUser,
	getAdminUserAudit,
	getCurrentUserSafe,
} from "@/lib/rustlens-api";
import { requirePermission } from "@/lib/session";

export default async function AdminUserDetailRoute({
	params,
}: {
	params: Promise<{ userId: string }>;
}) {
	const session = await requirePermission("users.read");
	const { userId } = await params;

	let userDetail;
	try {
		userDetail = await getAdminUser(userId);
	} catch {
		notFound();
	}

	const [csrfToken, currentUser, roles, audit] = await Promise.all([
		getCsrfToken(),
		getCurrentUserSafe(),
		getAdminRoles(),
		getAdminUserAudit(userId, { limit: 30 }),
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
							<UserDetailPage
								detail={userDetail}
								roles={roles}
								audit={audit.items}
								actorUserId={session.userId ?? ""}
								permissions={session.permissions ?? []}
							/>
						</div>
					</div>
				</div>
			</SidebarInset>
		</SidebarProvider>
	);
}
```

Note: confirm that `session.userId` exists on the SessionUser type. If it's named differently (e.g. `id`), adjust to match `apps/frontend/lib/session.ts`.

- [ ] **Step 2: Write the identity header component**

Create `apps/frontend/app/admin/users/_components/identity-header.tsx`:

```tsx
"use client";

import { CopyIcon, CheckIcon } from "lucide-react";
import * as React from "react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import type { AdminUserDetail } from "@/lib/rustlens-api";
import { cn } from "@/lib/utils";

type Props = { detail: AdminUserDetail };

function CopyButton({ value, label }: { value: string; label: string }) {
	const [copied, setCopied] = React.useState(false);
	return (
		<Button
			type="button"
			variant="ghost"
			size="sm"
			className="h-6 px-1.5 text-xs"
			onClick={async () => {
				await navigator.clipboard.writeText(value);
				setCopied(true);
				setTimeout(() => setCopied(false), 1500);
			}}
		>
			{copied ? <CheckIcon className="size-3" /> : <CopyIcon className="size-3" />}
			<span className="sr-only">Copy {label}</span>
		</Button>
	);
}

export function IdentityHeader({ detail }: Props) {
	const { user, discordId, discordUsername, discordGlobalName, discordAvatar, suspendedAt, deletedAt, suspendReason } = detail;
	return (
		<div className="flex flex-col gap-3 rounded-lg border bg-card p-4">
			<div className="flex items-start gap-4">
				{user.avatarUrl || discordAvatar ? (
					// eslint-disable-next-line @next/next/no-img-element
					<img
						src={user.avatarUrl || discordAvatar}
						alt=""
						className="size-16 rounded-full object-cover"
					/>
				) : (
					<div className="size-16 rounded-full bg-muted" aria-hidden />
				)}
				<div className="flex flex-1 flex-col gap-1">
					<div className="flex flex-wrap items-center gap-2">
						<h1 className="text-2xl font-semibold">{user.displayName}</h1>
						{deletedAt ? (
							<Badge variant="destructive">Deleted</Badge>
						) : suspendedAt ? (
							<Badge variant="outline" className="border-amber-600 text-amber-700">
								Suspended
							</Badge>
						) : (
							<Badge variant="secondary">Active</Badge>
						)}
					</div>
					<div className="flex flex-wrap items-center gap-2 text-sm text-muted-foreground">
						<span>@{discordUsername || "—"}</span>
						{discordGlobalName ? <span>· {discordGlobalName}</span> : null}
					</div>
					<div className="mt-2 grid grid-cols-1 gap-1 text-xs sm:grid-cols-2">
						<div className="flex items-center gap-1">
							<span className="text-muted-foreground">User ID</span>
							<code className={cn("rounded bg-muted px-1 py-0.5")}>{user.id}</code>
							<CopyButton value={user.id} label="user id" />
						</div>
						<div className="flex items-center gap-1">
							<span className="text-muted-foreground">Discord ID</span>
							<code className="rounded bg-muted px-1 py-0.5">{discordId || "—"}</code>
							{discordId ? <CopyButton value={discordId} label="discord id" /> : null}
						</div>
					</div>
					{suspendedAt && suspendReason ? (
						<div className="mt-2 rounded border border-amber-300 bg-amber-50 px-2 py-1 text-xs text-amber-900">
							<strong>Suspended:</strong> {suspendReason}
						</div>
					) : null}
					<div className="mt-2 grid grid-cols-2 gap-2 text-xs text-muted-foreground">
						<div>Joined {new Date(user.createdAt).toLocaleDateString()}</div>
						<div>Last login {new Date(user.lastLoginAt).toLocaleDateString()}</div>
					</div>
				</div>
			</div>
		</div>
	);
}
```

- [ ] **Step 3: Write a stub `user-detail-page.tsx` that renders just the identity header**

Create `apps/frontend/app/admin/users/_components/user-detail-page.tsx`:

```tsx
"use client";

import type { AdminRoleItem, AdminUserAuditItem, AdminUserDetail } from "@/lib/rustlens-api";

import { IdentityHeader } from "./identity-header";

type Props = {
	detail: AdminUserDetail;
	roles: AdminRoleItem[];
	audit: AdminUserAuditItem[];
	actorUserId: string;
	permissions: string[];
};

export function UserDetailPage({ detail }: Props) {
	return (
		<div className="flex flex-col gap-6">
			<IdentityHeader detail={detail} />
			{/* Roles, profile, audit, danger zone added in later tasks */}
		</div>
	);
}
```

- [ ] **Step 4: Verify the route renders**

Run the dev server (`make dev`) and navigate to `/admin/users/<a-real-user-id>`. Expected: identity header appears with avatar, name, status badge, IDs.

- [ ] **Step 5: Commit**

```
git add apps/frontend/app/admin/users/[userId]/page.tsx apps/frontend/app/admin/users/_components/identity-header.tsx apps/frontend/app/admin/users/_components/user-detail-page.tsx
git commit -m "feat(frontend): admin user detail route + identity header"
```

### Task 7: Roles section + grant/revoke dialogs

**Files:**
- Create: `apps/frontend/app/admin/users/_components/grant-role-dialog.tsx`
- Create: `apps/frontend/app/admin/users/_components/revoke-role-dialog.tsx`
- Create: `apps/frontend/app/admin/users/_components/roles-section.tsx`
- Create: `apps/frontend/app/admin/users/_components/__tests__/grant-role-dialog.test.tsx`
- Modify: `apps/frontend/app/admin/users/_components/user-detail-page.tsx`

- [ ] **Step 1: Write the failing test for `grant-role-dialog`**

Create `apps/frontend/app/admin/users/_components/__tests__/grant-role-dialog.test.tsx`:

```tsx
import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent, waitFor, act } from "@testing-library/react";
import { GrantRoleDialog } from "../grant-role-dialog";

const roles = [
	{ id: "r1", key: "moderator", name: "Moderator", description: "Mods", isSystem: false, permissions: [], memberCount: 0 },
	{ id: "r2", key: "superadmin", name: "Superadmin", description: "All", isSystem: true, permissions: [], memberCount: 0 },
];

describe("GrantRoleDialog", () => {
	it("calls onConfirm with userId, roleKey, and reason", async () => {
		const onConfirm = vi.fn().mockResolvedValue(undefined);
		render(
			<GrantRoleDialog
				open
				onOpenChange={() => {}}
				userId="u1"
				assignableRoles={roles.filter((r) => r.key !== "superadmin")}
				canGrantSuperadmin={false}
				onConfirm={onConfirm}
			/>,
		);

		// Pick the moderator role from the picker.
		fireEvent.click(screen.getByRole("combobox"));
		fireEvent.click(await screen.findByRole("option", { name: /moderator/i }));

		// Type a reason.
		const reason = await screen.findByPlaceholderText(/reason/i);
		fireEvent.change(reason, { target: { value: "promotion" } });

		await act(async () => {
			fireEvent.click(screen.getByRole("button", { name: /grant role/i }));
		});

		await waitFor(() => {
			expect(onConfirm).toHaveBeenCalledWith("u1", "moderator", "promotion");
		});
	});
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run:

```
pnpm --filter frontend test -- grant-role-dialog
```

Expected: FAIL with "cannot find module" or component not defined.

- [ ] **Step 3: Implement `grant-role-dialog.tsx`**

Create `apps/frontend/app/admin/users/_components/grant-role-dialog.tsx`:

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
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
	Select,
	SelectContent,
	SelectItem,
	SelectTrigger,
	SelectValue,
} from "@/components/ui/select";
import type { AdminRoleItem } from "@/lib/rustlens-api";

type Props = {
	open: boolean;
	onOpenChange: (open: boolean) => void;
	userId: string;
	assignableRoles: AdminRoleItem[];
	canGrantSuperadmin: boolean;
	onConfirm: (userId: string, roleKey: string, reason: string) => Promise<void>;
};

export function GrantRoleDialog({
	open,
	onOpenChange,
	userId,
	assignableRoles,
	canGrantSuperadmin,
	onConfirm,
}: Props) {
	const [roleKey, setRoleKey] = React.useState<string>("");
	const [reason, setReason] = React.useState("");
	const [submitting, setSubmitting] = React.useState(false);
	const [error, setError] = React.useState<string | null>(null);

	React.useEffect(() => {
		if (!open) {
			setRoleKey("");
			setReason("");
			setError(null);
			setSubmitting(false);
		}
	}, [open]);

	const submit = async () => {
		if (!roleKey || !reason.trim()) return;
		setSubmitting(true);
		setError(null);
		try {
			await onConfirm(userId, roleKey, reason.trim());
			onOpenChange(false);
		} catch (err) {
			setError((err as Error).message);
		} finally {
			setSubmitting(false);
		}
	};

	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent>
				<DialogHeader>
					<DialogTitle>Grant role</DialogTitle>
					<DialogDescription>
						Assign an admin role. Granting <code>superadmin</code> requires admins.grant.
					</DialogDescription>
				</DialogHeader>

				<div className="flex flex-col gap-3">
					<div className="flex flex-col gap-1">
						<Label htmlFor="role">Role</Label>
						<Select value={roleKey} onValueChange={setRoleKey}>
							<SelectTrigger id="role">
								<SelectValue placeholder="Pick a role" />
							</SelectTrigger>
							<SelectContent>
								{assignableRoles.map((r) => {
									const disabled = r.key === "superadmin" && !canGrantSuperadmin;
									return (
										<SelectItem key={r.key} value={r.key} disabled={disabled}>
											<div className="flex flex-col">
												<span>{r.name}</span>
												{r.description ? (
													<span className="text-xs text-muted-foreground">
														{r.description}
													</span>
												) : null}
												{disabled ? (
													<span className="text-xs text-amber-600">
														Requires admins.grant
													</span>
												) : null}
											</div>
										</SelectItem>
									);
								})}
							</SelectContent>
						</Select>
					</div>

					<div className="flex flex-col gap-1">
						<Label htmlFor="reason">Reason</Label>
						<Input
							id="reason"
							placeholder="Reason (required, audited)"
							value={reason}
							onChange={(e) => setReason(e.target.value)}
						/>
					</div>

					{error ? <div className="text-sm text-destructive">{error}</div> : null}
				</div>

				<DialogFooter>
					<Button variant="ghost" onClick={() => onOpenChange(false)} disabled={submitting}>
						Cancel
					</Button>
					<Button
						onClick={submit}
						disabled={submitting || !roleKey || !reason.trim()}
					>
						Grant role
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run:

```
pnpm --filter frontend test -- grant-role-dialog
```

Expected: PASS.

- [ ] **Step 5: Implement `revoke-role-dialog.tsx`**

Create `apps/frontend/app/admin/users/_components/revoke-role-dialog.tsx`:

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
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

type Props = {
	open: boolean;
	onOpenChange: (open: boolean) => void;
	userId: string;
	roleKey: string;
	roleName: string;
	onConfirm: (userId: string, roleKey: string, reason: string) => Promise<void>;
};

export function RevokeRoleDialog({
	open,
	onOpenChange,
	userId,
	roleKey,
	roleName,
	onConfirm,
}: Props) {
	const [reason, setReason] = React.useState("");
	const [submitting, setSubmitting] = React.useState(false);
	const [error, setError] = React.useState<string | null>(null);

	React.useEffect(() => {
		if (!open) {
			setReason("");
			setError(null);
			setSubmitting(false);
		}
	}, [open]);

	const submit = async () => {
		if (!reason.trim()) return;
		setSubmitting(true);
		setError(null);
		try {
			await onConfirm(userId, roleKey, reason.trim());
			onOpenChange(false);
		} catch (err) {
			setError((err as Error).message);
		} finally {
			setSubmitting(false);
		}
	};

	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent>
				<DialogHeader>
					<DialogTitle>Revoke {roleName}</DialogTitle>
					<DialogDescription>
						This removes the <code>{roleKey}</code> role from this user. The action is audited.
					</DialogDescription>
				</DialogHeader>
				<div className="flex flex-col gap-1">
					<Label htmlFor="revoke-reason">Reason</Label>
					<Input
						id="revoke-reason"
						placeholder="Reason (required, audited)"
						value={reason}
						onChange={(e) => setReason(e.target.value)}
					/>
				</div>
				{error ? <div className="text-sm text-destructive">{error}</div> : null}
				<DialogFooter>
					<Button variant="ghost" onClick={() => onOpenChange(false)} disabled={submitting}>
						Cancel
					</Button>
					<Button
						variant="destructive"
						onClick={submit}
						disabled={submitting || !reason.trim()}
					>
						Revoke role
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
```

- [ ] **Step 6: Implement `roles-section.tsx`**

Create `apps/frontend/app/admin/users/_components/roles-section.tsx`:

```tsx
"use client";

import { useRouter } from "next/navigation";
import { PlusIcon, XIcon } from "lucide-react";
import * as React from "react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
	Tooltip,
	TooltipContent,
	TooltipProvider,
	TooltipTrigger,
} from "@/components/ui/tooltip";
import type { AdminRoleItem, AdminUserDetail } from "@/lib/rustlens-api";

import { assignRoleAction, revokeRoleAction } from "../actions";
import { GrantRoleDialog } from "./grant-role-dialog";
import { RevokeRoleDialog } from "./revoke-role-dialog";

type Props = {
	detail: AdminUserDetail;
	roles: AdminRoleItem[];
	actorUserId: string;
	canAssign: boolean;
	canGrantSuperadmin: boolean;
};

export function RolesSection({
	detail,
	roles,
	actorUserId,
	canAssign,
	canGrantSuperadmin,
}: Props) {
	const router = useRouter();
	const [grantOpen, setGrantOpen] = React.useState(false);
	const [revoking, setRevoking] = React.useState<{ key: string; name: string } | null>(null);

	if (!canAssign) return null;

	const isSelf = detail.user.id === actorUserId;
	const currentRoles = detail.adminRoles;
	const assignableRoles = roles.filter((r) => !currentRoles.includes(r.key));

	const handleGrant = async (userId: string, roleKey: string, reason: string) => {
		const res = await assignRoleAction(userId, roleKey, reason);
		if (!res.ok) throw new Error(res.error);
		router.refresh();
	};

	const handleRevoke = async (userId: string, roleKey: string, reason: string) => {
		const res = await revokeRoleAction(userId, roleKey, reason);
		if (!res.ok) throw new Error(res.error);
		router.refresh();
	};

	return (
		<section className="flex flex-col gap-3 rounded-lg border bg-card p-4">
			<div>
				<h2 className="text-lg font-semibold">Roles</h2>
				<p className="text-sm text-muted-foreground">
					Admin roles control which actions this user can perform.
				</p>
			</div>

			<TooltipProvider>
				<div className="flex flex-wrap gap-2">
					{currentRoles.length === 0 ? (
						<span className="text-sm text-muted-foreground">No admin roles assigned.</span>
					) : (
						currentRoles.map((roleKey) => {
							const role = roles.find((r) => r.key === roleKey);
							const name = role?.name ?? roleKey;
							const description = role?.description ?? null;
							const isSuperadmin = roleKey === "superadmin";
							return (
								<Tooltip key={roleKey}>
									<TooltipTrigger asChild>
										<Badge
											variant={isSuperadmin ? "destructive" : "secondary"}
											className="gap-1"
										>
											{name}
											{isSelf ? (
												<Tooltip>
													<TooltipTrigger asChild>
														<XIcon className="size-3 cursor-not-allowed opacity-40" />
													</TooltipTrigger>
													<TooltipContent>
														You can&apos;t revoke your own admin roles.
													</TooltipContent>
												</Tooltip>
											) : (
												<button
													type="button"
													onClick={() => setRevoking({ key: roleKey, name })}
													className="rounded hover:bg-foreground/10"
													aria-label={`Revoke ${name}`}
												>
													<XIcon className="size-3" />
												</button>
											)}
										</Badge>
									</TooltipTrigger>
									{description ? <TooltipContent>{description}</TooltipContent> : null}
								</Tooltip>
							);
						})
					)}
				</div>
			</TooltipProvider>

			<div>
				<Button
					variant="outline"
					size="sm"
					onClick={() => setGrantOpen(true)}
					disabled={assignableRoles.length === 0}
				>
					<PlusIcon className="mr-2 size-4" />
					Add role…
				</Button>
			</div>

			<GrantRoleDialog
				open={grantOpen}
				onOpenChange={setGrantOpen}
				userId={detail.user.id}
				assignableRoles={assignableRoles}
				canGrantSuperadmin={canGrantSuperadmin}
				onConfirm={handleGrant}
			/>
			<RevokeRoleDialog
				open={revoking !== null}
				onOpenChange={(open) => {
					if (!open) setRevoking(null);
				}}
				userId={detail.user.id}
				roleKey={revoking?.key ?? ""}
				roleName={revoking?.name ?? ""}
				onConfirm={handleRevoke}
			/>
		</section>
	);
}
```

- [ ] **Step 7: Wire `RolesSection` into `user-detail-page.tsx`**

Replace `apps/frontend/app/admin/users/_components/user-detail-page.tsx`:

```tsx
"use client";

import type { AdminRoleItem, AdminUserAuditItem, AdminUserDetail } from "@/lib/rustlens-api";

import { IdentityHeader } from "./identity-header";
import { RolesSection } from "./roles-section";

type Props = {
	detail: AdminUserDetail;
	roles: AdminRoleItem[];
	audit: AdminUserAuditItem[];
	actorUserId: string;
	permissions: string[];
};

export function UserDetailPage({ detail, roles, actorUserId, permissions }: Props) {
	const canAssign = permissions.includes("users.roles.assign");
	const canGrantSuperadmin = permissions.includes("admins.grant");
	return (
		<div className="flex flex-col gap-6">
			<IdentityHeader detail={detail} />
			<RolesSection
				detail={detail}
				roles={roles}
				actorUserId={actorUserId}
				canAssign={canAssign}
				canGrantSuperadmin={canGrantSuperadmin}
			/>
			{/* Profile, audit, danger zone added in later tasks */}
		</div>
	);
}
```

- [ ] **Step 8: Verify in-browser**

Run `make dev`, sign in as a superadmin, navigate to `/admin/users/<id>`. Expected: roles section visible with current roles + "Add role…" button. Granting and revoking work end-to-end (check the audit table afterwards).

- [ ] **Step 9: Commit**

```
git add apps/frontend/app/admin/users/_components/grant-role-dialog.tsx apps/frontend/app/admin/users/_components/revoke-role-dialog.tsx apps/frontend/app/admin/users/_components/roles-section.tsx apps/frontend/app/admin/users/_components/__tests__/grant-role-dialog.test.tsx apps/frontend/app/admin/users/_components/user-detail-page.tsx
git commit -m "feat(frontend): roles section with grant/revoke dialogs"
```

### Task 8: Profile section (read + edit display name / avatar)

**Files:**
- Create: `apps/frontend/app/admin/users/_components/profile-section.tsx`
- Modify: `apps/frontend/app/admin/users/_components/user-detail-page.tsx`

- [ ] **Step 1: Implement `profile-section.tsx`**

Create `apps/frontend/app/admin/users/_components/profile-section.tsx`:

```tsx
"use client";

import { useRouter } from "next/navigation";
import * as React from "react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import type { AdminUserDetail } from "@/lib/rustlens-api";

import { editUserAction } from "../actions";

type Props = {
	detail: AdminUserDetail;
	canEdit: boolean;
};

export function ProfileSection({ detail, canEdit }: Props) {
	const router = useRouter();
	const [editing, setEditing] = React.useState(false);
	const [displayName, setDisplayName] = React.useState(detail.user.displayName);
	const [avatarUrl, setAvatarUrl] = React.useState(detail.user.avatarUrl ?? "");
	const [submitting, setSubmitting] = React.useState(false);
	const [error, setError] = React.useState<string | null>(null);

	const cancel = () => {
		setDisplayName(detail.user.displayName);
		setAvatarUrl(detail.user.avatarUrl ?? "");
		setEditing(false);
		setError(null);
	};

	const save = async () => {
		setSubmitting(true);
		setError(null);
		try {
			const res = await editUserAction(
				detail.user.id,
				{ displayName, avatarUrl },
				"",
			);
			if (!res.ok) throw new Error(res.error);
			router.refresh();
			setEditing(false);
		} catch (err) {
			setError((err as Error).message);
		} finally {
			setSubmitting(false);
		}
	};

	return (
		<section className="flex flex-col gap-3 rounded-lg border bg-card p-4">
			<div className="flex items-center justify-between">
				<div>
					<h2 className="text-lg font-semibold">Profile</h2>
					<p className="text-sm text-muted-foreground">
						Display name and avatar shown across RustLens.
					</p>
				</div>
				{canEdit && !editing ? (
					<Button variant="outline" size="sm" onClick={() => setEditing(true)}>
						Edit profile
					</Button>
				) : null}
			</div>

			<dl className="grid grid-cols-1 gap-3 sm:grid-cols-2">
				<div className="flex flex-col gap-1">
					<Label htmlFor="display-name">Display name</Label>
					{editing ? (
						<Input
							id="display-name"
							value={displayName}
							onChange={(e) => setDisplayName(e.target.value)}
						/>
					) : (
						<dd className="text-sm">{detail.user.displayName}</dd>
					)}
				</div>
				<div className="flex flex-col gap-1">
					<Label htmlFor="avatar-url">Avatar URL</Label>
					{editing ? (
						<Input
							id="avatar-url"
							value={avatarUrl}
							onChange={(e) => setAvatarUrl(e.target.value)}
						/>
					) : (
						<dd className="truncate text-sm text-muted-foreground">
							{detail.user.avatarUrl || "—"}
						</dd>
					)}
				</div>
			</dl>

			{error ? <div className="text-sm text-destructive">{error}</div> : null}

			{editing ? (
				<div className="flex justify-end gap-2">
					<Button variant="ghost" onClick={cancel} disabled={submitting}>
						Cancel
					</Button>
					<Button onClick={save} disabled={submitting}>
						Save
					</Button>
				</div>
			) : null}
		</section>
	);
}
```

- [ ] **Step 2: Wire into the detail page**

Update `user-detail-page.tsx` to render `<ProfileSection>` after `<RolesSection>`:

```tsx
"use client";

import type { AdminRoleItem, AdminUserAuditItem, AdminUserDetail } from "@/lib/rustlens-api";

import { IdentityHeader } from "./identity-header";
import { ProfileSection } from "./profile-section";
import { RolesSection } from "./roles-section";

type Props = {
	detail: AdminUserDetail;
	roles: AdminRoleItem[];
	audit: AdminUserAuditItem[];
	actorUserId: string;
	permissions: string[];
};

export function UserDetailPage({ detail, roles, actorUserId, permissions }: Props) {
	const canAssign = permissions.includes("users.roles.assign");
	const canGrantSuperadmin = permissions.includes("admins.grant");
	const canEdit = permissions.includes("users.edit");
	return (
		<div className="flex flex-col gap-6">
			<IdentityHeader detail={detail} />
			<RolesSection
				detail={detail}
				roles={roles}
				actorUserId={actorUserId}
				canAssign={canAssign}
				canGrantSuperadmin={canGrantSuperadmin}
			/>
			<ProfileSection detail={detail} canEdit={canEdit} />
			{/* Audit, danger zone added in later tasks */}
		</div>
	);
}
```

- [ ] **Step 3: Verify in-browser**

Reload the detail page. Expected: profile section appears below roles. Edit + Save updates the display name; the page refreshes to show the new value.

- [ ] **Step 4: Commit**

```
git add apps/frontend/app/admin/users/_components/profile-section.tsx apps/frontend/app/admin/users/_components/user-detail-page.tsx
git commit -m "feat(frontend): profile section with edit mode"
```

### Task 9: Audit section

**Files:**
- Create: `apps/frontend/app/admin/users/_components/audit-section.tsx`
- Modify: `apps/frontend/app/admin/users/_components/user-detail-page.tsx`

- [ ] **Step 1: Implement `audit-section.tsx`**

Create `apps/frontend/app/admin/users/_components/audit-section.tsx`:

```tsx
"use client";

import Link from "next/link";

import { Badge } from "@/components/ui/badge";
import { UserLink } from "@/components/admin/user-link";
import { RelativeTime } from "@/components/admin/relative-time";
import type { AdminUserAuditItem } from "@/lib/rustlens-api";

type Props = {
	userId: string;
	items: AdminUserAuditItem[];
};

export function AuditSection({ userId, items }: Props) {
	return (
		<section className="flex flex-col gap-3 rounded-lg border bg-card p-4">
			<div className="flex items-center justify-between">
				<div>
					<h2 className="text-lg font-semibold">Recent audit</h2>
					<p className="text-sm text-muted-foreground">
						Most recent admin actions involving this user.
					</p>
				</div>
				<Link
					href={`/admin/audit?targetUserId=${encodeURIComponent(userId)}`}
					className="text-sm underline"
				>
					View full history →
				</Link>
			</div>

			{items.length === 0 ? (
				<p className="text-sm text-muted-foreground">No audit events.</p>
			) : (
				<ul className="flex flex-col divide-y">
					{items.map((it) => {
						const result = it.result ?? "success";
						return (
							<li key={it.id} className="grid grid-cols-12 items-center gap-2 py-2 text-sm">
								<div className="col-span-3">
									<UserLink
										userId={it.actorUserId}
										displayName={it.actorDisplayName}
										avatarUrl={it.actorAvatarUrl}
									/>
								</div>
								<code className="col-span-3 truncate rounded bg-muted px-1 py-0.5 text-xs">
									{it.eventType}
								</code>
								<div className="col-span-1">
									<Badge variant={result === "error" ? "destructive" : "secondary"}>
										{result}
									</Badge>
								</div>
								<div className="col-span-3 truncate text-xs text-muted-foreground">
									{it.reason ?? ""}
								</div>
								<div className="col-span-2 text-right text-xs text-muted-foreground">
									<RelativeTime iso={it.createdAt} />
								</div>
							</li>
						);
					})}
				</ul>
			)}
		</section>
	);
}
```

- [ ] **Step 2: Wire into the detail page**

Update `user-detail-page.tsx` to render `<AuditSection>` after `<ProfileSection>`:

```tsx
"use client";

import type { AdminRoleItem, AdminUserAuditItem, AdminUserDetail } from "@/lib/rustlens-api";

import { AuditSection } from "./audit-section";
import { IdentityHeader } from "./identity-header";
import { ProfileSection } from "./profile-section";
import { RolesSection } from "./roles-section";

type Props = {
	detail: AdminUserDetail;
	roles: AdminRoleItem[];
	audit: AdminUserAuditItem[];
	actorUserId: string;
	permissions: string[];
};

export function UserDetailPage({ detail, roles, audit, actorUserId, permissions }: Props) {
	const canAssign = permissions.includes("users.roles.assign");
	const canGrantSuperadmin = permissions.includes("admins.grant");
	const canEdit = permissions.includes("users.edit");
	return (
		<div className="flex flex-col gap-6">
			<IdentityHeader detail={detail} />
			<RolesSection
				detail={detail}
				roles={roles}
				actorUserId={actorUserId}
				canAssign={canAssign}
				canGrantSuperadmin={canGrantSuperadmin}
			/>
			<ProfileSection detail={detail} canEdit={canEdit} />
			<AuditSection userId={detail.user.id} items={audit} />
			{/* Danger zone in next task */}
		</div>
	);
}
```

- [ ] **Step 3: Commit**

```
git add apps/frontend/app/admin/users/_components/audit-section.tsx apps/frontend/app/admin/users/_components/user-detail-page.tsx
git commit -m "feat(frontend): per-user audit section"
```

### Task 10: Danger zone (suspend / unsuspend / delete / restore / purge)

**Files:**
- Create: `apps/frontend/app/admin/users/_components/suspend-dialog.tsx`
- Create: `apps/frontend/app/admin/users/_components/delete-dialog.tsx`
- Create: `apps/frontend/app/admin/users/_components/restore-dialog.tsx`
- Create: `apps/frontend/app/admin/users/_components/purge-dialog.tsx`
- Create: `apps/frontend/app/admin/users/_components/danger-zone.tsx`
- Modify: `apps/frontend/app/admin/users/_components/user-detail-page.tsx`

- [ ] **Step 1: Implement `suspend-dialog.tsx`**

Create `apps/frontend/app/admin/users/_components/suspend-dialog.tsx`:

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
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

type Props = {
	open: boolean;
	onOpenChange: (open: boolean) => void;
	mode: "suspend" | "unsuspend";
	userId: string;
	displayName: string;
	onConfirm: (userId: string, reason: string) => Promise<void>;
};

export function SuspendDialog({ open, onOpenChange, mode, userId, displayName, onConfirm }: Props) {
	const [reason, setReason] = React.useState("");
	const [submitting, setSubmitting] = React.useState(false);
	const [error, setError] = React.useState<string | null>(null);

	React.useEffect(() => {
		if (!open) {
			setReason("");
			setError(null);
			setSubmitting(false);
		}
	}, [open]);

	const reasonRequired = mode === "suspend";
	const submit = async () => {
		if (reasonRequired && !reason.trim()) return;
		setSubmitting(true);
		try {
			await onConfirm(userId, reason.trim());
			onOpenChange(false);
		} catch (err) {
			setError((err as Error).message);
		} finally {
			setSubmitting(false);
		}
	};

	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent>
				<DialogHeader>
					<DialogTitle>
						{mode === "suspend" ? `Suspend ${displayName}` : `Unsuspend ${displayName}`}
					</DialogTitle>
					<DialogDescription>
						{mode === "suspend"
							? "The user will be locked out of authenticated routes until unsuspended."
							: "The user regains access immediately."}
					</DialogDescription>
				</DialogHeader>
				<div className="flex flex-col gap-1">
					<Label htmlFor="suspend-reason">Reason{reasonRequired ? "" : " (optional)"}</Label>
					<Input
						id="suspend-reason"
						placeholder="Reason (audited)"
						value={reason}
						onChange={(e) => setReason(e.target.value)}
					/>
				</div>
				{error ? <div className="text-sm text-destructive">{error}</div> : null}
				<DialogFooter>
					<Button variant="ghost" onClick={() => onOpenChange(false)} disabled={submitting}>
						Cancel
					</Button>
					<Button
						variant={mode === "suspend" ? "destructive" : "default"}
						onClick={submit}
						disabled={submitting || (reasonRequired && !reason.trim())}
					>
						{mode === "suspend" ? "Suspend" : "Unsuspend"}
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
```

- [ ] **Step 2: Implement `delete-dialog.tsx`**

Create `apps/frontend/app/admin/users/_components/delete-dialog.tsx`:

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
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

type Props = {
	open: boolean;
	onOpenChange: (open: boolean) => void;
	userId: string;
	displayName: string;
	onConfirm: (userId: string, reason: string, typed: string) => Promise<void>;
};

export function DeleteDialog({ open, onOpenChange, userId, displayName, onConfirm }: Props) {
	const [reason, setReason] = React.useState("");
	const [typed, setTyped] = React.useState("");
	const [submitting, setSubmitting] = React.useState(false);
	const [error, setError] = React.useState<string | null>(null);

	React.useEffect(() => {
		if (!open) {
			setReason("");
			setTyped("");
			setError(null);
			setSubmitting(false);
		}
	}, [open]);

	const submit = async () => {
		if (!reason.trim() || typed !== displayName) return;
		setSubmitting(true);
		try {
			await onConfirm(userId, reason.trim(), typed);
			onOpenChange(false);
		} catch (err) {
			setError((err as Error).message);
		} finally {
			setSubmitting(false);
		}
	};

	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent>
				<DialogHeader>
					<DialogTitle>Delete {displayName}</DialogTitle>
					<DialogDescription>
						This soft-deletes the account. Type the display name <code>{displayName}</code> to confirm.
					</DialogDescription>
				</DialogHeader>
				<div className="flex flex-col gap-3">
					<div className="flex flex-col gap-1">
						<Label htmlFor="delete-reason">Reason</Label>
						<Input
							id="delete-reason"
							placeholder="Reason (required, audited)"
							value={reason}
							onChange={(e) => setReason(e.target.value)}
						/>
					</div>
					<div className="flex flex-col gap-1">
						<Label htmlFor="delete-typed">Type the display name to confirm</Label>
						<Input
							id="delete-typed"
							placeholder={displayName}
							value={typed}
							onChange={(e) => setTyped(e.target.value)}
						/>
					</div>
					{error ? <div className="text-sm text-destructive">{error}</div> : null}
				</div>
				<DialogFooter>
					<Button variant="ghost" onClick={() => onOpenChange(false)} disabled={submitting}>
						Cancel
					</Button>
					<Button
						variant="destructive"
						onClick={submit}
						disabled={submitting || !reason.trim() || typed !== displayName}
					>
						Delete user
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
```

- [ ] **Step 3: Implement `restore-dialog.tsx`**

Create `apps/frontend/app/admin/users/_components/restore-dialog.tsx`:

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
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

type Props = {
	open: boolean;
	onOpenChange: (open: boolean) => void;
	userId: string;
	displayName: string;
	onConfirm: (userId: string, reason: string) => Promise<void>;
};

export function RestoreDialog({ open, onOpenChange, userId, displayName, onConfirm }: Props) {
	const [reason, setReason] = React.useState("");
	const [submitting, setSubmitting] = React.useState(false);
	const [error, setError] = React.useState<string | null>(null);

	React.useEffect(() => {
		if (!open) {
			setReason("");
			setError(null);
			setSubmitting(false);
		}
	}, [open]);

	const submit = async () => {
		setSubmitting(true);
		try {
			await onConfirm(userId, reason.trim());
			onOpenChange(false);
		} catch (err) {
			setError((err as Error).message);
		} finally {
			setSubmitting(false);
		}
	};

	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent>
				<DialogHeader>
					<DialogTitle>Restore {displayName}</DialogTitle>
					<DialogDescription>The account becomes accessible again.</DialogDescription>
				</DialogHeader>
				<div className="flex flex-col gap-1">
					<Label htmlFor="restore-reason">Reason (optional)</Label>
					<Input
						id="restore-reason"
						value={reason}
						onChange={(e) => setReason(e.target.value)}
					/>
				</div>
				{error ? <div className="text-sm text-destructive">{error}</div> : null}
				<DialogFooter>
					<Button variant="ghost" onClick={() => onOpenChange(false)} disabled={submitting}>
						Cancel
					</Button>
					<Button onClick={submit} disabled={submitting}>
						Restore
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
```

- [ ] **Step 4: Implement `purge-dialog.tsx`**

Create `apps/frontend/app/admin/users/_components/purge-dialog.tsx`:

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
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

type Props = {
	open: boolean;
	onOpenChange: (open: boolean) => void;
	userId: string;
	displayName: string;
	onConfirm: (userId: string, reason: string, typed: string) => Promise<void>;
};

export function PurgeDialog({ open, onOpenChange, userId, displayName, onConfirm }: Props) {
	const [reason, setReason] = React.useState("");
	const [typed, setTyped] = React.useState("");
	const [submitting, setSubmitting] = React.useState(false);
	const [error, setError] = React.useState<string | null>(null);

	React.useEffect(() => {
		if (!open) {
			setReason("");
			setTyped("");
			setError(null);
			setSubmitting(false);
		}
	}, [open]);

	const submit = async () => {
		if (!reason.trim() || typed !== userId) return;
		setSubmitting(true);
		try {
			await onConfirm(userId, reason.trim(), typed);
			onOpenChange(false);
		} catch (err) {
			setError((err as Error).message);
		} finally {
			setSubmitting(false);
		}
	};

	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent>
				<DialogHeader>
					<DialogTitle>Purge {displayName}</DialogTitle>
					<DialogDescription>
						This permanently removes the user and unrecoverable data.
						Type the user ID <code>{userId}</code> to confirm.
					</DialogDescription>
				</DialogHeader>
				<div className="flex flex-col gap-3">
					<div className="flex flex-col gap-1">
						<Label htmlFor="purge-reason">Reason</Label>
						<Input
							id="purge-reason"
							placeholder="Reason (required, audited)"
							value={reason}
							onChange={(e) => setReason(e.target.value)}
						/>
					</div>
					<div className="flex flex-col gap-1">
						<Label htmlFor="purge-typed">Type the user ID to confirm</Label>
						<Input
							id="purge-typed"
							placeholder={userId}
							value={typed}
							onChange={(e) => setTyped(e.target.value)}
						/>
					</div>
					{error ? <div className="text-sm text-destructive">{error}</div> : null}
				</div>
				<DialogFooter>
					<Button variant="ghost" onClick={() => onOpenChange(false)} disabled={submitting}>
						Cancel
					</Button>
					<Button
						variant="destructive"
						onClick={submit}
						disabled={submitting || !reason.trim() || typed !== userId}
					>
						Purge user
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
```

- [ ] **Step 5: Implement `danger-zone.tsx`**

Create `apps/frontend/app/admin/users/_components/danger-zone.tsx`:

```tsx
"use client";

import { useRouter } from "next/navigation";
import * as React from "react";

import { Button } from "@/components/ui/button";
import {
	Collapsible,
	CollapsibleContent,
	CollapsibleTrigger,
} from "@/components/ui/collapsible";
import type { AdminUserDetail } from "@/lib/rustlens-api";

import {
	deleteUserAction,
	purgeUserAction,
	restoreUserAction,
	suspendUserAction,
	unsuspendUserAction,
} from "../actions";
import { DeleteDialog } from "./delete-dialog";
import { PurgeDialog } from "./purge-dialog";
import { RestoreDialog } from "./restore-dialog";
import { SuspendDialog } from "./suspend-dialog";

type Props = {
	detail: AdminUserDetail;
	canSuspend: boolean;
	canDelete: boolean;
	canPurge: boolean;
};

type DialogKey = "suspend" | "unsuspend" | "delete" | "restore" | "purge" | null;

export function DangerZone({ detail, canSuspend, canDelete, canPurge }: Props) {
	const router = useRouter();
	const [dialog, setDialog] = React.useState<DialogKey>(null);
	const close = () => setDialog(null);

	const isSuspended = !!detail.suspendedAt;
	const isDeleted = !!detail.deletedAt;

	if (!canSuspend && !canDelete && !canPurge) return null;

	return (
		<section className="rounded-lg border border-destructive/40 bg-card p-4">
			<Collapsible>
				<CollapsibleTrigger asChild>
					<Button variant="outline" size="sm" className="border-destructive/50 text-destructive">
						Danger zone
					</Button>
				</CollapsibleTrigger>
				<CollapsibleContent className="mt-3 flex flex-col gap-2">
					{canSuspend && !isSuspended ? (
						<Button variant="destructive" size="sm" onClick={() => setDialog("suspend")}>
							Suspend user
						</Button>
					) : null}
					{canSuspend && isSuspended ? (
						<Button variant="outline" size="sm" onClick={() => setDialog("unsuspend")}>
							Unsuspend user
						</Button>
					) : null}
					{canDelete && !isDeleted ? (
						<Button variant="destructive" size="sm" onClick={() => setDialog("delete")}>
							Delete user
						</Button>
					) : null}
					{canDelete && isDeleted ? (
						<Button variant="outline" size="sm" onClick={() => setDialog("restore")}>
							Restore user
						</Button>
					) : null}
					{canPurge ? (
						<Button variant="destructive" size="sm" onClick={() => setDialog("purge")}>
							Purge user (permanent)
						</Button>
					) : null}
				</CollapsibleContent>
			</Collapsible>

			<SuspendDialog
				open={dialog === "suspend" || dialog === "unsuspend"}
				onOpenChange={(o) => !o && close()}
				mode={dialog === "unsuspend" ? "unsuspend" : "suspend"}
				userId={detail.user.id}
				displayName={detail.user.displayName}
				onConfirm={async (userId, reason) => {
					const action = dialog === "unsuspend" ? unsuspendUserAction : suspendUserAction;
					const res = await action(userId, reason);
					if (!res.ok) throw new Error(res.error);
					router.refresh();
				}}
			/>
			<DeleteDialog
				open={dialog === "delete"}
				onOpenChange={(o) => !o && close()}
				userId={detail.user.id}
				displayName={detail.user.displayName}
				onConfirm={async (userId, reason, typed) => {
					const res = await deleteUserAction(userId, reason, typed);
					if (!res.ok) throw new Error(res.error);
					router.refresh();
				}}
			/>
			<RestoreDialog
				open={dialog === "restore"}
				onOpenChange={(o) => !o && close()}
				userId={detail.user.id}
				displayName={detail.user.displayName}
				onConfirm={async (userId, reason) => {
					const res = await restoreUserAction(userId, reason);
					if (!res.ok) throw new Error(res.error);
					router.refresh();
				}}
			/>
			<PurgeDialog
				open={dialog === "purge"}
				onOpenChange={(o) => !o && close()}
				userId={detail.user.id}
				displayName={detail.user.displayName}
				onConfirm={async (userId, reason, typed) => {
					const res = await purgeUserAction(userId, reason, typed);
					if (!res.ok) throw new Error(res.error);
					// On purge the user is gone; navigate back to the list.
					router.push("/admin/users");
				}}
			/>
		</section>
	);
}
```

- [ ] **Step 6: Wire into the detail page**

Replace `user-detail-page.tsx`:

```tsx
"use client";

import type { AdminRoleItem, AdminUserAuditItem, AdminUserDetail } from "@/lib/rustlens-api";

import { AuditSection } from "./audit-section";
import { DangerZone } from "./danger-zone";
import { IdentityHeader } from "./identity-header";
import { ProfileSection } from "./profile-section";
import { RolesSection } from "./roles-section";

type Props = {
	detail: AdminUserDetail;
	roles: AdminRoleItem[];
	audit: AdminUserAuditItem[];
	actorUserId: string;
	permissions: string[];
};

export function UserDetailPage({ detail, roles, audit, actorUserId, permissions }: Props) {
	const canAssign = permissions.includes("users.roles.assign");
	const canGrantSuperadmin = permissions.includes("admins.grant");
	const canEdit = permissions.includes("users.edit");
	const canSuspend = permissions.includes("users.suspend");
	const canDelete = permissions.includes("users.delete");
	const canPurge = permissions.includes("admins.grant");
	return (
		<div className="flex flex-col gap-6">
			<IdentityHeader detail={detail} />
			<RolesSection
				detail={detail}
				roles={roles}
				actorUserId={actorUserId}
				canAssign={canAssign}
				canGrantSuperadmin={canGrantSuperadmin}
			/>
			<ProfileSection detail={detail} canEdit={canEdit} />
			<AuditSection userId={detail.user.id} items={audit} />
			<DangerZone
				detail={detail}
				canSuspend={canSuspend}
				canDelete={canDelete}
				canPurge={canPurge}
			/>
		</div>
	);
}
```

- [ ] **Step 7: Verify in-browser**

Reload the detail page. Expected: Danger Zone collapsed by default; expanding it shows the right buttons for the user's current state. Each dialog runs its action and refreshes (or redirects, on purge).

- [ ] **Step 8: Commit**

```
git add apps/frontend/app/admin/users/_components/suspend-dialog.tsx apps/frontend/app/admin/users/_components/delete-dialog.tsx apps/frontend/app/admin/users/_components/restore-dialog.tsx apps/frontend/app/admin/users/_components/purge-dialog.tsx apps/frontend/app/admin/users/_components/danger-zone.tsx apps/frontend/app/admin/users/_components/user-detail-page.tsx
git commit -m "feat(frontend): danger-zone (suspend/delete/restore/purge) on user detail"
```

---

## Phase 4: List page

### Task 11: List page route + toolbar + table

**Files:**
- Create: `apps/frontend/app/admin/users/page.tsx`
- Create: `apps/frontend/app/admin/users/_components/users-toolbar.tsx`
- Create: `apps/frontend/app/admin/users/_components/users-table.tsx`
- Create: `apps/frontend/app/admin/users/_components/users-page.tsx`

- [ ] **Step 1: Implement the route**

Create `apps/frontend/app/admin/users/page.tsx`:

```tsx
import { AppSidebar } from "../_components/app-sidebar";
import { UsersPage } from "./_components/users-page";
import { SiteHeader } from "@/components/site-header";
import { SidebarInset, SidebarProvider } from "@/components/ui/sidebar";
import { getCsrfToken } from "@/lib/csrf";
import {
	getAdminRoles,
	getAdminUsers,
	getCurrentUserSafe,
	type AdminUsersQuery,
} from "@/lib/rustlens-api";
import { requirePermission } from "@/lib/session";

const DEFAULT_PAGE_SIZE = 50;
const PAGE_SIZE_OPTIONS = [10, 20, 30, 40, 50];

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

export default async function AdminUsersRoute({
	searchParams,
}: {
	searchParams: Promise<Record<string, string | string[] | undefined>>;
}) {
	const session = await requirePermission("users.read");
	const sp = await searchParams;

	const query: AdminUsersQuery = {
		q: pickStr(sp.q),
		role: pickArr(sp.role),
		status: (pickStr(sp.status) as AdminUsersQuery["status"]) ?? "active",
		sort: pickStr(sp.sort),
		page: parsePositiveInt(pickStr(sp.page), 1),
		pageSize: PAGE_SIZE_OPTIONS.includes(parsePositiveInt(pickStr(sp.pageSize), DEFAULT_PAGE_SIZE))
			? parsePositiveInt(pickStr(sp.pageSize), DEFAULT_PAGE_SIZE)
			: DEFAULT_PAGE_SIZE,
	};

	const [csrfToken, currentUser, users, roles] = await Promise.all([
		getCsrfToken(),
		getCurrentUserSafe(),
		getAdminUsers(query),
		getAdminRoles(),
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
								<h1 className="text-2xl font-semibold">Users</h1>
								<p className="text-sm text-muted-foreground">
									Find users, assign admin roles, and manage account state.
								</p>
							</div>
							<UsersPage
								initialPage={users}
								roles={roles}
								initialQuery={query}
								pageSizeOptions={PAGE_SIZE_OPTIONS}
								permissions={session.permissions ?? []}
							/>
						</div>
					</div>
				</div>
			</SidebarInset>
		</SidebarProvider>
	);
}
```

- [ ] **Step 2: Implement `users-toolbar.tsx`**

Create `apps/frontend/app/admin/users/_components/users-toolbar.tsx`:

```tsx
"use client";

import { SearchIcon } from "lucide-react";
import * as React from "react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
	Select,
	SelectContent,
	SelectItem,
	SelectTrigger,
	SelectValue,
} from "@/components/ui/select";
import type { AdminRoleItem, AdminUsersQuery } from "@/lib/rustlens-api";

type Props = {
	query: AdminUsersQuery;
	roles: AdminRoleItem[];
	totalRows: number;
	exportUrl: string | null;
	onChange: (next: Partial<AdminUsersQuery>) => void;
};

const STATUSES: Array<NonNullable<AdminUsersQuery["status"]>> = [
	"active",
	"suspended",
	"deleted",
	"any",
];

const SORTS: Array<{ value: string; label: string }> = [
	{ value: "-lastLoginAt", label: "Last login (newest)" },
	{ value: "lastLoginAt", label: "Last login (oldest)" },
	{ value: "-createdAt", label: "Created (newest)" },
	{ value: "createdAt", label: "Created (oldest)" },
	{ value: "displayName", label: "Display name (A→Z)" },
	{ value: "-displayName", label: "Display name (Z→A)" },
];

export function UsersToolbar({ query, roles, totalRows, exportUrl, onChange }: Props) {
	const [text, setText] = React.useState(query.q ?? "");
	React.useEffect(() => {
		const id = setTimeout(() => {
			onChange({ q: text || undefined, page: 1 });
		}, 300);
		return () => clearTimeout(id);
		// eslint-disable-next-line react-hooks/exhaustive-deps
	}, [text]);

	const selectedRoles = new Set(query.role ?? []);
	const toggleRole = (key: string) => {
		const next = new Set(selectedRoles);
		if (next.has(key)) next.delete(key);
		else next.add(key);
		onChange({ role: Array.from(next), page: 1 });
	};

	return (
		<div className="flex flex-col gap-2">
			<div className="flex flex-wrap items-center gap-2">
				<div className="relative w-full max-w-sm">
					<SearchIcon className="absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
					<Input
						type="search"
						placeholder="Search by name, Discord, or ID"
						value={text}
						onChange={(e) => setText(e.target.value)}
						className="pl-8"
					/>
				</div>
				<div className="flex items-center gap-1">
					{STATUSES.map((s) => (
						<Button
							key={s}
							variant={query.status === s ? "default" : "outline"}
							size="sm"
							onClick={() => onChange({ status: s, page: 1 })}
						>
							{s}
						</Button>
					))}
				</div>
				<Select value={query.sort ?? "-lastLoginAt"} onValueChange={(v) => onChange({ sort: v })}>
					<SelectTrigger className="w-56">
						<SelectValue placeholder="Sort" />
					</SelectTrigger>
					<SelectContent>
						{SORTS.map((s) => (
							<SelectItem key={s.value} value={s.value}>
								{s.label}
							</SelectItem>
						))}
					</SelectContent>
				</Select>
				<div className="ml-auto">
					{exportUrl ? (
						<a href={exportUrl} download>
							<Button variant="outline" size="sm">
								Export filtered ({totalRows})
							</Button>
						</a>
					) : null}
				</div>
			</div>
			<div className="flex flex-wrap items-center gap-1">
				<span className="text-xs text-muted-foreground">Roles:</span>
				{roles.map((r) => (
					<Badge
						key={r.key}
						variant={selectedRoles.has(r.key) ? "default" : "outline"}
						className="cursor-pointer"
						onClick={() => toggleRole(r.key)}
					>
						{r.name}
					</Badge>
				))}
				{selectedRoles.size > 0 ? (
					<Button variant="ghost" size="sm" onClick={() => onChange({ role: [], page: 1 })}>
						Clear
					</Button>
				) : null}
			</div>
		</div>
	);
}
```

- [ ] **Step 3: Implement `users-table.tsx`**

Create `apps/frontend/app/admin/users/_components/users-table.tsx`:

```tsx
"use client";

import Link from "next/link";
import * as React from "react";

import { Badge } from "@/components/ui/badge";
import { Checkbox } from "@/components/ui/checkbox";
import { RelativeTime } from "@/components/admin/relative-time";
import {
	Table,
	TableBody,
	TableCell,
	TableHead,
	TableHeader,
	TableRow,
} from "@/components/ui/table";
import type { AdminUserItem } from "@/lib/rustlens-api";

type Props = {
	items: AdminUserItem[];
	selected: Set<string>;
	onToggle: (userId: string) => void;
	onToggleAll: () => void;
};

export function UsersTable({ items, selected, onToggle, onToggleAll }: Props) {
	const allOnPageSelected = items.length > 0 && items.every((it) => selected.has(it.user.id));
	return (
		<div className="rounded-md border">
			<Table>
				<TableHeader>
					<TableRow>
						<TableHead className="w-10">
							<Checkbox
								checked={allOnPageSelected}
								onCheckedChange={onToggleAll}
								aria-label="Select all on page"
							/>
						</TableHead>
						<TableHead>User</TableHead>
						<TableHead>Roles</TableHead>
						<TableHead>Status</TableHead>
						<TableHead>Last login</TableHead>
						<TableHead>Created</TableHead>
					</TableRow>
				</TableHeader>
				<TableBody>
					{items.map((it) => {
						const status = it.deletedAt ? "deleted" : it.suspendedAt ? "suspended" : "active";
						return (
							<TableRow key={it.user.id}>
								<TableCell>
									<Checkbox
										checked={selected.has(it.user.id)}
										onCheckedChange={() => onToggle(it.user.id)}
										aria-label={`Select ${it.user.displayName}`}
									/>
								</TableCell>
								<TableCell>
									<Link
										href={`/admin/users/${it.user.id}`}
										className="flex items-center gap-2 hover:underline"
									>
										{it.user.avatarUrl || it.discordAvatar ? (
											// eslint-disable-next-line @next/next/no-img-element
											<img
												src={it.user.avatarUrl || it.discordAvatar}
												alt=""
												className="size-7 rounded-full object-cover"
											/>
										) : (
											<div className="size-7 rounded-full bg-muted" />
										)}
										<div className="flex flex-col">
											<span className="font-medium">{it.user.displayName}</span>
											<span className="text-xs text-muted-foreground">
												@{it.discordUsername || "—"}
											</span>
										</div>
									</Link>
								</TableCell>
								<TableCell>
									<div className="flex flex-wrap gap-1">
										{it.adminRoles.length === 0 ? (
											<span className="text-xs text-muted-foreground">—</span>
										) : (
											it.adminRoles.map((r) => (
												<Badge
													key={r}
													variant={r === "superadmin" ? "destructive" : "secondary"}
												>
													{r}
												</Badge>
											))
										)}
									</div>
								</TableCell>
								<TableCell>
									<Badge
										variant={
											status === "deleted"
												? "destructive"
												: status === "suspended"
													? "outline"
													: "secondary"
										}
									>
										{status}
									</Badge>
								</TableCell>
								<TableCell>
									<RelativeTime iso={it.user.lastLoginAt} />
								</TableCell>
								<TableCell>
									<RelativeTime iso={it.user.createdAt} />
								</TableCell>
							</TableRow>
						);
					})}
				</TableBody>
			</Table>
		</div>
	);
}
```

- [ ] **Step 4: Implement `users-page.tsx` (orchestrator without bulk toolbar yet)**

Create `apps/frontend/app/admin/users/_components/users-page.tsx`:

```tsx
"use client";

import { usePathname, useRouter, useSearchParams } from "next/navigation";
import * as React from "react";

import { Button } from "@/components/ui/button";
import {
	Pagination,
	PaginationContent,
	PaginationItem,
	PaginationLink,
	PaginationNext,
	PaginationPrevious,
} from "@/components/ui/pagination";
import {
	type AdminRoleItem,
	type AdminUsersPage as AdminUsersPageData,
	type AdminUsersQuery,
	buildAdminUsersExportUrl,
} from "@/lib/rustlens-api";

import { UsersTable } from "./users-table";
import { UsersToolbar } from "./users-toolbar";

type Props = {
	initialPage: AdminUsersPageData;
	roles: AdminRoleItem[];
	initialQuery: AdminUsersQuery;
	pageSizeOptions: number[];
	permissions: string[];
};

export function UsersPage({ initialPage, roles, initialQuery, pageSizeOptions, permissions }: Props) {
	const router = useRouter();
	const pathname = usePathname();
	const searchParams = useSearchParams();
	const [selected, setSelected] = React.useState<Set<string>>(new Set());

	const updateQuery = (patch: Partial<AdminUsersQuery>) => {
		const sp = new URLSearchParams(searchParams.toString());
		const apply = (key: string, val: unknown) => {
			sp.delete(key);
			if (Array.isArray(val)) {
				for (const v of val) sp.append(key, v);
			} else if (val !== undefined && val !== null && val !== "") {
				sp.set(key, String(val));
			}
		};
		apply("q", patch.q ?? initialQuery.q);
		apply("role", patch.role ?? initialQuery.role);
		apply("status", patch.status ?? initialQuery.status);
		apply("sort", patch.sort ?? initialQuery.sort);
		apply("page", patch.page ?? initialQuery.page);
		apply("pageSize", patch.pageSize ?? initialQuery.pageSize);
		router.push(`${pathname}?${sp.toString()}`);
	};

	const toggle = (id: string) => {
		setSelected((prev) => {
			const next = new Set(prev);
			if (next.has(id)) next.delete(id);
			else next.add(id);
			return next;
		});
	};
	const toggleAll = () => {
		setSelected((prev) => {
			const ids = initialPage.items.map((it) => it.user.id);
			const allSelected = ids.every((id) => prev.has(id));
			if (allSelected) {
				const next = new Set(prev);
				for (const id of ids) next.delete(id);
				return next;
			}
			const next = new Set(prev);
			for (const id of ids) next.add(id);
			return next;
		});
	};

	const exportUrl = permissions.includes("users.export")
		? buildAdminUsersExportUrl({
			q: initialQuery.q,
			role: initialQuery.role,
			status: initialQuery.status,
			sort: initialQuery.sort,
		})
		: null;

	const totalPages = Math.max(
		1,
		Math.ceil(initialPage.total / (initialQuery.pageSize ?? 50)),
	);
	const currentPage = initialQuery.page ?? 1;

	return (
		<div className="flex flex-col gap-4">
			<UsersToolbar
				query={initialQuery}
				roles={roles}
				totalRows={initialPage.total}
				exportUrl={exportUrl}
				onChange={updateQuery}
			/>
			<UsersTable
				items={initialPage.items}
				selected={selected}
				onToggle={toggle}
				onToggleAll={toggleAll}
			/>
			<div className="flex items-center justify-between">
				<div className="text-sm text-muted-foreground">
					{initialPage.total} users · page {currentPage} of {totalPages}
				</div>
				<div className="flex items-center gap-2">
					<select
						value={initialQuery.pageSize}
						onChange={(e) =>
							updateQuery({ pageSize: Number.parseInt(e.target.value, 10), page: 1 })
						}
						className="rounded border px-2 py-1 text-sm"
					>
						{pageSizeOptions.map((n) => (
							<option key={n} value={n}>
								{n}/page
							</option>
						))}
					</select>
					<Button
						variant="outline"
						size="sm"
						disabled={currentPage <= 1}
						onClick={() => updateQuery({ page: Math.max(1, currentPage - 1) })}
					>
						Prev
					</Button>
					<Button
						variant="outline"
						size="sm"
						disabled={currentPage >= totalPages}
						onClick={() => updateQuery({ page: Math.min(totalPages, currentPage + 1) })}
					>
						Next
					</Button>
				</div>
			</div>
		</div>
	);
}
```

- [ ] **Step 5: Verify in-browser**

Run `make dev`, navigate to `/admin/users`. Expected: search box, status pills, role filter chips, sort dropdown, table of users. Filtering/sorting reflect in URL. Pagination buttons work. Clicking a row navigates to the detail page.

- [ ] **Step 6: Commit**

```
git add apps/frontend/app/admin/users/page.tsx apps/frontend/app/admin/users/_components/users-toolbar.tsx apps/frontend/app/admin/users/_components/users-table.tsx apps/frontend/app/admin/users/_components/users-page.tsx
git commit -m "feat(frontend): admin users list with filter, sort, pagination"
```

### Task 12: Bulk toolbar + bulk dialogs

**Files:**
- Create: `apps/frontend/app/admin/users/_components/users-bulk-toolbar.tsx`
- Create: `apps/frontend/app/admin/users/_components/bulk-suspend-dialog.tsx`
- Create: `apps/frontend/app/admin/users/_components/bulk-role-dialog.tsx`
- Create: `apps/frontend/app/admin/users/_components/__tests__/bulk-role-dialog.test.tsx`
- Modify: `apps/frontend/app/admin/users/_components/users-page.tsx`

- [ ] **Step 1: Write the failing test for `bulk-role-dialog`**

Create `apps/frontend/app/admin/users/_components/__tests__/bulk-role-dialog.test.tsx`:

```tsx
import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent, waitFor, act } from "@testing-library/react";
import { BulkRoleDialog } from "../bulk-role-dialog";

const roles = [
	{ id: "r1", key: "moderator", name: "Moderator", description: "Mods", isSystem: false, permissions: [], memberCount: 0 },
	{ id: "r2", key: "superadmin", name: "Superadmin", description: "All", isSystem: true, permissions: [], memberCount: 0 },
];

describe("BulkRoleDialog", () => {
	it("excludes superadmin from picker", () => {
		render(
			<BulkRoleDialog
				open
				onOpenChange={() => {}}
				mode="grant"
				userIds={["u1", "u2"]}
				roles={roles}
				onConfirm={vi.fn()}
			/>,
		);
		fireEvent.click(screen.getByRole("combobox"));
		expect(screen.queryByRole("option", { name: /superadmin/i })).toBeNull();
		expect(screen.getByRole("option", { name: /moderator/i })).toBeInTheDocument();
	});

	it("calls onConfirm with mode, userIds, params, reason", async () => {
		const onConfirm = vi.fn().mockResolvedValue({
			action: "roles.grant",
			succeeded: 2,
			failed: 0,
			results: [],
		});
		render(
			<BulkRoleDialog
				open
				onOpenChange={() => {}}
				mode="grant"
				userIds={["u1", "u2"]}
				roles={roles}
				onConfirm={onConfirm}
			/>,
		);
		fireEvent.click(screen.getByRole("combobox"));
		fireEvent.click(await screen.findByRole("option", { name: /moderator/i }));
		const reason = await screen.findByPlaceholderText(/reason/i);
		fireEvent.change(reason, { target: { value: "ops cleanup" } });
		await act(async () => {
			fireEvent.click(screen.getByRole("button", { name: /grant role to/i }));
		});
		await waitFor(() => {
			expect(onConfirm).toHaveBeenCalledWith(
				"roles.grant",
				["u1", "u2"],
				{ roleKey: "moderator" },
				"ops cleanup",
			);
		});
	});
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run:

```
pnpm --filter frontend test -- bulk-role-dialog
```

Expected: FAIL with "cannot find module".

- [ ] **Step 3: Implement `bulk-role-dialog.tsx`**

Create `apps/frontend/app/admin/users/_components/bulk-role-dialog.tsx`:

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
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
	Select,
	SelectContent,
	SelectItem,
	SelectTrigger,
	SelectValue,
} from "@/components/ui/select";
import type { AdminRoleItem, BulkUserResult, BulkUserSubAction } from "@/lib/rustlens-api";

type Mode = "grant" | "revoke";

type Props = {
	open: boolean;
	onOpenChange: (open: boolean) => void;
	mode: Mode;
	userIds: string[];
	roles: AdminRoleItem[];
	onConfirm: (
		action: BulkUserSubAction,
		userIds: string[],
		params: Record<string, unknown>,
		reason: string,
	) => Promise<BulkUserResult>;
};

export function BulkRoleDialog({ open, onOpenChange, mode, userIds, roles, onConfirm }: Props) {
	const [roleKey, setRoleKey] = React.useState("");
	const [reason, setReason] = React.useState("");
	const [submitting, setSubmitting] = React.useState(false);
	const [result, setResult] = React.useState<BulkUserResult | null>(null);
	const [error, setError] = React.useState<string | null>(null);

	React.useEffect(() => {
		if (!open) {
			setRoleKey("");
			setReason("");
			setResult(null);
			setError(null);
			setSubmitting(false);
		}
	}, [open]);

	const submit = async () => {
		if (!roleKey || !reason.trim()) return;
		setSubmitting(true);
		setError(null);
		try {
			const res = await onConfirm(
				mode === "grant" ? "roles.grant" : "roles.revoke",
				userIds,
				{ roleKey },
				reason.trim(),
			);
			setResult(res);
		} catch (err) {
			setError((err as Error).message);
		} finally {
			setSubmitting(false);
		}
	};

	const assignableRoles = roles.filter((r) => r.key !== "superadmin");
	const verb = mode === "grant" ? "Grant role to" : "Revoke role from";

	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent>
				<DialogHeader>
					<DialogTitle>
						{verb} {userIds.length} user{userIds.length === 1 ? "" : "s"}
					</DialogTitle>
					<DialogDescription>
						Each user is processed individually; failures don&apos;t stop the batch.
					</DialogDescription>
				</DialogHeader>

				{result ? (
					<div className="flex flex-col gap-2 text-sm">
						<div>
							{result.succeeded} succeeded · {result.failed} failed
						</div>
						<ul className="max-h-48 overflow-auto text-xs">
							{result.results.map((r) => (
								<li key={r.userId} className={r.ok ? "" : "text-destructive"}>
									{r.userId} — {r.ok ? "ok" : r.error}
								</li>
							))}
						</ul>
					</div>
				) : (
					<div className="flex flex-col gap-3">
						<div className="flex flex-col gap-1">
							<Label htmlFor="bulk-role">Role</Label>
							<Select value={roleKey} onValueChange={setRoleKey}>
								<SelectTrigger id="bulk-role">
									<SelectValue placeholder="Pick a role" />
								</SelectTrigger>
								<SelectContent>
									{assignableRoles.map((r) => (
										<SelectItem key={r.key} value={r.key}>
											{r.name}
										</SelectItem>
									))}
								</SelectContent>
							</Select>
						</div>
						<div className="flex flex-col gap-1">
							<Label htmlFor="bulk-reason">Reason</Label>
							<Input
								id="bulk-reason"
								placeholder="Reason (required, audited)"
								value={reason}
								onChange={(e) => setReason(e.target.value)}
							/>
						</div>
						{error ? <div className="text-sm text-destructive">{error}</div> : null}
					</div>
				)}

				<DialogFooter>
					<Button variant="ghost" onClick={() => onOpenChange(false)} disabled={submitting}>
						{result ? "Close" : "Cancel"}
					</Button>
					{result ? (
						<Button onClick={() => onOpenChange(false)}>Done</Button>
					) : (
						<Button
							onClick={submit}
							disabled={submitting || !roleKey || !reason.trim()}
						>
							{verb}
						</Button>
					)}
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run:

```
pnpm --filter frontend test -- bulk-role-dialog
```

Expected: PASS (both cases).

- [ ] **Step 5: Implement `bulk-suspend-dialog.tsx`**

Create `apps/frontend/app/admin/users/_components/bulk-suspend-dialog.tsx`:

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
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import type { BulkUserResult, BulkUserSubAction } from "@/lib/rustlens-api";

type Mode = "suspend" | "unsuspend";

type Props = {
	open: boolean;
	onOpenChange: (open: boolean) => void;
	mode: Mode;
	userIds: string[];
	onConfirm: (
		action: BulkUserSubAction,
		userIds: string[],
		params: Record<string, unknown>,
		reason: string,
	) => Promise<BulkUserResult>;
};

export function BulkSuspendDialog({ open, onOpenChange, mode, userIds, onConfirm }: Props) {
	const [reason, setReason] = React.useState("");
	const [submitting, setSubmitting] = React.useState(false);
	const [result, setResult] = React.useState<BulkUserResult | null>(null);
	const [error, setError] = React.useState<string | null>(null);

	React.useEffect(() => {
		if (!open) {
			setReason("");
			setResult(null);
			setError(null);
			setSubmitting(false);
		}
	}, [open]);

	const reasonRequired = mode === "suspend";
	const submit = async () => {
		if (reasonRequired && !reason.trim()) return;
		setSubmitting(true);
		setError(null);
		try {
			const res = await onConfirm(mode, userIds, {}, reason.trim());
			setResult(res);
		} catch (err) {
			setError((err as Error).message);
		} finally {
			setSubmitting(false);
		}
	};

	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent>
				<DialogHeader>
					<DialogTitle>
						{mode === "suspend" ? "Suspend" : "Unsuspend"} {userIds.length} user{userIds.length === 1 ? "" : "s"}
					</DialogTitle>
					<DialogDescription>
						Each user is processed individually; failures don&apos;t stop the batch.
					</DialogDescription>
				</DialogHeader>

				{result ? (
					<div className="flex flex-col gap-2 text-sm">
						<div>{result.succeeded} succeeded · {result.failed} failed</div>
						<ul className="max-h-48 overflow-auto text-xs">
							{result.results.map((r) => (
								<li key={r.userId} className={r.ok ? "" : "text-destructive"}>
									{r.userId} — {r.ok ? "ok" : r.error}
								</li>
							))}
						</ul>
					</div>
				) : (
					<div className="flex flex-col gap-1">
						<Label htmlFor="bulk-suspend-reason">
							Reason{reasonRequired ? "" : " (optional)"}
						</Label>
						<Input
							id="bulk-suspend-reason"
							placeholder="Reason (audited)"
							value={reason}
							onChange={(e) => setReason(e.target.value)}
						/>
						{error ? <div className="text-sm text-destructive">{error}</div> : null}
					</div>
				)}

				<DialogFooter>
					<Button variant="ghost" onClick={() => onOpenChange(false)} disabled={submitting}>
						{result ? "Close" : "Cancel"}
					</Button>
					{result ? (
						<Button onClick={() => onOpenChange(false)}>Done</Button>
					) : (
						<Button
							variant={mode === "suspend" ? "destructive" : "default"}
							onClick={submit}
							disabled={submitting || (reasonRequired && !reason.trim())}
						>
							{mode === "suspend" ? "Suspend" : "Unsuspend"}
						</Button>
					)}
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
```

- [ ] **Step 6: Implement `users-bulk-toolbar.tsx`**

Create `apps/frontend/app/admin/users/_components/users-bulk-toolbar.tsx`:

```tsx
"use client";

import * as React from "react";

import { Button } from "@/components/ui/button";
import {
	type AdminRoleItem,
	type BulkUserResult,
	type BulkUserSubAction,
	buildAdminUsersExportUrl,
} from "@/lib/rustlens-api";

import { bulkUserAction } from "../actions";
import { BulkRoleDialog } from "./bulk-role-dialog";
import { BulkSuspendDialog } from "./bulk-suspend-dialog";

type Props = {
	selectedIds: string[];
	roles: AdminRoleItem[];
	permissions: string[];
	onClear: () => void;
	onComplete: () => void;
};

export function UsersBulkToolbar({ selectedIds, roles, permissions, onClear, onComplete }: Props) {
	const [dialog, setDialog] = React.useState<
		| null
		| { kind: "suspend"; mode: "suspend" | "unsuspend" }
		| { kind: "role"; mode: "grant" | "revoke" }
	>(null);

	const canSuspend = permissions.includes("users.suspend");
	const canRoles = permissions.includes("users.roles.assign");
	const canExport = permissions.includes("users.export");

	const wrapBulk = async (
		action: BulkUserSubAction,
		userIds: string[],
		params: Record<string, unknown>,
		reason: string,
	): Promise<BulkUserResult> => {
		const res = await bulkUserAction(action, userIds, params, reason);
		if (!res.ok) throw new Error(res.error);
		return res.result;
	};

	if (selectedIds.length === 0) return null;

	const exportSelectedUrl = canExport ? buildAdminUsersExportUrl({ ids: selectedIds }) : null;

	return (
		<div className="sticky top-0 z-20 flex flex-wrap items-center gap-2 rounded-md border bg-card p-2 shadow-sm">
			<span className="text-sm font-medium">{selectedIds.length} selected</span>
			{canSuspend ? (
				<>
					<Button
						size="sm"
						variant="outline"
						onClick={() => setDialog({ kind: "suspend", mode: "suspend" })}
					>
						Suspend
					</Button>
					<Button
						size="sm"
						variant="outline"
						onClick={() => setDialog({ kind: "suspend", mode: "unsuspend" })}
					>
						Unsuspend
					</Button>
				</>
			) : null}
			{canRoles ? (
				<>
					<Button
						size="sm"
						variant="outline"
						onClick={() => setDialog({ kind: "role", mode: "grant" })}
					>
						Grant role…
					</Button>
					<Button
						size="sm"
						variant="outline"
						onClick={() => setDialog({ kind: "role", mode: "revoke" })}
					>
						Revoke role…
					</Button>
				</>
			) : null}
			{exportSelectedUrl ? (
				<a href={exportSelectedUrl} download>
					<Button size="sm" variant="outline">
						Export selected
					</Button>
				</a>
			) : null}
			<div className="ml-auto">
				<Button size="sm" variant="ghost" onClick={onClear}>
					Clear
				</Button>
			</div>

			{dialog?.kind === "suspend" ? (
				<BulkSuspendDialog
					open
					onOpenChange={(o) => {
						if (!o) {
							setDialog(null);
							onComplete();
						}
					}}
					mode={dialog.mode}
					userIds={selectedIds}
					onConfirm={wrapBulk}
				/>
			) : null}
			{dialog?.kind === "role" ? (
				<BulkRoleDialog
					open
					onOpenChange={(o) => {
						if (!o) {
							setDialog(null);
							onComplete();
						}
					}}
					mode={dialog.mode}
					userIds={selectedIds}
					roles={roles}
					onConfirm={wrapBulk}
				/>
			) : null}
		</div>
	);
}
```

- [ ] **Step 7: Wire into `users-page.tsx`**

Modify `users-page.tsx` to render `<UsersBulkToolbar>` above the table:

Add the import at the top:

```tsx
import { UsersBulkToolbar } from "./users-bulk-toolbar";
```

In the JSX, between `<UsersToolbar … />` and `<UsersTable … />`, add:

```tsx
<UsersBulkToolbar
	selectedIds={Array.from(selected)}
	roles={roles}
	permissions={permissions}
	onClear={() => setSelected(new Set())}
	onComplete={() => {
		setSelected(new Set());
		router.refresh();
	}}
/>
```

- [ ] **Step 8: Verify in-browser**

Reload `/admin/users`, select 1+ rows, confirm the bulk toolbar slides in. Try a bulk-grant + bulk-suspend and verify both result panels and refreshes work.

- [ ] **Step 9: Commit**

```
git add apps/frontend/app/admin/users/_components/users-bulk-toolbar.tsx apps/frontend/app/admin/users/_components/bulk-suspend-dialog.tsx apps/frontend/app/admin/users/_components/bulk-role-dialog.tsx apps/frontend/app/admin/users/_components/__tests__/bulk-role-dialog.test.tsx apps/frontend/app/admin/users/_components/users-page.tsx
git commit -m "feat(frontend): bulk action toolbar with suspend/role/export"
```

### Task 13: Export buttons "Logged to audit" microcopy

Both export buttons currently use a plain `<a download>`. Now wrap them so the user gets confirmation that the action was logged. The audit row is written before the stream starts, so the microcopy is accurate even on cancelled downloads.

**Files:**
- Create: `apps/frontend/app/admin/users/_components/export-button.tsx`
- Modify: `apps/frontend/app/admin/users/_components/users-toolbar.tsx`
- Modify: `apps/frontend/app/admin/users/_components/users-bulk-toolbar.tsx`

- [ ] **Step 1: Create `export-button.tsx`**

Create `apps/frontend/app/admin/users/_components/export-button.tsx`:

```tsx
"use client";

import { CheckIcon, DownloadIcon } from "lucide-react";
import * as React from "react";

import { Button } from "@/components/ui/button";

type Props = {
	url: string;
	label: string;
};

export function ExportButton({ url, label }: Props) {
	const [confirmed, setConfirmed] = React.useState(false);
	return (
		<Button
			asChild
			size="sm"
			variant="outline"
			onClick={() => {
				setConfirmed(true);
				setTimeout(() => setConfirmed(false), 2500);
			}}
			title="Logged to audit"
		>
			<a href={url} download>
				{confirmed ? (
					<>
						<CheckIcon className="mr-2 size-4" />
						Logged to audit
					</>
				) : (
					<>
						<DownloadIcon className="mr-2 size-4" />
						{label}
					</>
				)}
			</a>
		</Button>
	);
}
```

- [ ] **Step 2: Replace the `<a download>` blocks in toolbars with `<ExportButton>`**

In `users-toolbar.tsx`, replace:

```tsx
{exportUrl ? (
	<a href={exportUrl} download>
		<Button variant="outline" size="sm">
			Export filtered ({totalRows})
		</Button>
	</a>
) : null}
```

with:

```tsx
{exportUrl ? <ExportButton url={exportUrl} label={`Export filtered (${totalRows})`} /> : null}
```

And add the import:

```tsx
import { ExportButton } from "./export-button";
```

In `users-bulk-toolbar.tsx`, replace:

```tsx
{exportSelectedUrl ? (
	<a href={exportSelectedUrl} download>
		<Button size="sm" variant="outline">
			Export selected
		</Button>
	</a>
) : null}
```

with:

```tsx
{exportSelectedUrl ? <ExportButton url={exportSelectedUrl} label="Export selected" /> : null}
```

And add the import:

```tsx
import { ExportButton } from "./export-button";
```

- [ ] **Step 3: Verify in-browser**

Click "Export filtered". Expected: file downloads, the button briefly switches to "Logged to audit" with a checkmark for ~2.5s. Same for "Export selected".

- [ ] **Step 4: Commit**

```
git add apps/frontend/app/admin/users/_components/export-button.tsx apps/frontend/app/admin/users/_components/users-toolbar.tsx apps/frontend/app/admin/users/_components/users-bulk-toolbar.tsx
git commit -m "feat(frontend): export button shows 'Logged to audit' microcopy"
```

---

## Phase 5: Sidebar nav permission swap

### Task 14: Lower the sidebar nav permission gate

**Files:**
- Modify: `apps/frontend/components/app-shared.tsx`

- [ ] **Step 1: Replace the permissions array for `/admin/users`**

In `apps/frontend/components/app-shared.tsx`, find the line:

```tsx
{ href: "/admin/users", label: "Users", icon: UsersIcon, permissions: ["users.suspend", "users.force_unlink_steam"] },
```

Replace with:

```tsx
{ href: "/admin/users", label: "Users", icon: UsersIcon, permissions: ["users.read"] },
```

- [ ] **Step 2: Verify in-browser**

Sign in as a user who has `users.read` but not `users.suspend`. Expected: the "Users" sidebar entry is visible. Sign in as a user with no admin permissions. Expected: the entry is hidden (other admin entries are also hidden by the same `filterNavByPermissions` logic).

- [ ] **Step 3: Commit**

```
git add apps/frontend/components/app-shared.tsx
git commit -m "fix(frontend): /admin/users nav gate is users.read, not users.suspend"
```

---

## Phase 6: Verification

### Task 15: End-to-end verification

- [ ] **Step 1: Run the full backend test suite**

Run:

```
cd apps/backend && go test ./...
```

Expected: PASS.

- [ ] **Step 2: Run the full frontend test suite**

Run:

```
pnpm --filter frontend test
```

Expected: PASS.

- [ ] **Step 3: Run lint and typecheck**

Run:

```
make lint
```

Expected: PASS for both `pnpm lint` and `go vet ./...`.

- [ ] **Step 4: Manual smoke test (one short session)**

Sign in to the dev environment as a superadmin. Walk through these steps without skipping any:

1. Navigate to `/admin/users`. Expect: page renders, table populated.
2. Type a search query → URL updates with `?q=`. Clear search.
3. Click each status pill (Active / Suspended / Deleted / Any) and confirm the URL `status` param updates and rows reflect.
4. Toggle a role chip. URL adds `?role=…`. Untoggle. Toggle two roles, confirm both appear in URL.
5. Change sort dropdown. URL updates.
6. Click "Export filtered". CSV downloads. Button briefly says "Logged to audit". Open `audit_events` directly in psql: `SELECT action_type, params, result FROM audit_events ORDER BY created_at DESC LIMIT 4;` — expect the started + completed pair with matching `params->>'exportId'`.
7. Select 2 rows. Bulk toolbar appears. Click "Grant role…", pick a non-superadmin role, type a reason, confirm. Result panel shows 2 succeeded.
8. Click into one of those users. Roles section shows the new role. Audit section shows a `users.roles.grant` entry.
9. On the detail page: click the role's × on a non-self user, type a reason, confirm. Roles section refreshes; audit section shows revoke.
10. Try clicking × on your own admin role. Expect: the × is disabled with the tooltip "You can't revoke your own admin roles."
11. Edit profile: change display name, save. Identity header reflects the new name.
12. Open Danger zone, click Suspend, type reason, confirm. Status badge becomes "Suspended". Click Unsuspend, confirm. Returns to Active.
13. Bulk export selected: select rows, click Export selected. Confirm `audit_events` shows a started row whose `params.filters.ids` array contains those user IDs.
14. Open one of those users' detail audit feed. Confirm the `users.export.started` row appears in their per-user audit (verifying the JSONB OR-match).

If any step fails, fix and re-run. Don't tick this box until all 14 steps pass.

- [ ] **Step 5: Final commit (if any fixes were needed)**

If steps surfaced any small fixes, commit them with a focused message. Otherwise this step is a no-op.

---

## Self-review

**Spec coverage** — every spec section maps to at least one task:

- Routing & access control → Tasks 6 (detail route), 11 (list route), 14 (nav permission)
- List page (toolbar/filters/sort/pagination) → Task 11
- Bulk toolbar + bulk dialogs → Task 12
- "Logged to audit" microcopy → Task 13
- Detail page identity / roles / profile / audit / danger zone → Tasks 6–10
- CSV export auditing (paired events, completion on error, target_user_id, JSONB ids match) → Tasks 1–3
- Per-user audit JSONB OR-match → Task 3
- Frontend file layout → Tasks 4–13
- Library additions to `rustlens-api.ts` → Task 4
- Server actions → Task 5
- Acceptance checklist (everything) → Task 15

**Type consistency** — `BulkUserSubAction`, `BulkUserResult`, `EditUserPatch`, `assignAdminRole/revokeAdminRole/bulkAdminUserAction` are defined in `rustlens-api.ts` (Task 4) and consumed by `actions.ts` (Task 5) and the dialog components (Tasks 7, 10, 12). The `UsersPage` orchestrator owns selection and reads `permissions` array; `UsersBulkToolbar` accepts them as props consistently.

**Placeholder scan** — no TBDs, no "implement later", every step has the actual code or command. Each test step shows the failing-then-passing transition. Tasks 6–10 use a verification "open the page in dev" step rather than a unit test for layout-heavy components, which matches the project's existing testing conventions where pages aren't unit-tested.

One soft assumption to verify before Task 6 starts: `session.userId` (used to compare with `detail.user.id` in `RolesSection`'s self-revoke guard). If the SessionUser type uses a different key, the executor should adjust the prop name when calling `<UserDetailPage actorUserId={session.userId ?? ""} />`. This is called out in Task 6 step 1.

---

**Plan complete and saved to `docs/superpowers/plans/2026-05-04-user-management.md`.**

**Two execution options:**

1. **Subagent-Driven (recommended)** — I dispatch a fresh subagent per task, review between tasks, fast iteration.
2. **Inline Execution** — Execute tasks in this session using executing-plans, batch execution with checkpoints.

**Which approach?**
