"use client";

import Link from "next/link";

import { Badge } from "@/components/ui/badge";
import { UserLink } from "@/components/admin/user-link";
import { RelativeTime } from "@/components/admin/relative-time";
import type { AdminUserAuditItem } from "@/lib/rustlens-api";

type Props = {
	userId: string;
	items: AdminUserAuditItem[];
};

export function AuditSection({ userId, items }: Props) {
	return (
		<section className="flex flex-col gap-3 rounded-lg border bg-card p-4">
			<div className="flex items-center justify-between">
				<div>
					<h2 className="text-lg font-semibold">Recent audit</h2>
					<p className="text-sm text-muted-foreground">
						Most recent admin actions involving this user.
					</p>
				</div>
				<Link
					href={`/admin/audit?targetUserId=${encodeURIComponent(userId)}`}
					className="text-sm underline"
				>
					View full history →
				</Link>
			</div>

			{items.length === 0 ? (
				<p className="text-sm text-muted-foreground">No audit events.</p>
			) : (
				<ul className="flex flex-col divide-y">
					{items.map((it) => {
						const result = it.result ?? "success";
						return (
							<li key={it.id} className="grid grid-cols-12 items-center gap-2 py-2 text-sm">
								<div className="col-span-3">
									<UserLink
										userId={it.actorUserId}
										displayName={it.actorDisplayName}
										avatarUrl={it.actorAvatarUrl}
									/>
								</div>
								<code className="col-span-3 truncate rounded bg-muted px-1 py-0.5 text-xs">
									{it.eventType}
								</code>
								<div className="col-span-1">
									<Badge variant={result === "error" ? "destructive" : "secondary"}>
										{result}
									</Badge>
								</div>
								<div className="col-span-3 truncate text-xs text-muted-foreground">
									{it.reason?.trim() ? it.reason : "—"}
								</div>
								<div className="col-span-2 text-right text-xs text-muted-foreground">
									<RelativeTime iso={it.createdAt} />
								</div>
							</li>
						);
					})}
				</ul>
			)}
		</section>
	);
}
