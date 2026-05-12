"use server";

import { isRedirectError } from "next/dist/client/components/redirect-error";
import { getCurrentUser } from "@/lib/rustlens-api";
import { getSession, setSession, toSessionUser } from "@/lib/session";

const REFRESH_THRESHOLD_MS = 7 * 24 * 60 * 60 * 1000; // 7 days

export type RefreshResult =
	| { ok: true; refreshed: boolean }
	| { ok: false; reason: "no_session" | "fetch_failed" };

/**
 * Re-mints the rustlens_session cookie when the remaining TTL drops below 7
 * days. No-ops (returns ok:true, refreshed:false) when the cookie has more
 * than 7 days remaining. Returns ok:false when the cookie is missing or the
 * backend rejects /api/app/me (the caller is responsible for clearing state
 * and redirecting to /login — usually the SSE event handler does this).
 */
export async function refreshSession(): Promise<RefreshResult> {
	const session = await getSession();
	if (!session) {
		return { ok: false, reason: "no_session" };
	}
	const remaining = session.expiresAt - Date.now();
	if (remaining > REFRESH_THRESHOLD_MS) {
		return { ok: true, refreshed: false };
	}
	try {
		const current = await getCurrentUser();
		await setSession(toSessionUser(current));
		return { ok: true, refreshed: true };
	} catch (err) {
		// Next.js redirect() throws an internal signal — let it propagate so the
		// server component redirects to /login as intended.
		if (isRedirectError(err)) {
			throw err;
		}
		return { ok: false, reason: "fetch_failed" };
	}
}
