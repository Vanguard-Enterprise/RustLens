import { NextResponse } from "next/server";

import { validateCsrfRequest } from "@/lib/csrf";
import { adminApiKeyRevoke } from "@/lib/rustlens-api";
import { requirePermission } from "@/lib/session";

type RevokeBody = { csrfToken?: string; reason?: string };

export async function POST(
	request: Request,
	{ params }: { params: Promise<{ id: string }> },
) {
	const { id } = await params;
	const body = (await request.json().catch(() => ({}))) as RevokeBody;
	const csrfValid = await validateCsrfRequest(request, body.csrfToken ?? null);
	if (!csrfValid) {
		return NextResponse.json({ error: "invalid_csrf" }, { status: 400 });
	}
	try {
		await requirePermission("api_keys.manage");
	} catch {
		return NextResponse.json({ error: "forbidden" }, { status: 403 });
	}

	const reason = body.reason?.trim() ?? "";
	if (!id) {
		return NextResponse.json({ error: "keyId is required" }, { status: 400 });
	}
	if (!reason) {
		return NextResponse.json({ error: "reason is required" }, { status: 400 });
	}

	try {
		await adminApiKeyRevoke(id, reason);
		return NextResponse.json({ ok: true, result: { keyId: id } });
	} catch (err) {
		return NextResponse.json(
			{ error: (err as Error).message },
			{ status: 500 },
		);
	}
}
