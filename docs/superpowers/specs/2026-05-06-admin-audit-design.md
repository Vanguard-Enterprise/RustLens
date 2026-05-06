# Admin Audit Page — design

**Date:** 2026-05-06
**Branch:** `feature/user-management` (current); a dedicated `feature/admin-audit` branch may be cut at implementation time.
**Scope:** Admin-only audit log browsing UI at `/admin/audit`, plus the backend filter / cursor / detail / event-types endpoints it depends on.

## Goal

Build the `/admin/audit` page so internal admins can browse and filter the unified audit feed (new RBAC `audit_events` ∪ legacy `app_audit_events`) with a UX that matches `/admin/users`. The page must support the existing deep-link from `/admin/users/[userId]` (`?targetUserId=<id>`) and also support arbitrary filters by actor, event type, result, and time range. Selecting a row opens a right-side detail Sheet showing the full payload; the selection is URL-bound (`?event=<id>`) so events are linkable from outside the app.

## Existing context

- **Sidebar link is already wired**: `dashboard-app-sidebar.tsx` shows an "Audit" entry pointing to `/admin/audit`, gated by the `audit.read` permission. The page itself does not yet exist.
- **`audit-section.tsx` already deep-links** to `/admin/audit?targetUserId=<id>` from the per-user detail page.
- **Backend `GET /api/app/admin/audit`** currently accepts only `?limit=N` (cap 500) and returns a unified union over the new `audit_events` and legacy `app_audit_events` tables, with actor/target hydration (display name + avatar) in a single follow-up lookup against `app_users`. It returns no `nextCursor` and no total.
- **`fetchUserAudit`** (per-user audit) already supports a `before=<RFC3339>` cursor and a `target_user_id = $1 OR params.filters.ids @> [$1]` predicate that surfaces export-style events targeting a user. The global handler will reuse the same predicate when `targetUserId` is set.
- **`AuditEventItem` shape** is already finalized in `apps/frontend/lib/rustlens-api.ts`: `{ id, actorUserId, targetUserId, eventType, payload, createdAt, actorDisplayName?, actorAvatarUrl?, targetDisplayName?, targetAvatarUrl?, result?, reason? }`. No shape changes required.
- **Frontend helpers**: `getAdminUsers({ q, pageSize, status })` exists and is fast — the user-combobox reuses it. `RelativeTime`, `UserLink`, and the shadcn `Sheet`, `Command`, `Popover`, `Badge`, `Table`, `Button`, `Input`, `Select` primitives are all already present.

## Non-goals

- Calendar date-range picker (preset chips only — Last 24h / 7d / 30d / All time).
- Column sorts (audit is fundamentally chronological; cursor-only `created_at DESC`).
- Total event count or numbered pages (UNION + COUNT(*) over an unbounded log is expensive and the precision is misleading).
- CSV export from the audit page (audit data is already exportable via existing retention pipelines; no demand surfaced).
- Bulk operations / selection (audit is read-only).
- Live updates / SSE.

## Routing & access control

| Route | Server guard |
|---|---|
| `/admin/audit` | `requirePermission("audit.read")` |
| `GET /api/app/admin/audit` | `audit.read` |
| `GET /api/app/admin/audit/event-types` | `audit.read` |
| `GET /api/app/admin/audit/{id}` | `audit.read` |

The sidebar entry is already gated on `audit.read` so direct URL access is the only ungated path; the route's server component checks the permission and falls through to the existing admin forbidden flow on denial.

## Backend API contract

### `GET /api/app/admin/audit` *(extended)*

Query parameters (all optional, all combinable):

| Param | Type | Notes |
|---|---|---|
| `targetUserId` | UUID | Matches `target_user_id = $` **or** (new table only) `params -> 'filters' -> 'ids' @> to_jsonb($::text)`. Mirrors `fetchUserAudit` so export-style events targeting a user via `?ids=` still surface. |
| `actorUserId` | UUID | Exact match on `actor_user_id`. Applies to both halves of the UNION. |
| `eventType` | string, repeatable | OR semantics: `action_type = ANY($::text[])` (new) / `event_type = ANY($::text[])` (legacy). |
| `result` | `success` \| `error` | New table only. When set, the legacy half of the UNION is excluded (legacy rows have NULL result and would otherwise contaminate an "errors only" filter). |
| `since` | RFC3339 | `created_at >= $`. Applied to both halves. |
| `before` | RFC3339 | Cursor: `created_at < $`. Applied to both halves. |
| `pageSize` | int | Default 50, capped at 200. |

Response:

```json
{
  "items": [AuditEventItem, ...],
  "nextCursor": "2026-05-06T13:42:01.123Z" | null
}
```

`nextCursor` = `created_at` (RFC3339Nano) of the last item iff `len(items) == pageSize`, else `null`. Items keep the existing `AuditEventItem` shape with actor/target hydration.

Implementation: the existing UNION query becomes a parameterized builder. Filter clauses are appended to both halves where applicable. `eventType` uses `ANY($N::text[])`. The `result` filter short-circuits the legacy half (the builder returns the new-only query). User hydration is unchanged.

### `GET /api/app/admin/audit/event-types` *(new)*

No query params. Returns:

```json
{ "types": ["roles.create", "users.bulk.role.grant", "users.suspend", ...] }
```

Sorted alphabetically. Source: `SELECT DISTINCT action_type FROM audit_events UNION SELECT DISTINCT event_type FROM app_audit_events` (legacy half conditional on `tableExists`). Cached 60s in-process (single timestamp + slice; sufficient for the dampening goal).

### `GET /api/app/admin/audit/{id}` *(new)*

Returns a single `AuditEventItem` with actor/target hydration. 404 if not found in either table. 403 without `audit.read`. Used by the Sheet when `?event=<id>` arrives but the event is not in the current list response (e.g. shared link from Slack while the recipient has different filters applied).

## Frontend data flow

URL is the source of truth (mirrors `/admin/users`). The page reads the URL, fetches, renders. Filter changes do `router.push` with the patched query string; the React tree never owns filter state independently.

### URL parameters

```
?q=<text>                   -- free-text reason/eventType filter, applied client-side only
?targetUserId=<uuid>
?actorUserId=<uuid>
?eventType=<type>           -- repeatable
?result=success|error
?since=24h|7d|30d|all       -- preset key; mapped to RFC3339 in the server component
?pageSize=50|100|200
?before=<rfc3339>           -- cursor; absent on page 1
?event=<id>                 -- selected event for the Sheet
```

Defaults: `since=7d`, `pageSize=50`. `result` and `eventType` default to "any" / unset.

### Server component (`page.tsx`)

Fetches in parallel via `Promise.allSettled`:

1. `getAdminAudit(query)` — list page
2. `getAdminAuditEventTypes()` — for filter pills
3. If `?event=<id>` is present and `id` is not in the list response → `getAdminAuditEvent(id)` to hydrate the Sheet on first paint

Renders `<AuditPage initialPage={...} eventTypes={...} initialEvent={...} initialQuery={...} permissions={...} />`. Errors on the single-event fetch are swallowed and surface as `initialEvent: null`; errors on the primary list fetch propagate to the existing admin error boundary.

### Client component (`audit-page.tsx`)

Mirrors `users-page.tsx` shape:

- `usePathname` / `useSearchParams` / `useRouter`
- `updateQuery(patch)` helper identical to users-page
- Tracks the selected event id from `?event=`; opening the sheet calls `updateQuery({ event: id })`, closing calls `updateQuery({ event: undefined })`
- The Sheet prefers an in-list row matching the selected id; falls back to `initialEvent`; otherwise client-fetches via `getAdminAuditEvent(id)` with a small skeleton in the meantime
- No selection state; no bulk toolbar

### Pagination footer

Same row shape as `users-page.tsx` footer:

- Left: `pageSize` selector (50 / 100 / 200), identical primitive
- Center: `[Newer ←]` enabled iff `?before=` is set (clearing it jumps to page 1); `[Older →]` enabled iff `nextCursor != null` (clicking sets `?before=<nextCursor>`)
- Right: `Showing N events` (length of current page)

No first/last/page-N-of-M buttons — log feed has no total.

### Free-text search (`?q=`)

Client-side only. Filters the rendered page by case-insensitive substring match against `eventType` or `reason`. Matches what the toolbar implies without forcing a backend column-search. Cleared independently of the structured filters.

## Components & visual layout

### File layout

```
apps/frontend/app/admin/audit/
  page.tsx                              -- server component, audit.read gate
  _components/
    audit-page.tsx                      -- client; URL state, sheet state, refresh wiring
    audit-toolbar.tsx                   -- search + filter popover
    audit-table.tsx                     -- read-only table; rows clickable
    audit-detail-sheet.tsx              -- right-side Sheet with full event detail
    user-combobox.tsx                   -- async user picker (lives here for now)
```

`apps/frontend/lib/rustlens-api.ts` gains `AdminAuditQuery`, `getAdminAudit`, `getAdminAuditEventTypes`, `getAdminAuditEvent`. Backend gets a new `admin_audit_handlers.go` for the two new endpoints; `admin_read_handlers.go` extends `ListAdminAudit`; `router.go` and `openapi.go` register the routes.

### `audit-toolbar.tsx`

Same `flex flex-wrap items-center gap-2` shell as `users-toolbar.tsx`. Search input on the left, `Filters` popover trigger with active-filter count `<Badge>`, clear-all `✕` button.

Filter popover (single column, sectioned):

- **TIME** — preset chips: `[Last 24h] [Last 7d*] [Last 30d] [All]`. One always selected (default `Last 7d`).
- **RESULT** — toggle pills: `[Any*] [Success] [Error]`. Mutually exclusive.
- **ACTOR** — `<UserCombobox onSelect onClear>` with chip-when-set
- **TARGET** — `<UserCombobox onSelect onClear>` with chip-when-set
- **EVENT TYPES** — `Clear` link in the section header; multi-select pills built from `eventTypes` prop, identical pattern to roles in users-toolbar

### `user-combobox.tsx`

shadcn `Popover` + `Command`:

- `<CommandInput>` with 250ms debounce
- `onValueChange` ≥ 2 chars → `searchAdminUsers(q)` (thin wrapper over `getAdminUsers({ q, pageSize: 10, status: "any" })`)
- `<CommandItem>` rows: avatar + display name + `@discordUsername`
- `onSelect` → `props.onSelect(userId, displayLabel, avatarUrl)` and close
- Selected state rendered by parent as a chip with `✕` calling `props.onClear()`

This component is generic enough to lift to `apps/frontend/components/admin/user-combobox.tsx` later, but ships co-located until a second consumer appears.

### `audit-table.tsx`

Same `<div className="overflow-hidden rounded-lg border"><Table>...` shell. No checkboxes. Columns:

| Column | Width | Content |
|---|---|---|
| Time | 12% | `<RelativeTime iso={createdAt} />` with full-timestamp tooltip |
| Actor | 18% | `<UserLink>` (avatar + display name); `—` when empty |
| Target | 18% | `<UserLink>`; `—` when empty |
| Event | 30% | `<code className="rounded bg-muted px-1 py-0.5 text-xs">{eventType}</code>` |
| Result | 8% | `<Badge variant=...>` (`error` → destructive, `success` → secondary, null → outline `—`) |
| Reason | 14% | truncated `text-xs text-muted-foreground` |

Rows: `cursor-pointer hover:bg-muted/50`. Click → `updateQuery({ event: id })`. Selected row gets `bg-accent`. Empty state varies: with filters → "No events match these filters. [Clear filters]"; without filters → "No audit events yet."

### `audit-detail-sheet.tsx`

shadcn `Sheet` (right side, `sm:max-w-xl`). Open iff `?event=<id>` is set. Closing calls `updateQuery({ event: undefined })`.

Body sections:

1. **Header** — `<code>` heading with `eventType`, result `<Badge>`, full timestamp + relative-time
2. **Actor / Target** — two-column grid; each shows `<UserLink>` or `—`; user IDs copy-on-click
3. **Reason** — when present; otherwise the section is omitted
4. **Payload** — `<pre className="text-xs bg-muted rounded p-3 overflow-auto max-h-96">` with `JSON.stringify(payload, null, 2)`
5. **Footer** — `Event ID: <code>{id}</code>` plus a `Copy link` button that copies `window.location.href` (already includes `?event=<id>`)

When the selected id resolves to no event (404 or fall-through), the sheet shows "Event not found." with a Close action. The URL param is left intact so the user can hand-edit it.

## Error handling

- **Server-component fetch failure** on the primary list — propagates to the existing admin error boundary (consistent with `/admin/users`).
- **Permission denied (403)** — handled by the existing admin layout's permission middleware; users without `audit.read` redirect to the admin forbidden view.
- **Sheet single-event 404** — caught at the server level via `Promise.allSettled`; `initialEvent: null` is passed; the sheet renders "Event not found."
- **Combobox search failure** — popover shows "Search unavailable — try again."; the rest of the toolbar continues to work.
- **Empty table** — toolbar-aware (see above).

## Caching & staleness

- All audit list / detail fetches use `cache: "no-store"` (the existing `backendFetch` default) — audit is real-time.
- Event-types endpoint cached 60s in-process (single mutex-protected timestamp + slice). Frontend popover does not auto-refresh; types changing rarely matters within a session.

## Testing

### Backend (`apps/backend/internal/http/admin_audit_handlers_test.go`)

- `ListAdminAudit` — each filter individually (`targetUserId`, `actorUserId`, `eventType` repeatable, `result`, `since`, `before`); two filters combined; cursor pagination across two pages produces the same total set; `result=error` excludes legacy rows; `pageSize` cap honored at 200; `targetUserId` matches both `target_user_id` and `params.filters.ids` semantics.
- `ListAdminAuditEventTypes` — returns sorted distinct union; legacy table absent path; cache-hit does not re-query.
- `GetAdminAuditEvent` — returns hydrated row; 404 on unknown id; 403 without `audit.read`.

### Frontend (`apps/frontend/app/admin/audit/_components/__tests__/`, Vitest)

- `audit-toolbar.test.tsx` — filter changes call `onChange` with correct shape; clear-all resets to defaults; eventType pills toggle; preset chips emit correct `since` key.
- `audit-table.test.tsx` — row click invokes `onSelect` with id; empty state varies with `hasFilters`; null-result renders `—`, not "success".
- `audit-detail-sheet.test.tsx` — renders payload JSON; `Copy link` hits clipboard; null result rendered correctly; "Event not found" path on null prop.
- `user-combobox.test.tsx` — debounce coalesces rapid input; selection chip clear works; keyboard navigation through results.

No e2e: the manual smoke test is deep-linking from `/admin/users/{id}` audit-section into `/admin/audit?targetUserId=...` and confirming the chip + filtered list render correctly.

## Out of scope (explicit YAGNI)

- Calendar date-range picker (preset chips only).
- Column sorts (cursor-only chronological).
- Total event count.
- Export-to-CSV from the audit page.
- Bulk selection or bulk operations on audit rows.
- Real-time / SSE updates.
- Filtering by payload field values (e.g. "events whose `params.role` is `superadmin`") — out of scope until a use case appears.
