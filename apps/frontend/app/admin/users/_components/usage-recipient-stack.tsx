"use client";

import * as React from "react";
import Link from "next/link";

import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { HoverCard, HoverCardContent, HoverCardTrigger } from "@/components/ui/hover-card";
import { userAvatarUrl } from "@/lib/discord-cdn";
import type { AdminCommandUsageTarget } from "@/lib/rustlens-api";

type Props = {
	recipients: AdminCommandUsageTarget[];
	max?:       number;
};

function recipientLabel(t: AdminCommandUsageTarget): string {
	if (t.displayName && t.displayName.trim()) return t.displayName;
	return t.snowflake;
}

function recipientInitial(t: AdminCommandUsageTarget): string {
	const l = recipientLabel(t);
	return l.charAt(0).toUpperCase() || "?";
}

function recipientAvatarSrc(t: AdminCommandUsageTarget): string | null {
	if (t.kind !== "user") return null;
	return userAvatarUrl(t.snowflake, t.avatarHash, 64);
}

function RecipientLine({ t }: { t: AdminCommandUsageTarget }) {
	const label = recipientLabel(t);
	const src = recipientAvatarSrc(t);
	const body = (
		<div className="flex items-center gap-2">
			<Avatar className="size-6">
				{src && <AvatarImage src={src} alt={label} />}
				<AvatarFallback className="text-[10px]">{recipientInitial(t)}</AvatarFallback>
			</Avatar>
			<span className="truncate text-sm">{label}</span>
		</div>
	);
	if (t.kind === "user" && t.appUserId) {
		return (
			<Link href={`/admin/users/${t.appUserId}`} className="hover:underline">
				{body}
			</Link>
		);
	}
	return body;
}

export function UsageRecipientStack({ recipients, max = 5 }: Props) {
	if (recipients.length === 0) return null;
	const visible = recipients.slice(0, max);
	const overflow = Math.max(0, recipients.length - visible.length);
	return (
		<HoverCard openDelay={120}>
			<HoverCardTrigger asChild>
				<div className="flex items-center">
					{visible.map((t) => {
						const src = recipientAvatarSrc(t);
						const label = recipientLabel(t);
						return (
							<Avatar key={t.snowflake} className="size-7 -ml-2 ring-2 ring-background first:ml-0">
								{src && <AvatarImage src={src} alt={label} />}
								<AvatarFallback className="text-[10px]">
									{recipientInitial(t)}
								</AvatarFallback>
							</Avatar>
						);
					})}
					{overflow > 0 && (
						<div className="size-7 -ml-2 flex items-center justify-center rounded-full bg-muted text-[10px] font-medium ring-2 ring-background">
							+{overflow}
						</div>
					)}
				</div>
			</HoverCardTrigger>
			<HoverCardContent className="w-72 max-h-80 overflow-auto p-2">
				<div className="flex flex-col gap-1">
					{recipients.map((t) => (
						<RecipientLine key={t.snowflake} t={t} />
					))}
				</div>
			</HoverCardContent>
		</HoverCard>
	);
}
