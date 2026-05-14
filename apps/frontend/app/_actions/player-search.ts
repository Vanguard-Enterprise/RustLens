"use server";

import {
	type PlayerProfile,
	type PlayerSearchResponse,
	getPlayerProfile,
	searchPlayers,
} from "@/lib/rustlens-api";

function statusOf(err: unknown): number | undefined {
	return err instanceof Error
		? (err as Error & { status?: number }).status
		: undefined;
}

function messageOf(err: unknown): string {
	return err instanceof Error ? err.message : "";
}

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
		const status = statusOf(err);
		if (status === 403) return { error: "forbidden" };
		if (status === 429) return { error: "rate_limited" };
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
		const status = statusOf(err);
		const message = messageOf(err).toLowerCase();
		if (status === 404 && message.includes("battlemetrics profile private")) {
			return { error: "private" };
		}
		if (status === 404) return { error: "not_found" };
		return { error: "unknown" };
	}
}
