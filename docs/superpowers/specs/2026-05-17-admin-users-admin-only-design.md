# Admin Users page: admin-only view + Add user flow

**Date:** 2026-05-17
**Scope:** `apps/frontend/app/admin/users/*`
**Backend changes:** none

## Goal

`/admin/users` should list only users who hold at least one role that grants admin permissions, and provide an "Add user" button that lets an authorized admin search any user (including non-admin and suspended/deleted) and grant them a role in a single dialog.

This page becomes the "admin members" view; the broader user search is reserved for the Add user dialog.

## Definitions

- **Admin role**: an `AdminRoleItem` where `permissions.length > 0`.
- **Admin user**: an account assigned to at least one admin role.
- **Tag-only role**: a role with an empty `permissions` array. Users who only hold tag-only roles are excluded from the page.

## Architecture

### Server component: `app/admin/users/page.tsx`

1. `requirePermission("users.read")` (unchanged).
2. `getAdminRoles()` first (own request, awaited).
3. Compute `adminRoleKeys = roles.filter(r => r.permissions.length > 0).map(r => r.key)`.
4. **If `adminRoleKeys.length === 0`** → render `UsersPageEmptyState` (no call to `getAdminUsers`).
5. **Otherwise** → compute the effective `role[]` filter via `effectiveRoleFilter(urlRoles, adminRoleKeys)`:
   - If `urlRoles` is empty → use `adminRoleKeys`.
   - If `urlRoles` is non-empty → intersection with `adminRoleKeys`.
   - If intersection is empty → fall back to `adminRoleKeys` (never let the URL widen the page to non-admin users).
6. Call `getAdminUsers({ ...query, role: effectiveRoles })` and `getCurrentUserSafe()` and `getCsrfToken()` as today (parallel `Promise.all`).
7. Pass the admin-roles subset (not all roles) as the `roles` prop to `UsersPage` — the toolbar Roles popover should only ever surface admin roles.

`effectiveRoleFilter` is a pure helper colocated with `page.tsx` (or `_components/role-filter.ts`); unit-tested independently.

### Client component: `_components/users-page.tsx`

- Adds an **Add user** button to the header row (visible only when `permissions.includes("users.roles.assign")`).
- Owns the dialog open state.
- On dialog success: `router.refresh()` and close.
- No changes to selection, pagination, or the bulk toolbar.

### New component: `_components/add-admin-user-dialog.tsx`

Two-stage dialog. State machine: `stage: "search" | "role"`.

**Stage 1 — search**:
- A `CommandInput` + debounced search (mirror pattern from `audit/_components/user-combobox.tsx`: 120ms debounce, per-instance result cache, request-id guard for stale responses).
- Calls `searchUsersForGrantAction(q)` server action.
- Renders results as a `Command` list with avatar + display name + `@discordUsername`.
- Selecting a row sets `selectedUser` and advances `stage` to `"role"`.

**Stage 2 — role + reason**:
- Header shows the selected user with a small "Change" button that returns to stage 1.
- Role picker — same Combobox-in-Popover pattern as `grant-role-dialog.tsx` (search by name/key/description, disable `superadmin` when `canGrantSuperadmin === false`).
- Reason `Input` (required).
- "Grant role" button calls `grantRoleAction(selectedUser.id, roleKey, reason)`.
- On success: close dialog, parent calls `router.refresh()`.
- On error: show inline; keep dialog open.

**Reset behavior**: closing the dialog (via Cancel, ESC, or outside-click) resets stage, query, selectedUser, roleKey, reason, and error.

### New component: `_components/users-page-empty-state.tsx`

Server-renderable. Copy:

> **No admin roles defined yet**
>
> Create a role with permissions in /admin/roles to start adding users to the admin team.

CTA: link button to `/admin/roles`. The link is unconditional — the roles page already enforces its own permission, so we don't duplicate that check here.

### Server actions: `_components/../actions.ts`

Add one new action:

```ts
export async function searchUsersForGrantAction(q: string): Promise<AdminUserItem[]> {
  await requirePermission("users.roles.assign");
  return searchAdminUsers(q);
}
```

`searchAdminUsers` already pins `status: "any"` and `pageSize: 10`, so suspended/deleted users surface.

No other actions change. `grantRoleAction` already gates on `users.roles.assign` and revalidates `/admin/users` + the user detail path.

### Toolbar (`_components/users-toolbar.tsx`)

No structural change. It receives the admin-roles subset via its `roles` prop, so the Roles popover naturally lists only admin roles.

## URL contract

- `?role=<key>` — still honored, but intersected with admin roles. URL-hacking to add tag-only roles is silently dropped.
- All other query params (`q`, `status`, `sort`, `page`, `pageSize`) unchanged.

## Permissions

| Capability | Permission |
| --- | --- |
| View the page | `users.read` (unchanged) |
| See the "Add user" button | `users.roles.assign` |
| Open and submit the dialog | `users.roles.assign` (enforced server-side by both actions) |
| Grant `superadmin` from the dialog | `admins.grant` (mirrors existing `GrantRoleDialog` logic) |

`canGrantSuperadmin` is computed in `page.tsx` from `session.permissions` and passed to `UsersPage` → `AddAdminUserDialog`.

## Error handling

- **Zero permissioned roles**: empty state, no user fetch.
- **URL with non-admin role(s) only**: fall back to admin-role default; no error shown.
- **Search action fails**: dialog stage 1 shows an inline retry message, identical to the audit combobox pattern.
- **Grant fails** (duplicate role, network, validation): dialog stage 2 shows the error inline; user can edit and resubmit.
- **Permission lost mid-session**: server actions throw → dialog shows the error; page refresh will hide the button.

## Testing

1. **Unit — `effectiveRoleFilter`** (`page.tsx` helper):
   - empty URL + 3 admin roles → returns all 3.
   - URL has `[a, b]`, admin set is `[a, c]` → returns `[a]`.
   - URL has `[d]`, admin set is `[a, b]` → returns `[a, b]` (fallback).
   - URL empty + admin set empty → returns `[]` (caller branches to empty state before this).

2. **Component — `add-admin-user-dialog.test.tsx`**:
   - Dialog opens in `search` stage.
   - Typing triggers debounced search; results render; selecting a user advances stage.
   - In `role` stage, "Change" returns to `search` and clears `selectedUser`.
   - Role picker disables `superadmin` when `canGrantSuperadmin === false`.
   - Submit calls `grantRoleAction` with the selected user id, role key, and trimmed reason.
   - On success, `onOpenChange(false)` is called.
   - On error, dialog stays open and surfaces the message.

3. **Component — `users-page-empty-state.test.tsx`**: renders copy and CTA link to `/admin/roles`.

4. **Existing tests** (`grant-role-dialog.test.tsx`, `bulk-role-dialog.test.tsx`): unchanged.

## Non-goals

- No backend changes. No new endpoint, no new query param.
- No bulk "Add multiple users" flow.
- No change to the `[userId]` detail page.
- No change to the existing `Roles` filter UX beyond it surfacing fewer roles.
- No "remove from admin" affordance on this page; revocation continues to happen on the user detail page.

## Out of scope / follow-ups (not in this spec)

- Consider a `?adminOnly=false` escape hatch later if real workflows need it; for now, the user detail page and the audit page already let you view non-admin users by ID.
- Telemetry for the new dialog can be added in a separate change.

## File touch list

**New:**
- `apps/frontend/app/admin/users/_components/add-admin-user-dialog.tsx`
- `apps/frontend/app/admin/users/_components/users-page-empty-state.tsx`
- `apps/frontend/app/admin/users/_components/__tests__/add-admin-user-dialog.test.tsx`
- `apps/frontend/app/admin/users/_components/__tests__/users-page-empty-state.test.tsx`
- `apps/frontend/app/admin/users/_components/role-filter.ts` — exports `effectiveRoleFilter` (pure helper).
- `apps/frontend/app/admin/users/_components/__tests__/role-filter.test.ts`

**Modified:**
- `apps/frontend/app/admin/users/page.tsx` — admin-role derivation, branching, intersected role filter, pass admin-roles subset.
- `apps/frontend/app/admin/users/_components/users-page.tsx` — Add user button, dialog wiring.
- `apps/frontend/app/admin/users/actions.ts` — new `searchUsersForGrantAction`.

**Unchanged:**
- `apps/frontend/lib/rustlens-api.ts` (reuses `searchAdminUsers`, `getAdminUsers`, `getAdminRoles`, `grantAdminUserRole`).
- All backend code.
