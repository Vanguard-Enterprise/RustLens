"use client";

import { createContext, useCallback, useContext, useState } from "react";

import type { CurrentUser } from "@/lib/rustlens-api";
import { refreshCurrentUser } from "@/lib/session-actions";

export type SessionContextValue = {
	user: CurrentUser["user"] | null;
	roles: string[];
	adminRoles: string[];
	permissions: string[];
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

	const value: SessionContextValue = {
		user: snapshot?.user ?? null,
		roles: snapshot?.user.roles ?? [],
		adminRoles: snapshot?.adminRoles ?? [],
		permissions: snapshot?.permissions ?? [],
		refresh,
	};

	return <SessionContext.Provider value={value}>{children}</SessionContext.Provider>;
}

export function useSession() {
	const ctx = useContext(SessionContext);
	if (!ctx) {
		throw new Error("useSession must be used inside <SessionProvider>");
	}
	return ctx;
}
