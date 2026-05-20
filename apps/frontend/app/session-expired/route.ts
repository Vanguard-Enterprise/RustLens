import { NextResponse } from "next/server";
import { SESSION_COOKIE } from "@/lib/session";

// Server Components cannot mutate cookies, so backendFetch redirects 401s with
// a session-invalid error code here instead of calling clearSession() inline.
// This handler clears the cookie and forwards to /login?notice=... so the
// Sonner toast still fires on the next render.
export async function GET(request: Request) {
	const notice =
		new URL(request.url).searchParams.get("notice") ?? "expired";
	const target = new URL(
		`/login?notice=${encodeURIComponent(notice)}`,
		request.url,
	);
	const response = NextResponse.redirect(target);
	response.cookies.set({
		name: SESSION_COOKIE,
		value: "",
		path: "/",
		maxAge: 0,
		httpOnly: true,
		sameSite: "lax",
		secure: process.env.NODE_ENV === "production",
	});
	response.headers.set("Cache-Control", "no-store");
	return response;
}
