"use server";

import { revalidatePath } from "next/cache";

import {
	bulkAdminBmServers,
	createAdminBmServer,
	deleteAdminBmServer,
	updateAdminBmServer,
	type AdminBmServerInput,
	type AdminBmServerItem,
	type BulkBmServerAction,
	type BulkBmServerResult,
} from "@/lib/rustlens-api";
import { requirePermission } from "@/lib/session";

const BM_SERVERS_PATH = "/admin/bm-servers";

export type ActionResult<T = unknown> =
	| { ok: true; result: T }
	| { ok: false; error: string };

export async function createBmServerAction(
	input: AdminBmServerInput,
	reason: string,
): Promise<ActionResult<AdminBmServerItem>> {
	await requirePermission("bm_servers.edit");
	if (!input.bmId.trim()) return { ok: false, error: "bmId is required" };
	if (!input.name.trim()) return { ok: false, error: "name is required" };
	try {
		const result = await createAdminBmServer(
			{ ...input, bmId: input.bmId.trim(), name: input.name.trim() },
			reason.trim(),
		);
		revalidatePath(BM_SERVERS_PATH);
		return { ok: true, result };
	} catch (err) {
		return { ok: false, error: (err as Error).message };
	}
}

export async function updateBmServerAction(
	currentBmId: string,
	input: AdminBmServerInput,
	reason: string,
): Promise<ActionResult<AdminBmServerItem>> {
	await requirePermission("bm_servers.edit");
	if (!input.bmId.trim()) return { ok: false, error: "bmId is required" };
	if (!input.name.trim()) return { ok: false, error: "name is required" };
	try {
		const result = await updateAdminBmServer(
			currentBmId,
			{ ...input, bmId: input.bmId.trim(), name: input.name.trim() },
			reason.trim(),
		);
		revalidatePath(BM_SERVERS_PATH);
		return { ok: true, result };
	} catch (err) {
		return { ok: false, error: (err as Error).message };
	}
}

export async function deleteBmServerAction(
	bmId: string,
	reason: string,
): Promise<ActionResult<{ bmId: string }>> {
	await requirePermission("bm_servers.edit");
	try {
		await deleteAdminBmServer(bmId, reason.trim());
		revalidatePath(BM_SERVERS_PATH);
		return { ok: true, result: { bmId } };
	} catch (err) {
		return { ok: false, error: (err as Error).message };
	}
}

export async function bulkBmServerAction(
	action: BulkBmServerAction,
	bmIds: string[],
	reason: string,
): Promise<ActionResult<BulkBmServerResult>> {
	await requirePermission("bm_servers.edit");
	if (bmIds.length < 1 || bmIds.length > 200) {
		return { ok: false, error: "bmIds must be 1..200" };
	}
	try {
		const result = await bulkAdminBmServers(action, bmIds, reason.trim());
		revalidatePath(BM_SERVERS_PATH);
		return { ok: true, result };
	} catch (err) {
		return { ok: false, error: (err as Error).message };
	}
}
