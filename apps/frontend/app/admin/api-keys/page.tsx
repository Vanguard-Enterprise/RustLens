import type { Metadata } from "next";
import { AppSidebar } from "../_components/app-sidebar";
import { ApiKeysPage } from "./_components/api-keys-page";
import { SiteHeader } from "@/components/site-header";
import { SidebarInset, SidebarProvider } from "@/components/ui/sidebar";
import { getCsrfToken } from "@/lib/csrf";
import {
	getAdminApiKeys,
	getAdminPermissions,
	getCurrentUserSafe,
} from "@/lib/rustlens-api";
import { requirePermission } from "@/lib/session";

export const metadata: Metadata = { title: "API Keys" };

function pickStr(v: string | string[] | undefined): string | undefined {
	if (Array.isArray(v)) return v[0];
	return v;
}

function parsePositiveInt(v: string | undefined, fallback: number): number {
	if (!v) return fallback;
	const n = Number.parseInt(v, 10);
	return Number.isFinite(n) && n > 0 ? n : fallback;
}

export default async function AdminApiKeysRoute({
	searchParams,
}: {
	searchParams: Promise<Record<string, string | string[] | undefined>>;
}) {
	const session = await requirePermission("api_keys.manage");
	const sp = await searchParams;

	const q = pickStr(sp.q);
	const includeRevoked = pickStr(sp.includeRevoked) === "true";
	const page = parsePositiveInt(pickStr(sp.page), 1);

	const [csrfToken, currentUser, data, allPermissions] = await Promise.all([
		getCsrfToken(),
		getCurrentUserSafe(),
		getAdminApiKeys({ q, includeRevoked, page, pageSize: 50 }),
		getAdminPermissions(),
	]);

	const userPerms = new Set(session.permissions ?? []);
	const isSuperadmin =
		(session.permissions ?? []).includes("admins.grant") ||
		(session.adminRoles ?? []).includes("superadmin");
	const availablePermissions = allPermissions.filter(
		(p) => isSuperadmin || userPerms.has(p.key),
	);

	const user = {
		displayName: session.displayName,
		avatarUrl: session.avatarUrl || undefined,
		discordUsername: currentUser?.user.discordUsername,
		isInternalAdmin:
			(session.permissions?.length ?? 0) > 0 ||
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
								<h1 className="text-2xl font-semibold">API Keys</h1>
								<p className="text-sm text-muted-foreground">
									Manage API keys used for programmatic access to the RustLens API.
								</p>
							</div>
							<ApiKeysPage
								data={data}
								initialQuery={{ q, includeRevoked, page }}
								availablePermissions={availablePermissions}
								csrfToken={csrfToken}
							/>
						</div>
					</div>
				</div>
			</SidebarInset>
		</SidebarProvider>
	);
}
