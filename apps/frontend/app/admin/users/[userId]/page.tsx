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
	getAdminUserCommandUsage,
	getCurrentUserSafe,
} from "@/lib/rustlens-api";
import { requirePermission } from "@/lib/session";

export default async function AdminUserDetailRoute({
	params,
	searchParams,
}: {
	params: Promise<{ userId: string }>;
	searchParams: Promise<Record<string, string | string[] | undefined>>;
}) {
	const session = await requirePermission("users.read");
	const { userId } = await params;
	const sp = await searchParams;

	const usagePage = Number((sp.usagePage as string | undefined) ?? "1") || 1;
	const usagePageSize = 25;

	let userDetail;
	try {
		userDetail = await getAdminUser(userId);
	} catch (err) {
		const msg = (err as Error).message;
		if (msg.includes("404") || msg === "user_not_found") {
			notFound();
		}
		throw err;
	}

	const [csrfToken, currentUser, roles, audit, commandUsage] = await Promise.all([
		getCsrfToken(),
		getCurrentUserSafe(),
		getAdminRoles(),
		getAdminUserAudit(userId, { limit: 30 }),
		getAdminUserCommandUsage(userId, { page: usagePage, pageSize: usagePageSize }),
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
								commandUsage={commandUsage}
								permissions={session.permissions ?? []}
							/>
						</div>
					</div>
				</div>
			</SidebarInset>
		</SidebarProvider>
	);
}
