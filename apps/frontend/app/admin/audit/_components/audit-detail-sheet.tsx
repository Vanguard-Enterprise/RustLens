"use client";

import * as React from "react";
import {
	ArrowRightIcon,
	CalendarClockIcon,
	CheckIcon,
	CopyIcon,
	HashIcon,
	MessageSquareTextIcon,
} from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { DayTime } from "@/components/admin/day-time";
import { UserLink } from "@/components/admin/user-link";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogHeader,
	DialogTitle,
} from "@/components/ui/dialog";
import { cn } from "@/lib/utils";
import type { AuditEventItem } from "@/lib/rustlens-api";

type Props = {
	event: AuditEventItem | null;
	status: "ok" | "not-found" | "loading";
	onClose: () => void;
};

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

export function AuditDetailSheet({ event, status, onClose }: Props) {
	const open = status !== "loading" || event !== null;
	return (
		<Dialog open={open} onOpenChange={(v) => { if (!v) onClose(); }}>
			<DialogContent className="max-w-2xl gap-0 overflow-hidden p-0 sm:max-w-2xl">
				{status === "not-found" ? (
					<NotFound />
				) : status === "loading" || !event ? (
					<Loading />
				) : (
					<EventBody event={event} />
				)}
			</DialogContent>
		</Dialog>
	);
}

function NotFound() {
	return (
		<div className="flex flex-col gap-3 p-6">
			<DialogHeader>
				<DialogTitle>Event not found</DialogTitle>
				<DialogDescription>
					The event ID in the URL did not match any audit row.
				</DialogDescription>
			</DialogHeader>
		</div>
	);
}

function Loading() {
	return (
		<div className="flex flex-col gap-3 p-6">
			<DialogHeader>
				<DialogTitle>Loading…</DialogTitle>
			</DialogHeader>
			<div className="h-24 animate-pulse rounded-md bg-muted" />
		</div>
	);
}

function ResultBadge({ result }: { result: AuditEventItem["result"] }) {
	if (!result) return null;
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

function PartyCard({
	role,
	userId,
	displayName,
	avatarUrl,
}: {
	role: "Actor" | "Target";
	userId: string | null | undefined;
	displayName: string | null | undefined;
	avatarUrl: string | null | undefined;
}) {
	return (
		<div className="flex min-w-0 flex-1 flex-col gap-1.5 rounded-lg border bg-muted/30 p-3">
			<div className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">
				{role}
			</div>
			{userId ? (
				<UserLink
					userId={userId}
					displayName={displayName}
					avatarUrl={avatarUrl}
					className="text-sm font-medium"
				/>
			) : (
				<span className="text-sm text-muted-foreground">
					{role === "Actor" ? "System" : "—"}
				</span>
			)}
		</div>
	);
}

function EventBody({ event }: { event: AuditEventItem }) {
	const [copied, setCopied] = React.useState(false);
	const copy = async () => {
		try {
			await navigator.clipboard.writeText(window.location.href);
			setCopied(true);
			setTimeout(() => setCopied(false), 1500);
		} catch {
			/* clipboard blocked; ignore */
		}
	};

	const hasTarget = !!event.targetUserId;
	const reason = event.reason?.trim();

	return (
		<div className="flex max-h-[85vh] flex-col">
			{/* Hero */}
			<div className="flex flex-col gap-2 border-b bg-gradient-to-b from-muted/40 to-transparent px-6 pt-5 pb-4 pr-14">
				<div className="flex min-w-0 flex-col gap-1">
					<DialogTitle className="text-lg font-semibold leading-tight">
						{humanizeEvent(event.eventType)}
					</DialogTitle>
					<DialogDescription className="font-mono text-xs">
						{event.eventType}
					</DialogDescription>
				</div>
				<div className="flex flex-wrap items-center gap-x-3 gap-y-1.5">
					<ResultBadge result={event.result ?? null} />
					<div className="flex items-center gap-1.5 text-xs text-muted-foreground">
						<CalendarClockIcon className="size-3.5" />
						<DayTime iso={event.createdAt} />
					</div>
				</div>
			</div>

			{/* Body */}
			<div className="flex flex-col gap-4 overflow-y-auto px-6 py-4">
				<div className="flex items-stretch gap-2">
					<PartyCard
						role="Actor"
						userId={event.actorUserId}
						displayName={event.actorDisplayName}
						avatarUrl={event.actorAvatarUrl}
					/>
					{hasTarget ? (
						<>
							<div className="flex items-center text-muted-foreground">
								<ArrowRightIcon className="size-4" />
							</div>
							<PartyCard
								role="Target"
								userId={event.targetUserId}
								displayName={event.targetDisplayName}
								avatarUrl={event.targetAvatarUrl}
							/>
						</>
					) : null}
				</div>

				{reason ? (
					<div className="flex gap-3 rounded-lg border border-amber-500/30 bg-amber-500/5 p-3">
						<MessageSquareTextIcon className="mt-0.5 size-4 shrink-0 text-amber-600 dark:text-amber-400" />
						<div className="flex min-w-0 flex-col gap-1">
							<div className="text-[10px] font-semibold uppercase tracking-wider text-amber-700 dark:text-amber-300">
								Reason
							</div>
							<p className="whitespace-pre-wrap break-words text-sm">{reason}</p>
						</div>
					</div>
				) : null}

				<div className="flex flex-col gap-1.5">
					<div className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">
						Payload
					</div>
					<pre className="max-h-72 overflow-auto rounded-lg border bg-muted/40 p-3 text-xs leading-relaxed">
{JSON.stringify(event.payload, null, 2)}
					</pre>
				</div>
			</div>

			{/* Footer */}
			<div className="flex items-center justify-between gap-2 border-t bg-muted/30 px-6 py-3 text-xs">
				<div className="flex min-w-0 items-center gap-1.5 text-muted-foreground">
					<HashIcon className="size-3.5 shrink-0" />
					<code className="truncate font-mono">{event.id}</code>
				</div>
				<Button variant="outline" size="sm" onClick={copy} className="gap-1.5">
					{copied ? (
						<>
							<CheckIcon className="size-3.5" />
							Copied
						</>
					) : (
						<>
							<CopyIcon className="size-3.5" />
							Copy link
						</>
					)}
				</Button>
			</div>
		</div>
	);
}
