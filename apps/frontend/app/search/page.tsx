import type { Metadata } from "next";
import { Footer } from "@/components/footer";
import { Header } from "@/components/header";
import { getCsrfToken } from "@/lib/csrf";
import { requireSession } from "@/lib/session";
import { getCurrentUserSafe, searchPlayers } from "@/lib/rustlens-api";
import type { PlayerSearchResponse } from "@/lib/rustlens-api";

import { SearchResults } from "./_components/search-results";

export const metadata: Metadata = { title: "Search Players" };

type SearchParams = { q?: string; includeOffline?: string };

export default async function SearchPage({
	searchParams,
}: {
	searchParams: Promise<SearchParams>;
}) {
	const session = await requireSession();
	const csrfToken = await getCsrfToken();
	const { q = "", includeOffline = "false" } = await searchParams;
	const trimmed = q.trim();
	const includeOff = includeOffline === "true";
	const me = await getCurrentUserSafe();
	const isAdmin =
		(me?.permissions?.length ?? 0) > 0 ||
		(me?.user.roles ?? []).includes("internal_admin");
	const user = {
		displayName: session.displayName,
		avatarUrl: session.avatarUrl,
		isInternalAdmin:
			isAdmin || session.roles.includes("internal_admin"),
	};

	let initial: PlayerSearchResponse = { items: [], mode: "name" };
	if (trimmed.length >= 2) {
		try {
			initial = await searchPlayers(trimmed, includeOff && isAdmin);
		} catch {
			initial = { items: [], mode: /^\d+$/.test(trimmed) ? "identifier" : "name" };
		}
	}

	return (
		<div className="flex min-h-screen flex-col">
			<Header csrfToken={csrfToken} user={user} />
			<main className="flex-1">
				<div className="mx-auto max-w-4xl px-4 py-8">
					<h1 className="text-2xl font-semibold">Player search</h1>
					<SearchResults
						initialQuery={trimmed}
						initialIncludeOffline={includeOff}
						isAdmin={isAdmin}
						initialResponse={initial}
					/>
				</div>
			</main>
			<Footer />
		</div>
	);
}
