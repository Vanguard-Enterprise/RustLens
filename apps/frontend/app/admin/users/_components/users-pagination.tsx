"use client";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { ChevronLeftIcon, ChevronRightIcon } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
	Select,
	SelectContent,
	SelectItem,
	SelectTrigger,
	SelectValue,
} from "@/components/ui/select";
import { Label } from "@/components/ui/label";

const PAGE_SIZES = ["25", "50", "100", "200"];

export function UsersPagination({
	total,
	page,
	pageSize,
}: {
	total: number;
	page: number;
	pageSize: number;
}) {
	const router = useRouter();
	const pathname = usePathname();
	const sp = useSearchParams();
	const lastPage = Math.max(1, Math.ceil(total / pageSize));
	const go = (p: number) => {
		const next = new URLSearchParams(sp.toString());
		if (p <= 1) next.delete("page");
		else next.set("page", String(p));
		router.replace(`${pathname}?${next.toString()}`);
	};
	const setPageSize = (size: string) => {
		const next = new URLSearchParams(sp.toString());
		next.set("pageSize", size);
		next.delete("page");
		router.replace(`${pathname}?${next.toString()}`);
	};

	return (
		<nav
			aria-label="Users pagination"
			className="flex w-full flex-wrap items-center gap-3"
		>
			<span className="text-sm text-muted-foreground" aria-live="polite">
				{total === 0
					? "No users"
					: `${(page - 1) * pageSize + 1}–${Math.min(page * pageSize, total)} of ${total}`}
			</span>

			<div className="flex items-center gap-2">
				<Label
					htmlFor="users-page-size"
					className="text-xs text-muted-foreground"
				>
					Rows
				</Label>
				<Select value={String(pageSize)} onValueChange={setPageSize}>
					<SelectTrigger id="users-page-size" size="sm" className="w-[5.5rem]">
						<SelectValue />
					</SelectTrigger>
					<SelectContent>
						{PAGE_SIZES.map((s) => (
							<SelectItem key={s} value={s}>
								{s}
							</SelectItem>
						))}
					</SelectContent>
				</Select>
			</div>

			<div className="ml-auto flex items-center gap-2">
				<Button
					variant="outline"
					size="sm"
					disabled={page <= 1}
					onClick={() => go(page - 1)}
					aria-label="Previous page"
				>
					<ChevronLeftIcon aria-hidden="true" />
					Previous
				</Button>
				<span className="text-sm tabular-nums" aria-live="polite">
					Page {page} / {lastPage}
				</span>
				<Button
					variant="outline"
					size="sm"
					disabled={page >= lastPage}
					onClick={() => go(page + 1)}
					aria-label="Next page"
				>
					Next
					<ChevronRightIcon aria-hidden="true" />
				</Button>
			</div>
		</nav>
	);
}
