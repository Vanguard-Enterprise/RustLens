import { AppShell } from "@/components/app-shell";
import { getCsrfToken } from "@/lib/csrf";
import { getCurrentUser } from "@/lib/rustlens-api";
import { requireInternalAdminSession } from "@/lib/session";
import { redirect } from "next/navigation";

export default async function AdminLayout({
	children,
}: Readonly<{
	children: React.ReactNode;
}>) {
	const session = await requireInternalAdminSession();
	const currentUser = await getCurrentUser();
	const csrfToken = await getCsrfToken();
	if (!currentUser.user.roles.includes("internal_admin")) {
		redirect("/dashboard");
	}

	return (
		<AppShell
			csrfToken={csrfToken}
			user={{
				displayName: currentUser.user.displayName,
				email: currentUser.primaryEmail?.email,
				avatarUrl: currentUser.user.avatarUrl,
				isInternalAdmin: true,
				permissions: session.permissions ?? [],
			}}
			variant="admin"
		>
			{children}
		</AppShell>
	);
}
