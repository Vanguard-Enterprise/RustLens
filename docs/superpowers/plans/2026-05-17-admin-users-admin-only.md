# Admin Users page: admin-only view + Add user flow — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Restrict `/admin/users` to accounts holding at least one permissioned role, and add a two-stage "Add user" dialog that lets an authorized admin search any user and grant them a role.

**Architecture:** Pure-helper for role-set math, server component derives the admin-role keys and intersects the URL filter, a new client dialog handles user search → grant. No backend changes.

**Tech Stack:** Next.js (App Router, server + client components), React, TypeScript, Vitest + Testing Library, existing `rustlens-api.ts` SDK.

**Spec:** `docs/superpowers/specs/2026-05-17-admin-users-admin-only-design.md`

---

## File Structure

**New:**
- `apps/frontend/app/admin/users/_components/role-filter.ts` — `effectiveRoleFilter` pure helper.
- `apps/frontend/app/admin/users/_components/__tests__/role-filter.test.ts`
- `apps/frontend/app/admin/users/_components/users-page-empty-state.tsx` — "no admin roles" empty state.
- `apps/frontend/app/admin/users/_components/__tests__/users-page-empty-state.test.tsx`
- `apps/frontend/app/admin/users/_components/add-admin-user-dialog.tsx` — two-stage dialog.
- `apps/frontend/app/admin/users/_components/__tests__/add-admin-user-dialog.test.tsx`

**Modified:**
- `apps/frontend/app/admin/users/actions.ts` — adds `searchUsersForGrantAction`.
- `apps/frontend/app/admin/users/_components/users-page.tsx` — Add user button + dialog wiring.
- `apps/frontend/app/admin/users/page.tsx` — admin-role derivation, branching, intersected role filter.

---

## Conventions

- All commands run from `B:\.dev\RustLens\apps\frontend` unless noted.
- Test runner: `vitest run`. Single-file form: `npx vitest run <pattern>`.
- Type-check: `npm run lint` (`tsc --noEmit && eslint .`).
- Commit one task at a time with the message shown in its final step.

---

## Task 1: `effectiveRoleFilter` pure helper

Locks down the role-set math (intersection, fallback) before anything imports it.

**Files:**
- Create: `apps/frontend/app/admin/users/_components/role-filter.ts`
- Test: `apps/frontend/app/admin/users/_components/__tests__/role-filter.test.ts`

- [ ] **Step 1: Write the failing tests**

Create `apps/frontend/app/admin/users/_components/__tests__/role-filter.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { effectiveRoleFilter } from "../role-filter";

describe("effectiveRoleFilter", () => {
	it("returns all admin role keys when URL specifies none", () => {
		expect(effectiveRoleFilter([], ["a", "b", "c"])).toEqual(["a", "b", "c"]);
	});

	it("returns the intersection when URL specifies a subset", () => {
		expect(effectiveRoleFilter(["a", "b"], ["a", "c"])).toEqual(["a"]);
	});

	it("preserves URL order in the intersection", () => {
		expect(effectiveRoleFilter(["b", "a"], ["a", "b", "c"])).toEqual(["b", "a"]);
	});

	it("falls back to all admin role keys when URL has only non-admin roles", () => {
		expect(effectiveRoleFilter(["x"], ["a", "b"])).toEqual(["a", "b"]);
	});

	it("returns an empty array when both inputs are empty", () => {
		expect(effectiveRoleFilter([], [])).toEqual([]);
	});

	it("returns admin keys (fallback) when URL is empty even if admin keys is non-empty", () => {
		// Sanity check: the empty-URL branch fires before the intersection branch.
		expect(effectiveRoleFilter([], ["only"])).toEqual(["only"]);
	});
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `npx vitest run app/admin/users/_components/__tests__/role-filter.test.ts`
Expected: FAIL — cannot find module `../role-filter`.

- [ ] **Step 3: Create the helper**

Create `apps/frontend/app/admin/users/_components/role-filter.ts`:

```ts
/**
 * Returns the role-key filter the admin users page should send to the
 * backend. Never widens the page beyond admin roles: if the URL is empty
 * or only references non-admin role keys, returns the full admin set.
 */
export function effectiveRoleFilter(
	urlRoles: string[],
	adminRoleKeys: string[],
): string[] {
	if (urlRoles.length === 0) return adminRoleKeys;
	const adminSet = new Set(adminRoleKeys);
	const intersection = urlRoles.filter((k) => adminSet.has(k));
	return intersection.length > 0 ? intersection : adminRoleKeys;
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `npx vitest run app/admin/users/_components/__tests__/role-filter.test.ts`
Expected: PASS — 6 tests.

- [ ] **Step 5: Commit**

```bash
git add apps/frontend/app/admin/users/_components/role-filter.ts apps/frontend/app/admin/users/_components/__tests__/role-filter.test.ts
git commit -m "feat(admin-users): add effectiveRoleFilter helper"
```

---

## Task 2: Empty state component

Renders when no role in the system has any permissions.

**Files:**
- Create: `apps/frontend/app/admin/users/_components/users-page-empty-state.tsx`
- Test: `apps/frontend/app/admin/users/_components/__tests__/users-page-empty-state.test.tsx`

- [ ] **Step 1: Write the failing test**

Create `apps/frontend/app/admin/users/_components/__tests__/users-page-empty-state.test.tsx`:

```tsx
import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import { UsersPageEmptyState } from "../users-page-empty-state";

describe("UsersPageEmptyState", () => {
	it("renders the headline, body, and CTA link to /admin/roles", () => {
		render(<UsersPageEmptyState />);
		expect(
			screen.getByRole("heading", { name: /no admin roles defined yet/i }),
		).toBeInTheDocument();
		const cta = screen.getByRole("link", { name: /go to roles/i });
		expect(cta).toHaveAttribute("href", "/admin/roles");
	});
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `npx vitest run app/admin/users/_components/__tests__/users-page-empty-state.test.tsx`
Expected: FAIL — cannot find module `../users-page-empty-state`.

- [ ] **Step 3: Create the component**

Create `apps/frontend/app/admin/users/_components/users-page-empty-state.tsx`:

```tsx
import Link from "next/link";

import { Button } from "@/components/ui/button";

export function UsersPageEmptyState() {
	return (
		<div className="flex flex-col items-center gap-3 rounded-md border border-dashed p-10 text-center">
			<h2 className="text-lg font-semibold">No admin roles defined yet</h2>
			<p className="max-w-md text-sm text-muted-foreground">
				Create a role with permissions in Roles to start adding users to the
				admin team.
			</p>
			<Button asChild variant="default" size="sm">
				<Link href="/admin/roles">Go to Roles</Link>
			</Button>
		</div>
	);
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `npx vitest run app/admin/users/_components/__tests__/users-page-empty-state.test.tsx`
Expected: PASS — 1 test.

- [ ] **Step 5: Commit**

```bash
git add apps/frontend/app/admin/users/_components/users-page-empty-state.tsx apps/frontend/app/admin/users/_components/__tests__/users-page-empty-state.test.tsx
git commit -m "feat(admin-users): add empty-state for no-admin-roles case"
```

---

## Task 3: `searchUsersForGrantAction` server action

Permission-gated wrapper around `searchAdminUsers` for the dialog.

**Files:**
- Modify: `apps/frontend/app/admin/users/actions.ts`

- [ ] **Step 1: Add the action**

Open `apps/frontend/app/admin/users/actions.ts`. At the top of the file, update the import from `@/lib/rustlens-api` to also import `searchAdminUsers` and the type `AdminUserItem`. The current import block is:

```ts
import {
	grantAdminUserRole,
	bulkAdminUserAction,
	deleteAdminUser,
	editAdminUser,
	revokeAdminUserRole,
	suspendAdminUser,
	unsuspendAdminUser,
	type BulkUserResult,
	type BulkUserSubAction,
	type EditUserPatch,
} from "@/lib/rustlens-api";
```

Change it to:

```ts
import {
	grantAdminUserRole,
	bulkAdminUserAction,
	deleteAdminUser,
	editAdminUser,
	revokeAdminUserRole,
	searchAdminUsers,
	suspendAdminUser,
	unsuspendAdminUser,
	type AdminUserItem,
	type BulkUserResult,
	type BulkUserSubAction,
	type EditUserPatch,
} from "@/lib/rustlens-api";
```

Then append this function to the end of the file (after the existing `bulkUserAction` function):

```ts
export async function searchUsersForGrantAction(
	q: string,
): Promise<AdminUserItem[]> {
	await requirePermission("users.roles.assign");
	return searchAdminUsers(q);
}
```

- [ ] **Step 2: Type-check**

Run: `npm run lint`
Expected: PASS — no TypeScript errors. (ESLint may take a while; ignore warnings unrelated to this file.)

- [ ] **Step 3: Commit**

```bash
git add apps/frontend/app/admin/users/actions.ts
git commit -m "feat(admin-users): add searchUsersForGrantAction server action"
```

---

## Task 4: `AddAdminUserDialog` component

Two-stage dialog. Stage 1 searches users, stage 2 grants a role.

**Files:**
- Create: `apps/frontend/app/admin/users/_components/add-admin-user-dialog.tsx`
- Test: `apps/frontend/app/admin/users/_components/__tests__/add-admin-user-dialog.test.tsx`

- [ ] **Step 1: Write the failing tests**

Create `apps/frontend/app/admin/users/_components/__tests__/add-admin-user-dialog.test.tsx`:

```tsx
import { describe, it, expect, vi } from "vitest";
import {
	render,
	screen,
	fireEvent,
	waitFor,
	act,
} from "@testing-library/react";
import type { AdminRoleItem, AdminUserItem } from "@/lib/rustlens-api";
import { AddAdminUserDialog } from "../add-admin-user-dialog";

const roles: AdminRoleItem[] = [
	{
		id: "r1",
		key: "moderator",
		name: "Moderator",
		description: "Mods",
		isSystem: false,
		permissions: ["users.read"],
		memberCount: 0,
	},
	{
		id: "r2",
		key: "superadmin",
		name: "Superadmin",
		description: "All",
		isSystem: true,
		permissions: ["admins.grant"],
		memberCount: 0,
	},
];

const userBob: AdminUserItem = {
	user: {
		id: "u-bob",
		discordId: "111",
		discordUsername: "bob#0001",
		displayName: "Bob",
		avatarUrl: "",
		roles: [],
		createdAt: "2026-01-01T00:00:00Z",
		updatedAt: "2026-01-01T00:00:00Z",
		lastLoginAt: "2026-01-01T00:00:00Z",
	},
	discordId: "111",
	discordUsername: "bob#0001",
	guildCount: 0,
	steamConnectionCount: 0,
	adminRoles: [],
};

function renderDialog(overrides: Partial<React.ComponentProps<typeof AddAdminUserDialog>> = {}) {
	const props: React.ComponentProps<typeof AddAdminUserDialog> = {
		open: true,
		onOpenChange: vi.fn(),
		assignableRoles: roles,
		canGrantSuperadmin: false,
		onSearch: vi.fn().mockResolvedValue([userBob]),
		onConfirm: vi.fn().mockResolvedValue(undefined),
		...overrides,
	};
	render(<AddAdminUserDialog {...props} />);
	return props;
}

describe("AddAdminUserDialog", () => {
	it("opens in the search stage", () => {
		renderDialog();
		expect(
			screen.getByPlaceholderText(/search users/i),
		).toBeInTheDocument();
		// Role picker (stage 2) is not visible yet.
		expect(screen.queryByLabelText(/^role$/i)).not.toBeInTheDocument();
		expect(
			screen.queryByRole("button", { name: /grant role/i }),
		).not.toBeInTheDocument();
	});

	it("advances to the role stage when a user is picked", async () => {
		renderDialog();
		const input = screen.getByPlaceholderText(/search users/i);
		await act(async () => {
			fireEvent.change(input, { target: { value: "bob" } });
		});
		const option = await screen.findByRole("option", { name: /bob/i });
		await act(async () => {
			fireEvent.click(option);
		});
		expect(await screen.findByLabelText(/^role$/i)).toBeInTheDocument();
		expect(screen.getByText(/selected user/i)).toBeInTheDocument();
		expect(screen.getByText("Bob")).toBeInTheDocument();
	});

	it("returns to the search stage when Change is clicked", async () => {
		renderDialog();
		const input = screen.getByPlaceholderText(/search users/i);
		await act(async () => {
			fireEvent.change(input, { target: { value: "bob" } });
		});
		await act(async () => {
			fireEvent.click(await screen.findByRole("option", { name: /bob/i }));
		});
		await act(async () => {
			fireEvent.click(screen.getByRole("button", { name: /change/i }));
		});
		expect(screen.getByPlaceholderText(/search users/i)).toBeInTheDocument();
	});

	it("disables superadmin when canGrantSuperadmin is false", async () => {
		renderDialog();
		await act(async () => {
			fireEvent.change(screen.getByPlaceholderText(/search users/i), {
				target: { value: "bob" },
			});
		});
		await act(async () => {
			fireEvent.click(await screen.findByRole("option", { name: /bob/i }));
		});
		await act(async () => {
			fireEvent.click(screen.getByLabelText(/^role$/i));
		});
		const superadmin = await screen.findByRole("option", {
			name: /superadmin/i,
		});
		expect(superadmin).toHaveAttribute("aria-disabled", "true");
	});

	it("submits userId, roleKey, and trimmed reason", async () => {
		const onConfirm = vi.fn().mockResolvedValue(undefined);
		const onOpenChange = vi.fn();
		renderDialog({ onConfirm, onOpenChange });

		await act(async () => {
			fireEvent.change(screen.getByPlaceholderText(/search users/i), {
				target: { value: "bob" },
			});
		});
		await act(async () => {
			fireEvent.click(await screen.findByRole("option", { name: /bob/i }));
		});

		await act(async () => {
			fireEvent.click(screen.getByLabelText(/^role$/i));
		});
		await act(async () => {
			fireEvent.click(await screen.findByRole("option", { name: /moderator/i }));
		});

		const reason = screen.getByPlaceholderText(/reason/i);
		fireEvent.change(reason, { target: { value: "  promotion  " } });

		await act(async () => {
			fireEvent.click(screen.getByRole("button", { name: /grant role/i }));
		});

		await waitFor(() => {
			expect(onConfirm).toHaveBeenCalledWith("u-bob", "moderator", "promotion");
		});
		await waitFor(() => {
			expect(onOpenChange).toHaveBeenCalledWith(false);
		});
	});

	it("keeps the dialog open and shows the error when onConfirm rejects", async () => {
		const onConfirm = vi.fn().mockRejectedValue(new Error("already_has_role"));
		const onOpenChange = vi.fn();
		renderDialog({ onConfirm, onOpenChange });

		await act(async () => {
			fireEvent.change(screen.getByPlaceholderText(/search users/i), {
				target: { value: "bob" },
			});
		});
		await act(async () => {
			fireEvent.click(await screen.findByRole("option", { name: /bob/i }));
		});
		await act(async () => {
			fireEvent.click(screen.getByLabelText(/^role$/i));
		});
		await act(async () => {
			fireEvent.click(await screen.findByRole("option", { name: /moderator/i }));
		});
		fireEvent.change(screen.getByPlaceholderText(/reason/i), {
			target: { value: "x" },
		});
		await act(async () => {
			fireEvent.click(screen.getByRole("button", { name: /grant role/i }));
		});

		await waitFor(() => {
			expect(screen.getByText(/already_has_role/i)).toBeInTheDocument();
		});
		// Dialog stays open: onOpenChange(false) is NOT called on failure.
		expect(onOpenChange).not.toHaveBeenCalledWith(false);
	});
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `npx vitest run app/admin/users/_components/__tests__/add-admin-user-dialog.test.tsx`
Expected: FAIL — cannot find module `../add-admin-user-dialog`.

- [ ] **Step 3: Create the dialog component**

Create `apps/frontend/app/admin/users/_components/add-admin-user-dialog.tsx`:

```tsx
"use client";

import { CheckIcon, ChevronsUpDownIcon, Loader2Icon } from "lucide-react";
import * as React from "react";

import { Button } from "@/components/ui/button";
import {
	Command,
	CommandEmpty,
	CommandGroup,
	CommandInput,
	CommandItem,
	CommandList,
} from "@/components/ui/command";
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
	Popover,
	PopoverContent,
	PopoverTrigger,
} from "@/components/ui/popover";
import type { AdminRoleItem, AdminUserItem } from "@/lib/rustlens-api";
import { cn } from "@/lib/utils";

type Stage = "search" | "role";

type SelectedUser = {
	id: string;
	displayName: string;
	discordUsername: string;
	avatarUrl: string;
};

type Props = {
	open: boolean;
	onOpenChange: (open: boolean) => void;
	assignableRoles: AdminRoleItem[];
	canGrantSuperadmin: boolean;
	onSearch: (q: string) => Promise<AdminUserItem[]>;
	onConfirm: (userId: string, roleKey: string, reason: string) => Promise<void>;
};

const DEBOUNCE_MS = 120;
const MIN_QUERY_LEN = 1;

export function AddAdminUserDialog({
	open,
	onOpenChange,
	assignableRoles,
	canGrantSuperadmin,
	onSearch,
	onConfirm,
}: Props) {
	const [stage, setStage] = React.useState<Stage>("search");
	const [selectedUser, setSelectedUser] = React.useState<SelectedUser | null>(
		null,
	);
	const [roleKey, setRoleKey] = React.useState<string>("");
	const [reason, setReason] = React.useState("");
	const [submitting, setSubmitting] = React.useState(false);
	const [error, setError] = React.useState<string | null>(null);
	const [pickerOpen, setPickerOpen] = React.useState(false);

	// Search-stage state.
	const [text, setText] = React.useState("");
	const [cache, setCache] = React.useState<Map<string, AdminUserItem[]>>(
		() => new Map(),
	);
	const [loadingQuery, setLoadingQuery] = React.useState<string | null>(null);
	const [errorQuery, setErrorQuery] = React.useState<string | null>(null);
	const reqIdRef = React.useRef(0);

	const [prevOpen, setPrevOpen] = React.useState(open);
	if (prevOpen !== open) {
		setPrevOpen(open);
		if (!open) {
			setStage("search");
			setSelectedUser(null);
			setRoleKey("");
			setReason("");
			setError(null);
			setSubmitting(false);
			setPickerOpen(false);
			setText("");
			setCache(new Map());
			setLoadingQuery(null);
			setErrorQuery(null);
		}
	}

	const trimmed = text.trim();
	const tooShort = trimmed.length < MIN_QUERY_LEN;
	const cached = !tooShort ? cache.get(trimmed) : undefined;
	const displayResults: AdminUserItem[] = tooShort ? [] : cached ?? [];
	const displayLoading = !tooShort && !cached && loadingQuery === trimmed;
	const displayError = !tooShort && !cached && errorQuery === trimmed;

	React.useEffect(() => {
		if (stage !== "search" || tooShort || cached) return;
		const myReq = ++reqIdRef.current;
		const t = setTimeout(async () => {
			setLoadingQuery(trimmed);
			try {
				const res = await onSearch(trimmed);
				if (myReq !== reqIdRef.current) return;
				setCache((prev) => new Map(prev).set(trimmed, res));
				setErrorQuery((prev) => (prev === trimmed ? null : prev));
			} catch {
				if (myReq !== reqIdRef.current) return;
				setErrorQuery(trimmed);
			} finally {
				if (myReq === reqIdRef.current) {
					setLoadingQuery((prev) => (prev === trimmed ? null : prev));
				}
			}
		}, DEBOUNCE_MS);
		return () => clearTimeout(t);
	}, [trimmed, stage, tooShort, cached, onSearch]);

	const pickUser = (it: AdminUserItem) => {
		setSelectedUser({
			id: it.user.id,
			displayName: it.user.displayName || it.user.id,
			discordUsername: it.discordUsername,
			avatarUrl: it.user.avatarUrl ?? "",
		});
		setStage("role");
	};

	const backToSearch = () => {
		setStage("search");
		setSelectedUser(null);
		setRoleKey("");
		setReason("");
		setError(null);
	};

	const selectedRole = assignableRoles.find((r) => r.key === roleKey) ?? null;

	const submit = async () => {
		if (!selectedUser || !roleKey || !reason.trim()) return;
		setSubmitting(true);
		setError(null);
		try {
			await onConfirm(selectedUser.id, roleKey, reason.trim());
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
					<DialogTitle>Add user</DialogTitle>
					<DialogDescription>
						{stage === "search"
							? "Search for a user by name, Discord handle, or ID."
							: "Pick an admin role and reason. Granting superadmin requires admins.grant."}
					</DialogDescription>
				</DialogHeader>

				{stage === "search" ? (
					<div className="flex flex-col gap-3">
						<Command shouldFilter={false}>
							<div className="relative">
								<CommandInput
									placeholder="Search users…"
									value={text}
									onValueChange={setText}
								/>
								{displayLoading ? (
									<Loader2Icon className="pointer-events-none absolute right-3 top-1/2 size-4 -translate-y-1/2 animate-spin text-muted-foreground" />
								) : null}
							</div>
							<CommandList className="max-h-80">
								{displayError ? (
									<div className="p-3 text-xs text-muted-foreground">
										Search unavailable — try again.
									</div>
								) : (
									<>
										<CommandEmpty>
											{tooShort
												? "Type to search…"
												: displayLoading
													? "Searching…"
													: "No matches."}
										</CommandEmpty>
										<CommandGroup>
											{displayResults.map((it) => (
												<CommandItem
													key={it.user.id}
													value={it.user.id}
													onSelect={() => pickUser(it)}
													className="gap-2.5 py-2"
												>
													{it.user.avatarUrl ? (
														// eslint-disable-next-line @next/next/no-img-element
														<img
															src={it.user.avatarUrl}
															alt=""
															className="size-7 shrink-0 rounded-full object-cover"
														/>
													) : (
														<div className="size-7 shrink-0 rounded-full bg-muted" />
													)}
													<div className="flex min-w-0 flex-col leading-tight">
														<span className="truncate text-sm font-medium">
															{it.user.displayName || it.user.id}
														</span>
														{it.discordUsername ? (
															<span className="truncate text-xs text-muted-foreground">
																@{it.discordUsername}
															</span>
														) : null}
													</div>
												</CommandItem>
											))}
										</CommandGroup>
									</>
								)}
							</CommandList>
						</Command>
					</div>
				) : (
					<div className="flex flex-col gap-3">
						<div className="flex items-center justify-between rounded-md border bg-muted/40 p-2">
							<div className="flex items-center gap-2">
								{selectedUser?.avatarUrl ? (
									// eslint-disable-next-line @next/next/no-img-element
									<img
										src={selectedUser.avatarUrl}
										alt=""
										className="size-7 rounded-full object-cover"
									/>
								) : (
									<div className="size-7 rounded-full bg-background" />
								)}
								<div className="flex min-w-0 flex-col leading-tight">
									<span className="text-xs uppercase text-muted-foreground">
										Selected user
									</span>
									<span className="truncate text-sm font-medium">
										{selectedUser?.displayName}
									</span>
								</div>
							</div>
							<Button variant="ghost" size="sm" onClick={backToSearch}>
								Change
							</Button>
						</div>

						<div className="flex flex-col gap-1">
							<Label htmlFor="add-user-role-picker">Role</Label>
							<Popover open={pickerOpen} onOpenChange={setPickerOpen}>
								<PopoverTrigger asChild>
									<Button
										id="add-user-role-picker"
										variant="outline"
										role="combobox"
										aria-expanded={pickerOpen}
										aria-label="Role"
										className="w-full justify-between font-normal"
									>
										<span
											className={cn(!selectedRole && "text-muted-foreground")}
										>
											{selectedRole
												? selectedRole.name
												: "Search and pick a role…"}
										</span>
										<ChevronsUpDownIcon className="ml-2 size-4 shrink-0 opacity-50" />
									</Button>
								</PopoverTrigger>
								<PopoverContent
									className="w-[--radix-popover-trigger-width] p-0"
									align="start"
								>
									<Command>
										<CommandInput placeholder="Search by name, key, or description…" />
										<CommandList>
											<CommandEmpty>No roles found.</CommandEmpty>
											<CommandGroup>
												{assignableRoles.map((r) => {
													const disabled =
														r.key === "superadmin" && !canGrantSuperadmin;
													const searchValue = [
														r.name,
														r.key,
														r.description ?? "",
													].join(" ");
													return (
														<CommandItem
															key={r.key}
															value={searchValue}
															disabled={disabled}
															onSelect={() => {
																if (disabled) return;
																setRoleKey(r.key);
																setPickerOpen(false);
															}}
															className="flex items-start gap-2"
														>
															<CheckIcon
																className={cn(
																	"mt-0.5 size-4",
																	roleKey === r.key
																		? "opacity-100"
																		: "opacity-0",
																)}
															/>
															<div className="flex min-w-0 flex-col">
																<span className="truncate">{r.name}</span>
																<span className="truncate font-mono text-xs text-muted-foreground">
																	{r.key}
																</span>
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
														</CommandItem>
													);
												})}
											</CommandGroup>
										</CommandList>
									</Command>
								</PopoverContent>
							</Popover>
						</div>

						<div className="flex flex-col gap-1">
							<Label htmlFor="add-user-reason">Reason</Label>
							<Input
								id="add-user-reason"
								placeholder="Reason (required, audited)"
								value={reason}
								onChange={(e) => setReason(e.target.value)}
							/>
						</div>

						{error ? (
							<div className="text-sm text-destructive">{error}</div>
						) : null}
					</div>
				)}

				<DialogFooter>
					<Button
						variant="ghost"
						onClick={() => onOpenChange(false)}
						disabled={submitting}
					>
						Cancel
					</Button>
					{stage === "role" ? (
						<Button
							onClick={submit}
							disabled={submitting || !roleKey || !reason.trim()}
						>
							Grant role
						</Button>
					) : null}
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `npx vitest run app/admin/users/_components/__tests__/add-admin-user-dialog.test.tsx`
Expected: PASS — 6 tests.

- [ ] **Step 5: Commit**

```bash
git add apps/frontend/app/admin/users/_components/add-admin-user-dialog.tsx apps/frontend/app/admin/users/_components/__tests__/add-admin-user-dialog.test.tsx
git commit -m "feat(admin-users): add two-stage add-user dialog"
```

---

## Task 5: Wire the Add user button into `users-page.tsx`

Adds the button to the client page, owns dialog state, calls the two server actions.

**Files:**
- Modify: `apps/frontend/app/admin/users/_components/users-page.tsx`

- [ ] **Step 1: Update imports**

Open `apps/frontend/app/admin/users/_components/users-page.tsx`. Replace the existing icon import line:

```tsx
import {
	ChevronLeftIcon,
	ChevronRightIcon,
	ChevronsLeftIcon,
	ChevronsRightIcon,
	FileIcon,
} from "lucide-react";
```

with:

```tsx
import {
	ChevronLeftIcon,
	ChevronRightIcon,
	ChevronsLeftIcon,
	ChevronsRightIcon,
	FileIcon,
	UserPlusIcon,
} from "lucide-react";
```

Then add these imports near the other `./` imports at the top of the file:

```tsx
import { grantRoleAction, searchUsersForGrantAction } from "../actions";
import { AddAdminUserDialog } from "./add-admin-user-dialog";
```

- [ ] **Step 2: Update the `Props` type and component signature**

Find this block:

```tsx
type Props = {
	initialPage: AdminUsersPageData;
	roles: AdminRoleItem[];
	initialQuery: AdminUsersQuery;
	pageSizeOptions: number[];
	permissions: string[];
};

export function UsersPage({ initialPage, roles, initialQuery, pageSizeOptions, permissions }: Props) {
```

Replace it with:

```tsx
type Props = {
	initialPage: AdminUsersPageData;
	roles: AdminRoleItem[];
	initialQuery: AdminUsersQuery;
	pageSizeOptions: number[];
	permissions: string[];
	canGrantSuperadmin: boolean;
};

export function UsersPage({
	initialPage,
	roles,
	initialQuery,
	pageSizeOptions,
	permissions,
	canGrantSuperadmin,
}: Props) {
```

- [ ] **Step 3: Add dialog state and handlers**

Find the line:

```tsx
const [selected, setSelected] = React.useState<Set<string>>(new Set());
```

Immediately after it, add:

```tsx
const [addOpen, setAddOpen] = React.useState(false);
const canAssignRoles = permissions.includes("users.roles.assign");

const handleAddSearch = React.useCallback(
	(q: string) => searchUsersForGrantAction(q),
	[],
);

const handleAddConfirm = React.useCallback(
	async (userId: string, roleKey: string, reason: string) => {
		const res = await grantRoleAction(userId, roleKey, reason);
		if (!res.ok) throw new Error(res.error);
		router.refresh();
	},
	[router],
);
```

- [ ] **Step 4: Render the button + dialog**

Find the `UsersToolbar` element in the JSX:

```tsx
<UsersToolbar
	query={initialQuery}
	roles={roles}
	onChange={updateQuery}
/>
```

Wrap it in a flex row with the new button:

```tsx
<div className="flex flex-wrap items-center justify-between gap-2">
	<UsersToolbar
		query={initialQuery}
		roles={roles}
		onChange={updateQuery}
	/>
	{canAssignRoles ? (
		<Button size="sm" onClick={() => setAddOpen(true)}>
			<UserPlusIcon className="size-4" />
			Add user
		</Button>
	) : null}
</div>
{canAssignRoles ? (
	<AddAdminUserDialog
		open={addOpen}
		onOpenChange={setAddOpen}
		assignableRoles={roles}
		canGrantSuperadmin={canGrantSuperadmin}
		onSearch={handleAddSearch}
		onConfirm={handleAddConfirm}
	/>
) : null}
```

- [ ] **Step 5: Type-check**

Run: `npm run lint`
Expected: FAIL on `page.tsx` (it doesn't yet pass `canGrantSuperadmin`). The error message should be the only failure: `Property 'canGrantSuperadmin' is missing in type ...`. This is expected — Task 6 fixes it. If any OTHER errors appear in `users-page.tsx`, fix them now before moving on.

- [ ] **Step 6: Commit**

```bash
git add apps/frontend/app/admin/users/_components/users-page.tsx
git commit -m "feat(admin-users): wire Add user button and dialog into users page"
```

---

## Task 6: Derive admin roles and intersect filter in `page.tsx`

The server component branches: empty state vs. admin-filtered fetch.

**Files:**
- Modify: `apps/frontend/app/admin/users/page.tsx`

- [ ] **Step 1: Update imports**

Open `apps/frontend/app/admin/users/page.tsx`. Add these imports near the existing imports (place the `effectiveRoleFilter` import after the existing relative imports, and the `UsersPageEmptyState` import with it):

```tsx
import { effectiveRoleFilter } from "./_components/role-filter";
import { UsersPageEmptyState } from "./_components/users-page-empty-state";
```

- [ ] **Step 2: Replace the fetch + render block**

Find this block (lines starting at the `requirePermission` call):

```tsx
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
```

Replace with:

```tsx
const session = await requirePermission("users.read");
const sp = await searchParams;

const urlQuery: AdminUsersQuery = {
	q: pickStr(sp.q),
	role: pickArr(sp.role),
	status: (pickStr(sp.status) as AdminUsersQuery["status"]) ?? "active",
	sort: pickStr(sp.sort),
	page: parsePositiveInt(pickStr(sp.page), 1),
	pageSize: PAGE_SIZE_OPTIONS.includes(parsePositiveInt(pickStr(sp.pageSize), DEFAULT_PAGE_SIZE))
		? parsePositiveInt(pickStr(sp.pageSize), DEFAULT_PAGE_SIZE)
		: DEFAULT_PAGE_SIZE,
};

const allRoles = await getAdminRoles();
const adminRoles = allRoles.filter((r) => r.permissions.length > 0);
const adminRoleKeys = adminRoles.map((r) => r.key);
const canGrantSuperadmin = (session.permissions ?? []).includes("admins.grant");
```

- [ ] **Step 3: Replace the `return` block to branch on admin roles**

Find the existing block starting with:

```tsx
const user = {
	displayName: session.displayName,
```

and continuing through the entire `return ( ... );` to the end of the function. Replace the WHOLE block (from `const user = {` through the final `);`) with:

```tsx
const [csrfToken, currentUser] = await Promise.all([
	getCsrfToken(),
	getCurrentUserSafe(),
]);

const user = {
	displayName: session.displayName,
	avatarUrl: session.avatarUrl || undefined,
	discordUsername: currentUser?.user.discordUsername,
	isInternalAdmin:
		(session.adminRoles?.length ?? 0) > 0 ||
		session.roles.includes("internal_admin"),
};

let body: React.ReactNode;
if (adminRoleKeys.length === 0) {
	body = <UsersPageEmptyState />;
} else {
	const effectiveRoles = effectiveRoleFilter(urlQuery.role ?? [], adminRoleKeys);
	const query: AdminUsersQuery = { ...urlQuery, role: effectiveRoles };
	const users = await getAdminUsers(query);
	body = (
		<UsersPage
			initialPage={users}
			roles={adminRoles}
			initialQuery={query}
			pageSizeOptions={PAGE_SIZE_OPTIONS}
			permissions={session.permissions ?? []}
			canGrantSuperadmin={canGrantSuperadmin}
		/>
	);
}

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
								Find admin users, assign roles, and manage account state.
							</p>
						</div>
						{body}
					</div>
				</div>
			</div>
		</SidebarInset>
	</SidebarProvider>
);
```

- [ ] **Step 4: Type-check**

Run: `npm run lint`
Expected: PASS — `tsc --noEmit` reports no errors. ESLint may produce style-only warnings; treat actual errors as blocking.

- [ ] **Step 5: Run all tests for this slice**

Run: `npx vitest run app/admin/users`
Expected: PASS — all admin/users tests green (existing + the 3 new test files).

- [ ] **Step 6: Commit**

```bash
git add apps/frontend/app/admin/users/page.tsx
git commit -m "feat(admin-users): filter page to admin-only via derived role keys"
```

---

## Task 7: Manual smoke test

Verifies the feature end-to-end in a browser. Not committable.

- [ ] **Step 1: Start the dev server**

From `apps/frontend`: `npm run dev`
Wait until "Ready" appears.

- [ ] **Step 2: Open `/admin/users`**

Sign in as a user with `users.read` and at least one user assigned to a permissioned role. Confirm:
- The table shows ONLY users who hold at least one permissioned role.
- The Filters → Roles popover lists only permissioned roles.
- Selecting a role narrows further; clearing it returns to the full admin-user list.

- [ ] **Step 3: Add user dialog**

Click **Add user**. Confirm:
- The button is visible only when the session has `users.roles.assign`.
- Typing in the dialog input searches across all users (try someone you know is NOT an admin).
- Picking a user advances to stage 2 showing the selected user + role picker.
- "Change" returns you to the search stage.
- Picking a role + reason + clicking Grant role closes the dialog, refreshes the table, and the new admin user appears.

- [ ] **Step 4: URL hardening**

Navigate to `/admin/users?role=some-tag-only-role-key` (use a real role key from your DB with `permissions.length === 0`). Confirm the page falls back to the full admin list (does not widen to non-admin users).

- [ ] **Step 5: Empty-state path** (optional — only if you can safely strip permissions in a dev DB)

Temporarily set all role rows' permissions to `[]`. Reload `/admin/users` — the empty state should render with a CTA link to `/admin/roles`. Revert the DB change.

- [ ] **Step 6: Stop the dev server**

Ctrl-C the `npm run dev` process.

---

## Self-Review Notes

Verified against the spec:

- **Admin-role definition** (`permissions.length > 0`): Task 6 Step 2.
- **Hard server-side enforcement**: Task 6 Step 4 — URL roles always intersected; non-admin URL roles fall back.
- **Toolbar Roles surface only admin roles**: Task 6 Step 4 passes `adminRoles` (filtered) to `UsersPage`, which forwards to the existing `UsersToolbar`.
- **Two-stage Add user dialog with search + grant**: Task 4 + Task 5.
- **`users.roles.assign` gate on the button**: Task 5 Step 3 (`canAssignRoles`).
- **`superadmin` requires `admins.grant`**: Task 4 Step 3 (`canGrantSuperadmin`), Task 6 Step 2 (computed and passed through).
- **Empty-state branch**: Task 2 + Task 6 Step 4 (`adminRoleKeys.length === 0`).
- **Server action permission gate**: Task 3 Step 1 (`requirePermission("users.roles.assign")`).
- **All three test files**: Tasks 1, 2, 4.

No backend changes required; spec invariant preserved.
