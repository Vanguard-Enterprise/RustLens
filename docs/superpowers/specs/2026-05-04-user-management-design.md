# User Management — design

**Date:** 2026-05-04
**Branch:** `feature/user-management`
**Scope:** Admin-only user management UI surfaces at `/admin/users` and `/admin/users/[userId]`, plus auditing for the existing CSV export endpoint.

## Goal

Build the admin user management page so internal admins can find users, assign and revoke admin roles, manage account state (suspend / delete / restore / purge), edit profile fields, and export CSV — all using backend actions that already exist. The headline use case is **applying roles to users manually**; everything else is built on the same scaffolding because we're already there.

## Existing context

The backend already exposes everything this feature needs:

- `GET /api/app/admin/users` — list with `q`, `role[]`, `status`, `discord`, `sort`, `page`, `pageSize` (and an `ids=` override).
- `GET /api/app/admin/users/{id}` — detail with `adminRoles` and recent audit.
- `GET /api/app/admin/users/{id}/audit` — paginated user audit history.
- `GET /api/app/admin/users/export.csv` — streaming CSV (currently writes **zero** audit rows).
- Action dispatcher actions: `users.edit`, `users.suspend`, `users.unsuspend`, `users.delete`, `users.restore`, `users.purge`, `users.roles.grant`, `users.roles.revoke`, `users.bulk` (limited to suspend/unsuspend/roles.grant/roles.revoke).
- Permissions: `users.read`, `users.edit`, `users.suspend`, `users.delete`, `users.export`, `users.audit.read`, `users.roles.assign`, `admins.grant`.

Frontend has the typed API helpers (`getAdminUsers`, `getAdminUser`, `getAdminUserAudit`, `postAdminAction`) and the `ADMIN_ACTIONS` manifest entries for grant/revoke. The sidebar nav already links to `/admin/users` and `UserLink` already routes to `/admin/users/{userId}` — the pages themselves don't exist yet.

The frontend action manifest also includes placeholders for actions that **do not exist on the backend**: `users.password.forceReset`, `users.force_unlink_steam`, `users.email.*`, `users.invite`, `users.connections.unlink`. These stay as future work and are not surfaced in this UI.

## Non-goals

- Wiring placeholder actions that lack a backend handler.
- Inviting brand-new users (login is Discord OAuth only; users always pre-exist via OAuth).
- Live updates / SSE on the list page.
- "Select all matching filter" across pages — selection is bounded to the current page to prevent accidental 10k-row bulk operations.
- Self-service surfaces under `/dashboard`.

## Routing & access control

| Route | Server guard |
|---|---|
| `/admin/users` | `requirePermission("users.read")` |
| `/admin/users/[userId]` | `requirePermission("users.read")` |

Sidebar nav (`apps/frontend/components/app-shared.tsx`) entry for `/admin/users` changes from `permissions: ["users.suspend", "users.force_unlink_steam"]` to `permissions: ["users.read"]` so view-only admins see the link.

Per-action UI is gated by `session.permissions`:

| UI surface | Required permission |
|---|---|
| Roles section visible | `users.roles.assign` |
| Profile "Edit" button | `users.edit` |
| Bulk Suspend / Unsuspend | `users.suspend` |
| Bulk Grant / Revoke role | `users.roles.assign` |
| Bulk Export selected, top-right Export filtered | `users.export` |
| Suspend / Unsuspend buttons | `users.suspend` |
| Delete / Restore buttons | `users.delete` |
| Purge button | `admins.grant` |

Backend remains the source of truth for every check; the UI gating is purely to avoid showing buttons that would 403.

## List page (`/admin/users`)

### Layout

Toolbar row:

- Search input (debounced 300ms, written to URL as `q`).
- Status pills: Active (default) / Suspended / Deleted / Any → URL `status`.
- Role multi-select dropdown (loaded from `getAdminRoles()`; multi → repeated `role=` query params).
- Sort dropdown: Last login (default desc), Created, Display name; toggleable asc/desc → URL `sort` / `-sort`.
- Top-right "Export filtered" button (visible if actor has `users.export`).

Data table columns:

| Column | Source |
|---|---|
| ☐ checkbox | row select (page-bounded) |
| User | avatar + display name + Discord username (links to `/admin/users/{id}`) |
| Roles | role chips (badge variant per role; `superadmin` styled distinctly) |
| Status | active / suspended / deleted badge |
| Last login | relative time |
| Created | relative time |

Pagination: `pageSize` options `[10, 20, 30, 40, 50]`, default 50 (matches backend default). `page` and `pageSize` in URL.

All filter, sort, page, and pageSize state lives in URL search params so browser back/forward and shareable links work, mirroring `/admin/guilds`.

### Selection + bulk toolbar

- Header checkbox toggles select-all on the **current page only**.
- When ≥1 row is selected, a sticky bulk-action toolbar appears above the table:
  `[N selected] [Suspend] [Unsuspend] [Grant role…] [Revoke role…] [Export selected] [Clear]`
- Each button opens a dialog. Grant / Revoke open a dialog with role picker + shared reason. The role picker excludes `superadmin` (too consequential to grant in bulk). Suspend / Unsuspend open a dialog with shared reason.
- Dialog action calls `users.bulk` once. Result envelope `{succeeded, failed, results[]}` renders in a results panel showing per-row outcomes; user closes with "Done."
- Each bulk button hides if the actor lacks the relevant permission.

### Export buttons

- "Export filtered" hits `/api/app/admin/users/export.csv` with the current filter querystring.
- "Export selected" hits the same endpoint with `ids=<comma-joined>`.
- Browser handles the download. After triggering the download, the button briefly shows a checkmark + "Logged to audit" microcopy. The `users.export.started` row is written before any bytes leave the server, so the microcopy is accurate even if the user cancels the download.
- "Export filtered" button is disabled when total row count exceeds the export cap (currently 100 000); the button text shows the count.

## Detail page (`/admin/users/[userId]`)

Single-page stacked sections, top to bottom. No tabs.

### Identity header

Avatar, display name, Discord username + global name, copyable `app_users.id`, copyable Discord ID, status badges (active / suspended / deleted; suspended badge shows `suspendReason` next to it when present), member-since and last-login timestamps. No actions.

### Roles section (the headline feature)

- Title + short helper text: "Admin roles control which actions this user can perform."
- Current admin roles rendered as chips. Hover reveals the role description (from `getAdminRoles()`). The "×" on each chip opens the **Revoke role dialog**.
- Below the chips: an "Add role…" combobox listing roles the user doesn't yet have. Each option shows role name + description + member count. Selecting a role opens the **Grant role dialog**.
- Both dialogs require a reason (backend-enforced) and call `postAdminAction("users.roles.grant" | "users.roles.revoke", {userId, roleKey}, reason)`. On success, refresh via `router.refresh()`.
- **Self-revoke guard:** if `userId === session.userId`, the "×" on the actor's own admin roles is hidden with tooltip "You can't revoke your own admin roles." Backend last-superadmin guard remains the authoritative protection.
- Granting `superadmin` requires `admins.grant`. Combobox surfaces `superadmin` greyed out with "Requires admins.grant" tooltip when actor lacks it. Backend check remains source of truth.
- Whole section hides for actors lacking `users.roles.assign`.

### Profile section

- Read-only by default: display name, avatar URL.
- "Edit profile" button (visible if actor has `users.edit`) toggles into an edit form. "Save" calls `users.edit` (no reason required per backend). Cancel reverts.

### Recent audit section

- Renders the 30 most recent audit rows for this user via `getAdminUserAudit({ limit: 30 })`.
- Each row: actor (`UserLink` with avatar), action type, result badge (success / error), reason if present, params summary, timestamp. Style matches existing `/admin/audit` rows.
- "View full history" link sends the user to `/admin/audit?targetUserId=<id>`.

### Danger zone section

Always last. Collapsed by default; expands to show:

| Action | Visibility | Inputs | Backend |
|---|---|---|---|
| Suspend | actor has `users.suspend` && user not suspended | reason (required) | `users.suspend` |
| Unsuspend | actor has `users.suspend` && user suspended | confirmation | `users.unsuspend` |
| Delete | actor has `users.delete` && user not deleted | reason + typed display-name confirmation | `users.delete` |
| Restore | actor has `users.delete` && `deletedAt != null` | confirmation | `users.restore` |
| Purge | actor has `admins.grant` | reason + typed user-id confirmation | `users.purge` |

After any successful danger-zone action, the page refreshes via `router.refresh()` so the identity header, roles section, and audit section all reflect the new state.

## CSV export auditing (backend change)

The export handler at `apps/backend/internal/http/admin_export_handlers.go` currently writes no audit entries. Heavy auditing means **two paired audit events per export** so partial / aborted / errored exports are still recorded.

### `users.export.started`

Written *before* streaming begins, after permission check + count succeed but before any bytes are written.

| `audit_events` field | value |
|---|---|
| `action_type` | `users.export.started` |
| `actor_user_id` | session user |
| `actor_role_snapshot` | resolved roles at request time |
| `resource_type` | `user_export` |
| `resource_id` | UUID generated for this export (`exportId`) |
| `target_user_id` | the single user ID iff `ids=` resolves to exactly one ID, otherwise NULL |
| `params` | `{exportId, filters: {q, role[], status, discord, sort, ids[]}, expectedRowCount, ipAddress, userAgent}` |
| `result` | `success` (the *attempt* succeeded; completion is a separate row) |
| `ip_address` / `user_agent` | populated from request |

### `users.export.completed`

Written *after* the stream finishes (or fails).

| `audit_events` field | value |
|---|---|
| `action_type` | `users.export.completed` |
| `resource_id` | same `exportId` as the started row (correlates the pair) |
| `params` | `{exportId, rowsWritten, bytesWritten, durationMs}` |
| `result` | `success` (stream finished cleanly) or `error` (timeout / connection abort / SQL error) |
| `error_message` | populated on `error` |

### Per-user audit visibility

For the user-detail "Recent audit" view to show `users.export.started` events that targeted a user via `ids=`, the backend audit query for a given user needs to OR-match:

- `target_user_id = $userId`, OR
- `params -> 'filters' -> 'ids' ? $userId` (JSONB array contains the userId).

This is a single SQL change in the existing user-audit handler. Filter-only exports are not surfaced on individual user pages — only on the global `/admin/audit` feed and the actor's own audit trail.

### Why no reason field on export

The export endpoint is `GET` and is invoked by toolbar buttons (and possibly automation). Forcing a reason on every CSV download would push admins toward sharing exported files instead of running fresh ones. The deterrent here is that **every** export is permanently logged with full context, the `users.export` permission is tightly granted, and the audit pair makes silent downloads impossible.

## Frontend file layout

```
apps/frontend/app/admin/users/
  page.tsx                            # Server: requirePermission("users.read"); fetch initial users page + roles
  [userId]/page.tsx                   # Server: requirePermission("users.read"); fetch user + audit + roles
  _components/
    users-page.tsx                    # Client: orchestrates filter state ↔ URL search params, owns selection state
    users-toolbar.tsx                 # Search, status pills, role multi-select, sort dropdown, "Export filtered"
    users-table.tsx                   # Data table with checkbox column, row-click → detail
    users-bulk-toolbar.tsx            # Sticky bulk-action bar shown when ≥1 selected
    bulk-suspend-dialog.tsx           # Reason-required dialog → users.bulk(suspend|unsuspend)
    bulk-role-dialog.tsx              # Role picker + reason → users.bulk(roles.grant|roles.revoke)
    export-button.tsx                 # "Export filtered" / "Export selected" — hits CSV endpoint, shows logged-to-audit microcopy
    user-detail-page.tsx              # Client orchestrator for the detail view
    identity-header.tsx
    roles-section.tsx                 # Chips + "Add role…" combobox
    grant-role-dialog.tsx             # Single grant; reason-required
    revoke-role-dialog.tsx            # Single revoke; reason-required
    profile-section.tsx               # Read/edit profile
    audit-section.tsx                 # Last 30 audit rows + "View full history" link
    danger-zone.tsx                   # Suspend/Unsuspend/Delete/Restore/Purge entry points
    suspend-dialog.tsx
    delete-dialog.tsx
    restore-dialog.tsx
    purge-dialog.tsx
```

### Library additions to `apps/frontend/lib/rustlens-api.ts`

- `assignAdminRole(userId, roleKey, reason)` → wraps `postAdminAction("users.roles.grant", …)`
- `revokeAdminRole(userId, roleKey, reason)` → wraps `postAdminAction("users.roles.revoke", …)`
- `bulkUserAction(action, userIds, params, reason)` → wraps `postAdminAction("users.bulk", …)`
- `editAdminUser(userId, patch, reason?)` → wraps `postAdminAction("users.edit", …)`
- `suspendUser` / `unsuspendUser` / `deleteUser` / `restoreUser` / `purgeUser` thin wrappers
- `getAdminUsersExportUrl(query | { ids })` — returns the URL for the toolbar buttons (returned, not fetched, so the browser handles the download natively).

The wrappers exist so dialog components have a typed call signature instead of stringly-typed `postAdminAction` calls everywhere.

## Testing

### Frontend

- vitest + RTL for each dialog component, asserting the right `postAdminAction` payload is sent (matches the existing `apps/frontend/components/admin/__tests__/admin-action-button.test.tsx` pattern).
- vitest for `users-page.tsx` URL ↔ filter-state synchronization.
- vitest for selection / bulk-toolbar visibility.

### Backend

- Extend `admin_export_handlers_test.go` to assert:
  - The `users.export.started` row is written before any bytes go out.
  - The `users.export.completed` row is written after a successful stream and shares the same `exportId`.
  - The `error` completion row is written on a timeout / SQL-error path with `error_message` populated.
  - When `ids=<single-uuid>` is supplied, `target_user_id` on the started row is set.
  - When `ids=<a,b,c>` is supplied, `params.filters.ids` contains all three and `target_user_id` is NULL.
- Extend the user-audit handler tests to verify the JSONB `params.filters.ids ? $userId` predicate matches `users.export.started` rows correctly on the per-user feed.

## Acceptance checklist

- [ ] `/admin/users` lists users with search, status, role-multi, sort, and pagination working from URL state.
- [ ] Per-page selection drives a sticky bulk toolbar with Suspend / Unsuspend / Grant / Revoke / Export selected.
- [ ] "Export filtered" and "Export selected" produce a CSV download and write paired audit events.
- [ ] Clicking a row goes to `/admin/users/{id}`.
- [ ] Detail page shows identity header, Roles section, Profile section, Recent audit, and Danger zone.
- [ ] Roles section grants and revokes single roles via reason-required dialogs and refreshes on success.
- [ ] Self-revoke and `superadmin` UI guards behave as specified.
- [ ] Danger-zone actions (suspend / unsuspend / delete / restore / purge) work with the right confirmation shapes and refresh the page on success.
- [ ] Sidebar nav permission for `/admin/users` is `users.read`.
- [ ] Export endpoint writes `users.export.started` + `users.export.completed` audit rows on every call.
- [ ] User-detail audit feed surfaces `users.export.started` rows for exports that targeted that user via `ids=`.
- [ ] All UI surfaces hide buttons the actor lacks permissions for.
