"use client";

import { useEffect, useRef } from "react";

import { refreshSession } from "@/lib/session-refresh";

const MIN_INTERVAL_MS = 60 * 1000; // throttle
const POLL_INTERVAL_MS = 60 * 60 * 1000; // 1 hour

/**
 * Triggers refreshSession() on mount, on window focus, on online events, and
 * every hour while the tab is alive. Throttled so a rapid focus loop doesn't
 * hammer the backend. Renders nothing.
 */
export function SessionRefreshTrigger() {
	const lastAt = useRef(0);

	useEffect(() => {
		const tryRefresh = () => {
			const now = Date.now();
			if (now - lastAt.current < MIN_INTERVAL_MS) return;
			lastAt.current = now;
			void refreshSession();
		};

		tryRefresh();
		const onFocus = () => tryRefresh();
		const onOnline = () => tryRefresh();
		window.addEventListener("focus", onFocus);
		window.addEventListener("online", onOnline);
		const id = window.setInterval(tryRefresh, POLL_INTERVAL_MS);

		return () => {
			window.removeEventListener("focus", onFocus);
			window.removeEventListener("online", onOnline);
			window.clearInterval(id);
		};
	}, []);

	return null;
}
