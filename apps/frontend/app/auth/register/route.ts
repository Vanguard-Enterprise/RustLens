import { NextResponse } from "next/server";
import {
	consumeRateLimit,
	getClientAddress,
	redirectRateLimited,
} from "@/lib/rate-limit";
import { CSRF_FIELD_NAME, validateCsrfRequest } from "@/lib/csrf";
import {
	registerWithPassword,
	requestEmailVerification,
} from "@/lib/rustlens-api";
import { getAppOrigin } from "@/lib/app-origin";
import { serializeSessionToken, setSession, toSessionUser } from "@/lib/session";

export async function POST(request: Request) {
	const formData = await request.formData();
	if (!(await validateCsrfRequest(request, formData.get(CSRF_FIELD_NAME)))) {
		return NextResponse.redirect(new URL("/login?error=csrf", request.url), {
			status: 303,
		});
	}
	const displayName = String(formData.get("displayName") ?? "").trim();
	const email = String(formData.get("email") ?? "").trim();
	const password = String(formData.get("password") ?? "");
	const clientAddress = getClientAddress(request);
	const rateLimit = await consumeRateLimit(`auth:register:${clientAddress}:${email.toLowerCase()}`, {
		max: 5,
		windowMs: 60 * 60 * 1000,
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
		return NextResponse.redirect(new URL("/login?error=register-password", request.url), {
			status: 303,
		});
	}

	try {
		const currentUser = await registerWithPassword({
			displayName,
			email,
			password,
		});
		const sessionUser = toSessionUser(currentUser);
		await setSession(sessionUser);

		const settingsUrl = new URL("/dashboard/settings?notice=registered", request.url);
		try {
			const verifyResult = await requestEmailVerification(
				serializeSessionToken(sessionUser),
			);
			if (
				verifyResult.debugToken &&
				process.env.AUTH_DEBUG_TOKEN_PREVIEW === "true"
			) {
				settingsUrl.searchParams.set(
					"preview",
					`${getAppOrigin()}/auth/email/verify?token=${verifyResult.debugToken}`,
				);
			}
		} catch {
			// Registration already succeeded; verification can be requested later from settings.
		}

		return NextResponse.redirect(settingsUrl, { status: 303 });
	} catch (error) {
		const message = error instanceof Error ? error.message : "";
		const route =
			message === "email is already registered"
				? "/login?error=register-conflict"
				: "/login?error=register-password";
		return NextResponse.redirect(new URL(route, request.url), { status: 303 });
	}
}
