import { NextResponse } from "next/server";
import { redirect } from "next/navigation";
import { getCurrentUser } from "@/lib/rustlens-api";
import {
	getSessionToken,
	SESSION_COOKIE,
	SESSION_TTL_SECONDS,
	serializeSessionToken,
	toSessionUser,
} from "@/lib/session";

// Rebuilds the rustlens_session cookie from live backend state so a role grant
// / revoke takes effect without the user logging out and back in. The standard
// SessionRefreshTrigger only refreshes within 7 days of expiry; this endpoint
// is the explicit "refresh now" path. Cookies are set on the response object
// directly — the cookies() helper doesn't reliably flow into a manually
// returned NextResponse in Next.js 16 route handlers.
export async function GET(request: Request) {
	const next = sanitizeNext(
		new URL(request.url).searchParams.get("next"),
	);

	const token = await getSessionToken();
	if (!token) {
		redirect("/login");
	}

	// If the backend rejects the session, backendFetch redirects to
	// /session-expired (which clears the cookie). Otherwise we get fresh state.
	const current = await getCurrentUser();
	const serialized = serializeSessionToken(toSessionUser(current));

	const response = NextResponse.redirect(new URL(next, request.url));
	response.cookies.set({
		name: SESSION_COOKIE,
		value: serialized,
		path: "/",
		maxAge: SESSION_TTL_SECONDS,
		httpOnly: true,
		sameSite: "lax",
		secure: process.env.NODE_ENV === "production",
	});
	response.headers.set("Cache-Control", "no-store");
	return response;
}

// Only allow same-origin paths. Reject protocol-relative (//evil.com) and
// absolute URLs so this can't be turned into an open redirect.
function sanitizeNext(raw: string | null): string {
	if (!raw || !raw.startsWith("/") || raw.startsWith("//")) {
		return "/dashboard";
	}
	return raw;
}
