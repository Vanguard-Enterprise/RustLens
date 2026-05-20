"use client";

import * as React from "react";

import { Badge } from "@/components/ui/badge";
import { RelativeTime } from "@/components/admin/relative-time";
import { SortHeader, type SortDir } from "@/components/admin/sort-header";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import {
	Table, TableBody, TableCell, TableHead, TableHeader, TableRow,
} from "@/components/ui/table";
import { guildIconUrl, userAvatarUrl } from "@/lib/discord-cdn";
import type {
	AdminCommandUsageFilterOptions,
	AdminCommandUsageItem,
	AdminCommandUsagePage,
	AdminCommandUsageTarget,
} from "@/lib/rustlens-api";
import {
	getUserCommandUsageChildrenAction,
	getUserCommandUsageTargetsAction,
} from "../actions";
import { UsageFilterBar } from "./usage-filter-bar";
import { UsageRecipientStack } from "./usage-recipient-stack";

type Props = {
	userId:        string;
	data:          AdminCommandUsagePage;
	filterOptions?: AdminCommandUsageFilterOptions;
};

type SortKey = "when" | "command" | "outcome" | "latency";

function outcomeBadgeVariant(o: AdminCommandUsageItem["outcome"]): "secondary" | "destructive" | "outline" {
	switch (o) {
		case "success": return "secondary";
		case "error":   return "destructive";
		case "banned":  return "destructive";
		case "denied":  return "outline";
	}
}

function commandLabel(item: AdminCommandUsageItem): string {
	if (item.subcommand) return `/${item.command} ${item.subcommand}`;
	return `/${item.command}`;
}

function compareItems(a: AdminCommandUsageItem, b: AdminCommandUsageItem, key: SortKey): number {
	switch (key) {
		case "when":     return a.createdAt.localeCompare(b.createdAt);
		case "command":  return commandLabel(a).localeCompare(commandLabel(b));
		case "outcome":  return a.outcome.localeCompare(b.outcome);
		case "latency":  return a.latencyMs - b.latencyMs;
	}
}

function relationBucket(targets: AdminCommandUsageTarget[]) {
	return {
		subjects:   targets.filter((t) => t.relation === "subject"),
		scopes:     targets.filter((t) => t.relation === "scope"),
		recipients: targets.filter((t) => t.relation === "recipient"),
	};
}

export function UsageSection({ userId, data, filterOptions }: Props) {
	const [expandedId, setExpandedId] = React.useState<number | null>(null);
	const [sortKey, setSortKey] = React.useState<SortKey>("when");
	const [sortDir, setSortDir] = React.useState<SortDir>("desc");
	const [fullTargetsCache, setFullTargetsCache] = React.useState<Record<number, AdminCommandUsageTarget[]>>({});
	const [childrenCache, setChildrenCache] = React.useState<Record<number, AdminCommandUsageItem[]>>({});

	const { items, total, page, pageSize, summary } = data;
	const topCommands = summary.topCommands ?? [];

	const toggleSort = (key: SortKey) => {
		if (key === sortKey) setSortDir((d) => (d === "asc" ? "desc" : "asc"));
		else { setSortKey(key); setSortDir(key === "latency" ? "desc" : "asc"); }
	};

	const sorted = React.useMemo(() => {
		const copy = [...items];
		copy.sort((a, b) => {
			const cmp = compareItems(a, b, sortKey);
			return sortDir === "asc" ? cmp : -cmp;
		});
		return copy;
	}, [items, sortKey, sortDir]);

	const fetchFullTargets = async (item: AdminCommandUsageItem) => {
		if (!item.targetTotal || fullTargetsCache[item.id]) return;
		try {
			const res = await getUserCommandUsageTargetsAction(userId, item.id);
			setFullTargetsCache((m) => ({ ...m, [item.id]: res.targets }));
		} catch (err) {
			console.warn("[usage] failed to load full targets", err);
		}
	};

	const fetchChildren = async (item: AdminCommandUsageItem) => {
		if (!item.childCount || childrenCache[item.id]) return;
		try {
			const res = await getUserCommandUsageChildrenAction(userId, item.id);
			setChildrenCache((m) => ({ ...m, [item.id]: res.items }));
		} catch (err) {
			console.warn("[usage] failed to load children", err);
		}
	};

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

			{filterOptions && <UsageFilterBar filterOptions={filterOptions} />}

			<div className="text-sm text-muted-foreground">
				{total === 0 ? "No bot usage matches the filters." : `Showing ${items.length} of ${total}`}
			</div>

			{total === 0 ? null : (
				<div className="overflow-hidden rounded-lg border">
					<Table>
						<TableHeader>
							<TableRow>
								<SortHeader<SortKey> label="When"     sortKey="when"    currentKey={sortKey} currentDir={sortDir} onSort={toggleSort} />
								<TableHead>Where</TableHead>
								<TableHead>Type</TableHead>
								<SortHeader<SortKey> label="Command"  sortKey="command" currentKey={sortKey} currentDir={sortDir} onSort={toggleSort} />
								<TableHead>Target / Recipients</TableHead>
								<SortHeader<SortKey> label="Outcome"  sortKey="outcome" currentKey={sortKey} currentDir={sortDir} onSort={toggleSort} />
								<SortHeader<SortKey> label="Latency"  sortKey="latency" currentKey={sortKey} currentDir={sortDir} onSort={toggleSort} className="text-right" align="right" />
							</TableRow>
						</TableHeader>
						<TableBody>
							{sorted.map((item) => {
								const expanded = expandedId === item.id;
								const buckets = relationBucket(item.targets);
								return (
									<React.Fragment key={item.id}>
										<TableRow
											className="cursor-pointer"
											onClick={() => {
												setExpandedId(expanded ? null : item.id);
												if (!expanded) {
													void fetchFullTargets(item);
													void fetchChildren(item);
												}
											}}
										>
											<TableCell className="text-xs text-muted-foreground">
												<RelativeTime iso={item.createdAt} />
											</TableCell>
											<TableCell>
												<WhereCell item={item} />
											</TableCell>
											<TableCell>
												<Badge variant="outline">{item.interactionType}</Badge>
											</TableCell>
											<TableCell>
												<div className="flex items-center gap-1.5">
													<code className="rounded bg-muted px-1 py-0.5 text-xs">{commandLabel(item)}</code>
													{item.childCount && item.childCount > 0 ? (
														<Badge variant="outline" className="text-[10px]" title={`${item.childCount} follow-up interaction${item.childCount === 1 ? "" : "s"}`}>
															+{item.childCount}
														</Badge>
													) : null}
												</div>
											</TableCell>
											<TableCell>
												{buckets.recipients.length > 0 ? (
													<UsageRecipientStack recipients={buckets.recipients} />
												) : buckets.subjects[0] ? (
													<TargetChip t={buckets.subjects[0]} />
												) : buckets.scopes[0] ? (
													<TargetChip t={buckets.scopes[0]} />
												) : item.target ? (
													<code className="text-xs text-muted-foreground">{item.target}</code>
												) : (
													<span className="text-xs text-muted-foreground">—</span>
												)}
											</TableCell>
											<TableCell>
												<Badge variant={outcomeBadgeVariant(item.outcome)}>{item.outcome}</Badge>
											</TableCell>
											<TableCell className="text-right tabular-nums text-xs">{item.latencyMs}ms</TableCell>
										</TableRow>
										{expanded && (
											<TableRow>
												<TableCell colSpan={7} className="bg-muted/30">
													<ExpandedRow
														item={item}
														fullTargets={fullTargetsCache[item.id]}
														children={childrenCache[item.id]}
														guildId={item.guildId}
													/>
												</TableCell>
											</TableRow>
										)}
									</React.Fragment>
								);
							})}
						</TableBody>
					</Table>
				</div>
			)}

			{total > 0 && (
				<UsagePagination userId={userId} page={page} pageSize={pageSize} total={total} />
			)}
		</section>
	);
}

function WhereCell({ item }: { item: AdminCommandUsageItem }) {
	// Prefer the backend-supplied channelKind when present. Older rows may not
	// have it — fall back to guildId-presence so we still render something useful.
	const kind = item.channelKind ?? (item.guildId ? "guild" : "unknown");

	if (kind === "guild" && item.guildId) {
		const name = item.guildName?.trim() || item.guildId;
		const src  = guildIconUrl(item.guildId, item.guildIcon ?? null, 64);
		return (
			<div className="flex items-center gap-2">
				<Avatar className="size-5">
					{src && <AvatarImage src={src} alt={name} />}
					<AvatarFallback className="text-[10px]">{name.charAt(0).toUpperCase()}</AvatarFallback>
				</Avatar>
				<span className="truncate text-sm">{name}</span>
			</div>
		);
	}

	if (kind === "group_dm") {
		const name = item.channelName?.trim() || "Group DM";
		return (
			<div className="flex items-center gap-2">
				<span className="truncate text-sm">{name}</span>
				<Badge variant="outline" className="text-[10px]">GC</Badge>
			</div>
		);
	}

	if (kind === "dm") {
		return <Badge variant="outline">DM</Badge>;
	}

	// Unknown / missing — surface the channelId if we have one so admins can still
	// trace the row in Discord, otherwise fall through to an em-dash.
	return (
		<span className="text-xs text-muted-foreground">
			{item.channelId ? <code>{item.channelId}</code> : "—"}
		</span>
	);
}

function TargetChip({ t }: { t: AdminCommandUsageTarget }) {
	const label = t.displayName?.trim() || t.snowflake;
	if (t.kind === "user") {
		const src = userAvatarUrl(t.snowflake, t.avatarHash, 64);
		return (
			<div className="flex items-center gap-2">
				<Avatar className="size-5">
					{src && <AvatarImage src={src} alt={label} />}
					<AvatarFallback className="text-[10px]">{label.charAt(0).toUpperCase()}</AvatarFallback>
				</Avatar>
				<span className="text-sm">{label}</span>
			</div>
		);
	}
	if (t.kind === "guild") {
		const src = guildIconUrl(t.snowflake, t.avatarHash, 64);
		return (
			<div className="flex items-center gap-2">
				<Avatar className="size-5">
					{src && <AvatarImage src={src} alt={label} />}
					<AvatarFallback className="text-[10px]">{label.charAt(0).toUpperCase()}</AvatarFallback>
				</Avatar>
				<span className="text-sm">{label}</span>
			</div>
		);
	}
	return <Badge variant="outline">{t.kind}: {label}</Badge>;
}

function ExpandedRow({
	item,
	fullTargets,
	children,
	guildId,
}: {
	item: AdminCommandUsageItem;
	fullTargets: AdminCommandUsageTarget[] | undefined;
	children: AdminCommandUsageItem[] | undefined;
	guildId: string | null;
}) {
	const targets = fullTargets ?? item.targets;
	const buckets = relationBucket(targets);
	const childCount = item.childCount ?? 0;
	return (
		<div className="grid grid-cols-1 gap-3 p-3 text-xs">
			{guildId && (
				<div className="text-muted-foreground">guild: <code>{guildId}</code></div>
			)}
			<div>
				<div className="font-semibold text-muted-foreground">What they put in</div>
				<pre className="mt-1 whitespace-pre-wrap break-words">
					{JSON.stringify(item.options ?? {}, null, 2)}
				</pre>
			</div>
			{(buckets.subjects.length + buckets.scopes.length + buckets.recipients.length > 0) && (
				<div className="flex flex-col gap-2">
					{buckets.subjects.length > 0 && (
						<TargetGroup title="Subject" targets={buckets.subjects} />
					)}
					{buckets.scopes.length > 0 && (
						<TargetGroup title="Scope" targets={buckets.scopes} />
					)}
					{buckets.recipients.length > 0 && (
						<TargetGroup
							title={item.targetTotal && item.targetTotal > item.targets.length
								? `Recipients (${item.targetTotal})`
								: `Recipients (${buckets.recipients.length})`}
							targets={buckets.recipients}
						/>
					)}
				</div>
			)}
			{childCount > 0 && (
				<FollowupsBlock count={childCount} items={children} />
			)}
			<div className="text-muted-foreground">
				{item.outcome}{item.errorCode ? ` — ${item.errorCode}` : ""} · {item.latencyMs}ms
			</div>
		</div>
	);
}

function FollowupsBlock({
	count,
	items,
}: {
	count: number;
	items: AdminCommandUsageItem[] | undefined;
}) {
	return (
		<div>
			<div className="font-semibold text-muted-foreground">
				Follow-ups ({count})
			</div>
			{!items ? (
				<div className="mt-1 text-muted-foreground">Loading…</div>
			) : items.length === 0 ? (
				<div className="mt-1 text-muted-foreground">No follow-ups linked.</div>
			) : (
				<div className="mt-1 flex flex-col gap-2">
					{items.map((child) => (
						<div key={child.id} className="rounded border bg-background p-2">
							<div className="flex flex-wrap items-center gap-2">
								<Badge variant="outline">{child.interactionType}</Badge>
								<RelativeTime iso={child.createdAt} />
								<Badge variant={outcomeBadgeVariant(child.outcome)}>{child.outcome}</Badge>
								<span className="tabular-nums text-muted-foreground">{child.latencyMs}ms</span>
								{child.errorCode ? (
									<span className="text-muted-foreground">— {child.errorCode}</span>
								) : null}
							</div>
							{child.options && Object.keys(child.options).length > 0 && (
								<pre className="mt-1 whitespace-pre-wrap break-words">
									{JSON.stringify(child.options, null, 2)}
								</pre>
							)}
						</div>
					))}
				</div>
			)}
		</div>
	);
}

function TargetGroup({ title, targets }: { title: string; targets: AdminCommandUsageTarget[] }) {
	return (
		<div>
			<div className="font-semibold text-muted-foreground">{title}</div>
			<div className="mt-1 flex flex-wrap gap-2">
				{targets.map((t) => (
					<TargetChip key={`${t.kind}:${t.snowflake}`} t={t} />
				))}
			</div>
		</div>
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
		if (typeof window === "undefined") {
			return `?usagePage=${p}#usage`;
		}
		const sp = new URLSearchParams(window.location.search);
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
