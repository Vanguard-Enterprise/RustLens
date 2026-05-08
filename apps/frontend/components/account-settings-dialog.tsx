"use client";

import * as React from "react";

import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Switch } from "@/components/ui/switch";
import { Label } from "@/components/ui/label";
import { Alert, AlertDescription } from "@/components/ui/alert";

import {
	loadNotificationPreferencesAction,
	saveNotificationPreferencesAction,
} from "@/app/dashboard/_actions/preferences";
import type { NotificationPreferences } from "@/lib/rustlens-api";

type LoadState =
	| { status: "idle" }
	| { status: "loading" }
	| { status: "loaded"; loaded: NotificationPreferences; draft: NotificationPreferences }
	| { status: "error"; message: string };

type SaveState =
	| { status: "idle" }
	| { status: "saving" }
	| { status: "error"; message: string };

export function AccountSettingsDialog({
	open,
	onOpenChange,
}: {
	open: boolean;
	onOpenChange: (open: boolean) => void;
}) {
	const [loadState, setLoadState] = React.useState<LoadState>({ status: "idle" });
	const [saveState, setSaveState] = React.useState<SaveState>({ status: "idle" });

	const load = React.useCallback(async () => {
		setLoadState({ status: "loading" });
		const result = await loadNotificationPreferencesAction();
		if (!result.ok) {
			setLoadState({ status: "error", message: result.error });
			return;
		}
		setLoadState({
			status: "loaded",
			loaded: result.preferences,
			draft: result.preferences,
		});
	}, []);

	// Load preferences each time the dialog opens; reset state when it closes.
	React.useEffect(() => {
		if (open) {
			void load();
			setSaveState({ status: "idle" });
		} else {
			setLoadState({ status: "idle" });
		}
	}, [open, load]);

	const setDraftField = (field: keyof NotificationPreferences, value: boolean) => {
		setLoadState((prev) =>
			prev.status === "loaded"
				? { ...prev, draft: { ...prev.draft, [field]: value } }
				: prev,
		);
	};

	const dirty =
		loadState.status === "loaded" &&
		(loadState.draft.notifyInApp !== loadState.loaded.notifyInApp ||
			loadState.draft.notifyDiscordAnnouncements !==
				loadState.loaded.notifyDiscordAnnouncements);

	const handleSave = async () => {
		if (loadState.status !== "loaded" || !dirty) return;
		setSaveState({ status: "saving" });
		const patch: Partial<NotificationPreferences> = {};
		if (loadState.draft.notifyInApp !== loadState.loaded.notifyInApp) {
			patch.notifyInApp = loadState.draft.notifyInApp;
		}
		if (
			loadState.draft.notifyDiscordAnnouncements !==
			loadState.loaded.notifyDiscordAnnouncements
		) {
			patch.notifyDiscordAnnouncements = loadState.draft.notifyDiscordAnnouncements;
		}
		const result = await saveNotificationPreferencesAction(patch);
		if (!result.ok) {
			setSaveState({ status: "error", message: result.error });
			return;
		}
		setSaveState({ status: "idle" });
		onOpenChange(false);
	};

	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent className="sm:max-w-md">
				<DialogHeader>
					<DialogTitle>Account settings</DialogTitle>
					<DialogDescription>
						Manage how RustLens contacts you.
					</DialogDescription>
				</DialogHeader>

				<section aria-labelledby="notifications-heading" className="space-y-4">
					<div>
						<h3 id="notifications-heading" className="text-sm font-medium">
							Notifications
						</h3>
					</div>

					{loadState.status === "loading" || loadState.status === "idle" ? (
						<div className="text-sm text-muted-foreground">Loading…</div>
					) : loadState.status === "error" ? (
						<Alert variant="destructive">
							<AlertDescription className="flex items-center justify-between gap-3">
								<span>Couldn&apos;t load preferences: {loadState.message}</span>
								<Button size="sm" variant="outline" onClick={() => void load()}>
									Retry
								</Button>
							</AlertDescription>
						</Alert>
					) : (
						<div className="space-y-4">
							<div className="flex items-start justify-between gap-4">
								<div className="space-y-1">
									<Label htmlFor="notify-in-app">In-app notifications</Label>
									<p className="text-xs text-muted-foreground">
										Show alerts in the dashboard about activity in your guilds.
									</p>
								</div>
								<Switch
									id="notify-in-app"
									checked={loadState.draft.notifyInApp}
									onCheckedChange={(v) => setDraftField("notifyInApp", v)}
								/>
							</div>

							<div className="flex items-start justify-between gap-4">
								<div className="space-y-1">
									<Label htmlFor="notify-discord">Discord product updates</Label>
									<p className="text-xs text-muted-foreground">
										Allow the bot to DM you about RustLens product updates and
										announcements. Guild-event DMs are unaffected.
									</p>
								</div>
								<Switch
									id="notify-discord"
									checked={loadState.draft.notifyDiscordAnnouncements}
									onCheckedChange={(v) =>
										setDraftField("notifyDiscordAnnouncements", v)
									}
								/>
							</div>
						</div>
					)}
				</section>

				{saveState.status === "error" ? (
					<Alert variant="destructive">
						<AlertDescription>
							Couldn&apos;t save: {saveState.message}
						</AlertDescription>
					</Alert>
				) : null}

				<DialogFooter>
					<Button
						variant="outline"
						onClick={() => onOpenChange(false)}
						disabled={saveState.status === "saving"}
					>
						Cancel
					</Button>
					<Button
						onClick={() => void handleSave()}
						disabled={
							loadState.status !== "loaded" ||
							!dirty ||
							saveState.status === "saving"
						}
					>
						{saveState.status === "saving" ? "Saving…" : "Save changes"}
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
