import type { Metadata } from "next";
import { notFound } from "next/navigation";

import { Footer } from "@/components/footer";
import { Header } from "@/components/header";
import { PlayerProfileCard } from "@/components/player-profile-card";
import { getCsrfToken } from "@/lib/csrf";
import { requireSession } from "@/lib/session";
import { getPlayerProfile } from "@/lib/rustlens-api";

export const metadata: Metadata = { title: "Player Profile" };

type PageProps = { params: Promise<{ steamId: string }> };

export default async function PlayerProfilePage({ params }: PageProps) {
	const session = await requireSession();
	const csrfToken = await getCsrfToken();
	const user = {
		displayName: session.displayName,
		avatarUrl: session.avatarUrl,
		isInternalAdmin:
			(session.adminRoles?.length ?? 0) > 0 ||
			session.roles.includes("internal_admin"),
	};
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
		<div className="flex min-h-screen flex-col">
			<Header csrfToken={csrfToken} user={user} />
			<main className="flex-1">
				<div className="mx-auto max-w-3xl px-4 py-8">
					<PlayerProfileCard profile={profile} />
				</div>
			</main>
			<Footer />
		</div>
	);
}
