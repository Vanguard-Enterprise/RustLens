import { notFound } from "next/navigation";

import { PlayerProfileCard } from "@/components/player-profile-card";
import { requireSession } from "@/lib/session";
import { getPlayerProfile } from "@/lib/rustlens-api";

type PageProps = { params: Promise<{ steamId: string }> };

export default async function PlayerProfilePage({ params }: PageProps) {
	await requireSession();
	const { steamId } = await params;
	let profile;
	try {
		profile = await getPlayerProfile(steamId);
	} catch (err) {
		const message = err instanceof Error ? err.message : "";
		if (message.includes("404")) notFound();
		throw err;
	}
	return (
		<main className="mx-auto max-w-3xl px-4 py-8">
			<PlayerProfileCard profile={profile} />
		</main>
	);
}
