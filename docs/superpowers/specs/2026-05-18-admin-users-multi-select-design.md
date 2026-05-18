# Multi-user select on /admin/users

Date: 2026-05-18
Status: Approved

## Goal

Let admins select multiple users on the `/admin/users` page via row checkboxes and
run bulk actions (suspend, unsuspend, copy Discord IDs) from a toolbar that appears
when one or more rows are selected.

## Non-goals

- Selection that persists across pages, search changes, or page-size changes.
- A "select all N matching" affordance that expands selection beyond the current page.
- Bulk role grant/revoke (the backend supports it; deferred until needed).
- Bulk CSV export.

## Existing infrastructure to reuse

- Server action `bulkUserAction(action, userIds, params, reason)` in
  `apps/frontend/app/admin/users/actions.ts:146`.
- API client `bulkAdminUserAction` in `apps/frontend/lib/rustlens-api.ts:992`,
  backed by the `users.bulk` admin endpoint, which already returns a per-user
  success/failure breakdown: `{ succeeded, failed, results: [{ userId, ok, error? }] }`.
- `Checkbox` component at `apps/frontend/components/ui/checkbox.tsx`.
- `sonner` toaster already mounted in `apps/frontend/app/layout.tsx` — import
  `toast` from `sonner` directly.
- `SuspendDialog` at `apps/frontend/app/admin/users/_components/suspend-dialog.tsx` —
  reused (extended) for the bulk flow.

Because the backend already aggregates per-user outcomes, the frontend does not
need parallel `Promise.allSettled` over per-row calls. One round trip per bulk
action.

## Scope of changes

All work is in:

- `apps/frontend/app/admin/users/_components/members-table.tsx` (selection state,
  checkbox column, toolbar wiring, dialog handling).
- `apps/frontend/app/admin/users/_components/suspend-dialog.tsx` (extend to support
  a `bulk` mode).
- `apps/frontend/app/admin/users/_components/__tests__/members-table.test.tsx`
  (new test cases).

No backend, API client, or server-action changes.

## Selection model

- `MembersTable` owns `selectedIds: Set<string>` in local state, keyed by
  `user.id` (not `discordId`) — the bulk endpoint contract uses internal IDs.
- A `React.useEffect` watching the `data` prop reference clears `selectedIds` on
  every server-rendered update. This delivers "current page only" semantics for
  free: pagination, search, and page-size changes all re-fetch on the server
  and replace `data`, which clears selection.
- Helper derivations:
  - `pageIds = data.items.map((u) => u.user.id)`
  - `selectedOnPage = pageIds.filter((id) => selectedIds.has(id))`
  - Header checkbox state:
    - `selectedOnPage.length === 0` → unchecked
    - `selectedOnPage.length === pageIds.length` → checked
    - otherwise → indeterminate
- Header checkbox click:
  - If currently checked or indeterminate → remove all `pageIds` from selection.
  - Otherwise → add all `pageIds` to selection.

## UI changes

### Checkbox column

- New leading `<TableHead>` (width `w-10` to match the avatar column rhythm) with
  a `Checkbox` whose tri-state is driven by the page selection state above.
- New leading `<TableCell>` per row with a `Checkbox` bound to
  `selectedIds.has(u.user.id)`.
- `colSpan` on the "No members found" row updates to account for the new column.
- The checkbox cell uses `onClick={(e) => e.stopPropagation()}` and the checkbox's
  `onCheckedChange` to toggle membership — it must NOT trigger the row's
  `Link`-based navigation.
- The per-row suspend action column stays unchanged; quick single-row suspend
  remains available.

### Bulk action toolbar

- Rendered between the search input row and the table when `selectedIds.size > 0`.
- Visual: a small `border` + `rounded-lg` strip, `flex items-center gap-2`,
  matching the existing pagination footer's density.
- Contents, in order:
  - `"<N> selected"` text label.
  - `Clear` ghost button — clears `selectedIds`.
  - `Suspend` destructive-variant button (rendered only if `canSuspend`).
  - `Unsuspend` outline button (rendered only if `canSuspend`).
  - `Copy IDs` outline button.
- The toolbar is in normal document flow (not sticky) for this pass.

### Bulk suspend / unsuspend flow

- Clicking `Suspend` or `Unsuspend` opens the existing `SuspendDialog`, but in
  a new `bulk` mode (see "SuspendDialog changes" below).
- Dialog title becomes `"Suspend N users"` / `"Unsuspend N users"`.
- On confirm, the dialog awaits a bulk handler supplied by `MembersTable` that
  calls `bulkUserAction("suspend" | "unsuspend", Array.from(selectedIds), {}, reason)`.

### Bulk copy IDs

- Pure client-side. Collect Discord IDs from `data.items` for each
  `id ∈ selectedIds` that appears on the current page, join with `"\n"`, write
  to clipboard via `navigator.clipboard.writeText`.
- On success: `toast.success("Copied <N> Discord IDs")`.
- On clipboard failure: `toast.error("Could not copy to clipboard")`.
- Important: this operates only on rows present on the current page. Since
  selection is current-page-only by design, that matches the selection scope.

## SuspendDialog changes

Extend `SuspendDialog` props to support a bulk variant without breaking the
single-user callsite:

```ts
type Props =
  | {
      open: boolean;
      onOpenChange: (open: boolean) => void;
      mode: "suspend" | "unsuspend";
      variant?: "single";
      userId: string;
      displayName: string;
      onConfirm: (userId: string, reason: string) => Promise<void>;
    }
  | {
      open: boolean;
      onOpenChange: (open: boolean) => void;
      mode: "suspend" | "unsuspend";
      variant: "bulk";
      count: number;
      onConfirm: (reason: string) => Promise<void>;
    };
```

- Title for bulk variant: `"Suspend <count> users"` / `"Unsuspend <count> users"`.
- Description for bulk variant: same copy as single, pluralized
  ("The users will be locked out…").
- Reason field, validation, and submit/error UX are unchanged.
- The submit handler picks the right callback based on `variant`.

## Bulk-result handling

After `bulkUserAction(...)` returns:

- If the action itself rejected (network/permission/etc.): keep dialog open,
  show the error inline. Selection unchanged.
- Else inspect `BulkUserResult`:
  - `failed === 0`:
    - Close dialog.
    - `setSelectedIds(new Set())`.
    - `toast.success("<N> users suspended")` / `"<N> users unsuspended"`.
  - `failed > 0`:
    - Close dialog.
    - Reduce `selectedIds` to only IDs from `result.results.filter((r) => !r.ok)`.
    - `toast.warning("<succeeded> of <total> suspended. <failed> failed.")`
      with the first error message in the toast description.
- Either way, call `router.refresh()` so the server-fetched table reflects the
  new suspended state. Note: that refresh will replace `data` and clear
  selection via the existing effect, which deliberately overrides the
  "keep failed IDs" behavior above. That's acceptable — the failed-rows
  visualization still tells the admin who failed via the toast, and the table
  will show post-refresh state. We accept this trade-off rather than deferring
  the refresh.

## Testing

Add to `members-table.test.tsx`:

- Header checkbox toggles every row on the current page.
- Header checkbox renders indeterminate when a subset of rows is selected.
- Selecting rows reveals the toolbar showing "<N> selected".
- Clicking a row checkbox does not trigger row navigation.
- `Clear` empties the selection and hides the toolbar.
- `Copy IDs` writes the newline-joined Discord IDs of selected rows to
  `navigator.clipboard` and toasts.
- `Suspend` opens the dialog titled `"Suspend N users"` and, on confirm,
  invokes `bulkUserAction("suspend", [...ids], {}, reason)`.
- `Unsuspend` likewise for the unsuspend sub-action.
- A `BulkUserResult` with `failed > 0` produces a warning toast and (prior to
  the `router.refresh()` resetting selection) reduces selection to the failed
  IDs.

The existing single-row suspend tests must continue to pass.

## Permissions

- `Suspend` and `Unsuspend` toolbar buttons render only when `canSuspend` is
  true (already wired to `users.suspend`). The server action `bulkUserAction`
  also enforces `requirePermission("users.suspend")` for these sub-actions,
  so the UI gating is purely cosmetic.
- `Copy IDs` is always available — it operates on already-rendered data.

## Risks and follow-ups

- The `Copy IDs` shortcut shows newline-joined IDs only; if admins want
  alternative separators or formats, that's a follow-up.
- Cross-page selection is intentionally out of scope; revisit if admins
  routinely act on > 1 page of users.
