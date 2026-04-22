import { AppShell } from "@/components/app-shell";
import { getCsrfToken } from "@/lib/csrf";
import { getCurrentUserSafe } from "@/lib/rustlens-api";
import { clearSession, requireSession } from "@/lib/session";
import { redirect } from "next/navigation";

export default async function DashboardLayout({
	children,
}: Readonly<{
	children: React.ReactNode;
}>) {
	await requireSession();
	const currentUser = await getCurrentUserSafe();
	const csrfToken = await getCsrfToken();

	if (!currentUser) {
		await clearSession();
		redirect("/login?error=session");
	}

	const isInternalAdmin =
		(currentUser.adminRoles ?? []).length > 0 ||
		(currentUser.permissions ?? []).length > 0 ||
		// Legacy fallback for users still carrying the pre-RBAC sentinel.
		currentUser.user.roles.includes("internal_admin");

	return (
		<AppShell
			csrfToken={csrfToken}
			user={{
				displayName: currentUser.user.displayName,
				email: currentUser.primaryEmail?.email,
				avatarUrl: currentUser.user.avatarUrl,
				isInternalAdmin,
			}}
			variant="dashboard"
		>
			{children}
		</AppShell>
	);
}
