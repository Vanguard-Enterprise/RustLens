"use server";

import { revalidatePath } from "next/cache";

import {
	adminApiKeyCreate,
	adminApiKeyRotate,
	adminApiKeyRevoke,
	type CreateApiKeyInput,
	type CreateApiKeyResponse,
} from "@/lib/rustlens-api";
import { requirePermission } from "@/lib/session";

const API_KEYS_PATH = "/admin/api-keys";

export type ActionResult<T = unknown> =
	| { ok: true; result: T }
	| { ok: false; error: string };

export async function createApiKeyAction(
	input: CreateApiKeyInput,
	reason: string,
): Promise<ActionResult<CreateApiKeyResponse>> {
	await requirePermission("api_keys.manage");
	if (!input.name.trim()) return { ok: false, error: "name is required" };
	if (!reason.trim()) return { ok: false, error: "reason is required" };
	try {
		const result = await adminApiKeyCreate(
			{ ...input, name: input.name.trim() },
			reason.trim(),
		);
		revalidatePath(API_KEYS_PATH);
		return { ok: true, result };
	} catch (err) {
		return { ok: false, error: (err as Error).message };
	}
}

export async function rotateApiKeyAction(
	keyId: string,
	reason: string,
): Promise<ActionResult<{ secret: string }>> {
	await requirePermission("api_keys.manage");
	if (!keyId) return { ok: false, error: "keyId is required" };
	if (!reason.trim()) return { ok: false, error: "reason is required" };
	try {
		const result = await adminApiKeyRotate(keyId, reason.trim());
		revalidatePath(API_KEYS_PATH);
		return { ok: true, result };
	} catch (err) {
		return { ok: false, error: (err as Error).message };
	}
}

export async function revokeApiKeyAction(
	keyId: string,
	reason: string,
): Promise<ActionResult<{ keyId: string }>> {
	await requirePermission("api_keys.manage");
	if (!keyId) return { ok: false, error: "keyId is required" };
	if (!reason.trim()) return { ok: false, error: "reason is required" };
	try {
		await adminApiKeyRevoke(keyId, reason.trim());
		revalidatePath(API_KEYS_PATH);
		return { ok: true, result: { keyId } };
	} catch (err) {
		return { ok: false, error: (err as Error).message };
	}
}
