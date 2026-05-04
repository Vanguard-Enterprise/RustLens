import { AppSidebar } from "../_components/app-sidebar";
import { RolesPage } from "./_components/roles-page";
import { SiteHeader } from "@/components/site-header";
import { SidebarInset, SidebarProvider } from "@/components/ui/sidebar";
import { getCsrfToken } from "@/lib/csrf";
import {
	getAdminPermissions,
	getAdminRoles,
	getCurrentUserSafe,
} from "@/lib/rustlens-api";
import { requirePermission } from "@/lib/session";

export default async function AdminRolesPage() {
	const session = await requirePermission("admins.grant");

	const [csrfToken, currentUser, roles, permissionsCatalog] = await Promise.all([
		getCsrfToken(),
		getCurrentUserSafe(),
		getAdminRoles(),
		getAdminPermissions(),
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
							<div>
								<h1 className="text-2xl font-semibold">Roles</h1>
								<p className="text-sm text-muted-foreground">
									Create and manage admin roles. System roles are read-only.
								</p>
							</div>
							<RolesPage initialRoles={roles} permissionsCatalog={permissionsCatalog} />
						</div>
					</div>
				</div>
			</SidebarInset>
		</SidebarProvider>
	);
}
