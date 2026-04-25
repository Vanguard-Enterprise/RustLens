import { AppShell } from "@/components/app-shell";
import { getCsrfToken } from "@/lib/csrf";
import { getCurrentUser } from "@/lib/rustlens-api";
import { requireAdminSession } from "@/lib/session";

export default async function AdminLayout({
	children,
}: Readonly<{
	children: React.ReactNode;
}>) {
	// requireAdminSession guarantees the caller has at least one admin role or
	// permission (or the legacy "internal_admin" string); redirects otherwise.
	// No second authorization check is needed here.
	const session = await requireAdminSession();
	const [currentUser, csrfToken] = await Promise.all([
		getCurrentUser(),
		getCsrfToken(),
	]);

	return (
		<AppShell
			csrfToken={csrfToken}
			user={{
				displayName: currentUser.user.displayName,
				discordUsername: currentUser.user.discordUsername,
				avatarUrl: currentUser.user.avatarUrl,
				isInternalAdmin: true,
				permissions: session.permissions ?? currentUser.permissions ?? [],
			}}
			variant="admin"
		>
			{children}
		</AppShell>
	);
}
