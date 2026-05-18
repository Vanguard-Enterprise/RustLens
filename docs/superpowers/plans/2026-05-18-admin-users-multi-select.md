# Admin Users Multi-Select Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add per-row checkboxes and a bulk-action toolbar (suspend, unsuspend, copy Discord IDs) to `/admin/users`, with current-page selection scope and best-effort error reporting.

**Architecture:** All changes are confined to the existing `MembersTable` client component and the `SuspendDialog` it already uses. Selection lives in `MembersTable` as a `Set<string>` keyed by `user.id`, and it resets whenever the server-rendered `data` prop changes (which covers pagination/search/page-size). Bulk suspend and unsuspend ride on the existing `bulkUserAction` server action; the backend returns per-user success/failure so partial outcomes are surfaced via a sonner toast.

**Tech Stack:** Next.js (App Router), React 19, TypeScript, shadcn/ui (Checkbox, Dialog), sonner (toasts), Vitest + @testing-library/react.

---

## File Structure

- **Modify** `apps/frontend/app/admin/users/_components/suspend-dialog.tsx`
  - Add a `bulk` discriminated-union variant to `Props` so the same dialog handles N-user confirmations.
- **Modify** `apps/frontend/app/admin/users/_components/members-table.tsx`
  - Selection state, checkbox column, bulk toolbar, bulk-result handling.
- **Modify** `apps/frontend/app/admin/users/_components/__tests__/members-table.test.tsx`
  - New cases for selection, toolbar, clipboard, and bulk actions.

No backend, API client, or server-action changes — `bulkUserAction` already
exposes everything we need.

---

## Task 1: Extend `SuspendDialog` with a `bulk` variant

**Files:**
- Modify: `apps/frontend/app/admin/users/_components/suspend-dialog.tsx`

The dialog currently takes a single `userId` and a `displayName`. We add a
second variant for bulk operations that passes a `count` instead, and an
`onConfirm` that only receives `reason`. We use a discriminated union on a new
`variant` field so the single-user callsite keeps working with zero changes.

- [ ] **Step 1: Replace the `Props` type with a discriminated union**

Edit `apps/frontend/app/admin/users/_components/suspend-dialog.tsx`. Replace
the existing `type Props = { ... }` block with:

```tsx
type CommonProps = {
	open: boolean;
	onOpenChange: (open: boolean) => void;
	mode: "suspend" | "unsuspend";
};

type SingleProps = CommonProps & {
	variant?: "single";
	userId: string;
	displayName: string;
	onConfirm: (userId: string, reason: string) => Promise<void>;
};

type BulkProps = CommonProps & {
	variant: "bulk";
	count: number;
	onConfirm: (reason: string) => Promise<void>;
};

type Props = SingleProps | BulkProps;
```

- [ ] **Step 2: Branch the submit handler and title on `variant`**

Still in `suspend-dialog.tsx`, change the component signature to accept the
union, and replace the body's `displayName`-based title and the existing
`onConfirm(userId, reason.trim())` call with variant-aware versions:

```tsx
export function SuspendDialog(props: Props) {
	const { open, onOpenChange, mode } = props;
	const [reason, setReason] = React.useState("");
	const [submitting, setSubmitting] = React.useState(false);
	const [error, setError] = React.useState<string | null>(null);
	const [prevOpen, setPrevOpen] = React.useState(open);

	if (prevOpen !== open) {
		setPrevOpen(open);
		if (!open) {
			setReason("");
			setError(null);
			setSubmitting(false);
		}
	}

	const reasonRequired = mode === "suspend";

	const titleSubject =
		props.variant === "bulk"
			? `${props.count} ${props.count === 1 ? "user" : "users"}`
			: props.displayName;

	const submit = async () => {
		if (reasonRequired && !reason.trim()) return;
		setSubmitting(true);
		try {
			if (props.variant === "bulk") {
				await props.onConfirm(reason.trim());
			} else {
				await props.onConfirm(props.userId, reason.trim());
			}
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
						{mode === "suspend" ? `Suspend ${titleSubject}` : `Unsuspend ${titleSubject}`}
					</DialogTitle>
					<DialogDescription>
						{mode === "suspend"
							? props.variant === "bulk"
								? "The users will be locked out of authenticated routes until unsuspended."
								: "The user will be locked out of authenticated routes until unsuspended."
							: props.variant === "bulk"
								? "The users regain access immediately."
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

- [ ] **Step 3: Type-check**

Run from `apps/frontend`:

```
npm run lint
```

Expected: no type errors. The existing single-user callsite in
`members-table.tsx` continues to compile because `variant` defaults to
`"single"` in the union via the optional field.

- [ ] **Step 4: Run existing tests**

Run from `apps/frontend`:

```
npm test
```

Expected: all existing tests pass — this task is a pure additive refactor.

- [ ] **Step 5: Commit**

```bash
git add apps/frontend/app/admin/users/_components/suspend-dialog.tsx
git commit -m "refactor(frontend): add bulk variant to SuspendDialog"
```

---

## Task 2: Add row selection state and checkbox column

**Files:**
- Modify: `apps/frontend/app/admin/users/_components/members-table.tsx`
- Test: `apps/frontend/app/admin/users/_components/__tests__/members-table.test.tsx`

This task adds the leading checkbox column, the header tri-state checkbox, and
the `selectedIds` state that resets on every `data`-prop change. No toolbar
yet — that's Task 3.

- [ ] **Step 1: Write the failing test for header tri-state**

Append to `apps/frontend/app/admin/users/_components/__tests__/members-table.test.tsx`:

```tsx
it("header checkbox selects all rows on the current page", () => {
  render(
    <MembersTable
      initialQuery=""
      pageSizeOptions={[10, 20, 30, 40, 50]}
      data={fixturePage(3)}
      canSuspend={false}
    />,
  );

  const selectAll = screen.getByRole("checkbox", { name: /select all/i });
  expect(selectAll).toHaveAttribute("data-state", "unchecked");

  fireEvent.click(selectAll);
  expect(selectAll).toHaveAttribute("data-state", "checked");

  const rowChecks = screen
    .getAllByRole("checkbox", { name: /select row/i });
  expect(rowChecks).toHaveLength(3);
  for (const c of rowChecks) {
    expect(c).toHaveAttribute("data-state", "checked");
  }
});

it("header checkbox is indeterminate when a subset is selected", () => {
  render(
    <MembersTable
      initialQuery=""
      pageSizeOptions={[10, 20, 30, 40, 50]}
      data={fixturePage(3)}
      canSuspend={false}
    />,
  );

  const rowChecks = screen.getAllByRole("checkbox", { name: /select row/i });
  fireEvent.click(rowChecks[0]);

  const selectAll = screen.getByRole("checkbox", { name: /select all/i });
  expect(selectAll).toHaveAttribute("data-state", "indeterminate");
});
```

Also add `fireEvent` to the existing import at the top of the file:

```tsx
import { fireEvent, render, screen } from "@testing-library/react";
```

- [ ] **Step 2: Run the new tests and confirm they fail**

Run from `apps/frontend`:

```
npm test -- members-table
```

Expected: the two new tests fail with "Unable to find a checkbox" (no such
elements yet). Existing tests still pass.

- [ ] **Step 3: Add the leading checkbox column to `MembersTable`**

Edit `apps/frontend/app/admin/users/_components/members-table.tsx`.

1. Add the import near the other `@/components/ui/*` imports:

```tsx
import { Checkbox } from "@/components/ui/checkbox";
```

2. Inside the `MembersTable` function body, just below the existing
   `useState` calls, add selection state and the reset effect (note: the
   effect runs whenever `data` changes by reference, which covers
   pagination/search/page-size):

```tsx
const [selectedIds, setSelectedIds] = React.useState<Set<string>>(() => new Set());

React.useEffect(() => {
  setSelectedIds(new Set());
}, [data]);

const pageIds = React.useMemo(() => data.items.map((u) => u.user.id), [data.items]);
const selectedOnPageCount = pageIds.reduce(
  (n, id) => (selectedIds.has(id) ? n + 1 : n),
  0,
);
const allOnPageSelected =
  pageIds.length > 0 && selectedOnPageCount === pageIds.length;
const someOnPageSelected =
  selectedOnPageCount > 0 && selectedOnPageCount < pageIds.length;

const toggleRow = (id: string, checked: boolean) => {
  setSelectedIds((prev) => {
    const next = new Set(prev);
    if (checked) next.add(id);
    else next.delete(id);
    return next;
  });
};

const toggleAllOnPage = (checked: boolean) => {
  setSelectedIds((prev) => {
    const next = new Set(prev);
    if (checked) for (const id of pageIds) next.add(id);
    else for (const id of pageIds) next.delete(id);
    return next;
  });
};
```

3. In the `<TableHeader>` block, insert a new `<TableHead>` as the very first
   header cell:

```tsx
<TableHead className="w-10">
  <Checkbox
    aria-label="Select all"
    checked={
      allOnPageSelected
        ? true
        : someOnPageSelected
          ? "indeterminate"
          : false
    }
    onCheckedChange={(value) => toggleAllOnPage(value === true)}
  />
</TableHead>
```

4. In the `data.items.map((u) => ...)` block, insert a new `<TableCell>` as
   the very first cell, before the avatar cell:

```tsx
<TableCell
  className="w-10"
  onClick={(e) => e.stopPropagation()}
>
  <Checkbox
    aria-label="Select row"
    checked={selectedIds.has(u.user.id)}
    onCheckedChange={(value) => toggleRow(u.user.id, value === true)}
  />
</TableCell>
```

5. Update the empty-state row's `colSpan` to account for the new column:

```tsx
<TableCell colSpan={canSuspend ? 6 : 5} className="h-24 text-center">
  No members found.
</TableCell>
```

- [ ] **Step 4: Run tests and confirm they pass**

Run from `apps/frontend`:

```
npm test -- members-table
```

Expected: all `MembersTable` tests pass, including the two new ones.

- [ ] **Step 5: Commit**

```bash
git add apps/frontend/app/admin/users/_components/members-table.tsx \
  apps/frontend/app/admin/users/_components/__tests__/members-table.test.tsx
git commit -m "feat(frontend): add row-selection checkboxes to admin users table"
```

---

## Task 3: Add bulk toolbar with Clear and Copy IDs

**Files:**
- Modify: `apps/frontend/app/admin/users/_components/members-table.tsx`
- Test: `apps/frontend/app/admin/users/_components/__tests__/members-table.test.tsx`

The toolbar appears between the search input and the table whenever at least
one row is selected. This task wires up the visible state, the Clear button,
and the Copy IDs button (purely client-side clipboard write). Suspend/unsuspend
land in Task 4.

- [ ] **Step 1: Write the failing tests**

Append to `members-table.test.tsx`. First add a `vi.mock` block near the top
of the file so the `sonner` import inside `MembersTable` resolves to a spy.
Place it next to the existing `vi.mock("next/navigation", ...)`:

```tsx
const toastSuccess = vi.fn();
const toastError = vi.fn();
const toastWarning = vi.fn();

vi.mock("sonner", () => ({
  toast: {
    success: (...args: unknown[]) => toastSuccess(...args),
    error: (...args: unknown[]) => toastError(...args),
    warning: (...args: unknown[]) => toastWarning(...args),
  },
}));
```

Add at the top with the other imports:

```tsx
import { beforeEach } from "vitest";
```

Add a `beforeEach` reset just before the existing `describe`:

```tsx
beforeEach(() => {
  toastSuccess.mockReset();
  toastError.mockReset();
  toastWarning.mockReset();
});
```

Now add the new test cases inside the existing `describe("MembersTable", ...)`:

```tsx
it("does not render the bulk toolbar when nothing is selected", () => {
  render(
    <MembersTable
      initialQuery=""
      pageSizeOptions={[10, 20, 30, 40, 50]}
      data={fixturePage(2)}
      canSuspend={false}
    />,
  );

  expect(screen.queryByText(/selected/i)).not.toBeInTheDocument();
});

it("shows the bulk toolbar with the selected count and a Clear button", () => {
  render(
    <MembersTable
      initialQuery=""
      pageSizeOptions={[10, 20, 30, 40, 50]}
      data={fixturePage(3)}
      canSuspend={false}
    />,
  );

  const rowChecks = screen.getAllByRole("checkbox", { name: /select row/i });
  fireEvent.click(rowChecks[0]);
  fireEvent.click(rowChecks[2]);

  expect(screen.getByText("2 selected")).toBeInTheDocument();

  fireEvent.click(screen.getByRole("button", { name: /^clear$/i }));
  expect(screen.queryByText(/selected/i)).not.toBeInTheDocument();
});

it("copies selected Discord IDs to the clipboard and toasts", async () => {
  const writeText = vi.fn().mockResolvedValue(undefined);
  Object.defineProperty(navigator, "clipboard", {
    configurable: true,
    value: { writeText },
  });

  render(
    <MembersTable
      initialQuery=""
      pageSizeOptions={[10, 20, 30, 40, 50]}
      data={fixturePage(3)}
      canSuspend={false}
    />,
  );

  const rowChecks = screen.getAllByRole("checkbox", { name: /select row/i });
  fireEvent.click(rowChecks[0]);
  fireEvent.click(rowChecks[2]);

  fireEvent.click(screen.getByRole("button", { name: /copy ids/i }));

  await waitFor(() => expect(writeText).toHaveBeenCalledTimes(1));
  expect(writeText).toHaveBeenCalledWith("1000000000000000\n1000000000000002");
  await waitFor(() => expect(toastSuccess).toHaveBeenCalled());
  expect(toastSuccess.mock.calls[0][0]).toMatch(/2 discord ids/i);
});
```

Add `waitFor` to the `@testing-library/react` import at the top:

```tsx
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
```

- [ ] **Step 2: Run the new tests and confirm they fail**

Run from `apps/frontend`:

```
npm test -- members-table
```

Expected: the three new tests fail (no toolbar, no Clear, no Copy IDs button).
Earlier tests still pass.

- [ ] **Step 3: Add the toolbar and Copy IDs handler**

Edit `members-table.tsx`.

1. Add the `sonner` import near the top with the other imports:

```tsx
import { toast } from "sonner";
```

2. Inside the component, just below the `toggleAllOnPage` definition, add the
   Copy IDs handler. It joins Discord IDs from the visible page that are in
   the selection (current-page-only scope matches the selection scope):

```tsx
const copySelectedIds = async () => {
  const ids = data.items
    .filter((u) => selectedIds.has(u.user.id))
    .map((u) => u.discordId);
  if (ids.length === 0) return;
  try {
    await navigator.clipboard.writeText(ids.join("\n"));
    toast.success(
      `Copied ${ids.length} Discord ${ids.length === 1 ? "ID" : "IDs"}`,
    );
  } catch {
    toast.error("Could not copy to clipboard");
  }
};

const clearSelection = () => setSelectedIds(new Set());
```

3. Render the toolbar between the search-input row and the table. Replace the
   existing block:

```tsx
<div className="flex items-center gap-2">
  <Input
    ...
  />
</div>

<div className="overflow-hidden rounded-lg border">
```

with the search row unchanged, then a conditional toolbar block, then the
table block:

```tsx
<div className="flex items-center gap-2">
  <Input
    placeholder="Search by username, global name, or Discord ID…"
    value={searchValue}
    onChange={(e) => setSearchValue(e.target.value)}
    className="max-w-md"
    aria-label="Search members"
  />
</div>

{selectedIds.size > 0 ? (
  <div className="flex items-center gap-2 rounded-lg border bg-muted/30 px-3 py-2">
    <span className="text-sm font-medium">
      {selectedIds.size} selected
    </span>
    <div className="flex-1" />
    <Button variant="ghost" size="sm" onClick={clearSelection}>
      Clear
    </Button>
    <Button variant="outline" size="sm" onClick={copySelectedIds}>
      Copy IDs
    </Button>
  </div>
) : null}

<div className="overflow-hidden rounded-lg border">
```

- [ ] **Step 4: Run tests and confirm they pass**

Run from `apps/frontend`:

```
npm test -- members-table
```

Expected: every `MembersTable` test passes, including the three new toolbar
tests.

- [ ] **Step 5: Commit**

```bash
git add apps/frontend/app/admin/users/_components/members-table.tsx \
  apps/frontend/app/admin/users/_components/__tests__/members-table.test.tsx
git commit -m "feat(frontend): add bulk toolbar with Clear and Copy IDs"
```

---

## Task 4: Wire bulk suspend and unsuspend through `SuspendDialog`

**Files:**
- Modify: `apps/frontend/app/admin/users/_components/members-table.tsx`
- Test: `apps/frontend/app/admin/users/_components/__tests__/members-table.test.tsx`

Bulk suspend opens the existing `SuspendDialog` in its new `bulk` variant. On
confirm we call the existing `bulkUserAction` server action and surface
results via sonner.

- [ ] **Step 1: Write the failing test for opening the bulk suspend dialog**

In `members-table.test.tsx`, add a mock for the suspend actions module just
below the existing `vi.mock("sonner", ...)` (mocks for action imports must be
at the top of the file alongside the other `vi.mock` calls):

```tsx
const bulkUser = vi.fn();
const suspendUser = vi.fn();
const unsuspendUser = vi.fn();

vi.mock("../../actions", () => ({
  suspendUserAction: (userId: string, reason: string) =>
    suspendUser(userId, reason),
  unsuspendUserAction: (userId: string, reason: string) =>
    unsuspendUser(userId, reason),
  bulkUserAction: (
    action: string,
    userIds: string[],
    params: Record<string, unknown>,
    reason: string,
  ) => bulkUser(action, userIds, params, reason),
}));
```

Update the `beforeEach` reset to include the new mocks:

```tsx
beforeEach(() => {
  toastSuccess.mockReset();
  toastError.mockReset();
  toastWarning.mockReset();
  bulkUser.mockReset();
  suspendUser.mockReset();
  unsuspendUser.mockReset();
});
```

Append new test cases:

```tsx
it("bulk suspend opens a dialog titled with the count and dispatches the action", async () => {
  bulkUser.mockResolvedValue({
    ok: true,
    result: {
      action: "suspend",
      succeeded: 2,
      failed: 0,
      results: [
        { userId: "usr_0", ok: true },
        { userId: "usr_2", ok: true },
      ],
    },
  });

  render(
    <MembersTable
      initialQuery=""
      pageSizeOptions={[10, 20, 30, 40, 50]}
      data={fixturePage(3)}
      canSuspend
    />,
  );

  const rowChecks = screen.getAllByRole("checkbox", { name: /select row/i });
  fireEvent.click(rowChecks[0]);
  fireEvent.click(rowChecks[2]);

  fireEvent.click(screen.getByRole("button", { name: /^suspend$/i }));

  expect(
    await screen.findByRole("heading", { name: /suspend 2 users/i }),
  ).toBeInTheDocument();

  const reasonInput = screen.getByLabelText(/^reason$/i);
  fireEvent.change(reasonInput, { target: { value: "spamming" } });

  fireEvent.click(
    screen.getByRole("button", { name: /^suspend$/i, hidden: false }),
  );

  await waitFor(() => expect(bulkUser).toHaveBeenCalledTimes(1));
  expect(bulkUser).toHaveBeenCalledWith(
    "suspend",
    ["usr_0", "usr_2"],
    {},
    "spamming",
  );
  await waitFor(() => expect(toastSuccess).toHaveBeenCalled());
  expect(toastSuccess.mock.calls[0][0]).toMatch(/2 users suspended/i);
});

it("bulk unsuspend dispatches the unsuspend sub-action", async () => {
  bulkUser.mockResolvedValue({
    ok: true,
    result: {
      action: "unsuspend",
      succeeded: 1,
      failed: 0,
      results: [{ userId: "usr_1", ok: true }],
    },
  });

  render(
    <MembersTable
      initialQuery=""
      pageSizeOptions={[10, 20, 30, 40, 50]}
      data={fixturePage(2)}
      canSuspend
    />,
  );

  const rowChecks = screen.getAllByRole("checkbox", { name: /select row/i });
  fireEvent.click(rowChecks[1]);

  fireEvent.click(screen.getByRole("button", { name: /^unsuspend$/i }));
  expect(
    await screen.findByRole("heading", { name: /unsuspend 1 user/i }),
  ).toBeInTheDocument();

  fireEvent.click(
    screen.getAllByRole("button", { name: /^unsuspend$/i }).at(-1)!,
  );

  await waitFor(() => expect(bulkUser).toHaveBeenCalledTimes(1));
  expect(bulkUser).toHaveBeenCalledWith("unsuspend", ["usr_1"], {}, "");
});

it("partial bulk failures produce a warning toast", async () => {
  bulkUser.mockResolvedValue({
    ok: true,
    result: {
      action: "suspend",
      succeeded: 1,
      failed: 1,
      results: [
        { userId: "usr_0", ok: true },
        { userId: "usr_1", ok: false, error: "already_suspended" },
      ],
    },
  });

  render(
    <MembersTable
      initialQuery=""
      pageSizeOptions={[10, 20, 30, 40, 50]}
      data={fixturePage(2)}
      canSuspend
    />,
  );

  const rowChecks = screen.getAllByRole("checkbox", { name: /select row/i });
  fireEvent.click(rowChecks[0]);
  fireEvent.click(rowChecks[1]);

  fireEvent.click(screen.getByRole("button", { name: /^suspend$/i }));
  fireEvent.change(screen.getByLabelText(/^reason$/i), {
    target: { value: "spamming" },
  });
  fireEvent.click(
    screen.getAllByRole("button", { name: /^suspend$/i }).at(-1)!,
  );

  await waitFor(() => expect(toastWarning).toHaveBeenCalled());
  expect(toastWarning.mock.calls[0][0]).toMatch(/1 of 2/i);
});

it("does not render bulk suspend buttons without canSuspend", () => {
  render(
    <MembersTable
      initialQuery=""
      pageSizeOptions={[10, 20, 30, 40, 50]}
      data={fixturePage(2)}
      canSuspend={false}
    />,
  );

  fireEvent.click(
    screen.getAllByRole("checkbox", { name: /select row/i })[0],
  );

  expect(
    screen.queryByRole("button", { name: /^suspend$/i }),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: /^unsuspend$/i }),
  ).not.toBeInTheDocument();
});
```

Also: extend `fixturePage` so it produces stable Discord IDs that are unique
per row (the existing helper concatenates the index onto
`"100000000000000"`, which is fine — leave it). The user IDs `usr_0`, `usr_1`,
`usr_2` already differ, so the new tests can rely on them as-is.

- [ ] **Step 2: Run the new tests and confirm they fail**

Run from `apps/frontend`:

```
npm test -- members-table
```

Expected: the four new tests fail (no bulk Suspend/Unsuspend buttons, etc.).
Earlier tests still pass.

- [ ] **Step 3: Wire the bulk action state and dialog into `MembersTable`**

Edit `apps/frontend/app/admin/users/_components/members-table.tsx`.

1. Update the import from `../actions` to pull in `bulkUserAction`:

```tsx
import { bulkUserAction, suspendUserAction, unsuspendUserAction } from "../actions";
```

2. Add a new state type and state hook near the existing `suspendTarget`
   state:

```tsx
type BulkAction = "suspend" | "unsuspend";
const [bulkAction, setBulkAction] = React.useState<BulkAction | null>(null);
```

3. Add the bulk-confirm handler just below `copySelectedIds`:

```tsx
const runBulk = async (action: BulkAction, reason: string) => {
  const ids = Array.from(selectedIds);
  const res = await bulkUserAction(action, ids, {}, reason);
  if (!res.ok) throw new Error(res.error);
  const { succeeded, failed, results } = res.result;
  const verb = action === "suspend" ? "suspended" : "unsuspended";
  if (failed === 0) {
    toast.success(`${succeeded} ${succeeded === 1 ? "user" : "users"} ${verb}`);
    setSelectedIds(new Set());
  } else {
    const firstError = results.find((r) => !r.ok)?.error;
    toast.warning(
      `${succeeded} of ${succeeded + failed} ${verb}. ${failed} failed.`,
      firstError ? { description: firstError } : undefined,
    );
    setSelectedIds(new Set(results.filter((r) => !r.ok).map((r) => r.userId)));
  }
  router.refresh();
};
```

4. In the toolbar JSX (added in Task 3), render Suspend/Unsuspend buttons
   before the `Copy IDs` button, gated by `canSuspend`:

```tsx
{selectedIds.size > 0 ? (
  <div className="flex items-center gap-2 rounded-lg border bg-muted/30 px-3 py-2">
    <span className="text-sm font-medium">
      {selectedIds.size} selected
    </span>
    <div className="flex-1" />
    <Button variant="ghost" size="sm" onClick={clearSelection}>
      Clear
    </Button>
    {canSuspend ? (
      <>
        <Button
          variant="destructive"
          size="sm"
          onClick={() => setBulkAction("suspend")}
        >
          Suspend
        </Button>
        <Button
          variant="outline"
          size="sm"
          onClick={() => setBulkAction("unsuspend")}
        >
          Unsuspend
        </Button>
      </>
    ) : null}
    <Button variant="outline" size="sm" onClick={copySelectedIds}>
      Copy IDs
    </Button>
  </div>
) : null}
```

5. Render the bulk `SuspendDialog` alongside the existing single-user
   `SuspendDialog` (inside the `{canSuspend ? ... : null}` guard, append a
   second dialog):

```tsx
{canSuspend ? (
  <>
    <SuspendDialog
      open={suspendTarget !== null}
      onOpenChange={(o) => {
        if (!o) setSuspendTarget(null);
      }}
      mode={suspendTarget?.mode ?? "suspend"}
      userId={suspendTarget?.userId ?? ""}
      displayName={suspendTarget?.displayName ?? ""}
      onConfirm={async (userId, reason) => {
        const action =
          suspendTarget?.mode === "unsuspend" ? unsuspendUserAction : suspendUserAction;
        const res = await action(userId, reason);
        if (!res.ok) throw new Error(res.error);
        router.refresh();
      }}
    />
    <SuspendDialog
      variant="bulk"
      open={bulkAction !== null}
      onOpenChange={(o) => {
        if (!o) setBulkAction(null);
      }}
      mode={bulkAction ?? "suspend"}
      count={selectedIds.size}
      onConfirm={async (reason) => {
        if (!bulkAction) return;
        await runBulk(bulkAction, reason);
      }}
    />
  </>
) : null}
```

- [ ] **Step 4: Run tests and confirm they pass**

Run from `apps/frontend`:

```
npm test -- members-table
```

Expected: all `MembersTable` tests pass, including the four new bulk tests.

- [ ] **Step 5: Run lint and the full test suite**

Run from `apps/frontend`:

```
npm run lint
npm test
```

Expected: clean type-check, clean ESLint, all tests pass.

- [ ] **Step 6: Manual smoke test**

Start the dev server and exercise the feature end-to-end:

```
npm run dev
```

In a browser, sign in as an admin with `users.suspend`, navigate to
`/admin/users`, and verify:

- Header checkbox selects every row on the current page.
- Selecting a subset shows the header checkbox in the indeterminate state.
- Changing pages or running a search clears the selection.
- Toolbar appears with "N selected" when 1+ rows are selected.
- `Copy IDs` writes a newline-joined string of Discord IDs to the clipboard
  and shows a success toast.
- `Suspend` opens a dialog titled `Suspend N users` and, after submitting a
  reason, suspends the selected users (success toast).
- `Unsuspend` works likewise without a reason.
- A user without `users.suspend` does not see the Suspend/Unsuspend buttons.

If anything looks wrong, fix it and re-run `npm test` before continuing.

- [ ] **Step 7: Commit**

```bash
git add apps/frontend/app/admin/users/_components/members-table.tsx \
  apps/frontend/app/admin/users/_components/__tests__/members-table.test.tsx
git commit -m "feat(frontend): add bulk suspend/unsuspend to admin users table"
```

---

## Self-Review Notes

- **Spec coverage:** selection state (Task 2), checkbox column (Task 2),
  toolbar with Clear / Copy IDs (Task 3), bulk suspend / unsuspend through
  reused `SuspendDialog` (Tasks 1 + 4), partial-failure handling (Task 4),
  permission gating (Task 4), tests for every behavior listed in the spec
  (Tasks 2–4).
- **Out-of-scope items** (cross-page selection persistence, "select all N
  matching", bulk roles, CSV export) are not addressed — matches the spec.
- **Types** (`BulkAction`, `BulkProps`, `SingleProps`, `Props` union,
  `selectedIds: Set<string>`) are consistent between tasks.
