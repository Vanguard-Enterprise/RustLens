import "server-only";

import { createHmac, randomBytes, timingSafeEqual } from "node:crypto";
import { getAppOrigin } from "@/lib/app-origin";

export const CSRF_FIELD_NAME = "csrfToken";

const CSRF_TTL_SECONDS = 60 * 60 * 8;

function createToken() {
	const issuedAt = Math.floor(Date.now() / 1000).toString();
	const nonce = randomBytes(32).toString("base64url");
	const payload = `${issuedAt}.${nonce}`;
	const signature = createHmac("sha256", getCsrfSecret())
		.update(payload)
		.digest("base64url");
	return `${payload}.${signature}`;
}

function getCsrfSecret() {
	const secret = process.env.APP_SESSION_SECRET;
	if (!secret) {
		throw new Error("APP_SESSION_SECRET is required");
	}
	return secret;
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
	return createToken();
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

	const [issuedAtRaw, nonce, signature] = token.split(".");
	if (!issuedAtRaw || !nonce || !signature) {
		return false;
	}

	const issuedAt = Number.parseInt(issuedAtRaw, 10);
	if (!Number.isFinite(issuedAt)) {
		return false;
	}

	const now = Math.floor(Date.now() / 1000);
	if (issuedAt > now || now - issuedAt > CSRF_TTL_SECONDS) {
		return false;
	}

	const payload = `${issuedAtRaw}.${nonce}`;
	const expectedSignature = createHmac("sha256", getCsrfSecret())
		.update(payload)
		.digest("base64url");

	const submittedBuffer = Buffer.from(signature);
	const expectedBuffer = Buffer.from(expectedSignature);
	return (
		submittedBuffer.length === expectedBuffer.length &&
		timingSafeEqual(submittedBuffer, expectedBuffer)
	);
}
