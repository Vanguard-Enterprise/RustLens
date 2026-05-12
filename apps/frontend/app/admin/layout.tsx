import { ReactNode } from "react";

import { SessionEvents } from "@/components/session-events";
import { SessionProvider } from "@/components/session-provider";
import { getCurrentUserSafe } from "@/lib/rustlens-api";
import { requireAdminSession } from "@/lib/session";

export default async function AdminLayout({ children }: { children: ReactNode }) {
	// Admin pages also call requireAdminSession themselves; this layout-level
	// touch ensures the provider has an authenticated user before mounting.
	await requireAdminSession();
	const currentUser = await getCurrentUserSafe();
	return (
		<SessionProvider initial={currentUser}>
			<SessionEvents />
			{children}
		</SessionProvider>
	);
}
