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
 */
export function openSessionEventStream(handlers: SessionEventHandlers): () => void {
	const source = new EventSource("/api/events/stream", { withCredentials: true });
	let failures: number[] = [];
	let closed = false;

	const safeClose = () => {
		if (closed) return;
		closed = true;
		source.close();
	};

	source.addEventListener("roles.changed", (ev: Event) => {
		const data = parseData(ev as MessageEvent);
		if (data?.type === "roles.changed") handlers.onRolesChanged(data);
	});
	source.addEventListener("session.revoked", (ev: Event) => {
		const data = parseData(ev as MessageEvent);
		if (data?.type === "session.revoked") {
			handlers.onSessionRevoked(data);
			safeClose();
		}
	});
	source.addEventListener("session.expiring", (ev: Event) => {
		const data = parseData(ev as MessageEvent);
		if (data?.type === "session.expiring") handlers.onSessionExpiring(data);
	});
	source.addEventListener("ping", () => {
		// no-op; presence of the event proves the channel is alive.
	});
	source.addEventListener("error", () => {
		const now = Date.now();
		failures = failures.filter((t) => now - t < FAILURE_WINDOW_MS);
		failures.push(now);
		if (failures.length >= FAILURE_THRESHOLD) {
			handlers.onConnectionLost();
			safeClose();
		}
	});

	return safeClose;
}

function parseData(ev: MessageEvent): SessionEvent | null {
	try {
		return JSON.parse(ev.data) as SessionEvent;
	} catch {
		return null;
	}
}
