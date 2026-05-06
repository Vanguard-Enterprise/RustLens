"use server";

import {
	getAdminAuditEvent,
	searchAdminUsers,
	type AdminUserItem,
	type AuditEventItem,
} from "@/lib/rustlens-api";
import { requirePermission } from "@/lib/session";

export type ActionResult<T> = { ok: true; result: T } | { ok: false; error: string };

export async function getAuditEventAction(
	id: string,
): Promise<ActionResult<AuditEventItem>> {
	await requirePermission("audit.read");
	try {
		const result = await getAdminAuditEvent(id);
		return { ok: true, result };
	} catch (err) {
		return { ok: false, error: (err as Error).message };
	}
}

export async function searchAdminUsersAction(q: string): Promise<AdminUserItem[]> {
	await requirePermission("audit.read");
	return searchAdminUsers(q);
}
