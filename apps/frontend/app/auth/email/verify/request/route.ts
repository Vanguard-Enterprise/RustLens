import { NextResponse } from "next/server";
import {
	consumeRateLimit,
	getClientAddress,
	redirectRateLimited,
} from "@/lib/rate-limit";
import { CSRF_FIELD_NAME, validateCsrfRequest } from "@/lib/csrf";
import { getAppOrigin } from "@/lib/app-origin";
import { requestEmailVerification } from "@/lib/rustlens-api";
import { requireSession } from "@/lib/session";

export async function POST(request: Request) {
	const formData = await request.formData();
	if (!(await validateCsrfRequest(request, formData.get(CSRF_FIELD_NAME)))) {
		return NextResponse.redirect(new URL("/dashboard/settings?error=csrf", request.url), {
			status: 303,
		});
	}
	const session = await requireSession();
	const clientAddress = getClientAddress(request);
	const rateLimit = await consumeRateLimit(`auth:verify-request:${clientAddress}:${session.userId}`, {
		max: 5,
		windowMs: 60 * 60 * 1000,
	});
	if (!rateLimit.allowed) {
		return redirectRateLimited(
			request,
			"/dashboard/settings?error=auth-rate-limit",
			rateLimit.retryAfterSeconds,
		);
	}

	try {
		const result = await requestEmailVerification();
		const redirectUrl = new URL("/dashboard/settings?notice=email-verify-requested", request.url);
		if (
			result.debugToken &&
			process.env.AUTH_DEBUG_TOKEN_PREVIEW === "true"
		) {
			redirectUrl.searchParams.set(
				"preview",
				`${getAppOrigin()}/auth/email/verify?token=${result.debugToken}`,
			);
		}
		return NextResponse.redirect(redirectUrl, { status: 303 });
	} catch (error) {
		const message = error instanceof Error ? error.message : "";
		const code =
			message === "email is already verified"
				? "email-already-verified"
				: "verify-request";
		return NextResponse.redirect(new URL(`/dashboard/settings?error=${code}`, request.url), {
			status: 303,
		});
	}
}
