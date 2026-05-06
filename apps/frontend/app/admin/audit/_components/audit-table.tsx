"use client";

import * as React from "react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { RelativeTime } from "@/components/admin/relative-time";
import { UserLink } from "@/components/admin/user-link";
import {
	Table,
	TableBody,
	TableCell,
	TableHead,
	TableHeader,
	TableRow,
} from "@/components/ui/table";
import { cn } from "@/lib/utils";
import type { AuditEventItem } from "@/lib/rustlens-api";

type Props = {
	items: AuditEventItem[];
	selectedId: string | null;
	onSelect: (id: string) => void;
	hasFilters: boolean;
	onClearFilters: () => void;
};

function ResultBadge({ result }: { result: AuditEventItem["result"] }) {
	if (!result) return <span className="text-xs text-muted-foreground">—</span>;
	return (
		<Badge variant={result === "error" ? "destructive" : "secondary"}>{result}</Badge>
	);
}

export function AuditTable({ items, selectedId, onSelect, hasFilters, onClearFilters }: Props) {
	return (
		<div className="overflow-hidden rounded-lg border">
			<Table>
				<TableHeader>
					<TableRow>
						<TableHead className="w-[12%]">Time</TableHead>
						<TableHead className="w-[18%]">Actor</TableHead>
						<TableHead className="w-[18%]">Target</TableHead>
						<TableHead className="w-[30%]">Event</TableHead>
						<TableHead className="w-[8%]">Result</TableHead>
						<TableHead className="w-[14%]">Reason</TableHead>
					</TableRow>
				</TableHeader>
				<TableBody>
					{items.length === 0 ? (
						<TableRow>
							<TableCell colSpan={6} className="h-24 text-center">
								{hasFilters ? (
									<div className="flex flex-col items-center gap-2">
										<span>No events match these filters.</span>
										<Button variant="outline" size="sm" onClick={onClearFilters}>
											Clear filters
										</Button>
									</div>
								) : (
									<span>No audit events yet.</span>
								)}
							</TableCell>
						</TableRow>
					) : (
						items.map((it) => (
							<TableRow
								key={it.id}
								className={cn("cursor-pointer hover:bg-muted/50", selectedId === it.id && "bg-accent")}
								onClick={() => onSelect(it.id)}
							>
								<TableCell>
									<RelativeTime iso={it.createdAt} />
								</TableCell>
								<TableCell>
									{it.actorUserId ? (
										<UserLink
											userId={it.actorUserId}
											displayName={it.actorDisplayName}
											avatarUrl={it.actorAvatarUrl}
										/>
									) : (
										<span className="text-xs text-muted-foreground">—</span>
									)}
								</TableCell>
								<TableCell>
									{it.targetUserId ? (
										<UserLink
											userId={it.targetUserId}
											displayName={it.targetDisplayName}
											avatarUrl={it.targetAvatarUrl}
										/>
									) : (
										<span className="text-xs text-muted-foreground">—</span>
									)}
								</TableCell>
								<TableCell>
									<code className="rounded bg-muted px-1 py-0.5 text-xs">{it.eventType}</code>
								</TableCell>
								<TableCell>
									<ResultBadge result={it.result ?? null} />
								</TableCell>
								<TableCell>
									<span className="block truncate text-xs text-muted-foreground" title={it.reason ?? undefined}>
										{it.reason?.trim() ? it.reason : "—"}
									</span>
								</TableCell>
							</TableRow>
						))
					)}
				</TableBody>
			</Table>
		</div>
	);
}
