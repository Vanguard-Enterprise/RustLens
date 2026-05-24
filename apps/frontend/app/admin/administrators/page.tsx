import { AppSidebar } from "../_components/app-sidebar";
import { UsersPage } from "../users/_components/users-page";
import { effectiveRoleFilter } from "../users/_components/role-filter";
import { UsersPageEmptyState } from "../users/_components/users-page-empty-state";
import { SiteHeader } from "@/components/site-header";
import { SidebarInset, SidebarProvider } from "@/components/ui/sidebar";
import { getCsrfToken } from "@/lib/csrf";
import {
	getAdminRoles,
	getAdministrators,
	getCurrentUserSafe,
	type AdminUsersQuery,
} from "@/lib/rustlens-api";
import { requirePermission } from "@/lib/session";

const DEFAULT_PAGE_SIZE = 50;
const PAGE_SIZE_OPTIONS = [10, 20, 30, 40, 50];

function pickStr(v: string | string[] | undefined): string | undefined {
	if (Array.isArray(v)) return v[0];
	return v;
}

function pickArr(v: string | string[] | undefined): string[] {
	if (Array.isArray(v)) return v;
	if (typeof v === "string" && v !== "") return [v];
	return [];
}

function parsePositiveInt(v: string | undefined, fallback: number): number {
	if (!v) return fallback;
	const n = Number.parseInt(v, 10);
	return Number.isFinite(n) && n > 0 ? n : fallback;
}

export default async function AdminAdministratorsRoute({
	searchParams,
}: {
	searchParams: Promise<Record<string, string | string[] | undefined>>;
}) {
	const session = await requirePermission("users.read");
	const sp = await searchParams;

	const urlQuery: AdminUsersQuery = {
		q: pickStr(sp.q),
		role: pickArr(sp.role),
		status: (pickStr(sp.status) as AdminUsersQuery["status"]) ?? "active",
		sort: pickStr(sp.sort),
		page: parsePositiveInt(pickStr(sp.page), 1),
		pageSize: PAGE_SIZE_OPTIONS.includes(parsePositiveInt(pickStr(sp.pageSize), DEFAULT_PAGE_SIZE))
			? parsePositiveInt(pickStr(sp.pageSize), DEFAULT_PAGE_SIZE)
			: DEFAULT_PAGE_SIZE,
	};

	const allRoles = await getAdminRoles();
	const adminRoles = allRoles.filter((r) => r.permissions.length > 0);
	const adminRoleKeys = adminRoles.map((r) => r.key);
	const canGrantSuperadmin = (session.permissions ?? []).includes("admins.grant");

	const [csrfToken, currentUser] = await Promise.all([
		getCsrfToken(),
		getCurrentUserSafe(),
	]);

	const user = {
		displayName: session.displayName,
		avatarUrl: session.avatarUrl || undefined,
		discordUsername: currentUser?.user.discordUsername,
		isInternalAdmin:
			(session.permissions?.length ?? 0) > 0 ||
			session.roles.includes("internal_admin"),
	};

	let body: React.ReactNode;
	if (adminRoleKeys.length === 0) {
		body = <UsersPageEmptyState />;
	} else {
		const effectiveRoles = effectiveRoleFilter(urlQuery.role ?? [], adminRoleKeys);
		const query: AdminUsersQuery = { ...urlQuery, role: effectiveRoles };
		const users = await getAdministrators(query);
		body = (
			<UsersPage
				initialPage={users}
				roles={adminRoles}
				initialQuery={query}
				pageSizeOptions={PAGE_SIZE_OPTIONS}
				permissions={session.permissions ?? []}
				canGrantSuperadmin={canGrantSuperadmin}
			/>
		);
	}

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
								<h1 className="text-2xl font-semibold">Administrators</h1>
								<p className="text-sm text-muted-foreground">
									Find admin users, assign roles, and manage account state.
								</p>
							</div>
							{body}
						</div>
					</div>
				</div>
			</SidebarInset>
		</SidebarProvider>
	);
}
