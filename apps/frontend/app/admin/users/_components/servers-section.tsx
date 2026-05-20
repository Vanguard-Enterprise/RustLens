"use client";

import type { AdminUserGuild } from "@/lib/rustlens-api";
import { cn } from "@/lib/utils";

type Props = {
	guilds: AdminUserGuild[];
};

export function ServersSection({ guilds }: Props) {
	return (
		<section className="flex flex-col gap-3 rounded-lg border bg-card p-4">
			<div>
				<h2 className="text-lg font-semibold">Servers</h2>
				<p className="text-sm text-muted-foreground">
					Discord servers this user is a member of.
				</p>
			</div>

			{guilds.length === 0 ? (
				<p className="text-sm text-muted-foreground">No server memberships.</p>
			) : (
				<ul className="flex flex-col divide-y">
					{guilds.map((g) => (
						<li
							key={g.guildId}
							className="grid grid-cols-12 items-center gap-2 py-2 text-sm"
						>
							<div className="col-span-4">
								<span className="text-muted-foreground">Guild ID</span>
							</div>
							<code className={cn("col-span-5 truncate rounded bg-muted px-1 py-0.5 text-xs")}>
								{g.guildId}
							</code>
							<div className="col-span-3 truncate text-xs text-muted-foreground">
								{g.nick ? `Nick: ${g.nick}` : "—"}
							</div>
						</li>
					))}
				</ul>
			)}
		</section>
	);
}
