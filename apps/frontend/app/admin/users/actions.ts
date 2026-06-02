"use server";

import { revalidatePath } from "next/cache";

import {
	getAdminUserCommandUsageChildren,
	getAdminUserCommandUsageTargets,
	grantAdminUserRole,
	bulkAdminUserAction,
	deleteAdminUser,
	editAdminUser,
	revokeAdminUserRole,
	searchAdminUsers,
	setAdminUserBan,
	suspendAdminUser,
	unsuspendAdminUser,
	type AdminBanCategory,
	type AdminCommandUsageItem,
	type AdminCommandUsageTarget,
	type AdminUserItem,
	type BulkUserResult,
	type BulkUserSubAction,
	type EditUserPatch,
} from "@/lib/rustlens-api";
import { requirePermission } from "@/lib/session";

const USERS_PATH = "/admin/users";
const ADMINISTRATORS_PATH = "/admin/administrators";

export type ActionResult<T = unknown> =
	| { ok: true; result: T }
	| { ok: false; error: string };

function userPath(userId: string) {
	return `${USERS_PATH}/${userId}`;
}

function revalidateLists() {
	revalidatePath(USERS_PATH);
	revalidatePath(ADMINISTRATORS_PATH);
}

async function withReason<T>(
	reason: string,
	fn: () => Promise<T>,
): Promise<ActionResult<T>> {
	if (!reason.trim()) return { ok: false, error: "reason_required" };
	try {
		const result = await fn();
		return { ok: true, result };
	} catch (err) {
		return { ok: false, error: (err as Error).message };
	}
}

export async function grantRoleAction(
	userId: string,
	roleKey: string,
	reason: string,
): Promise<ActionResult> {
	await requirePermission("users.roles.assign");
	const res = await withReason(reason, () =>
		grantAdminUserRole(userId, roleKey, reason.trim()),
	);
	if (res.ok) {
		revalidateLists();
		revalidatePath(userPath(userId));
	}
	return res;
}

export async function revokeRoleAction(
	userId: string,
	roleKey: string,
	reason: string,
): Promise<ActionResult> {
	await requirePermission("users.roles.assign");
	const res = await withReason(reason, () =>
		revokeAdminUserRole(userId, roleKey, reason.trim()),
	);
	if (res.ok) {
		revalidateLists();
		revalidatePath(userPath(userId));
	}
	return res;
}

export async function editUserAction(
	userId: string,
	patch: EditUserPatch,
	reason: string,
): Promise<ActionResult> {
	await requirePermission("users.edit");
	// users.edit doesn't require a reason; pass empty string through.
	try {
		await editAdminUser(userId, patch, reason.trim());
		revalidateLists();
		revalidatePath(userPath(userId));
		return { ok: true, result: { userId } };
	} catch (err) {
		return { ok: false, error: (err as Error).message };
	}
}

export async function suspendUserAction(
	userId: string,
	reason: string,
): Promise<ActionResult> {
	await requirePermission("users.suspend");
	const res = await withReason(reason, () =>
		suspendAdminUser(userId, reason.trim()),
	);
	if (res.ok) {
		revalidateLists();
		revalidatePath(userPath(userId));
	}
	return res;
}

export async function unsuspendUserAction(
	userId: string,
	reason: string,
): Promise<ActionResult> {
	await requirePermission("users.suspend");
	// users.unsuspend doesn't require a reason on the backend.
	try {
		await unsuspendAdminUser(userId, reason.trim());
		revalidateLists();
		revalidatePath(userPath(userId));
		return { ok: true, result: { userId } };
	} catch (err) {
		return { ok: false, error: (err as Error).message };
	}
}

export async function setUserBanAction(
	userId: string,
	categories: AdminBanCategory[],
	reason: string,
): Promise<ActionResult> {
	await requirePermission("users.suspend");
	const res = await withReason(reason, () =>
		setAdminUserBan(userId, categories, reason.trim()),
	);
	if (res.ok) {
		revalidateLists();
		revalidatePath(userPath(userId));
	}
	return res;
}

export async function deleteUserAction(
	userId: string,
	reason: string,
	typedConfirmation: string,
): Promise<ActionResult> {
	await requirePermission("users.delete");
	const res = await withReason(reason, () =>
		deleteAdminUser(userId, reason.trim(), typedConfirmation),
	);
	if (res.ok) {
		revalidateLists();
		revalidatePath(userPath(userId));
	}
	return res;
}

export async function bulkUserAction(
	action: BulkUserSubAction,
	userIds: string[],
	params: Record<string, unknown>,
	reason: string,
): Promise<ActionResult<BulkUserResult>> {
	if (action === "suspend" || action === "unsuspend") {
		await requirePermission("users.suspend");
	} else {
		await requirePermission("users.roles.assign");
	}
	if (action === "suspend" || action === "roles.grant" || action === "roles.revoke") {
		// All three require a reason at the sub-action level.
		if (!reason.trim()) return { ok: false, error: "reason_required" };
	}
	if (userIds.length < 1 || userIds.length > 200) {
		return { ok: false, error: "userIds must be 1..200" };
	}
	try {
		const result = await bulkAdminUserAction(action, userIds, params, reason.trim());
		revalidateLists();
		return { ok: true, result };
	} catch (err) {
		return { ok: false, error: (err as Error).message };
	}
}

export async function searchUsersForGrantAction(
	q: string,
): Promise<AdminUserItem[]> {
	await requirePermission("users.roles.assign");
	return searchAdminUsers(q);
}

export async function getUserCommandUsageTargetsAction(
	userId: string,
	usageId: number,
): Promise<{ targets: AdminCommandUsageTarget[] }> {
	await requirePermission("users.read");
	return getAdminUserCommandUsageTargets(userId, usageId);
}

export async function getUserCommandUsageChildrenAction(
	userId: string,
	usageId: number,
): Promise<{ items: AdminCommandUsageItem[] }> {
	await requirePermission("users.read");
	return getAdminUserCommandUsageChildren(userId, usageId);
}
