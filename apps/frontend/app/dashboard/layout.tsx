import { ReactNode } from "react";

import { SessionEvents } from "@/components/session-events";
import { SessionProvider } from "@/components/session-provider";
import { SessionRefreshTrigger } from "@/components/session-refresh-trigger";
import { getCurrentUserSafe } from "@/lib/rustlens-api";
import { requireSession } from "@/lib/session";

export default async function DashboardLayout({ children }: { children: ReactNode }) {
	// Page-level loaders will redirect themselves if the session is gone,
	// but we still need a session-touch here so we don't crash inside
	// SessionProvider with a null cookie on a soft refresh.
	await requireSession();
	const currentUser = await getCurrentUserSafe();
	return (
		<SessionProvider initial={currentUser}>
			<SessionEvents />
			<SessionRefreshTrigger />
			{children}
		</SessionProvider>
	);
}
