import "server-only";

import { randomBytes, timingSafeEqual } from "node:crypto";
import { cookies } from "next/headers";
import { getAppOrigin } from "@/lib/app-origin";

export const CSRF_FIELD_NAME = "csrfToken";

const CSRF_COOKIE = "rustlens_csrf";
const CSRF_TTL_SECONDS = 60 * 60 * 8;

function isSecureCookie() {
	return process.env.NODE_ENV === "production";
}

function createToken() {
	return randomBytes(32).toString("base64url");
}

function sameOrigin(value: string | null, expectedOrigin: string) {
	if (!value) {
		return false;
	}

	try {
		return new URL(value).origin === expectedOrigin;
	} catch {
		return false;
	}
}

export async function getCsrfToken() {
	const cookieStore = await cookies();
	const existingToken = cookieStore.get(CSRF_COOKIE)?.value;
	if (existingToken) {
		return existingToken;
	}

	const token = createToken();
	cookieStore.set(CSRF_COOKIE, token, {
		httpOnly: false,
		sameSite: "lax",
		secure: isSecureCookie(),
		path: "/",
		maxAge: CSRF_TTL_SECONDS,
	});
	return token;
}

export async function validateCsrfRequest(
	request: Request,
	submittedToken: FormDataEntryValue | null,
) {
	const token = typeof submittedToken === "string" ? submittedToken.trim() : "";
	if (!token) {
		return false;
	}

	const trustedOrigin = getAppOrigin();
	const origin = request.headers.get("origin");
	const referer = request.headers.get("referer");
	if (!sameOrigin(origin, trustedOrigin) && !sameOrigin(referer, trustedOrigin)) {
		return false;
	}

	const cookieStore = await cookies();
	const cookieToken = cookieStore.get(CSRF_COOKIE)?.value;
	if (!cookieToken) {
		return false;
	}

	const submittedBuffer = Buffer.from(token);
	const cookieBuffer = Buffer.from(cookieToken);
	return (
		submittedBuffer.length === cookieBuffer.length &&
		timingSafeEqual(submittedBuffer, cookieBuffer)
	);
}
