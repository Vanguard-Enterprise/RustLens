import { NextResponse } from "next/server";
import { CSRF_FIELD_NAME, validateCsrfRequest } from "@/lib/csrf";
import { clearSession } from "@/lib/session";

export async function POST(request: Request) {
	const formData = await request.formData();
	if (!(await validateCsrfRequest(request, formData.get(CSRF_FIELD_NAME)))) {
		return NextResponse.redirect(new URL("/dashboard/settings?error=csrf", request.url), {
			status: 303,
		});
	}
	await clearSession();
	return NextResponse.redirect(new URL("/", request.url));
}
