import { requireSession } from "@/lib/session";
import { getCurrentUserSafe, searchPlayers } from "@/lib/rustlens-api";
import type { PlayerSearchResponse } from "@/lib/rustlens-api";

import { SearchResults } from "./_components/search-results";

type SearchParams = { q?: string; includeOffline?: string };

export default async function SearchPage({
	searchParams,
}: {
	searchParams: Promise<SearchParams>;
}) {
	await requireSession();
	const { q = "", includeOffline = "false" } = await searchParams;
	const trimmed = q.trim();
	const includeOff = includeOffline === "true";
	const me = await getCurrentUserSafe();
	const isAdmin = (me?.adminRoles?.length ?? 0) > 0;

	let initial: PlayerSearchResponse = { items: [], mode: "name" };
	if (trimmed.length >= 2) {
		try {
			initial = await searchPlayers(trimmed, includeOff && isAdmin);
		} catch {
			initial = { items: [], mode: /^\d+$/.test(trimmed) ? "identifier" : "name" };
		}
	}

	return (
		<main className="mx-auto max-w-4xl px-4 py-8">
			<h1 className="text-2xl font-semibold">Player search</h1>
			<SearchResults
				initialQuery={trimmed}
				initialIncludeOffline={includeOff}
				isAdmin={isAdmin}
				initialResponse={initial}
			/>
		</main>
	);
}
