"use client";

import { createContext, useCallback, useContext, useMemo, useState } from "react";

import type { CurrentUser } from "@/lib/rustlens-api";
import { refreshCurrentUser } from "@/lib/session-actions";

export type SessionContextValue = {
	user: CurrentUser["user"] | null;
	roles: string[];
	adminRoles: string[];
	permissions: string[];
	entitlements: string[];
	refresh: () => Promise<void>;
};

const SessionContext = createContext<SessionContextValue | null>(null);

export function SessionProvider({
	initial,
	children,
}: {
	initial: CurrentUser | null;
	children: React.ReactNode;
}) {
	const [snapshot, setSnapshot] = useState<CurrentUser | null>(initial);

	const refresh = useCallback(async () => {
		const next = await refreshCurrentUser();
		setSnapshot(next);
	}, []);

	// Memoize the context value so consumers depending on `session` identity
	// (notably the SSE-opening effect in SessionEvents) don't tear down and
	// re-open the stream on every unrelated parent re-render.
	const value = useMemo<SessionContextValue>(
		() => ({
			user: snapshot?.user ?? null,
			roles: snapshot?.user.roles ?? [],
			adminRoles: snapshot?.adminRoles ?? [],
			permissions: snapshot?.permissions ?? [],
			entitlements: snapshot?.entitlements ?? [],
			refresh,
		}),
		[snapshot, refresh],
	);

	return <SessionContext.Provider value={value}>{children}</SessionContext.Provider>;
}

export function useSession() {
	const ctx = useContext(SessionContext);
	if (!ctx) {
		throw new Error("useSession must be used inside <SessionProvider>");
	}
	return ctx;
}
