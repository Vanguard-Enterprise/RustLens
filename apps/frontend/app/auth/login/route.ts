import { NextResponse } from "next/server";
import {
	consumeRateLimit,
	getClientAddress,
	redirectRateLimited,
} from "@/lib/rate-limit";
import { CSRF_FIELD_NAME, validateCsrfRequest } from "@/lib/csrf";
import { loginWithPassword } from "@/lib/rustlens-api";
import { setSession, toSessionUser } from "@/lib/session";

export async function POST(request: Request) {
	const formData = await request.formData();
	if (!(await validateCsrfRequest(request, formData.get(CSRF_FIELD_NAME)))) {
		return NextResponse.redirect(new URL("/login?error=csrf", request.url), {
			status: 303,
		});
	}
	const email = String(formData.get("email") ?? "").trim();
	const password = String(formData.get("password") ?? "");
	const clientAddress = getClientAddress(request);
	const rateLimit = await consumeRateLimit(`auth:login:${clientAddress}:${email.toLowerCase()}`, {
		max: 8,
		windowMs: 10 * 60 * 1000,
	});
	if (!rateLimit.allowed) {
		return redirectRateLimited(
			request,
			"/login?error=auth-rate-limit",
			rateLimit.retryAfterSeconds,
		);
	}

	if (!email) {
		return NextResponse.redirect(new URL("/login?error=register-email", request.url), {
			status: 303,
		});
	}
	if (password.trim().length < 12) {
		return NextResponse.redirect(new URL("/login?error=login-password", request.url), {
			status: 303,
		});
	}

	try {
		const currentUser = await loginWithPassword({ email, password });
		await setSession(toSessionUser(currentUser));
		return NextResponse.redirect(new URL("/dashboard", request.url), { status: 303 });
	} catch {
		return NextResponse.redirect(new URL("/login?error=login-invalid", request.url), {
			status: 303,
		});
	}
}
