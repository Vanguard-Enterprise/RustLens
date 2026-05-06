"use client";

import { usePathname, useRouter, useSearchParams } from "next/navigation";
import * as React from "react";
import {
	ChevronLeftIcon,
	ChevronRightIcon,
	ChevronsLeftIcon,
	ChevronsRightIcon,
	FileIcon,
} from "lucide-react";

import { Button } from "@/components/ui/button";
import {
	Select,
	SelectContent,
	SelectGroup,
	SelectItem,
	SelectTrigger,
	SelectValue,
} from "@/components/ui/select";
import type {
	AdminRoleItem,
	AdminUsersPage as AdminUsersPageData,
	AdminUsersQuery,
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

	const initialQueryKey = `${initialQuery.q ?? ""}|${(initialQuery.role ?? []).join(",")}|${initialQuery.status ?? ""}|${initialQuery.sort ?? ""}|${initialQuery.page ?? ""}|${initialQuery.pageSize ?? ""}`;
	const [prevQueryKey, setPrevQueryKey] = React.useState(initialQueryKey);

	if (prevQueryKey !== initialQueryKey) {
		setPrevQueryKey(initialQueryKey);
		setSelected(new Set());
	}

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

	const pageSize = initialQuery.pageSize ?? 50;
	const totalPages = Math.max(1, Math.ceil(initialPage.total / pageSize));
	const currentPage = initialQuery.page ?? 1;
	const canPrev = currentPage > 1;
	const canNext = currentPage < totalPages;
	const goToPage = (p: number) => updateQuery({ page: p });

	return (
		<div className="flex flex-col gap-4">
			<UsersToolbar
				query={initialQuery}
				roles={roles}
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
				sort={initialQuery.sort}
				onSortChange={(sort) => updateQuery({ sort, page: 1 })}
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
					{initialPage.total} {initialPage.total === 1 ? "user" : "users"}
				</div>
			</div>
		</div>
	);
}
