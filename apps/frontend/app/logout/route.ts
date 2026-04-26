import { NextResponse } from "next/server";
import { CSRF_FIELD_NAME, validateCsrfRequest } from "@/lib/csrf";
import { clearSession } from "@/lib/session";

export async function POST(request: Request) {
	const formData = await request.formData();
	const csrfValid = await validateCsrfRequest(
		request,
		formData.get(CSRF_FIELD_NAME),
	);
	if (!csrfValid) {
		return NextResponse.json({ error: "invalid_csrf" }, { status: 400 });
	}

	await clearSession();

	return NextResponse.redirect(new URL("/", request.url), { status: 303 });
}
