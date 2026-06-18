import type { Metadata } from "next";
import { notFound } from "next/navigation";

import { AppSidebar } from "../../_components/app-sidebar";
import { GuildDetailPage } from "../_components/guild-detail-page";
import { SiteHeader } from "@/components/site-header";
import { SidebarInset, SidebarProvider } from "@/components/ui/sidebar";
import { getCsrfToken } from "@/lib/csrf";
import { getAdminGuild, getCurrentUserSafe } from "@/lib/rustlens-api";
import { requirePermission } from "@/lib/session";

export const metadata: Metadata = { title: "Guild Details" };

export default async function AdminGuildDetailRoute({
	params,
	searchParams,
}: {
	params: Promise<{ guildId: string }>;
	searchParams: Promise<Record<string, string | string[] | undefined>>;
}) {
	const session = await requirePermission("guilds.read");
	const { guildId } = await params;
	const sp = await searchParams;
	const pageParam = (name: string) => {
		const raw = Array.isArray(sp[name]) ? sp[name]?.[0] : sp[name];
		const value = Number.parseInt(raw ?? "", 10);
		return Number.isFinite(value) && value > 0 ? value : undefined;
	};

	let detail;
	try {
		detail = await getAdminGuild(guildId, {
			memberPage: pageParam("memberPage"),
			linkPage: pageParam("linkPage"),
			trackGroupPage: pageParam("trackGroupPage"),
			usagePage: pageParam("usagePage"),
		});
	} catch (err) {
		const status = (err as Error & { status?: number }).status;
		const message = (err as Error).message;
		if (status === 404 || message.includes("404") || message === "guild not found") {
			notFound();
		}
		throw err;
	}

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
						<div className="flex flex-col gap-4 px-4 py-4 md:gap-6 md:py-6 lg:px-6">
							<GuildDetailPage detail={detail} />
						</div>
					</div>
				</div>
			</SidebarInset>
		</SidebarProvider>
	);
}
