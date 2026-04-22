# Admin Users Filter Consolidation — Design

**Date:** 2026-04-22
**Scope:** `/admin/users` page, `UsersFilterBar` component
**Status:** Approved design, ready for implementation planning

## Problem

The `/admin/users` filter bar currently exposes five separate controls inline: a Role dropdown (multi-select), plus four `Select` dropdowns for Status, Discord, Steam, and Verified. The bar is crowded, visually noisy, and makes it hard to see the underlying table when more filters are added. We want a single unified Filter control that houses all of these, with the filter options presented as segmented tri-state toggles instead of native selects.

## Goals

- Collapse the four `Select` filters (Status, Discord, Steam, Verified) and the Role multi-select into a single Filter popover.
- Use shadcn `ToggleGroup` (tri-state / quad-state segmented control) for the four tri-state filters.
- Use a nested shadcn `DropdownMenu` with checkbox items for the Role multi-select inside the popover.
- Preserve URL-search-params as the source of truth for filter state.
- Preserve the existing search input and Invite button as-is, outside the popover.

## Non-Goals

- Adding any new filter beyond those present today. No "etc." filter.
- Changing the admin users API or query parameter names.
- Reworking the users table, pagination, or bulk toolbar.
- Mobile-specific redesign (we accept that a 360px popover is acceptable on desktop-first admin surfaces; narrow-screen adjustments can be handled later if needed).
- Internationalization changes.

## UX

### Top bar (outside the popover)

```
[🔍 Search users…]   [⎕ Filter (3)]                                  [+ Invite]
```

- Search input: unchanged.
- Filter button: single outline button with filter icon, label `Filter`, and a `Badge` showing the count of active filters housed inside the popover. The badge count does NOT include the search term (search has its own visible control).
- Invite button: unchanged, pinned right.
- The old standalone `Clear` button is removed from the top bar.

**Badge count formula:**

```
badgeCount =
    (status !== undefined ? 1 : 0)
  + (discord !== undefined ? 1 : 0)
  + (steam !== undefined ? 1 : 0)
  + (emailVerified !== undefined ? 1 : 0)
  + roles.length
```

The badge is hidden when `badgeCount === 0`.

### Popover contents

Approximately 360px wide. Vertical stack with labeled rows.

```
┌─ Filters ─────────────────────────┐
│                                    │
│  Role       [Select roles ▾]  (2)  │  ← nested DropdownMenu, checkboxes
│                                    │
│  ─────────────────────────────     │
│                                    │
│  Status                            │
│  [Any | Active | Suspended | Del.] │  ← ToggleGroup, type="single"
│                                    │
│  Discord                           │
│  [Any | Linked | Unlinked]         │
│                                    │
│  Steam                             │
│  [Any | Linked | Unlinked]         │
│                                    │
│  Verified                          │
│  [Any | Verified | Unverified]     │
│                                    │
│  ─────────────────────────────     │
│                          [Clear]   │
└────────────────────────────────────┘
```

### Role nested selector

Inside the popover, Role is rendered as its own `DropdownMenu`:

- Trigger: outline `Button` labeled `Select roles` with a small count badge when roles are selected.
- Content: `DropdownMenuCheckboxItem` for each role, identical behavior to today (each toggle appends/removes a `role` URL param).

Opening the Role dropdown opens a second floating layer above the popover. The popover does not close when the Role dropdown opens.

### Toggle groups

- `type="single"` (radio-like semantics — exactly one option selected at any time).
- Initial selection per filter:
  - Status: `Any`
  - Discord: `Any`
  - Steam: `Any`
  - Verified: `Any`
- When the user selects `Any`, the corresponding URL param is removed (`updateParam(key, null)`).
- When the user selects any other value, the URL param is set.

### Clear button

- Lives at the bottom of the popover, right-aligned, as a `ghost`-variant button.
- Clicking it clears all filters AND the search input, and navigates to `${pathname}` with no query string (same effect as today's Clear button).
- Disabled (via the `disabled` prop) when no filters are active AND the search input is empty.

## State and URL contract

URL search params remain the single source of truth. The component is a thin controller over `useRouter` / `useSearchParams`, matching today's pattern.

**URL param mapping (unchanged from today):**

| UI control        | URL param       | Values                                 |
|-------------------|-----------------|----------------------------------------|
| Search input      | `q`             | string                                 |
| Role multi-select | `role` (repeat) | role key(s)                            |
| Status toggle     | `status`        | `active` / `suspended` / `deleted`     |
| Discord toggle    | `discord`       | `linked` / `unlinked`                  |
| Steam toggle      | `steam`         | `linked` / `unlinked`                  |
| Verified toggle   | `emailVerified` | `yes` / `no`                           |

`Any` is represented by the absence of the corresponding param, not by `any=…`.

## Behavior change: default status

The current behavior defaults to `status=active` when the URL omits `status`. This is enforced server-side in `app/admin/users/page.tsx#parseQuery`, where `status` falls back to `"active"`.

With the new design, `Any` is the neutral default. On a fresh load with no query string, the page shows all users including suspended and deleted. This is an intentional UX change the user approved.

**Change required in `page.tsx#parseQuery`:**

```ts
// Before
const status = (first(sp.status) as AdminUsersQuery["status"]) ?? "active";

// After
const status = first(sp.status) as AdminUsersQuery["status"] | undefined;
```

If the `AdminUsersQuery["status"]` type excludes `undefined`, we either:
- (a) make `status` optional in `AdminUsersQuery` and pass `undefined`, or
- (b) pass no `status` key into the query object at all.

This will be resolved during planning after checking the type in `@/lib/rustlens-api`.

## Components

### New shadcn components to add

Run via shadcn CLI inside `apps/frontend`:

- `popover`
- `toggle`
- `toggle-group`

### Reused components

- `dropdown-menu` (Role nested selector)
- `button`
- `badge`
- `separator`
- `input-group` (search)

### Removed from this component

- `select` — no longer imported in `users-filter-bar.tsx` after the refactor.

## Accessibility

- Each `ToggleGroup` is wrapped with a visible `<label>` that uses `id` + `aria-labelledby` on the group.
- `ToggleGroup` uses `type="single"` for radio-like semantics.
- Arrow keys navigate within a `ToggleGroup`; Tab moves between groups.
- Filter button trigger has `aria-label="Filter users"`.
- Escape closes the popover and returns focus to the Filter trigger.
- Clear button is a real `<button>`, reachable via Tab.

## Tests

`apps/frontend/app/admin/users/_components/__tests__/users-filter-bar.test.tsx` currently has two tests:

1. **"pressing Enter in the search field resets page and writes the URL"** — still passes unchanged; search input is untouched.
2. **"Clear resets the URL to the base pathname"** — must be updated because Clear now lives inside the popover. The updated test opens the popover, then clicks Clear.

### New tests to add

- Opening the popover, selecting `Suspended` on the Status toggle group, asserts URL contains `status=suspended`.
- Opening the popover, selecting `Any` on the Status toggle group (after it was set to `suspended`), asserts URL no longer contains `status`.
- Opening the popover, opening the Role nested dropdown, toggling a role, asserts URL contains `role=<key>`.
- Filter button badge reflects correct count (e.g. with `status=suspended&role=admin` in URL, badge shows `2`).
- Clear button inside the popover resets URL to base pathname and clears the search input state.

All tests use the existing `vitest` + `@testing-library/react` setup and the existing `next/navigation` mock pattern.

## Files touched

- `apps/frontend/app/admin/users/_components/users-filter-bar.tsx` — full refactor.
- `apps/frontend/app/admin/users/page.tsx` — one-line change to `parseQuery` to drop the `"active"` default for `status`.
- `apps/frontend/app/admin/users/_components/__tests__/users-filter-bar.test.tsx` — update Clear test, add new tests.
- `apps/frontend/components/ui/popover.tsx` — new (added via shadcn CLI).
- `apps/frontend/components/ui/toggle.tsx` — new (added via shadcn CLI).
- `apps/frontend/components/ui/toggle-group.tsx` — new (added via shadcn CLI).

## Open questions

None at design time. Type resolution for `AdminUsersQuery["status"]` will be handled during planning.
