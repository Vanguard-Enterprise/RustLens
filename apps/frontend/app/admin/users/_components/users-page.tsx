"use client";

import { usePathname, useRouter, useSearchParams } from "next/navigation";
import * as React from "react";

import { Button } from "@/components/ui/button";
import {
	type AdminRoleItem,
	type AdminUsersPage as AdminUsersPageData,
	type AdminUsersQuery,
	buildAdminUsersExportUrl,
} from "@/lib/rustlens-api";

import { UsersBulkToolbar } from "./users-bulk-toolbar";
import { UsersTable } from "./users-table";
import { UsersToolbar } from "./users-toolbar";

type Props = {
	initialPage: AdminUsersPageData;
	roles: AdminRoleItem[];
	initialQuery: AdminUsersQuery;
	pageSizeOptions: number[];
	permissions: string[];
};

export function UsersPage({ initialPage, roles, initialQuery, pageSizeOptions, permissions }: Props) {
	const router = useRouter();
	const pathname = usePathname();
	const searchParams = useSearchParams();
	const [selected, setSelected] = React.useState<Set<string>>(new Set());

	const updateQuery = (patch: Partial<AdminUsersQuery>) => {
		const sp = new URLSearchParams(searchParams.toString());
		const apply = (key: string, val: unknown) => {
			if (Array.isArray(val)) {
				sp.delete(key);
				for (const v of val) sp.append(key, v);
			} else if (val === undefined || val === null || val === "") {
				sp.delete(key);
			} else {
				sp.set(key, String(val));
			}
		};
		for (const [k, v] of Object.entries(patch)) apply(k, v);
		router.push(`${pathname}?${sp.toString()}`);
	};

	const toggle = (id: string) => {
		setSelected((prev) => {
			const next = new Set(prev);
			if (next.has(id)) next.delete(id);
			else next.add(id);
			return next;
		});
	};
	const toggleAll = () => {
		setSelected((prev) => {
			const ids = initialPage.items.map((it) => it.user.id);
			const allSelected = ids.every((id) => prev.has(id));
			if (allSelected) {
				const next = new Set(prev);
				for (const id of ids) next.delete(id);
				return next;
			}
			const next = new Set(prev);
			for (const id of ids) next.add(id);
			return next;
		});
	};

	const exportUrl = permissions.includes("users.export")
		? buildAdminUsersExportUrl({
			q: initialQuery.q,
			role: initialQuery.role,
			status: initialQuery.status,
			sort: initialQuery.sort,
		})
		: null;

	const totalPages = Math.max(
		1,
		Math.ceil(initialPage.total / (initialQuery.pageSize ?? 50)),
	);
	const currentPage = initialQuery.page ?? 1;

	return (
		<div className="flex flex-col gap-4">
			<UsersToolbar
				query={initialQuery}
				roles={roles}
				totalRows={initialPage.total}
				exportUrl={exportUrl}
				onChange={updateQuery}
			/>
			<UsersBulkToolbar
				selectedIds={Array.from(selected)}
				roles={roles}
				permissions={permissions}
				onClear={() => setSelected(new Set())}
				onComplete={() => {
					setSelected(new Set());
					router.refresh();
				}}
			/>
			<UsersTable
				items={initialPage.items}
				selected={selected}
				onToggle={toggle}
				onToggleAll={toggleAll}
			/>
			<div className="flex items-center justify-between">
				<div className="text-sm text-muted-foreground">
					{initialPage.total} users · page {currentPage} of {totalPages}
				</div>
				<div className="flex items-center gap-2">
					<select
						value={initialQuery.pageSize}
						onChange={(e) =>
							updateQuery({ pageSize: Number.parseInt(e.target.value, 10), page: 1 })
						}
						className="rounded border px-2 py-1 text-sm"
					>
						{pageSizeOptions.map((n) => (
							<option key={n} value={n}>
								{n}/page
							</option>
						))}
					</select>
					<Button
						variant="outline"
						size="sm"
						disabled={currentPage <= 1}
						onClick={() => updateQuery({ page: Math.max(1, currentPage - 1) })}
					>
						Prev
					</Button>
					<Button
						variant="outline"
						size="sm"
						disabled={currentPage >= totalPages}
						onClick={() => updateQuery({ page: Math.min(totalPages, currentPage + 1) })}
					>
						Next
					</Button>
				</div>
			</div>
		</div>
	);
}
