"use client";

import * as React from "react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import {
	ChevronLeftIcon,
	ChevronRightIcon,
	ChevronsLeftIcon,
	ChevronsRightIcon,
	FileIcon,
	PlusIcon,
	SearchIcon,
} from "lucide-react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
	Select,
	SelectContent,
	SelectGroup,
	SelectItem,
	SelectTrigger,
	SelectValue,
} from "@/components/ui/select";
import type { AdminPermissionItem, AdminRoleItem } from "@/lib/rustlens-api";

import {
	createRoleAction,
	deleteRoleAction,
	setRolePermissionsAction,
	updateRoleAction,
} from "../actions";
import { DeleteRoleDialog } from "./delete-role-dialog";
import { RoleDialog } from "./role-dialog";
import { RolesTable } from "./roles-table";

export type RolesInitialQuery = {
	q: string | undefined;
	sort: string | undefined;
	page: number;
	pageSize: number;
};

type Props = {
	initialRoles: AdminRoleItem[];
	permissionsCatalog: AdminPermissionItem[];
	initialQuery: RolesInitialQuery;
	pageSizeOptions: number[];
};

type DialogState =
	| { open: false }
	| { open: true; mode: "create"; role: null }
	| { open: true; mode: "edit"; role: AdminRoleItem };

type SortableField = "name" | "key" | "permissions" | "memberCount";

function parseSort(sort: string | undefined): { field: SortableField; desc: boolean } {
	const value = sort ?? "name";
	const desc = value.startsWith("-");
	const field = (desc ? value.slice(1) : value) as SortableField;
	const allowed: SortableField[] = ["name", "key", "permissions", "memberCount"];
	if (!allowed.includes(field)) return { field: "name", desc: false };
	return { field, desc };
}

function compareRoles(a: AdminRoleItem, b: AdminRoleItem, field: SortableField): number {
	switch (field) {
		case "name":
			return a.name.localeCompare(b.name);
		case "key":
			return a.key.localeCompare(b.key);
		case "permissions":
			return a.permissions.length - b.permissions.length;
		case "memberCount":
			return a.memberCount - b.memberCount;
	}
}

export function RolesPage({
	initialRoles,
	permissionsCatalog,
	initialQuery,
	pageSizeOptions,
}: Props) {
	const router = useRouter();
	const pathname = usePathname();
	const searchParams = useSearchParams();
	const [dialog, setDialog] = React.useState<DialogState>({ open: false });
	const [deleting, setDeleting] = React.useState<AdminRoleItem | null>(null);

	const updateQuery = (patch: Partial<RolesInitialQuery>) => {
		const sp = new URLSearchParams(searchParams.toString());
		for (const [k, v] of Object.entries(patch)) {
			if (v === undefined || v === null || v === "") sp.delete(k);
			else sp.set(k, String(v));
		}
		router.push(`${pathname}?${sp.toString()}`);
	};

	const [text, setText] = React.useState(initialQuery.q ?? "");
	React.useEffect(() => {
		// Same guard as users-toolbar — skip when text already matches URL.
		if ((text || undefined) === (initialQuery.q || undefined)) return;
		const id = setTimeout(() => {
			updateQuery({ q: text || undefined, page: 1 });
		}, 300);
		return () => clearTimeout(id);
		// eslint-disable-next-line react-hooks/exhaustive-deps
	}, [text]);

	const filteredSortedRoles = React.useMemo(() => {
		const q = (initialQuery.q ?? "").trim().toLowerCase();
		const filtered = q
			? initialRoles.filter(
					(r) =>
						r.name.toLowerCase().includes(q) ||
						r.key.toLowerCase().includes(q) ||
						(r.description ?? "").toLowerCase().includes(q),
				)
			: initialRoles.slice();
		const { field, desc } = parseSort(initialQuery.sort);
		filtered.sort((a, b) => {
			const cmp = compareRoles(a, b, field);
			return desc ? -cmp : cmp;
		});
		return filtered;
	}, [initialRoles, initialQuery.q, initialQuery.sort]);

	const pageSize = initialQuery.pageSize;
	const total = filteredSortedRoles.length;
	const totalPages = Math.max(1, Math.ceil(total / pageSize));
	const currentPage = Math.min(initialQuery.page, totalPages);
	const canPrev = currentPage > 1;
	const canNext = currentPage < totalPages;
	const goToPage = (p: number) => updateQuery({ page: p });
	const start = (currentPage - 1) * pageSize;
	const pageRoles = filteredSortedRoles.slice(start, start + pageSize);

	const onSubmit = async (
		input: Parameters<React.ComponentProps<typeof RoleDialog>["onSubmit"]>[0],
	) => {
		if (input.mode === "create") {
			const res = await createRoleAction(
				{
					key: input.next.key,
					name: input.next.name,
					description: input.next.description,
					permissions: input.next.permissions,
				},
				input.reason,
			);
			if (!res.ok) throw new Error(res.error);
			router.refresh();
			return;
		}
		// Edit: post update + permissions.set sequentially. Reuse one reason.
		// Backend exposes these as separate actions, so a failure between them
		// can leave the role partially updated; on any failure we refresh so
		// the table reflects actual persisted state and prefix the error to
		// make partial-success obvious to the admin.
		if (!input.role) return;
		const role = input.role;
		const nameChanged = role.name !== input.next.name;
		const descChanged = (role.description ?? "") !== input.next.description;
		const oldPerms = [...role.permissions].sort().join(",");
		const newPerms = [...input.next.permissions].sort().join(",");
		const permsChanged = oldPerms !== newPerms;

		let metadataApplied = false;
		try {
			if (nameChanged || descChanged) {
				const r = await updateRoleAction(
					role.id,
					{ name: input.next.name, description: input.next.description },
					input.reason,
				);
				if (!r.ok) throw new Error(r.error);
				metadataApplied = true;
			}
			if (permsChanged) {
				const r = await setRolePermissionsAction(
					role.id,
					input.next.permissions,
					input.reason,
				);
				if (!r.ok) {
					throw new Error(
						metadataApplied
							? `Name/description saved, but permissions update failed: ${r.error}. Reopen the role to retry.`
							: r.error,
					);
				}
			}
			router.refresh();
		} catch (err) {
			router.refresh();
			throw err;
		}
	};

	const onDeleteConfirm = async (reason: string) => {
		if (!deleting) return;
		const res = await deleteRoleAction(deleting.id, reason);
		if (!res.ok) throw new Error(res.error);
		router.refresh();
	};

	return (
		<div className="flex flex-col gap-4">
			<div className="flex flex-wrap items-center justify-between gap-2">
				<div className="relative w-full max-w-sm">
					<SearchIcon className="absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
					<Input
						type="search"
						placeholder="Search by name, key, or description"
						value={text}
						onChange={(e) => setText(e.target.value)}
						className="pl-8"
					/>
				</div>
				<Button onClick={() => setDialog({ open: true, mode: "create", role: null })}>
					<PlusIcon className="mr-2 size-4" />
					New role
				</Button>
			</div>

			<RolesTable
				roles={pageRoles}
				sort={initialQuery.sort}
				onSortChange={(sort) => updateQuery({ sort, page: 1 })}
				onEdit={(role) => setDialog({ open: true, mode: "edit", role })}
				onDelete={(role) => setDeleting(role)}
			/>

			<div className="flex items-center gap-4">
				<div className="flex flex-1 items-center">
					<Select
						value={String(pageSize)}
						onValueChange={(value) =>
							updateQuery({ pageSize: Number.parseInt(value, 10), page: 1 })
						}
					>
						<SelectTrigger
							size="sm"
							className="w-20"
							id="rows-per-page"
							aria-label="Rows per page"
						>
							<FileIcon className="size-4 text-muted-foreground" />
							<SelectValue placeholder={pageSize} />
						</SelectTrigger>
						<SelectContent side="top">
							<SelectGroup>
								{pageSizeOptions.map((size) => (
									<SelectItem key={size} value={String(size)}>
										{size}
									</SelectItem>
								))}
							</SelectGroup>
						</SelectContent>
					</Select>
				</div>
				<div className="flex items-center gap-2">
					<Button
						variant="outline"
						className="hidden size-8 lg:flex"
						size="icon"
						onClick={() => goToPage(1)}
						disabled={!canPrev}
					>
						<span className="sr-only">Go to first page</span>
						<ChevronsLeftIcon />
					</Button>
					<Button
						variant="outline"
						className="size-8"
						size="icon"
						onClick={() => goToPage(currentPage - 1)}
						disabled={!canPrev}
					>
						<span className="sr-only">Go to previous page</span>
						<ChevronLeftIcon />
					</Button>
					<div className="min-w-[7rem] text-center text-sm font-medium">
						Page {currentPage} of {totalPages}
					</div>
					<Button
						variant="outline"
						className="size-8"
						size="icon"
						onClick={() => goToPage(currentPage + 1)}
						disabled={!canNext}
					>
						<span className="sr-only">Go to next page</span>
						<ChevronRightIcon />
					</Button>
					<Button
						variant="outline"
						className="hidden size-8 lg:flex"
						size="icon"
						onClick={() => goToPage(totalPages)}
						disabled={!canNext}
					>
						<span className="sr-only">Go to last page</span>
						<ChevronsRightIcon />
					</Button>
				</div>
				<div className="flex flex-1 justify-end text-sm text-muted-foreground">
					{total} {total === 1 ? "role" : "roles"}
				</div>
			</div>

			<RoleDialog
				open={dialog.open}
				mode={dialog.open ? dialog.mode : "create"}
				role={dialog.open && dialog.mode === "edit" ? dialog.role : null}
				catalog={permissionsCatalog}
				onOpenChange={(open) => setDialog(open ? dialog : { open: false })}
				onSubmit={onSubmit}
			/>

			<DeleteRoleDialog
				role={deleting}
				open={deleting !== null}
				onOpenChange={(open) => {
					if (!open) setDeleting(null);
				}}
				onConfirm={onDeleteConfirm}
			/>
		</div>
	);
}
