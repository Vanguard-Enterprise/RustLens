"use server";

import { redirect } from "next/navigation";
import { unstable_rethrow } from "next/navigation";

import { deleteDiscordRoleConnection } from "@/lib/rustlens-api";
import { requireSession } from "@/lib/session";

export async function connectDiscordRoleAction(): Promise<void> {
	redirect("/api/auth/discord/role-connection?return=dashboard");
}

export async function disconnectDiscordRoleAction(): Promise<void> {
	await requireSession();
	try {
		await deleteDiscordRoleConnection();
	} catch (err) {
		unstable_rethrow(err);
		throw err;
	}
	redirect("/dashboard?notice=discord-unlinked");
}
