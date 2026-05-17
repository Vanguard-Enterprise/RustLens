"use client";

import { useEffect, useState } from "react";

import { HoverCard, HoverCardContent, HoverCardTrigger } from "@/components/ui/hover-card";
import { PlayerProfileCard } from "@/components/player-profile-card";
import { getPlayerProfileAction } from "@/app/_actions/player-search";
import type { PlayerProfile } from "@/lib/rustlens-api";

const sessionCache = new Map<string, PlayerProfile>();

type Props = {
	steamId: string;
	children: React.ReactNode;
};

export function PlayerHoverCard({ steamId, children }: Props) {
	const [profile, setProfile] = useState<PlayerProfile | null>(() => sessionCache.get(steamId) ?? null);
	const [open, setOpen] = useState(false);

	useEffect(() => {
		if (!open || profile) return;
		let cancelled = false;
		(async () => {
			const res = await getPlayerProfileAction(steamId);
			if (cancelled) return;
			if ("error" in res) return;
			sessionCache.set(steamId, res);
			setProfile(res);
		})();
		return () => {
			cancelled = true;
		};
	}, [open, profile, steamId]);

	return (
		<HoverCard openDelay={200} onOpenChange={setOpen}>
			<HoverCardTrigger asChild>{children}</HoverCardTrigger>
			<HoverCardContent className="w-96 p-0">
				{profile ? (
					<PlayerProfileCard profile={profile} compact />
				) : (
					<div className="p-3 text-sm text-muted-foreground">Loading…</div>
				)}
			</HoverCardContent>
		</HoverCard>
	);
}
