"use client";

import { CopyIcon, CheckIcon } from "lucide-react";
import * as React from "react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import type { AdminUserDetail } from "@/lib/rustlens-api";
import { cn } from "@/lib/utils";

type Props = { detail: AdminUserDetail };

function CopyButton({ value, label }: { value: string; label: string }) {
	const [copied, setCopied] = React.useState(false);
	return (
		<Button
			type="button"
			variant="ghost"
			size="sm"
			className="h-6 px-1.5 text-xs"
			onClick={async () => {
				await navigator.clipboard.writeText(value);
				setCopied(true);
				setTimeout(() => setCopied(false), 1500);
			}}
		>
			{copied ? <CheckIcon className="size-3" /> : <CopyIcon className="size-3" />}
			<span className="sr-only">Copy {label}</span>
		</Button>
	);
}

export function IdentityHeader({ detail }: Props) {
	const { user, discordId, discordUsername, discordGlobalName, discordAvatar, suspendedAt, deletedAt, suspendReason } = detail;
	return (
		<div className="flex flex-col gap-3 rounded-lg border bg-card p-4">
			<div className="flex items-start gap-4">
				{user.avatarUrl || discordAvatar ? (
					// eslint-disable-next-line @next/next/no-img-element
					<img
						src={user.avatarUrl || discordAvatar}
						alt=""
						className="size-16 rounded-full object-cover"
					/>
				) : (
					<div className="size-16 rounded-full bg-muted" aria-hidden />
				)}
				<div className="flex flex-1 flex-col gap-1">
					<div className="flex flex-wrap items-center gap-2">
						<h1 className="text-2xl font-semibold">{user.displayName}</h1>
						{deletedAt ? (
							<Badge variant="destructive">Deleted</Badge>
						) : suspendedAt ? (
							<Badge variant="outline" className="border-amber-600 text-amber-700">
								Suspended
							</Badge>
						) : (
							<Badge variant="secondary">Active</Badge>
						)}
					</div>
					<div className="flex flex-wrap items-center gap-2 text-sm text-muted-foreground">
						<span>@{discordUsername || "—"}</span>
						{discordGlobalName ? <span>· {discordGlobalName}</span> : null}
					</div>
					<div className="mt-2 grid grid-cols-1 gap-1 text-xs sm:grid-cols-2">
						<div className="flex items-center gap-1">
							<span className="text-muted-foreground">User ID</span>
							<code className={cn("rounded bg-muted px-1 py-0.5")}>{user.id}</code>
							<CopyButton value={user.id} label="user id" />
						</div>
						<div className="flex items-center gap-1">
							<span className="text-muted-foreground">Discord ID</span>
							<code className="rounded bg-muted px-1 py-0.5">{discordId || "—"}</code>
							{discordId ? <CopyButton value={discordId} label="discord id" /> : null}
						</div>
					</div>
					{suspendedAt && suspendReason ? (
						<div className="mt-2 rounded border border-amber-300 bg-amber-50 px-2 py-1 text-xs text-amber-900">
							<strong>Suspended:</strong> {suspendReason}
						</div>
					) : null}
					<div className="mt-2 grid grid-cols-2 gap-2 text-xs text-muted-foreground">
						<div>Joined {new Date(user.createdAt).toLocaleDateString()}</div>
						<div>Last login {new Date(user.lastLoginAt).toLocaleDateString()}</div>
					</div>
				</div>
			</div>
		</div>
	);
}
