"use server";

import { unstable_rethrow } from "next/navigation";

import {
	getNotificationPreferences,
	updateNotificationPreferences,
	type NotificationPreferences,
	type NotificationPreferencesPatch,
} from "@/lib/rustlens-api";
import { requireSession } from "@/lib/session";

export type LoadPreferencesResult =
	| { ok: true; preferences: NotificationPreferences }
	| { ok: false; error: string };

export type SavePreferencesResult =
	| { ok: true; preferences: NotificationPreferences }
	| { ok: false; error: string };

export async function loadNotificationPreferencesAction(): Promise<LoadPreferencesResult> {
	await requireSession();
	try {
		const preferences = await getNotificationPreferences();
		return { ok: true, preferences };
	} catch (err) {
		// Let Next.js redirect/notFound exceptions propagate so that a revoked
		// session redirect from backendFetch reaches the framework.
		unstable_rethrow(err);
		return { ok: false, error: (err as Error).message };
	}
}

export async function saveNotificationPreferencesAction(
	patch: NotificationPreferencesPatch,
): Promise<SavePreferencesResult> {
	await requireSession();
	try {
		const preferences = await updateNotificationPreferences(patch);
		return { ok: true, preferences };
	} catch (err) {
		unstable_rethrow(err);
		return { ok: false, error: (err as Error).message };
	}
}
