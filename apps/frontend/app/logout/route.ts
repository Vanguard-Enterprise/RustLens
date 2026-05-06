import { NextResponse } from "next/server";
import { CSRF_FIELD_NAME, validateCsrfRequest } from "@/lib/csrf";
import { SESSION_COOKIE } from "@/lib/session";

export async function POST(request: Request) {
	const formData = await request.formData();
	const csrfValid = await validateCsrfRequest(
		request,
		formData.get(CSRF_FIELD_NAME),
	);
	if (!csrfValid) {
		return NextResponse.json({ error: "invalid_csrf" }, { status: 400 });
	}

	// Match every attribute setSession() used; otherwise the browser may not
	// match and delete the existing cookie.
	const response = NextResponse.json({ ok: true });
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
