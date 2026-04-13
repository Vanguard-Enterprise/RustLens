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

	return (
		<AppShell
			csrfToken={csrfToken}
			user={{
				displayName: currentUser.user.displayName,
				email: currentUser.primaryEmail?.email,
				avatarUrl: currentUser.user.avatarUrl,
				isInternalAdmin: currentUser.user.roles.includes("internal_admin"),
			}}
			variant="dashboard"
		>
			{children}
		</AppShell>
	);
}
