import { NextResponse } from "next/server";
import { CSRF_FIELD_NAME, validateCsrfRequest } from "@/lib/csrf";
import { consumePasswordReset } from "@/lib/rustlens-api";
import { setSession, toSessionUser } from "@/lib/session";

export async function POST(request: Request) {
	const formData = await request.formData();
	if (!(await validateCsrfRequest(request, formData.get(CSRF_FIELD_NAME)))) {
		return NextResponse.redirect(new URL("/reset-password?error=csrf", request.url), {
			status: 303,
		});
	}
	const token = String(formData.get("token") ?? "").trim();
	const newPassword = String(formData.get("password") ?? "");

	if (!token) {
		return NextResponse.redirect(new URL("/reset-password?error=reset-invalid", request.url), {
			status: 303,
		});
	}
	if (newPassword.trim().length < 12) {
		return NextResponse.redirect(
			new URL(`/reset-password?token=${encodeURIComponent(token)}&error=reset-password`, request.url),
			{ status: 303 },
		);
	}

	try {
		const currentUser = await consumePasswordReset({ token, newPassword });
		await setSession(toSessionUser(currentUser));
		return NextResponse.redirect(new URL("/dashboard/settings?notice=password-reset", request.url), {
			status: 303,
		});
	} catch {
		return NextResponse.redirect(
			new URL(`/reset-password?token=${encodeURIComponent(token)}&error=reset-invalid`, request.url),
			{ status: 303 },
		);
	}
}
