import "server-only";

/**
 * Thrown by lib/rustlens-api.ts when the backend rejects a session for a
 * recoverable reason (revoked by admin / suspended / deleted / expired /
 * missing). Server components that catch this should clear the cookie and
 * redirect to /login with the matching ?notice= value so the user sees a
 * Sonner toast on the next render.
 */
export class SessionRevokedError extends Error {
	readonly notice: SessionRevokedNotice;

	constructor(notice: SessionRevokedNotice) {
		super(`session_revoked:${notice}`);
		this.name = "SessionRevokedError";
		this.notice = notice;
	}
}

export type SessionRevokedNotice =
	| "force_logout"
	| "suspended"
	| "deleted"
	| "expired"
	| "connection_lost"
	| "unknown";

// Maps backend error codes to user-facing notice slugs. Keep in sync with the
// classifySessionError() function in apps/backend/internal/http/events_handlers.go
// and with SESSION_INVALID_ERRORS in apps/frontend/lib/rustlens-api.ts.
export function noticeFromBackendError(code: string): SessionRevokedNotice {
	switch (code) {
		case "session_revoked":
			// We don't know the precise reason here — the SSE channel delivers
			// reason; pre-SSE 401 paths see only the generic code.
			return "unknown";
		case "session_expired":
			return "expired";
		case "session_required":
		case "invalid_session":
			return "expired";
		default:
			return "unknown";
	}
}
