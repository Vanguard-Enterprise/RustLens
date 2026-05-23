"use client";

import { useEffect, useRef } from "react";
import { toast } from "sonner";

const NOTICE_MESSAGES: Record<string, { variant: "success" | "info"; text: string }> = {
	"discord-linked": {
		variant: "success",
		text: "Discord connected - your community role will sync shortly.",
	},
	"discord-unlinked": {
		variant: "info",
		text: "Discord disconnected. You can reconnect anytime from your dashboard.",
	},
};

const ERROR_MESSAGES: Record<string, string> = {
	"discord-mismatch":
		"That Discord account doesn't match your RustLens account. Sign in with the Discord account you used to register.",
	"discord-state": "Discord sign-in failed (invalid state). Please try again.",
	"discord-token": "Discord sign-in failed (token exchange). Please try again.",
	"discord-profile": "Discord sign-in failed (profile fetch). Please try again.",
	"discord-config": "Discord integration not configured.",
	"discord-encrypt": "Couldn't save your Discord link - please try again.",
	"discord-persist": "Couldn't save your Discord link - please try again.",
};

/**
 * Reads ?notice= and ?error= from the URL and fires one-shot Sonner toasts.
 * Server-render safe - only fires on first client mount, with ref guards
 * against React 19 strict-mode double-invocation.
 */
export function DashboardNoticeToast({
	notice,
	error,
}: {
	notice: string | undefined;
	error: string | undefined;
}) {
	const noticeFired = useRef(false);
	const errorFired = useRef(false);

	useEffect(() => {
		if (notice && !noticeFired.current) {
			const entry = NOTICE_MESSAGES[notice];
			if (entry) {
				noticeFired.current = true;
				toast[entry.variant](entry.text);
			}
		}
	}, [notice]);

	useEffect(() => {
		if (error && !errorFired.current) {
			const message = ERROR_MESSAGES[error];
			if (message) {
				errorFired.current = true;
				toast.error(message);
			}
		}
	}, [error]);

	return null;
}
