"use client";

import { useEffect, useRef } from "react";
import { toast } from "sonner";

const MESSAGES: Record<string, { variant: "error" | "info" | "warning"; text: string }> = {
	force_logout: { variant: "error", text: "You were signed out by an administrator." },
	suspended: { variant: "error", text: "Your account has been suspended." },
	deleted: { variant: "error", text: "Your account has been deleted." },
	expired: { variant: "info", text: "Your session expired, please sign in again." },
};

/**
 * Reads ?notice= from the URL and fires a one-shot Sonner toast. Server-render
 * safe — only fires on first client mount, with a ref guard against React 19
 * strict-mode double-invocation.
 */
export function NoticeToast({ notice }: { notice: string | undefined }) {
	const fired = useRef(false);
	useEffect(() => {
		if (!notice || fired.current) return;
		const entry = MESSAGES[notice];
		if (!entry) return;
		fired.current = true;
		toast[entry.variant](entry.text);
	}, [notice]);
	return null;
}
