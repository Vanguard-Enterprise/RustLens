"use server";

import { revalidatePath } from "next/cache";

import {
    createAdminRole,
    deleteAdminRole,
    setAdminRolePermissions,
    updateAdminRole,
    type CreateAdminRoleInput,
} from "@/lib/rustlens-api";
import { requirePermission } from "@/lib/session";

const ROLES_PATH = "/admin/roles";

export type SaveRoleResult =
    | { ok: true }
    | { ok: false; error: string };

export async function createRoleAction(
    input: CreateAdminRoleInput,
    reason: string,
): Promise<SaveRoleResult> {
    await requirePermission("admins.grant");
    if (!reason.trim()) return { ok: false, error: "reason_required" };
    try {
        await createAdminRole(input, reason.trim());
        revalidatePath(ROLES_PATH);
        return { ok: true };
    } catch (err) {
        return { ok: false, error: (err as Error).message };
    }
}

export async function updateRoleAction(
    id: string,
    patch: { name: string; description: string },
    reason: string,
): Promise<SaveRoleResult> {
    await requirePermission("admins.grant");
    if (!reason.trim()) return { ok: false, error: "reason_required" };
    try {
        await updateAdminRole(id, patch, reason.trim());
        revalidatePath(ROLES_PATH);
        return { ok: true };
    } catch (err) {
        return { ok: false, error: (err as Error).message };
    }
}

export async function setRolePermissionsAction(
    id: string,
    permissions: string[],
    reason: string,
): Promise<SaveRoleResult> {
    await requirePermission("admins.grant");
    if (!reason.trim()) return { ok: false, error: "reason_required" };
    try {
        await setAdminRolePermissions(id, permissions, reason.trim());
        revalidatePath(ROLES_PATH);
        return { ok: true };
    } catch (err) {
        return { ok: false, error: (err as Error).message };
    }
}

export async function deleteRoleAction(id: string, reason: string): Promise<SaveRoleResult> {
    await requirePermission("admins.grant");
    if (!reason.trim()) return { ok: false, error: "reason_required" };
    try {
        await deleteAdminRole(id, reason.trim());
        revalidatePath(ROLES_PATH);
        return { ok: true };
    } catch (err) {
        return { ok: false, error: (err as Error).message };
    }
}
