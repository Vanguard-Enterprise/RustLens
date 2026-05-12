"use server";

import { revalidatePath } from "next/cache";

import {
	bulkAdminProxies,
	createAdminProxy,
	deleteAdminProxy,
	updateAdminProxy,
	type BulkProxyAction,
	type BulkProxyResult,
} from "@/lib/rustlens-api";
import { requirePermission } from "@/lib/session";

const PROXIES_PATH = "/admin/proxies";

export type ActionResult<T = unknown> =
	| { ok: true; result: T }
	| { ok: false; error: string };

export async function createProxyAction(
	endpoint: string,
	reason: string,
): Promise<ActionResult> {
	await requirePermission("proxies.edit");
	if (!endpoint.trim()) return { ok: false, error: "endpoint_required" };
	try {
		const result = await createAdminProxy(endpoint.trim(), reason.trim());
		revalidatePath(PROXIES_PATH);
		return { ok: true, result };
	} catch (err) {
		return { ok: false, error: (err as Error).message };
	}
}

export async function toggleProxyAction(
	id: number,
	enabled: boolean,
	reason: string,
): Promise<ActionResult> {
	await requirePermission("proxies.edit");
	try {
		const result = await updateAdminProxy(id, enabled, reason.trim());
		revalidatePath(PROXIES_PATH);
		return { ok: true, result };
	} catch (err) {
		return { ok: false, error: (err as Error).message };
	}
}

export async function deleteProxyAction(
	id: number,
	reason: string,
): Promise<ActionResult> {
	await requirePermission("proxies.edit");
	try {
		await deleteAdminProxy(id, reason.trim());
		revalidatePath(PROXIES_PATH);
		return { ok: true, result: { id } };
	} catch (err) {
		return { ok: false, error: (err as Error).message };
	}
}

export async function bulkProxyAction(
	action: BulkProxyAction,
	ids: number[],
	reason: string,
): Promise<ActionResult<BulkProxyResult>> {
	await requirePermission("proxies.edit");
	if (ids.length < 1 || ids.length > 200) {
		return { ok: false, error: "ids must be 1..200" };
	}
	try {
		const result = await bulkAdminProxies(action, ids, reason.trim());
		revalidatePath(PROXIES_PATH);
		return { ok: true, result };
	} catch (err) {
		return { ok: false, error: (err as Error).message };
	}
}
