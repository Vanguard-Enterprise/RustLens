# Admin Users Filter Consolidation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Consolidate the five inline filter controls on `/admin/users` (Role, Status, Discord, Steam, Verified) into a single `Filter` popover with tri-state toggle groups and a nested Role dropdown.

**Architecture:** Refactor the existing `UsersFilterBar` client component. Replace the four `Select` dropdowns with shadcn `ToggleGroup` components (type="single") and move the Role multi-select into a nested `DropdownMenu` inside a shadcn `Popover`. URL search params remain the single source of truth — no new state management. The `parseQuery` helper in `page.tsx` drops its `"active"` fallback so "Any" (absent param) correctly shows all users on fresh load.

**Tech Stack:** Next.js 16, React 19, TypeScript, shadcn/ui (`radix-nova` style, `@efferd` registry), `radix-ui` meta-package, Tailwind v4, Vitest + `@testing-library/react`, pnpm.

**Spec:** `docs/superpowers/specs/2026-04-22-admin-users-filter-consolidation-design.md`

---

## File Structure

**Modify:**
- `apps/frontend/app/admin/users/_components/users-filter-bar.tsx` — full refactor to use Popover + ToggleGroups + nested Role DropdownMenu.
- `apps/frontend/app/admin/users/page.tsx` — drop `"active"` fallback for `status` in `parseQuery`.
- `apps/frontend/app/admin/users/_components/__tests__/users-filter-bar.test.tsx` — update Clear test; add new tests for toggle groups, nested Role dropdown, badge count.

**Create (via shadcn CLI):**
- `apps/frontend/components/ui/popover.tsx`
- `apps/frontend/components/ui/toggle.tsx`
- `apps/frontend/components/ui/toggle-group.tsx`

No other files change.

---

## Task 1: Install shadcn `popover`, `toggle`, `toggle-group` components

**Files:**
- Create: `apps/frontend/components/ui/popover.tsx`
- Create: `apps/frontend/components/ui/toggle.tsx`
- Create: `apps/frontend/components/ui/toggle-group.tsx`

- [ ] **Step 1: Run shadcn CLI from the frontend directory**

```bash
cd B:/.dev/RustLens/RustLens-monorepo/apps/frontend
pnpm dlx shadcn@latest add @efferd/popover @efferd/toggle @efferd/toggle-group
```

Expected: three new files in `components/ui/`. The CLI may prompt to overwrite existing files — answer No for any file that already exists.

- [ ] **Step 2: Verify files were created**

```bash
ls apps/frontend/components/ui/popover.tsx apps/frontend/components/ui/toggle.tsx apps/frontend/components/ui/toggle-group.tsx
```

Expected: all three exist.

- [ ] **Step 3: Verify TS compiles**

```bash
cd B:/.dev/RustLens/RustLens-monorepo/apps/frontend
pnpm lint
```

Expected: no TypeScript errors. If the generated files import from `@radix-ui/react-*` instead of the `radix-ui` meta-package, fix the imports to match the project convention (see `components/ui/dropdown-menu.tsx` line 4 for the pattern: `import { DropdownMenu as DropdownMenuPrimitive } from "radix-ui"`).

**If the efferd registry is unreachable or does not ship these components**, fall back to the stock shadcn registry:

```bash
pnpm dlx shadcn@latest add popover toggle toggle-group
```

Then adjust imports in the three new files from `@radix-ui/react-popover` / `@radix-ui/react-toggle` / `@radix-ui/react-toggle-group` to use the `radix-ui` meta-package pattern:

```tsx
import { Popover as PopoverPrimitive } from "radix-ui"
import { Toggle as TogglePrimitive } from "radix-ui"
import { ToggleGroup as ToggleGroupPrimitive } from "radix-ui"
```

- [ ] **Step 4: Commit**

```bash
cd B:/.dev/RustLens/RustLens-monorepo
git add apps/frontend/components/ui/popover.tsx apps/frontend/components/ui/toggle.tsx apps/frontend/components/ui/toggle-group.tsx
git commit -m "feat: add shadcn popover, toggle, toggle-group components"
```

---

## Task 2: Drop `"active"` default for status in `parseQuery`

**Files:**
- Modify: `apps/frontend/app/admin/users/page.tsx:27-56` (the `parseQuery` function)

- [ ] **Step 1: Edit `parseQuery`**

Replace line 33-34 (the `status` assignment):

```ts
// Before
const status =
    (first(sp.status) as AdminUsersQuery["status"]) ?? "active";
```

With:

```ts
const status =
    (first(sp.status) as AdminUsersQuery["status"]) ?? undefined;
```

Full updated `parseQuery` for reference (after the change, it's):

```ts
function parseQuery(sp: SP): AdminUsersQuery {
	const page = Number(first(sp.page) ?? "1") || 1;
	const pageSize = Math.min(
		Math.max(Number(first(sp.pageSize) ?? "50") || 50, 1),
		200,
	);
	const status =
		(first(sp.status) as AdminUsersQuery["status"]) ?? undefined;
	const discord =
		(first(sp.discord) as AdminUsersQuery["discord"]) ?? undefined;
	const steam = (first(sp.steam) as AdminUsersQuery["steam"]) ?? undefined;
	const emailVerified =
		(first(sp.emailVerified) as AdminUsersQuery["emailVerified"]) ?? undefined;
	const role = sp.role
		? Array.isArray(sp.role)
			? sp.role
			: [sp.role]
		: undefined;
	return {
		q: first(sp.q),
		role,
		status,
		discord,
		steam,
		emailVerified,
		sort: first(sp.sort),
		page,
		pageSize,
	};
}
```

Rationale: `AdminUsersQuery.status` is `"active" | "suspended" | "deleted" | "any"` (see `apps/frontend/lib/rustlens-api.ts:95`), and `getAdminUsers` (line 358) only appends `status` to the URL if `params.status` is truthy. So passing `undefined` correctly results in no `status` filter being sent to the backend.

- [ ] **Step 2: Run lint to verify types still work**

```bash
cd B:/.dev/RustLens/RustLens-monorepo/apps/frontend
pnpm lint
```

Expected: no new TypeScript errors.

- [ ] **Step 3: Commit**

```bash
cd B:/.dev/RustLens/RustLens-monorepo
git add apps/frontend/app/admin/users/page.tsx
git commit -m "refactor: default admin users page to show all statuses"
```

---

## Task 3: Write failing tests for new filter bar behavior

**Files:**
- Modify: `apps/frontend/app/admin/users/_components/__tests__/users-filter-bar.test.tsx`

- [ ] **Step 1: Replace the test file contents**

Full new content for `apps/frontend/app/admin/users/_components/__tests__/users-filter-bar.test.tsx`:

```tsx
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { UsersFilterBar } from "../users-filter-bar";

const replace = vi.fn();
let currentSearch = "page=5";

vi.mock("next/navigation", () => ({
	useRouter: () => ({ replace }),
	usePathname: () => "/admin/users",
	useSearchParams: () => new URLSearchParams(currentSearch),
}));

beforeEach(() => {
	replace.mockClear();
	currentSearch = "page=5";
});

const roleOptions = [
	{ value: "superadmin", label: "Superadmin" },
	{ value: "moderator", label: "Moderator" },
];

function openFilterPopover() {
	fireEvent.click(screen.getByRole("button", { name: /^Filter/i }));
}

describe("UsersFilterBar", () => {
	it("pressing Enter in the search field resets page and writes the URL", () => {
		render(<UsersFilterBar roleOptions={roleOptions} />);
		const input = screen.getByLabelText(/Search users/i);
		fireEvent.change(input, { target: { value: "alice" } });
		fireEvent.keyDown(input, { key: "Enter" });
		expect(replace).toHaveBeenCalled();
		const arg = (replace.mock.calls.at(-1) ?? [""])[0] as string;
		expect(arg).toContain("q=alice");
		expect(arg).not.toContain("page=");
	});

	it("Clear inside the filter popover resets the URL to the base pathname", () => {
		currentSearch = "page=5&status=suspended&q=alice";
		render(<UsersFilterBar roleOptions={roleOptions} />);
		openFilterPopover();
		fireEvent.click(screen.getByRole("button", { name: /^Clear/i }));
		expect(replace).toHaveBeenCalledWith("/admin/users");
	});

	it("selecting 'Suspended' on the Status toggle group writes status=suspended", () => {
		render(<UsersFilterBar roleOptions={roleOptions} />);
		openFilterPopover();
		fireEvent.click(screen.getByRole("radio", { name: /^Suspended$/i }));
		expect(replace).toHaveBeenCalled();
		const arg = (replace.mock.calls.at(-1) ?? [""])[0] as string;
		expect(arg).toContain("status=suspended");
		expect(arg).not.toContain("page=");
	});

	it("selecting 'Any' on the Status toggle group removes the status param", () => {
		currentSearch = "page=5&status=suspended";
		render(<UsersFilterBar roleOptions={roleOptions} />);
		openFilterPopover();
		// Four ToggleGroups each have an "Any" option. The first in render order is Status.
		const anyRadios = screen.getAllByRole("radio", { name: /^Any$/i });
		fireEvent.click(anyRadios[0]);
		expect(replace).toHaveBeenCalled();
		const arg = (replace.mock.calls.at(-1) ?? [""])[0] as string;
		expect(arg).not.toContain("status=");
	});

	it("toggling a role inside the nested Role dropdown writes role=<key>", () => {
		render(<UsersFilterBar roleOptions={roleOptions} />);
		openFilterPopover();
		fireEvent.click(screen.getByRole("button", { name: /Select roles/i }));
		fireEvent.click(screen.getByRole("menuitemcheckbox", { name: /Moderator/i }));
		expect(replace).toHaveBeenCalled();
		const arg = (replace.mock.calls.at(-1) ?? [""])[0] as string;
		expect(arg).toContain("role=moderator");
	});

	it("Filter button shows a badge with the count of active filters (excluding search)", () => {
		currentSearch = "status=suspended&discord=linked&role=admin&q=alice";
		render(<UsersFilterBar roleOptions={roleOptions} />);
		// search (q=alice) is NOT counted; status + discord + role = 3
		const trigger = screen.getByRole("button", { name: /^Filter/i });
		expect(trigger).toHaveTextContent("3");
	});

	it("Filter button shows no badge when no filters are active", () => {
		currentSearch = "";
		render(<UsersFilterBar roleOptions={roleOptions} />);
		const trigger = screen.getByRole("button", { name: /^Filter/i });
		expect(trigger).not.toHaveTextContent(/\d/);
	});
});
```

Notes on the test:
- `screen.getByRole("radio", ...)` matches `ToggleGroupItem` elements because shadcn's toggle-group with `type="single"` sets `role="radio"` on each item.
- `screen.getByRole("menuitemcheckbox", ...)` matches `DropdownMenuCheckboxItem` because radix-ui's CheckboxItem sets `role="menuitemcheckbox"`.
- The "Any" test uses `getAllByRole` because multiple `ToggleGroup` components each have their own "Any" item; the first one is Status.

- [ ] **Step 2: Run tests — they should fail**

```bash
cd B:/.dev/RustLens/RustLens-monorepo/apps/frontend
pnpm test -- users-filter-bar
```

Expected: most tests fail because the component has not been refactored yet. The "pressing Enter in the search field" test should still pass.

- [ ] **Step 3: Commit the failing tests**

```bash
cd B:/.dev/RustLens/RustLens-monorepo
git add apps/frontend/app/admin/users/_components/__tests__/users-filter-bar.test.tsx
git commit -m "test: describe new UsersFilterBar popover + toggle group behavior"
```

---

## Task 4: Refactor `users-filter-bar.tsx`

**Files:**
- Modify: `apps/frontend/app/admin/users/_components/users-filter-bar.tsx` (full refactor)

- [ ] **Step 1: Replace the component source**

Full new content for `apps/frontend/app/admin/users/_components/users-filter-bar.tsx`:

```tsx
"use client";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useCallback, useState, useTransition } from "react";
import { FilterIcon, SearchIcon } from "lucide-react";
import {
	InputGroup,
	InputGroupAddon,
	InputGroupInput,
} from "@/components/ui/input-group";
import { Button } from "@/components/ui/button";
import {
	DropdownMenu,
	DropdownMenuCheckboxItem,
	DropdownMenuContent,
	DropdownMenuLabel,
	DropdownMenuSeparator,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
	Popover,
	PopoverContent,
	PopoverTrigger,
} from "@/components/ui/popover";
import {
	ToggleGroup,
	ToggleGroupItem,
} from "@/components/ui/toggle-group";
import { Badge } from "@/components/ui/badge";
import { Separator } from "@/components/ui/separator";
import { InviteUserDialog } from "./invite-user-dialog";

type Option = { value: string; label: string };

const STATUS_OPTIONS: Option[] = [
	{ value: "any", label: "Any" },
	{ value: "active", label: "Active" },
	{ value: "suspended", label: "Suspended" },
	{ value: "deleted", label: "Deleted" },
];
const TRISTATE: Option[] = [
	{ value: "any", label: "Any" },
	{ value: "linked", label: "Linked" },
	{ value: "unlinked", label: "Unlinked" },
];
const VERIFIED: Option[] = [
	{ value: "any", label: "Any" },
	{ value: "yes", label: "Verified" },
	{ value: "no", label: "Unverified" },
];

export function UsersFilterBar({
	roleOptions,
	csrfToken,
	canInvite,
}: {
	roleOptions: Option[];
	csrfToken?: string;
	canInvite?: boolean;
}) {
	const router = useRouter();
	const pathname = usePathname();
	const sp = useSearchParams();
	const [, startTransition] = useTransition();
	const [q, setQ] = useState(sp.get("q") ?? "");

	const updateParam = useCallback(
		(key: string, value: string | string[] | null) => {
			const next = new URLSearchParams(sp.toString());
			next.delete(key);
			if (Array.isArray(value)) value.forEach((v) => next.append(key, v));
			else if (value) next.set(key, value);
			next.delete("page"); // reset to page 1 when filters change
			startTransition(() => router.replace(`${pathname}?${next.toString()}`));
		},
		[sp, pathname, router],
	);

	const currentRoles = sp.getAll("role");
	const currentStatus = sp.get("status") ?? "any";
	const currentDiscord = sp.get("discord") ?? "any";
	const currentSteam = sp.get("steam") ?? "any";
	const currentVerified = sp.get("emailVerified") ?? "any";

	// Badge counts only filters housed inside the popover — not the search term.
	const activeFilterCount =
		(sp.get("status") ? 1 : 0) +
		(sp.get("discord") ? 1 : 0) +
		(sp.get("steam") ? 1 : 0) +
		(sp.get("emailVerified") ? 1 : 0) +
		currentRoles.length;

	// Clear is enabled when anything — filters OR search — is active.
	const hasAnything = activeFilterCount > 0 || q.length > 0;

	return (
		<div className="flex flex-wrap items-center gap-2">
			<InputGroup className="h-9 max-w-xs flex-1 min-w-[14rem]">
				<InputGroupAddon>
					<SearchIcon aria-hidden="true" />
				</InputGroupAddon>
				<InputGroupInput
					aria-label="Search users"
					placeholder="Search name, email, id, Discord…"
					value={q}
					onChange={(e) => setQ(e.target.value)}
					onKeyDown={(e) => {
						if (e.key === "Enter") updateParam("q", q || null);
					}}
				/>
			</InputGroup>

			<Popover>
				<PopoverTrigger asChild>
					<Button
						variant="outline"
						size="sm"
						aria-label="Filter users"
					>
						<FilterIcon aria-hidden="true" />
						Filter
						{activeFilterCount > 0 ? (
							<Badge variant="secondary" className="ml-1">
								{activeFilterCount}
							</Badge>
						) : null}
					</Button>
				</PopoverTrigger>
				<PopoverContent align="start" className="w-[360px] p-0">
					<div className="flex flex-col gap-3 p-3">
						<FilterRow label="Role">
							<RoleDropdown
								options={roleOptions}
								selected={currentRoles}
								onChange={(next) =>
									updateParam("role", next.length ? next : null)
								}
							/>
						</FilterRow>

						<Separator />

						<FilterRow label="Status">
							<FilterToggleGroup
								ariaLabel="Status"
								value={currentStatus}
								options={STATUS_OPTIONS}
								onChange={(v) =>
									updateParam("status", v === "any" ? null : v)
								}
							/>
						</FilterRow>

						<FilterRow label="Discord">
							<FilterToggleGroup
								ariaLabel="Discord"
								value={currentDiscord}
								options={TRISTATE}
								onChange={(v) =>
									updateParam("discord", v === "any" ? null : v)
								}
							/>
						</FilterRow>

						<FilterRow label="Steam">
							<FilterToggleGroup
								ariaLabel="Steam"
								value={currentSteam}
								options={TRISTATE}
								onChange={(v) =>
									updateParam("steam", v === "any" ? null : v)
								}
							/>
						</FilterRow>

						<FilterRow label="Verified">
							<FilterToggleGroup
								ariaLabel="Verified"
								value={currentVerified}
								options={VERIFIED}
								onChange={(v) =>
									updateParam("emailVerified", v === "any" ? null : v)
								}
							/>
						</FilterRow>
					</div>

					<Separator />

					<div className="flex justify-end p-2">
						<Button
							variant="ghost"
							size="sm"
							disabled={!hasAnything}
							onClick={() => {
								setQ("");
								router.replace(pathname);
							}}
						>
							Clear
						</Button>
					</div>
				</PopoverContent>
			</Popover>

			{canInvite && csrfToken ? (
				<div className="ml-auto">
					<InviteUserDialog csrfToken={csrfToken} />
				</div>
			) : null}
		</div>
	);
}

function FilterRow({
	label,
	children,
}: {
	label: string;
	children: React.ReactNode;
}) {
	return (
		<div className="flex flex-col gap-1.5">
			<span className="text-xs font-medium text-muted-foreground">
				{label}
			</span>
			{children}
		</div>
	);
}

function FilterToggleGroup({
	ariaLabel,
	value,
	options,
	onChange,
}: {
	ariaLabel: string;
	value: string;
	options: Option[];
	onChange: (v: string) => void;
}) {
	return (
		<ToggleGroup
			type="single"
			value={value}
			onValueChange={(v) => {
				// Treat empty string (user clicked the already-selected item) as "any".
				onChange(v || "any");
			}}
			aria-label={ariaLabel}
			className="justify-start"
		>
			{options.map((o) => (
				<ToggleGroupItem key={o.value} value={o.value} aria-label={o.label}>
					{o.label}
				</ToggleGroupItem>
			))}
		</ToggleGroup>
	);
}

function RoleDropdown({
	options,
	selected,
	onChange,
}: {
	options: Option[];
	selected: string[];
	onChange: (next: string[]) => void;
}) {
	return (
		<DropdownMenu>
			<DropdownMenuTrigger asChild>
				<Button variant="outline" size="sm" className="justify-start">
					Select roles
					{selected.length > 0 ? (
						<Badge variant="secondary" className="ml-1">
							{selected.length}
						</Badge>
					) : null}
				</Button>
			</DropdownMenuTrigger>
			<DropdownMenuContent align="start">
				<DropdownMenuLabel>Filter by role</DropdownMenuLabel>
				<DropdownMenuSeparator />
				{options.map((o) => (
					<DropdownMenuCheckboxItem
						key={o.value}
						checked={selected.includes(o.value)}
						onCheckedChange={(v) => {
							const next = v
								? [...selected, o.value]
								: selected.filter((x) => x !== o.value);
							onChange(next);
						}}
					>
						{o.label}
					</DropdownMenuCheckboxItem>
				))}
			</DropdownMenuContent>
		</DropdownMenu>
	);
}
```

Notes on the refactor:
- `Select` is no longer imported — removed entirely.
- `XIcon` is no longer imported — the old `Clear` button used it, the new one doesn't.
- The helper `FilterSelect` is gone; replaced by `FilterRow` + `FilterToggleGroup` + `RoleDropdown`.
- Role is a nested `DropdownMenu` inside the `Popover`; it works because both use portals that render on top of each other.
- The badge count formula matches the spec: status + discord + steam + emailVerified + role count, search excluded.

- [ ] **Step 2: Run tests — they should now pass**

```bash
cd B:/.dev/RustLens/RustLens-monorepo/apps/frontend
pnpm test -- users-filter-bar
```

Expected: all 7 tests pass.

If a test fails due to jsdom limitations with portals or radix-ui behavior, adjust the test selectors rather than the component (e.g. use `getAllByRole` vs `getByRole`, or query within `document.body`).

- [ ] **Step 3: Run lint**

```bash
cd B:/.dev/RustLens/RustLens-monorepo/apps/frontend
pnpm lint
```

Expected: no TypeScript or ESLint errors.

- [ ] **Step 4: Commit**

```bash
cd B:/.dev/RustLens/RustLens-monorepo
git add apps/frontend/app/admin/users/_components/users-filter-bar.tsx
git commit -m "refactor: consolidate admin users filters into a single Filter popover"
```

---

## Task 5: Full verification — run tests and build

**Files:**
- None modified.

- [ ] **Step 1: Run the full frontend test suite**

```bash
cd B:/.dev/RustLens/RustLens-monorepo/apps/frontend
pnpm test
```

Expected: all tests pass (including tests unrelated to this feature).

- [ ] **Step 2: Run lint**

```bash
cd B:/.dev/RustLens/RustLens-monorepo/apps/frontend
pnpm lint
```

Expected: zero errors.

- [ ] **Step 3: Run the Next.js build**

```bash
cd B:/.dev/RustLens/RustLens-monorepo/apps/frontend
pnpm build
```

Expected: build completes without errors. This catches issues the test suite misses (e.g. server/client boundary problems, metadata validation).

- [ ] **Step 4: Manual smoke check (optional, only if a dev environment is running)**

```bash
cd B:/.dev/RustLens/RustLens-monorepo/apps/frontend
pnpm dev
```

Navigate to `http://localhost:3000/admin/users` as a user with `users.read`. Verify:

1. Top bar shows: search input, `Filter` button, Invite button (if authorized).
2. No inline `Status / Discord / Steam / Verified` dropdowns.
3. Clicking `Filter` opens the popover with five rows: Role, Status, Discord, Steam, Verified.
4. Toggle group selections update the URL immediately and re-render the table.
5. Clear button at the bottom of the popover is disabled initially, becomes enabled when a filter or the search is set, and clears everything when clicked.
6. Filter button shows a numeric badge when any non-search filter is active.
7. On fresh load (no query string), the table shows active, suspended, AND deleted users.

- [ ] **Step 5: Nothing to commit here**

This task is verification-only. No commit.

---

## Done

The work is complete when:
- [ ] `apps/frontend/components/ui/popover.tsx`, `toggle.tsx`, `toggle-group.tsx` exist.
- [ ] `apps/frontend/app/admin/users/page.tsx` no longer defaults status to `"active"`.
- [ ] `apps/frontend/app/admin/users/_components/users-filter-bar.tsx` no longer imports from `@/components/ui/select`.
- [ ] All tests in `apps/frontend/app/admin/users/_components/__tests__/users-filter-bar.test.tsx` pass.
- [ ] `pnpm lint` and `pnpm build` pass in `apps/frontend`.
- [ ] All changes committed on the current branch.
