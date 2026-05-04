# Admin RBAC management — Design

**Date:** 2026-05-04
**Status:** Approved

## Summary

Add a permission-gated `/admin/roles` page that lets superadmins (and any future holders of `admins.grant`) create, rename, delete, and edit the permission set of **custom** admin roles. The seven seeded **system** roles (`superadmin`, `moderator`, `infra_admin`, `guild_ops`, `user_support`, `user_admin`, `user_auditor`) remain locked: visible in the table but read-only — no rename, no permission changes, no delete.

Mutations route through the existing admin action dispatcher (`POST /api/app/admin/actions/{type}`) so they inherit audit logging, rate limiting, and required-reason capture for free. Four new actions are added (`roles.create`, `roles.update`, `roles.delete`, `roles.permissions.set`), all gated by `rbac.PermAdminsGrant` and all requiring a reason.

The existing `GET /api/app/admin/roles` endpoint is extended additively (adds `memberCount` per role) and one new read endpoint is introduced (`GET /api/app/admin/permissions`) so the UI can render the permission-checkbox grid against the canonical catalog.

A new "Access" nav group is added to the live admin sidebar (`components/dashboard-app-sidebar.tsx`) with a single permission-gated entry pointing to the new page.

## Permission gating

- **Page**: server-side `requirePermission("admins.grant")`. Non-holders are redirected to `/admin`.
- **Sidebar entry**: rendered only when the current session's `permissions` array contains `admins.grant`. Empty group → group is not rendered.
- **All four actions**: dispatcher rejects requests from actors lacking `admins.grant`.

In effect today this means **superadmin only** (the only seeded role granted `admins.grant` implicitly), but the gate is permission-based rather than role-based so a future custom role with `admins.grant` would automatically gain access.

## Backend reads

### New: `GET /api/app/admin/permissions`

Returns the static canonical permission catalog from `rbac.AllPermissions()`. No DB read.

```json
{
  "items": [
    { "key": "users.read",  "description": "View user profiles and the users list" },
    { "key": "users.edit",  "description": "Edit a user's profile fields (display name, avatar)" },
    ...
  ]
}
```

Mounted under `/api/app/admin/` in `internal/http/router.go` next to `GET /roles`. Inherits `requireSession` from the admin route group and is additionally gated by `admins.grant` inside the handler (consistent with how the four mutations gate themselves via the dispatcher).

### Modified: `GET /api/app/admin/roles`

Each item gains a `memberCount: number` field equal to `COUNT(admin_user_roles.user_id)` for that role. Implemented by switching `Store.ListRoles` to a `LEFT JOIN admin_user_roles GROUP BY admin_roles.id` query, or by a small dedicated method (e.g. `ListRolesWithMemberCount`) called from the handler — implementation choice deferred to the plan.

The change is purely additive; existing callers (the `getAdminRoles()` helper in `lib/rustlens-api.ts`, plus tests) keep working without modification.

```json
{
  "items": [
    {
      "id": "...",
      "key": "moderator",
      "name": "Moderator",
      "description": "...",
      "isSystem": true,
      "permissions": ["users.suspend", ...],
      "memberCount": 4
    }
  ]
}
```

## Backend mutations

Four new actions registered with the dispatcher. Each lives in its own file under `apps/backend/internal/admin/actions/` following the existing one-file-per-action convention (e.g. `roles_create.go`, `roles_create_test.go`).

| Action type             | Params                                              | Behavior |
|---|---|---|
| `roles.create`          | `{ key, name, description, permissions: string[] }` | Validates `key` non-empty, lowercase snake_case (`^[a-z][a-z0-9_]*$`), not already taken (`admin_roles.key` UNIQUE), and not equal to any seeded system-role key. Validates every permission key against `rbac.IsValidPermission`. Inserts the `admin_roles` row with `is_system=false` and the `admin_role_permissions` rows in one transaction. |
| `roles.update`          | `{ id, name, description }`                         | Loads role; rejects with permission-denied-style error if `is_system=true`. Updates `name`, `description`, bumps `updated_at`. **`key` is not in the param set — immutable post-create.** |
| `roles.delete`          | `{ id }`                                            | Rejects if `is_system=true`. `DELETE FROM admin_roles WHERE id=$1`. FK `ON DELETE CASCADE` already cleans up `admin_role_permissions` and `admin_user_roles` (verified in `apps/backend/internal/store/postgres.go`). |
| `roles.permissions.set` | `{ id, permissions: string[] }`                     | Rejects if `is_system=true`. Validates every key. In one transaction: `DELETE FROM admin_role_permissions WHERE role_id=$1`, then bulk INSERT the new set. |

All four actions:

- `Permission()` returns `rbac.PermAdminsGrant`.
- `RequiresReason()` returns `true`.
- `Dangerous()` returns `false` (no special "type-the-name-to-confirm" UX needed; the reason field is sufficient).
- `SensitiveParamKeys()` returns `nil` (no sensitive content in these params — keys/names/descriptions are inherently non-secret).
- `ResourceType()` returns `"role"`.
- `Validate()` returns `ValidatedParams{ResourceID: <role-id-or-key-as-string>}`. `TargetUserID` is left nil since these actions don't target a user.

A small shared helper, `assertNotSystemRole(ctx, pool, roleID) error`, is added (probably in `apps/backend/internal/admin/actions/guards.go` next to existing guards) to centralize the `is_system=true` check used by `update`, `delete`, and `permissions.set`.

### Audit shape

Every action writes one `audit_events` row via the existing dispatcher pipeline:

- `action_type` = the action's `Type()`
- `resource_type` = `"role"`
- `resource_id` = the role's UUID (or, for `create`, the new role's ID — set in `Execute`'s `ActionResult.ResourceID`)
- `params` = the JSON params blob (no redaction needed)
- `reason` = the admin-supplied reason
- `actor_user_id`, `actor_role_snapshot`, `ip_address`, `user_agent` = filled by the dispatcher

### Edge cases

- **Deleting a role currently assigned to users**: allowed. The cascade strips the assignments. The audit event captures the actor and reason; affected users are not separately audited (consistent with how `users.roles.revoke` would individually audit each).
- **Concurrent edits**: last-writer-wins. No optimistic locking. Acceptable given low write rate and the audit trail.
- **Self-lockout via the superadmin role**: not possible — `superadmin` is `is_system=true` and refused by the guard.
- **Removing the last superadmin user**: this concern is orthogonal to this design. The only path to that state is `users.roles.revoke` on the superadmin role, which already exists and isn't changed here.

## Frontend page

### Route

`apps/frontend/app/admin/roles/page.tsx`. Server component. Calls `requirePermission("admins.grant")` (already exists in `lib/session.ts`); non-holders are redirected to `/admin`.

Server-side, in parallel:

```ts
const [csrfToken, currentUser, roles, permissionsCatalog] = await Promise.all([
  getCsrfToken(),
  getCurrentUserSafe(),
  getAdminRoles(),           // existing — now returns memberCount
  getAdminPermissions(),     // new helper
]);
```

`getAdminPermissions()` is added to `lib/rustlens-api.ts` next to `getAdminRoles()` and hits `GET /api/app/admin/permissions` with `requireSession: true`.

Both `roles` and `permissionsCatalog` are passed into a single client component (`<RolesPage>`) which owns table state and drawer state.

### Page layout

Mirrors `app/admin/guilds/page.tsx` exactly: `SidebarProvider` + `AppSidebar` + `SiteHeader` + a content well with an `h1` ("Roles"), a sub-paragraph ("Create and manage admin roles. System roles are read-only."), and the table.

### Components

```
apps/frontend/app/admin/roles/
  page.tsx
  _components/
    roles-page.tsx          # client; owns drawer/dialog state
    roles-table.tsx         # client; columns + row actions
    role-drawer.tsx         # client; create/edit form (shadcn Sheet)
    role-permissions-grid.tsx  # client; checkbox grid grouped by namespace
    delete-role-dialog.tsx  # client; reason-required confirm
```

### Table

Columns:

| Column      | Source                                               |
|---|---|
| Name        | `role.name`                                          |
| Key         | `role.key` (mono font)                               |
| Description | `role.description` (truncated to one line w/ tooltip)|
| System      | Badge if `role.isSystem`                             |
| Permissions | `{role.permissions.length}`                          |
| Members     | `{role.memberCount}`                                 |
| Actions     | Row dropdown menu: **Edit**, **Delete**              |

Header has a primary "New role" button.

For system rows, the action menu shows **View** instead of **Edit**, and **Delete** is hidden.

### Drawer (Sheet)

A single shadcn `Sheet` reused for both create and edit, switched on a `mode: "create" | "edit"` prop:

- **Create mode**: empty form. Editable: `key` (with inline format hint and live duplicate-check disabled — handled server-side), `name`, `description`, full permission grid.
- **Edit mode (custom role)**: editable `name`, `description`, permission grid. `key` shown read-only (greyed input, copy-to-clipboard affordance).
- **Edit mode (system role)**: every field read-only with `disabled` styling. Banner at the top of the sheet: "System role — cannot be modified." No save button; only a Close button.

### Permission grid

Renders the catalog grouped by namespace prefix (`users.*`, `proxies.*`, `links.*`, `guilds.*`, `audit.*`, `admins.*`). Group headers are derived client-side by splitting each `key` on the first `.`. Each row in the grid is a checkbox with the permission's `key` (mono) and `description` (helper text).

When a row is locked (system role), the checkboxes render disabled but reflect current state.

### Reason capture

Both **Save** (create / update / permissions-change) and **Delete** open a small confirmation dialog with a required `reason` textarea before posting to the action dispatcher. Empty reason disables the confirm button. This matches the existing pattern used by other admin mutations.

For edit mode where both metadata and permissions change in the same drawer session, two actions are posted sequentially: `roles.update` first (if `name`/`description` changed), then `roles.permissions.set` (if the permission set changed). The reason is reused for both; a partial failure (one succeeds, one fails) surfaces a partial-success toast and the page refreshes to reflect actual state.

### Delete confirmation

Two-step: row action **Delete** → `<DeleteRoleDialog>` shows "Delete role 'X'? This will also remove it from N users." (using `memberCount` from the row) and a required reason textarea, then posts `roles.delete`.

### Client mutation flow

A small generic helper is added to `lib/rustlens-api.ts`:

```ts
export async function postAdminAction<T = unknown>(
  type: string,
  params: Record<string, unknown>,
  reason: string,
): Promise<T>
```

It POSTs to `/api/app/admin/actions/${type}` with the standard envelope `{params, reason}` and returns the parsed response. Four typed wrappers are added on top:

- `createAdminRole(input, reason)`
- `updateAdminRole(id, patch, reason)`
- `setAdminRolePermissions(id, permissions, reason)`
- `deleteAdminRole(id, reason)`

After a successful mutation the page calls `router.refresh()` to re-fetch the server-rendered table. No client-side optimistic updates.

## Frontend nav

The live admin sidebar is `components/dashboard-app-sidebar.tsx`. It currently hardcodes a single "Discord" `NavGroup`. The `ADMIN_NAV` array in `app-shared.tsx` is unused by the live sidebar and stays untouched (same as it was after the `/admin/guilds` change).

A second permission-gated nav group is added alongside the Discord group:

```
Access
  └ Roles  (KeyRoundIcon)  → /admin/roles  [gated by "admins.grant"]
```

The sidebar component does not currently know about session permissions, so the prop chain is extended:

- `apps/frontend/app/admin/roles/page.tsx`, `apps/frontend/app/admin/guilds/page.tsx`, and `apps/frontend/app/admin/page.tsx` all pass `permissions={session.permissions}` into `<AppSidebar>`.
- `app/admin/_components/app-sidebar.tsx` forwards it to `DashboardAppSidebar`.
- `DashboardAppSidebar` accepts an optional `permissions?: string[]` prop (defaults to `[]`) and uses it to filter admin nav items. The "Access" group renders only if at least one item passes the filter.

## Tests

### Backend

Following existing `actions/*_test.go` conventions:

- `roles_create_test.go` — happy path; duplicate key rejected; system-key collision rejected; bad permission key rejected; missing `admins.grant` rejected by dispatcher; reason required.
- `roles_update_test.go` — happy path on custom role; system role rejected; non-existent ID rejected.
- `roles_delete_test.go` — happy path on custom role; cascade removes assignments and role-permissions; system role rejected.
- `roles_permissions_set_test.go` — happy path adds + removes permissions in one call; idempotent re-set yields no diff but still audits; bad permission key rejected; system role rejected.
- One small handler test for `GET /api/app/admin/permissions` (auth + permission gated, returns the static list).
- Additive assertion in the existing `admin_read_handlers_test.go` for the new `memberCount` field.

Each action test asserts that exactly one `audit_events` row is written with the expected `action_type`, `resource_type="role"`, `resource_id`, and `reason`.

### Frontend

None. The existing admin pages have no component tests; the route's behavior is mostly thin glue over the action dispatcher (which is heavily covered server-side).

## OpenAPI

Add to `internal/http/openapi.go`:

- `GET /api/app/admin/permissions` — request/response shape.
- `POST /api/app/admin/actions/roles.create`, `roles.update`, `roles.delete`, `roles.permissions.set` — request/response shapes (or document them under the existing generic action endpoint, matching how existing action types are documented today — implementation choice deferred to the plan after checking what's already there).

## Documentation

Update the root `AGENTS.md` (symlinked as `CLAUDE.md`) and `apps/backend/AGENTS.md` to mention:

- The new `/admin/roles` page in the frontend section, in the same style as the existing `/admin/guilds` line.
- The new `GET /api/app/admin/permissions` endpoint in the backend "Key patterns" list.
- The four new action types under the admin RBAC section.

## Out of scope

- Editing the canonical permission catalog (still hardcoded in `rbac/permissions.go`; adding new permissions remains a code change).
- Per-user role assignment UI (already happens elsewhere via `users.roles.grant`/`revoke`; not part of this page).
- Renaming a role's `key` (immutable post-create, intentionally).
- Diff-style audit ("before/after permissions" instead of just "after"). The current `params` blob captures the new state; reconstructing the prior state from prior audit events is sufficient.
- Soft-delete / undo for deleted roles. Deletion is permanent.
- Bulk actions (delete multiple roles at once, copy permissions from one role to another).
