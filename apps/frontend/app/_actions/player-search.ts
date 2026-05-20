"use server";

import {
	type PlayerProfile,
	type PlayerSearchResponse,
	getPlayerProfile,
	searchPlayers,
} from "@/lib/rustlens-api";
import { consumeRateLimit } from "@/lib/rate-limit";
import { getSession } from "@/lib/session";

function statusOf(err: unknown): number | undefined {
	return err instanceof Error
		? (err as Error & { status?: number }).status
		: undefined;
}

function messageOf(err: unknown): string {
	return err instanceof Error ? err.message : "";
}

function isUnauthenticated(err: unknown): boolean {
	const status = statusOf(err);
	if (status === 401) return true;
	return messageOf(err).toLowerCase().includes("rustlens session is required");
}

export async function searchPlayersAction(
	q: string,
	includeOffline = false,
): Promise<
	| PlayerSearchResponse
	| { error: "unauthenticated" }
	| { error: "forbidden" }
	| { error: "rate_limited" }
	| { error: "unknown" }
> {
	const session = await getSession();
	if (!session) return { error: "unauthenticated" };
	// Defensive rate limit on the frontend so a logged-in user can't run a
	// tight loop pinging the backend; backend also enforces its own per-session
	// limit. 30 req / 60s matches the backend players_search budget.
	const limit = await consumeRateLimit(`fe:player-search:${session.userId}`, {
		max: 30,
		windowMs: 60_000,
	});
	if (!limit.allowed) return { error: "rate_limited" };
	try {
		return await searchPlayers(q, includeOffline);
	} catch (err) {
		if (isUnauthenticated(err)) return { error: "unauthenticated" };
		const status = statusOf(err);
		if (status === 403) return { error: "forbidden" };
		if (status === 429) return { error: "rate_limited" };
		return { error: "unknown" };
	}
}

export async function getPlayerProfileAction(
	identifier: string,
): Promise<
	| PlayerProfile
	| { error: "unauthenticated" }
	| { error: "not_found" }
	| { error: "private" }
	| { error: "rate_limited" }
	| { error: "unknown" }
> {
	const session = await getSession();
	if (!session) return { error: "unauthenticated" };
	const limit = await consumeRateLimit(`fe:player-profile:${session.userId}`, {
		max: 60,
		windowMs: 60_000,
	});
	if (!limit.allowed) return { error: "rate_limited" };
	try {
		return await getPlayerProfile(identifier);
	} catch (err) {
		if (isUnauthenticated(err)) return { error: "unauthenticated" };
		const status = statusOf(err);
		const message = messageOf(err).toLowerCase();
		if (status === 404 && message.includes("battlemetrics profile private")) {
			return { error: "private" };
		}
		if (status === 404) return { error: "not_found" };
		return { error: "unknown" };
	}
}
