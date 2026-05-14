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
		const status = err instanceof Error ? (err as Error & { status?: number }).status : undefined;
		if (status === 404) notFound();
		throw err;
	}
	return (
		<main className="mx-auto max-w-3xl px-4 py-8">
			<PlayerProfileCard profile={profile} />
		</main>
	);
}
