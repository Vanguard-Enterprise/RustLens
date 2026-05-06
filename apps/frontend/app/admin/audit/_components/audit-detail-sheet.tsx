"use client";

import * as React from "react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { RelativeTime } from "@/components/admin/relative-time";
import { UserLink } from "@/components/admin/user-link";
import {
	Sheet,
	SheetContent,
	SheetDescription,
	SheetHeader,
	SheetTitle,
} from "@/components/ui/sheet";
import type { AuditEventItem } from "@/lib/rustlens-api";

type Props = {
	event: AuditEventItem | null;
	status: "ok" | "not-found" | "loading";
	onClose: () => void;
};

export function AuditDetailSheet({ event, status, onClose }: Props) {
	const open = status !== "loading" || event !== null;
	return (
		<Sheet open={open} onOpenChange={(v) => { if (!v) onClose(); }}>
			<SheetContent side="right" className="sm:max-w-xl">
				{status === "not-found" ? (
					<NotFound onClose={onClose} />
				) : status === "loading" || !event ? (
					<Loading />
				) : (
					<EventBody event={event} />
				)}
			</SheetContent>
		</Sheet>
	);
}

function NotFound({ onClose }: { onClose: () => void }) {
	return (
		<div className="flex h-full flex-col gap-4">
			<SheetHeader>
				<SheetTitle>Event not found</SheetTitle>
				<SheetDescription>The event ID in the URL did not match any audit row.</SheetDescription>
			</SheetHeader>
			<Button variant="outline" onClick={onClose} className="self-start">
				Close
			</Button>
		</div>
	);
}

function Loading() {
	return (
		<div className="flex h-full flex-col gap-4">
			<SheetHeader>
				<SheetTitle>Loading…</SheetTitle>
			</SheetHeader>
			<div className="h-24 animate-pulse rounded bg-muted" />
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
	return (
		<div className="flex h-full flex-col gap-4 overflow-y-auto">
			<SheetHeader>
				<SheetTitle className="flex items-center gap-2">
					<code className="rounded bg-muted px-1 py-0.5 text-xs">{event.eventType}</code>
					{event.result ? (
						<Badge variant={event.result === "error" ? "destructive" : "secondary"}>
							{event.result}
						</Badge>
					) : null}
				</SheetTitle>
				<SheetDescription>
					<RelativeTime iso={event.createdAt} /> · {new Date(event.createdAt).toLocaleString()}
				</SheetDescription>
			</SheetHeader>

			<div className="grid grid-cols-2 gap-3 text-sm">
				<div>
					<div className="text-xs font-medium uppercase text-muted-foreground">Actor</div>
					{event.actorUserId ? (
						<UserLink
							userId={event.actorUserId}
							displayName={event.actorDisplayName}
							avatarUrl={event.actorAvatarUrl}
						/>
					) : (
						<span className="text-muted-foreground">—</span>
					)}
				</div>
				<div>
					<div className="text-xs font-medium uppercase text-muted-foreground">Target</div>
					{event.targetUserId ? (
						<UserLink
							userId={event.targetUserId}
							displayName={event.targetDisplayName}
							avatarUrl={event.targetAvatarUrl}
						/>
					) : (
						<span className="text-muted-foreground">—</span>
					)}
				</div>
			</div>

			{event.reason?.trim() ? (
				<div>
					<div className="text-xs font-medium uppercase text-muted-foreground">Reason</div>
					<p className="text-sm">{event.reason}</p>
				</div>
			) : null}

			<div>
				<div className="text-xs font-medium uppercase text-muted-foreground">Payload</div>
				<pre className="max-h-96 overflow-auto rounded bg-muted p-3 text-xs">
{JSON.stringify(event.payload, null, 2)}
				</pre>
			</div>

			<div className="mt-auto flex items-center justify-between gap-2 border-t pt-3 text-xs text-muted-foreground">
				<span>
					Event ID: <code className="rounded bg-muted px-1 py-0.5">{event.id}</code>
				</span>
				<Button variant="outline" size="sm" onClick={copy}>
					{copied ? "Copied" : "Copy link"}
				</Button>
			</div>
		</div>
	);
}
