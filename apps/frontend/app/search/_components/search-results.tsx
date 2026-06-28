"use client";

import { useEffect, useState } from "react";
import Link from "next/link";

import { Switch } from "@/components/ui/switch";
import { PlayerHoverCard } from "@/components/player-hover-card";
import { searchPlayersAction } from "@/app/_actions/player-search";
import type { PlayerSearchResponse } from "@/lib/rustlens-api";

type Props = {
	initialQuery: string;
	initialIncludeOffline: boolean;
	isAdmin: boolean;
	initialResponse: PlayerSearchResponse;
};

export function SearchResults({
	initialQuery,
	initialIncludeOffline,
	isAdmin,
	initialResponse,
}: Props) {
	const [includeOffline, setIncludeOffline] = useState(initialIncludeOffline);
	const [response, setResponse] = useState<PlayerSearchResponse>(initialResponse);

	useEffect(() => {
		if (includeOffline === initialIncludeOffline) {
			setResponse(initialResponse);
			return;
		}
		let cancelled = false;
		(async () => {
			const res = await searchPlayersAction(initialQuery, includeOffline);
			if (cancelled) return;
			if (!("error" in res)) setResponse(res);
		})();
		return () => {
			cancelled = true;
		};
	}, [includeOffline, initialIncludeOffline, initialQuery, initialResponse]);

	return (
		<div className="mt-4 flex flex-col gap-4">
			{isAdmin && response.mode === "name" ? (
				<label className="flex items-center gap-2 text-sm">
					<Switch checked={includeOffline} onCheckedChange={setIncludeOffline} />
					Include offline (&lt; 24h)
				</label>
			) : null}

			{response.items.length === 0 ? (
				<p className="text-sm text-muted-foreground">No players match.</p>
			) : (
				<ul className="flex flex-col divide-y rounded-md border">
					{response.items.map((hit) => (
						<li key={hit.steamId} className="flex items-center gap-4 p-3">
							<PlayerHoverCard steamId={hit.steamId}>
								<Link
									href={`/player/${encodeURIComponent(hit.steamId)}`}
									className="flex flex-1 items-center gap-3"
								>
									{hit.avatarUrl ? (
										<img alt="" src={hit.avatarUrl} className="h-10 w-10 rounded" />
									) : (
										<div className="h-10 w-10 rounded bg-muted" />
									)}
									<div className="flex flex-col text-sm">
										<span className="font-medium">{hit.bmName ?? hit.steamId}</span>
										<span className="text-xs text-muted-foreground">
											{hit.currentServer
												? hit.currentServer.name
												: hit.isOnline
													? "online"
													: "offline"}
										</span>
									</div>
								</Link>
							</PlayerHoverCard>
						</li>
					))}
				</ul>
			)}
		</div>
	);
}
