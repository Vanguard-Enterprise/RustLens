import { NextResponse } from "next/server";

import { validateCsrfRequest } from "@/lib/csrf";
import { consumeRateLimit } from "@/lib/rate-limit";
import {
	adminApiKeyCreate,
	type CreateApiKeyInput,
} from "@/lib/rustlens-api";
import { requirePermission } from "@/lib/session";

type CreateBody = {
	csrfToken?: string;
	input?: CreateApiKeyInput;
	reason?: string;
};

export async function POST(request: Request) {
	const body = (await request.json().catch(() => ({}))) as CreateBody;
	const csrfValid = await validateCsrfRequest(request, body.csrfToken ?? null);
	if (!csrfValid) {
		return NextResponse.json({ error: "invalid_csrf" }, { status: 400 });
	}
	let session;
	try {
		session = await requirePermission("api_keys.manage");
	} catch {
		return NextResponse.json({ error: "forbidden" }, { status: 403 });
	}

	// Returning a fresh API-key secret is high-value; cap to 10/min/user so a
	// compromised cookie can't brute-mint keys at line rate.
	const limit = await consumeRateLimit(`api-keys:create:${session.userId}`, {
		max: 10,
		windowMs: 60_000,
	});
	if (!limit.allowed) {
		return NextResponse.json(
			{ error: "rate_limited" },
			{ status: 429, headers: { "Retry-After": String(limit.retryAfterSeconds) } },
		);
	}

	const input = body.input;
	const reason = body.reason?.trim() ?? "";
	if (!input || typeof input.name !== "string" || !input.name.trim()) {
		return NextResponse.json({ error: "name is required" }, { status: 400 });
	}
	if (!reason) {
		return NextResponse.json({ error: "reason is required" }, { status: 400 });
	}
	if (!Array.isArray(input.permissions)) {
		return NextResponse.json(
			{ error: "permissions must be an array" },
			{ status: 400 },
		);
	}

	try {
		const result = await adminApiKeyCreate(
			{ ...input, name: input.name.trim() },
			reason,
		);
		return NextResponse.json({ ok: true, result });
	} catch (err) {
		return NextResponse.json(
			{ error: (err as Error).message },
			{ status: 500 },
		);
	}
}
