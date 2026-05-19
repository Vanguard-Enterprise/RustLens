import { NextResponse } from "next/server";

import { validateCsrfRequest } from "@/lib/csrf";
import { consumeRateLimit } from "@/lib/rate-limit";
import { adminApiKeyRotate } from "@/lib/rustlens-api";
import { requirePermission } from "@/lib/session";

type RotateBody = { csrfToken?: string; reason?: string };

export async function POST(
	request: Request,
	{ params }: { params: Promise<{ id: string }> },
) {
	const { id } = await params;
	const body = (await request.json().catch(() => ({}))) as RotateBody;
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

	// Rotate returns a freshly minted secret; rate-limit it like create.
	const limit = await consumeRateLimit(`api-keys:rotate:${session.userId}`, {
		max: 10,
		windowMs: 60_000,
	});
	if (!limit.allowed) {
		return NextResponse.json(
			{ error: "rate_limited" },
			{ status: 429, headers: { "Retry-After": String(limit.retryAfterSeconds) } },
		);
	}

	const reason = body.reason?.trim() ?? "";
	if (!id) {
		return NextResponse.json({ error: "keyId is required" }, { status: 400 });
	}
	if (!reason) {
		return NextResponse.json({ error: "reason is required" }, { status: 400 });
	}

	try {
		const result = await adminApiKeyRotate(id, reason);
		return NextResponse.json({ ok: true, result });
	} catch (err) {
		return NextResponse.json(
			{ error: (err as Error).message },
			{ status: 500 },
		);
	}
}
