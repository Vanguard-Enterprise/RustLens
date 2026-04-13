import { NextResponse } from "next/server";
import {
	consumeRateLimit,
	getClientAddress,
	redirectRateLimited,
} from "@/lib/rate-limit";
import { CSRF_FIELD_NAME, validateCsrfRequest } from "@/lib/csrf";
import { getAppOrigin } from "@/lib/app-origin";
import { requestPasswordReset } from "@/lib/rustlens-api";

export async function POST(request: Request) {
	const formData = await request.formData();
	if (!(await validateCsrfRequest(request, formData.get(CSRF_FIELD_NAME)))) {
		return NextResponse.redirect(new URL("/forgot-password?error=csrf", request.url), {
			status: 303,
		});
	}
	const email = String(formData.get("email") ?? "").trim();
	const clientAddress = getClientAddress(request);
	const rateLimit = await consumeRateLimit(`auth:reset-request:${clientAddress}:${email.toLowerCase()}`, {
		max: 5,
		windowMs: 60 * 60 * 1000,
	});
	if (!rateLimit.allowed) {
		return redirectRateLimited(
			request,
			"/forgot-password?error=auth-rate-limit",
			rateLimit.retryAfterSeconds,
		);
	}

	if (!email) {
		return NextResponse.redirect(new URL("/forgot-password?error=register-email", request.url), {
			status: 303,
		});
	}

	try {
		const result = await requestPasswordReset(email);
		const redirectUrl = new URL("/forgot-password?notice=reset-requested", request.url);
		if (
			result.debugToken &&
			process.env.AUTH_DEBUG_TOKEN_PREVIEW === "true"
		) {
			redirectUrl.searchParams.set(
				"preview",
				`${getAppOrigin()}/reset-password?token=${result.debugToken}`,
			);
		}
		return NextResponse.redirect(redirectUrl, { status: 303 });
	} catch {
		return NextResponse.redirect(new URL("/forgot-password?error=reset-request", request.url), {
			status: 303,
		});
	}
}
