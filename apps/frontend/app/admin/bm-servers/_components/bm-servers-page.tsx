"use client";

import { usePathname, useRouter, useSearchParams } from "next/navigation";
import * as React from "react";
import {
	ChevronLeftIcon,
	ChevronRightIcon,
	ChevronsLeftIcon,
	ChevronsRightIcon,
	FileIcon,
	PlusIcon,
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
	AdminBmServersPage as AdminBmServersPageData,
	AdminBmServersQuery,
} from "@/lib/rustlens-api";

import { BmServerDialog } from "./bm-server-dialog";
import { BmServersBulkToolbar } from "./bm-servers-bulk-toolbar";
import { BmServersTable } from "./bm-servers-table";
import { BmServersToolbar } from "./bm-servers-toolbar";

type Props = {
	initialPage: AdminBmServersPageData;
	initialQuery: AdminBmServersQuery;
	pageSizeOptions: number[];
	permissions: string[];
};

export function BmServersPage({
	initialPage,
	initialQuery,
	pageSizeOptions,
	permissions,
}: Props) {
	const router = useRouter();
	const pathname = usePathname();
	const searchParams = useSearchParams();
	const [selected, setSelected] = React.useState<Set<string>>(new Set());
	const [addOpen, setAddOpen] = React.useState(false);

	const initialQueryKey = [
		initialQuery.q ?? "",
		initialQuery.highFrequency ?? "",
		initialQuery.hasError ?? "",
		initialQuery.isVitality ?? "",
		initialQuery.isAtlas ?? "",
		initialQuery.isVital ?? "",
		initialQuery.isModded ?? "",
		initialQuery.isOfficial ?? "",
		initialQuery.isBuild ?? "",
		initialQuery.page ?? "",
		initialQuery.pageSize ?? "",
		initialQuery.sortBy ?? "",
		initialQuery.sortOrder ?? "",
	].join("|");
	const [prevQueryKey, setPrevQueryKey] = React.useState(initialQueryKey);
	if (prevQueryKey !== initialQueryKey) {
		setPrevQueryKey(initialQueryKey);
		setSelected(new Set());
	}

	const canEdit = permissions.includes("bm_servers.edit");

	const updateQuery = (patch: Partial<AdminBmServersQuery>) => {
		const sp = new URLSearchParams(searchParams.toString());
		const apply = (key: string, val: unknown) => {
			if (val === undefined || val === null || val === "") {
				sp.delete(key);
			} else {
				sp.set(key, String(val));
			}
		};
		for (const [k, v] of Object.entries(patch)) apply(k, v);
		router.push(`${pathname}?${sp.toString()}`);
	};

	const toggle = (bmId: string) => {
		setSelected((prev) => {
			const next = new Set(prev);
			if (next.has(bmId)) next.delete(bmId);
			else next.add(bmId);
			return next;
		});
	};
	const toggleAll = () => {
		setSelected((prev) => {
			const ids = initialPage.items.map((it) => it.bmId);
			const allSelected = ids.length > 0 && ids.every((id) => prev.has(id));
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

	const refresh = () => {
		setSelected(new Set());
		router.refresh();
	};

	const pageSize = initialQuery.pageSize ?? 50;
	const totalPages = Math.max(1, Math.ceil(initialPage.total / pageSize));
	const currentPage = initialQuery.page ?? 1;
	const canPrev = currentPage > 1;
	const canNext = currentPage < totalPages;
	const goToPage = (p: number) => updateQuery({ page: p });

	return (
		<div className="flex flex-col gap-4">
			<div className="flex flex-wrap items-center gap-2">
				<BmServersToolbar query={initialQuery} onChange={updateQuery} />
				{canEdit ? (
					<Button className="ml-auto" onClick={() => setAddOpen(true)}>
						<PlusIcon className="mr-2 size-4" />
						New server
					</Button>
				) : null}
			</div>
			<BmServersBulkToolbar
				selectedIds={Array.from(selected)}
				permissions={permissions}
				onClear={() => setSelected(new Set())}
				onComplete={refresh}
			/>
			<BmServersTable
				items={initialPage.items}
				query={initialQuery}
				onSortChange={(sortBy, sortOrder) =>
					updateQuery({ sortBy, sortOrder, page: 1 })
				}
				selected={selected}
				onToggle={toggle}
				onToggleAll={toggleAll}
				permissions={permissions}
				onComplete={refresh}
			/>
			<div className="flex items-center gap-4">
				<div className="flex flex-1 items-center">
					<Select
						value={String(pageSize)}
						onValueChange={(value) =>
							updateQuery({
								pageSize: Number.parseInt(value, 10),
								page: 1,
							})
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
					{initialPage.total} {initialPage.total === 1 ? "server" : "servers"}
				</div>
			</div>
			{canEdit ? (
				<BmServerDialog
					mode="create"
					open={addOpen}
					onOpenChange={setAddOpen}
					onSuccess={refresh}
				/>
			) : null}
		</div>
	);
}
