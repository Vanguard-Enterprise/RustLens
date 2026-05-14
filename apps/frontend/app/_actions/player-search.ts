"use server";

import {
	type PlayerProfile,
	type PlayerSearchResponse,
	getPlayerProfile,
	searchPlayers,
} from "@/lib/rustlens-api";

export async function searchPlayersAction(
	q: string,
	includeOffline = false,
): Promise<
	| PlayerSearchResponse
	| { error: "forbidden" }
	| { error: "rate_limited" }
	| { error: "unknown" }
> {
	try {
		return await searchPlayers(q, includeOffline);
	} catch (err) {
		const message = err instanceof Error ? err.message : "";
		if (message.includes("403") || message.toLowerCase().includes("forbidden")) {
			return { error: "forbidden" };
		}
		if (message.includes("429")) {
			return { error: "rate_limited" };
		}
		return { error: "unknown" };
	}
}

export async function getPlayerProfileAction(
	identifier: string,
): Promise<
	PlayerProfile | { error: "not_found" } | { error: "private" } | { error: "unknown" }
> {
	try {
		return await getPlayerProfile(identifier);
	} catch (err) {
		const message = err instanceof Error ? err.message : "";
		if (message.toLowerCase().includes("battlemetrics profile private")) {
			return { error: "private" };
		}
		if (message.includes("404")) {
			return { error: "not_found" };
		}
		return { error: "unknown" };
	}
}
