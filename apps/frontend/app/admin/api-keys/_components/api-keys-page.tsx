"use client";

import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useState } from "react";
import type { AdminApiKeysPage } from "@/lib/rustlens-api";
import { ApiKeysTable } from "./api-keys-table";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";

export function ApiKeysPage({
	data,
	initialQuery,
}: {
	data: AdminApiKeysPage;
	initialQuery: { q?: string; includeRevoked?: boolean; page: number };
}) {
	const router = useRouter();
	const pathname = usePathname();
	const search = useSearchParams();
	const [q, setQ] = useState(initialQuery.q ?? "");
	const [includeRevoked, setIncludeRevoked] = useState(
		initialQuery.includeRevoked ?? false,
	);

	function applyFilters(next: { q?: string; includeRevoked?: boolean }) {
		const sp = new URLSearchParams(search?.toString());
		if (next.q) sp.set("q", next.q);
		else sp.delete("q");
		if (next.includeRevoked) sp.set("includeRevoked", "true");
		else sp.delete("includeRevoked");
		sp.delete("page");
		router.push(`${pathname}?${sp.toString()}`);
	}

	return (
		<div className="flex flex-col gap-4">
			<div className="flex flex-wrap items-center gap-3">
				<Input
					value={q}
					placeholder="Search by name or prefix"
					onChange={(e) => setQ(e.target.value)}
					onBlur={() => applyFilters({ q, includeRevoked })}
					onKeyDown={(e) => {
						if (e.key === "Enter") applyFilters({ q, includeRevoked });
					}}
					className="max-w-xs"
				/>
				<label className="flex items-center gap-2 text-sm">
					<Checkbox
						checked={includeRevoked}
						onCheckedChange={(v) => {
							const next = v === true;
							setIncludeRevoked(next);
							applyFilters({ q, includeRevoked: next });
						}}
					/>
					Show revoked
				</label>
				<Button
					className="ml-auto"
					onClick={() => alert("Create dialog wired in Task 19")}
				>
					New API key
				</Button>
			</div>
			<ApiKeysTable items={data.items} />
		</div>
	);
}
