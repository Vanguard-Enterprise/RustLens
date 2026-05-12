"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { toast } from "sonner";

import { useSession } from "@/components/session-provider";
import { openSessionEventStream } from "@/lib/events-client";
import { clearSessionAction } from "@/lib/session-actions";
import { refreshSession } from "@/lib/session-refresh";

const REVOKED_NOTICE = {
	force_logout: "force_logout",
	suspended: "suspended",
	deleted: "deleted",
} as const;

/**
 * Mounts inside <SessionProvider>. Opens the SSE stream on mount, dispatches
 * server-pushed events to Sonner + session refresh + redirect. No DOM output.
 */
export function SessionEvents() {
	const session = useSession();
	const router = useRouter();

	useEffect(() => {
		const close = openSessionEventStream({
			async onRolesChanged() {
				await session.refresh();
				router.refresh();
				toast.success("Your permissions were updated");
			},
			async onSessionRevoked(ev) {
				await clearSessionAction();
				const notice = REVOKED_NOTICE[ev.reason] ?? "unknown";
				router.push(`/login?notice=${notice}`);
			},
			async onSessionExpiring() {
				const result = await refreshSession();
				if (!result.ok) {
					await clearSessionAction();
					router.push("/login?notice=expired");
				}
			},
			onConnectionLost() {
				// Only surface a non-destructive toast. A repeatedly-failing
				// EventSource can be caused by transient network blips or a
				// 429 too_many_connections from too many open tabs — neither
				// implies the session cookie is invalid. Real revocations
				// arrive as a session.revoked event, and stale cookies are
				// caught by the next authenticated request.
				toast.warning("Live updates disconnected", {
					description: "Refresh the page to reconnect.",
				});
			},
		});
		return close;
	}, [router, session]);

	return null;
}
