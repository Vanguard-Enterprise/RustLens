// Pure browser helpers — keep React out so this file can be unit-tested with
// no DOM-React harness. The hook is in components/session-events.tsx.

export type SessionEvent =
	| { type: "roles.changed"; reason: "grant" | "revoke" | "role_perms_updated" }
	| { type: "session.revoked"; reason: "force_logout" | "suspended" | "deleted" }
	| { type: "session.expiring"; expiresAt: number }
	| { type: "ping" };

export type SessionEventHandlers = {
	onRolesChanged: (e: Extract<SessionEvent, { type: "roles.changed" }>) => void;
	onSessionRevoked: (e: Extract<SessionEvent, { type: "session.revoked" }>) => void;
	onSessionExpiring: (e: Extract<SessionEvent, { type: "session.expiring" }>) => void;
	onConnectionLost: () => void;
};

const FAILURE_WINDOW_MS = 30_000;
const FAILURE_THRESHOLD = 3;

/**
 * Opens an EventSource against /api/events/stream and routes parsed events
 * to the supplied handlers. The returned function closes the connection.
 *
 * Treats 3 consecutive failures in a 30s window as a "connection lost" event
 * — handlers should treat it like a revoked session and redirect to /login.
 *
 * Pauses the stream when the document is hidden and reopens it on visible /
 * online so background tabs don't burn the 5-connections-per-user backend cap.
 */
export function openSessionEventStream(handlers: SessionEventHandlers): () => void {
	let source: EventSource | null = null;
	let failures: number[] = [];
	let closed = false;
	let suspended = false;

	const open = () => {
		if (closed || suspended || source) return;
		const es = new EventSource("/api/events/stream", { withCredentials: true });
		source = es;

		es.addEventListener("roles.changed", (ev: Event) => {
			const data = parseData(ev as MessageEvent);
			if (data?.type === "roles.changed") handlers.onRolesChanged(data);
		});
		es.addEventListener("session.revoked", (ev: Event) => {
			const data = parseData(ev as MessageEvent);
			if (data?.type === "session.revoked") {
				handlers.onSessionRevoked(data);
				safeClose();
			}
		});
		es.addEventListener("session.expiring", (ev: Event) => {
			const data = parseData(ev as MessageEvent);
			if (data?.type === "session.expiring") handlers.onSessionExpiring(data);
		});
		es.addEventListener("ping", () => {
			// Live channel proof — also reset the failure window since a ping
			// implies the prior reconnect succeeded.
			failures = [];
		});
		es.addEventListener("error", () => {
			const now = Date.now();
			failures = failures.filter((t) => now - t < FAILURE_WINDOW_MS);
			failures.push(now);
			if (failures.length >= FAILURE_THRESHOLD) {
				handlers.onConnectionLost();
				safeClose();
			}
			// Native EventSource reconnect is fast and synchronous, which thrashes
			// the backend on transient errors. Don't intervene further here —
			// browsers respect the implicit 3s retry interval.
		});
	};

	const closeSource = () => {
		if (source) {
			source.close();
			source = null;
		}
	};

	const safeClose = () => {
		if (closed) return;
		closed = true;
		closeSource();
		if (typeof document !== "undefined") {
			document.removeEventListener("visibilitychange", onVisibility);
		}
		if (typeof window !== "undefined") {
			window.removeEventListener("online", onOnline);
		}
	};

	const onVisibility = () => {
		if (closed) return;
		if (document.hidden) {
			suspended = true;
			closeSource();
		} else {
			suspended = false;
			open();
		}
	};

	const onOnline = () => {
		if (closed || suspended) return;
		// Force a fresh connection on network recovery.
		closeSource();
		open();
	};

	if (typeof document !== "undefined") {
		document.addEventListener("visibilitychange", onVisibility);
		suspended = document.hidden;
	}
	if (typeof window !== "undefined") {
		window.addEventListener("online", onOnline);
	}

	open();
	return safeClose;
}

function parseData(ev: MessageEvent): SessionEvent | null {
	try {
		return JSON.parse(ev.data) as SessionEvent;
	} catch {
		return null;
	}
}
