"use client";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { Button } from "@/components/ui/button";

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
	return (
		<div className="flex items-center gap-2 mt-4">
			<span className="text-sm text-muted-foreground">
				{total === 0
					? "No users"
					: `${(page - 1) * pageSize + 1}–${Math.min(page * pageSize, total)} of ${total}`}
			</span>
			<div className="flex-1" />
			<Button
				variant="outline"
				size="sm"
				disabled={page <= 1}
				onClick={() => go(page - 1)}
			>
				Previous
			</Button>
			<span className="text-sm">
				Page {page} / {lastPage}
			</span>
			<Button
				variant="outline"
				size="sm"
				disabled={page >= lastPage}
				onClick={() => go(page + 1)}
			>
				Next
			</Button>
		</div>
	);
}
