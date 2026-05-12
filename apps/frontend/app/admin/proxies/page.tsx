import { AppSidebar } from "../_components/app-sidebar";
import { ProxiesPage } from "./_components/proxies-page";
import { SiteHeader } from "@/components/site-header";
import { SidebarInset, SidebarProvider } from "@/components/ui/sidebar";
import { getCsrfToken } from "@/lib/csrf";
import {
	getAdminProxies,
	getCurrentUserSafe,
	type AdminProxiesQuery,
	type AdminProxyStatus,
} from "@/lib/rustlens-api";
import { requirePermission } from "@/lib/session";

const DEFAULT_PAGE_SIZE = 50;
const PAGE_SIZE_OPTIONS = [10, 20, 30, 40, 50];
const VALID_STATUSES: AdminProxyStatus[] = ["active", "paused", "broken"];

function pickStr(v: string | string[] | undefined): string | undefined {
	if (Array.isArray(v)) return v[0];
	return v;
}

function parsePositiveInt(v: string | undefined, fallback: number): number {
	if (!v) return fallback;
	const n = Number.parseInt(v, 10);
	return Number.isFinite(n) && n > 0 ? n : fallback;
}

export default async function AdminProxiesRoute({
	searchParams,
}: {
	searchParams: Promise<Record<string, string | string[] | undefined>>;
}) {
	const session = await requirePermission("proxies.read");
	const sp = await searchParams;

	const statusRaw = pickStr(sp.status);
	const status =
		statusRaw && (VALID_STATUSES as string[]).includes(statusRaw)
			? (statusRaw as AdminProxyStatus)
			: undefined;

	const requestedPageSize = parsePositiveInt(
		pickStr(sp.pageSize),
		DEFAULT_PAGE_SIZE,
	);
	const pageSize = PAGE_SIZE_OPTIONS.includes(requestedPageSize)
		? requestedPageSize
		: DEFAULT_PAGE_SIZE;

	const query: AdminProxiesQuery = {
		q: pickStr(sp.q),
		status,
		page: parsePositiveInt(pickStr(sp.page), 1),
		pageSize,
	};

	const [csrfToken, currentUser, proxies] = await Promise.all([
		getCsrfToken(),
		getCurrentUserSafe(),
		getAdminProxies(query),
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
								<h1 className="text-2xl font-semibold">Proxies</h1>
								<p className="text-sm text-muted-foreground">
									Manage outbound proxy providers used for upstream API traffic.
								</p>
							</div>
							<ProxiesPage
								initialPage={proxies}
								initialQuery={query}
								pageSizeOptions={PAGE_SIZE_OPTIONS}
								permissions={session.permissions ?? []}
							/>
						</div>
					</div>
				</div>
			</SidebarInset>
		</SidebarProvider>
	);
}
