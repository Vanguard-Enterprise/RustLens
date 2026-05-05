import { notFound } from "next/navigation";

import { AppSidebar } from "../../_components/app-sidebar";
import { UserDetailPage } from "../_components/user-detail-page";
import { SiteHeader } from "@/components/site-header";
import { SidebarInset, SidebarProvider } from "@/components/ui/sidebar";
import { getCsrfToken } from "@/lib/csrf";
import {
	getAdminRoles,
	getAdminUser,
	getAdminUserAudit,
	getCurrentUserSafe,
} from "@/lib/rustlens-api";
import { requirePermission } from "@/lib/session";

export default async function AdminUserDetailRoute({
	params,
}: {
	params: Promise<{ userId: string }>;
}) {
	const session = await requirePermission("users.read");
	const { userId } = await params;

	let userDetail;
	try {
		userDetail = await getAdminUser(userId);
	} catch {
		notFound();
	}

	const [csrfToken, currentUser, roles, audit] = await Promise.all([
		getCsrfToken(),
		getCurrentUserSafe(),
		getAdminRoles(),
		getAdminUserAudit(userId, { limit: 30 }),
	]);

	const user = {
		displayName: session.displayName,
		avatarUrl: session.avatarUrl || undefined,
		discordUsername: currentUser?.user.discordUsername,
		isInternalAdmin:
			(session.adminRoles?.length ?? 0) > 0 ||
			session.roles.includes("internal_admin"),
	};

	return (
		<SidebarProvider
			style={
				{
					"--sidebar-width": "calc(var(--spacing) * 72)",
					"--header-height": "calc(var(--spacing) * 12)",
				} as React.CSSProperties
			}
		>
			<AppSidebar
				csrfToken={csrfToken}
				user={user}
				permissions={session.permissions ?? []}
				variant="inset"
			/>
			<SidebarInset>
				<SiteHeader />
				<div className="flex flex-1 flex-col">
					<div className="@container/main flex flex-1 flex-col gap-2">
						<div className="flex flex-col gap-4 py-4 md:gap-6 md:py-6 px-4 lg:px-6">
							<UserDetailPage
								detail={userDetail}
								roles={roles}
								audit={audit.items}
								actorUserId={session.userId ?? ""}
								permissions={session.permissions ?? []}
							/>
						</div>
					</div>
				</div>
			</SidebarInset>
		</SidebarProvider>
	);
}
