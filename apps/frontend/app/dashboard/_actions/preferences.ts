"use server";

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
		return { ok: false, error: (err as Error).message };
	}
}
