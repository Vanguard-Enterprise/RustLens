import type { Metadata } from "next";
import { AppSidebar } from "../_components/app-sidebar";
import { BmServersPage } from "./_components/bm-servers-page";
import { SiteHeader } from "@/components/site-header";
import { SidebarInset, SidebarProvider } from "@/components/ui/sidebar";
import { getCsrfToken } from "@/lib/csrf";
import {
	getAdminBmServers,
	getCurrentUserSafe,
	type AdminBmServerSortKey,
	type AdminBmServerSortOrder,
	type AdminBmServersQuery,
} from "@/lib/rustlens-api";
import { requirePermission } from "@/lib/session";

const DEFAULT_PAGE_SIZE = 50;
const PAGE_SIZE_OPTIONS = [10, 20, 30, 40, 50, 100];

const VALID_SORT_KEYS: AdminBmServerSortKey[] = [
	"name",
	"bmId",
	"highFrequency",
	"lastPolledAt",
	"pollStatus",
];
const VALID_SORT_ORDERS: AdminBmServerSortOrder[] = ["asc", "desc"];

function pickStr(v: string | string[] | undefined): string | undefined {
	if (Array.isArray(v)) return v[0];
	return v;
}

function parsePositiveInt(v: string | undefined, fallback: number): number {
	if (!v) return fallback;
	const n = Number.parseInt(v, 10);
	return Number.isFinite(n) && n > 0 ? n : fallback;
}

function parseBoolParam(v: string | undefined): boolean | undefined {
	if (!v) return undefined;
	if (v === "true") return true;
	if (v === "false") return false;
	return undefined;
}

export const metadata: Metadata = { title: "BattleMetrics Servers" };

export default async function AdminBmServersRoute({
	searchParams,
}: {
	searchParams: Promise<Record<string, string | string[] | undefined>>;
}) {
	const session = await requirePermission("bm_servers.read");
	const sp = await searchParams;

	const requestedPageSize = parsePositiveInt(
		pickStr(sp.pageSize),
		DEFAULT_PAGE_SIZE,
	);
	const pageSize = PAGE_SIZE_OPTIONS.includes(requestedPageSize)
		? requestedPageSize
		: DEFAULT_PAGE_SIZE;

	const sortByRaw = pickStr(sp.sortBy);
	const sortBy =
		sortByRaw && (VALID_SORT_KEYS as string[]).includes(sortByRaw)
			? (sortByRaw as AdminBmServerSortKey)
			: undefined;
	const sortOrderRaw = pickStr(sp.sortOrder);
	const sortOrder =
		sortOrderRaw && (VALID_SORT_ORDERS as string[]).includes(sortOrderRaw)
			? (sortOrderRaw as AdminBmServerSortOrder)
			: undefined;

	const query: AdminBmServersQuery = {
		q: pickStr(sp.q),
		highFrequency: parseBoolParam(pickStr(sp.highFrequency)),
		hasError: parseBoolParam(pickStr(sp.hasError)),
		isVitality: parseBoolParam(pickStr(sp.isVitality)),
		isAtlas: parseBoolParam(pickStr(sp.isAtlas)),
		isVital: parseBoolParam(pickStr(sp.isVital)),
		isModded: parseBoolParam(pickStr(sp.isModded)),
		isOfficial: parseBoolParam(pickStr(sp.isOfficial)),
		isBuild: parseBoolParam(pickStr(sp.isBuild)),
		page: parsePositiveInt(pickStr(sp.page), 1),
		pageSize,
		sortBy,
		sortOrder,
	};

	const [csrfToken, currentUser, servers] = await Promise.all([
		getCsrfToken(),
		getCurrentUserSafe(),
		getAdminBmServers(query),
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
								<h1 className="text-2xl font-semibold">BM Servers</h1>
								<p className="text-sm text-muted-foreground">
									Manage tracked BattleMetrics server targets polled by the
									backend and selectable in guild settings.
								</p>
							</div>
							<BmServersPage
								initialPage={servers}
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
