"use server";

import { getCurrentUserSafe } from "@/lib/rustlens-api";
import { clearSession } from "@/lib/session";

/**
 * Re-fetches /api/app/me from the backend so the client SessionProvider can
 * update without a navigation. Returns null when the backend rejects the
 * cookie — callers should treat null as "session ended" and redirect.
 */
export async function refreshCurrentUser() {
	return getCurrentUserSafe();
}

/**
 * Clears the rustlens_session cookie. Used by the SSE event handler when the
 * server emits session.revoked so the next navigation hits the unauthenticated
 * surface.
 */
export async function clearSessionAction() {
	await clearSession();
}
