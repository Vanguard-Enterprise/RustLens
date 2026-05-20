"use client";

import { useState } from "react";

import { Badge } from "@/components/ui/badge";
import { RelativeTime } from "@/components/admin/relative-time";
import type {
	AdminCommandUsageFilterOptions,
	AdminCommandUsageItem,
	AdminCommandUsagePage,
} from "@/lib/rustlens-api";

type Props = {
	userId: string;
	data:   AdminCommandUsagePage;
	filterOptions?: AdminCommandUsageFilterOptions; // consumed in D5/D6
};

function outcomeBadgeVariant(o: AdminCommandUsageItem["outcome"]): "secondary" | "destructive" | "outline" {
	switch (o) {
		case "success": return "secondary";
		case "error":   return "destructive";
		case "banned":  return "destructive";
		case "denied":  return "outline";
	}
}

function summarizeInput(item: AdminCommandUsageItem): string {
	const entries = Object.entries(item.options ?? {});
	if (entries.length === 0) {
		return item.target ?? "—";
	}
	const joined = entries
		.map(([k, v]) => `${k}=${typeof v === "string" ? v : JSON.stringify(v)}`)
		.join(", ");
	return joined.length > 120 ? `${joined.slice(0, 117)}…` : joined;
}

function commandLabel(item: AdminCommandUsageItem): string {
	if (item.subcommand) return `${item.command} ${item.subcommand}`;
	return item.command;
}

export function UsageSection({ userId, data, filterOptions: _filterOptions }: Props) {
	const [expandedId, setExpandedId] = useState<number | null>(null);
	const { items, total, page, pageSize, summary } = data;
	const topCommands = summary.topCommands ?? [];

	return (
		<section id="usage" className="flex flex-col gap-4 rounded-lg border bg-card p-4">
			<div>
				<h2 className="text-lg font-semibold">Usage</h2>
				<p className="text-sm text-muted-foreground">
					Bot interactions this user has run in the last 90 days.
				</p>
			</div>

			<div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
				<div className="rounded-md border p-3">
					<div className="text-xs uppercase text-muted-foreground">Total (90d)</div>
					<div className="text-2xl font-semibold">{summary.totalCount}</div>
				</div>
				<div className="rounded-md border p-3">
					<div className="text-xs uppercase text-muted-foreground">Error rate</div>
					<div className="text-2xl font-semibold">
						{summary.totalCount === 0 ? "—" : `${(summary.errorRate * 100).toFixed(1)}%`}
					</div>
					<div className="text-xs text-muted-foreground">{summary.errorCount} errors</div>
				</div>
				<div className="rounded-md border p-3">
					<div className="text-xs uppercase text-muted-foreground">Top commands</div>
					<div className="mt-1 flex flex-wrap gap-1">
						{topCommands.length === 0 ? (
							<span className="text-sm text-muted-foreground">—</span>
						) : (
							topCommands.map((tc) => (
								<Badge key={tc.command} variant="secondary">
									{tc.command} · {tc.count}
								</Badge>
							))
						)}
					</div>
				</div>
			</div>

			{items.length === 0 ? (
				<p className="text-sm text-muted-foreground">No bot usage in the last 90 days.</p>
			) : (
				<>
					<ul className="flex flex-col divide-y">
						{items.map((it) => {
							const expanded = expandedId === it.id;
							return (
								<li key={it.id} className="py-2">
									<button
										type="button"
										aria-label={commandLabel(it)}
										onClick={() => setExpandedId(expanded ? null : it.id)}
										className="grid w-full grid-cols-12 items-center gap-2 text-left text-sm hover:bg-muted/40 rounded px-1 py-1"
									>
										<div className="col-span-2 text-xs text-muted-foreground">
											<RelativeTime iso={it.createdAt} />
										</div>
										<div className="col-span-1">
											<Badge variant="outline">{it.interactionType}</Badge>
										</div>
										<code className="col-span-3 truncate rounded bg-muted px-1 py-0.5 text-xs">
											{commandLabel(it)}
										</code>
										<div className="col-span-4 truncate text-xs text-muted-foreground">
											{summarizeInput(it)}
										</div>
										<div className="col-span-2 text-right">
											<Badge variant={outcomeBadgeVariant(it.outcome)}>{it.outcome}</Badge>
										</div>
									</button>
									{expanded && (
										<div className="mt-2 grid grid-cols-1 gap-2 rounded-md border bg-muted/30 p-3 text-xs">
											<div>
												<div className="font-semibold text-muted-foreground">What they put in</div>
												<pre className="mt-1 whitespace-pre-wrap break-words">
													{JSON.stringify(it.options ?? {}, null, 2)}
												</pre>
											</div>
											<div>
												<div className="font-semibold text-muted-foreground">What happened</div>
												<div>{it.outcome}{it.errorCode ? ` — ${it.errorCode}` : ""}</div>
											</div>
											{it.guildId && (
												<div className="text-muted-foreground">guild: {it.guildId}</div>
											)}
										</div>
									)}
								</li>
							);
						})}
					</ul>

					<UsagePagination
						userId={userId}
						page={page}
						pageSize={pageSize}
						total={total}
					/>
				</>
			)}
		</section>
	);
}

function UsagePagination({
	userId: _userId,
	page,
	pageSize,
	total,
}: { userId: string; page: number; pageSize: number; total: number }) {
	const lastPage = Math.max(1, Math.ceil(total / pageSize));
	if (lastPage <= 1) return null;

	const mkHref = (p: number) => {
		const sp = new URLSearchParams();
		sp.set("usagePage", String(p));
		return `?${sp.toString()}#usage`;
	};

	return (
		<div className="flex items-center justify-between text-xs">
			<span className="text-muted-foreground">
				Page {page} of {lastPage} · {total} total
			</span>
			<div className="flex gap-2">
				{page > 1 && (
					<a href={mkHref(page - 1)} className="underline">
						Prev
					</a>
				)}
				{page < lastPage && (
					<a href={mkHref(page + 1)} className="underline">
						Next
					</a>
				)}
			</div>
		</div>
	);
}
