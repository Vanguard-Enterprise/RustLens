"use client";

import * as React from "react";
import { MessageSquareTextIcon } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { DayTime } from "@/components/admin/day-time";
import {
	Popover,
	PopoverContent,
	PopoverTrigger,
} from "@/components/ui/popover";
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
	const isError = result === "error";
	return (
		<Badge
			variant={isError ? "destructive" : "secondary"}
			className={cn(
				"capitalize",
				!isError &&
					"bg-emerald-500/15 text-emerald-700 dark:bg-emerald-500/20 dark:text-emerald-300",
			)}
		>
			{isError ? "Failure" : result}
		</Badge>
	);
}

function humanizeEvent(eventType: string): string {
	return eventType
		.split(".")
		.map((part) =>
			part
				.replace(/[_-]/g, " ")
				.split(" ")
				.filter(Boolean)
				.map((w) => w.charAt(0).toUpperCase() + w.slice(1))
				.join(" "),
		)
		.join(" · ");
}

function ReasonCell({ reason }: { reason: string | null | undefined }) {
	const trimmed = reason?.trim();
	if (!trimmed) {
		return <span className="text-xs text-muted-foreground">—</span>;
	}
	return (
		<Popover>
			<PopoverTrigger asChild>
				<Button
					variant="ghost"
					size="icon"
					className="size-7"
					aria-label="View reason"
					onClick={(e) => e.stopPropagation()}
				>
					<MessageSquareTextIcon className="size-4" />
				</Button>
			</PopoverTrigger>
			<PopoverContent
				align="end"
				className="w-80"
				onClick={(e) => e.stopPropagation()}
			>
				<div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
					Reason
				</div>
				<p className="text-sm whitespace-pre-wrap break-words">{trimmed}</p>
			</PopoverContent>
		</Popover>
	);
}

export function AuditTable({
	items,
	selectedId,
	onSelect,
	hasFilters,
	onClearFilters,
}: Props) {
	return (
		<div className="overflow-hidden rounded-lg border">
			<Table>
				<TableHeader>
					<TableRow>
						<TableHead className="w-[18%]">Actor</TableHead>
						<TableHead className="w-[18%]">Target</TableHead>
						<TableHead className="w-[28%]">Event</TableHead>
						<TableHead className="w-[10%]">Result</TableHead>
						<TableHead className="w-[20%]">Date / Time</TableHead>
						<TableHead className="w-[6%] text-right">Reason</TableHead>
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
								className={cn(
									"cursor-pointer hover:bg-muted/50",
									selectedId === it.id && "bg-accent",
								)}
								onClick={() => onSelect(it.id)}
							>
								<TableCell>
									{it.actorUserId ? (
										<UserLink
											userId={it.actorUserId}
											displayName={it.actorDisplayName}
											avatarUrl={it.actorAvatarUrl}
										/>
									) : (
										<span className="text-muted-foreground">System</span>
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
									<span className="font-medium" title={it.eventType}>
										{humanizeEvent(it.eventType)}
									</span>
								</TableCell>
								<TableCell>
									<ResultBadge result={it.result ?? null} />
								</TableCell>
								<TableCell className="text-sm text-muted-foreground">
									<DayTime iso={it.createdAt} />
								</TableCell>
								<TableCell className="text-right">
									<ReasonCell reason={it.reason} />
								</TableCell>
							</TableRow>
						))
					)}
				</TableBody>
			</Table>
		</div>
	);
}
