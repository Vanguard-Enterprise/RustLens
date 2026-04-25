import { AppShell } from "@/components/app-shell";
import { getCsrfToken } from "@/lib/csrf";
import { getCurrentUserSafe } from "@/lib/rustlens-api";
import { requireSession } from "@/lib/session";
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
		redirect("/logout?reason=session");
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
				avatarUrl: currentUser.user.avatarUrl,
				isInternalAdmin,
			}}
			variant="dashboard"
		>
			{children}
		</AppShell>
	);
}
