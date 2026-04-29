import Link from "next/link";
import { notFound } from "next/navigation";
import { Suspense } from "react";

import { AppSidebar } from "../../_components/app-sidebar";
import { LinksList } from "./_components/links-list";
import { LinksSkeleton } from "./_components/links-skeleton";
import { SiteHeader } from "@/components/site-header";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { SidebarInset, SidebarProvider } from "@/components/ui/sidebar";
import { guildIconUrl } from "@/lib/discord-cdn";
import { getCsrfToken } from "@/lib/csrf";
import { getCurrentUserSafe, getMyInstalledGuilds } from "@/lib/rustlens-api";
import { requireSession } from "@/lib/session";

function initials(name: string): string {
	return name
		.split(/\s+/)
		.filter(Boolean)
		.slice(0, 2)
		.map((part) => part[0]!.toUpperCase())
		.join("");
}

type PageProps = { params: Promise<{ guildId: string }> };

export default async function Page({ params }: PageProps) {
	const { guildId } = await params;
	const session = await requireSession();
	const [csrfToken, currentUser, guilds] = await Promise.all([
		getCsrfToken(),
		getCurrentUserSafe(),
		getMyInstalledGuilds(),
	]);

	const guild = guilds.find((g) => g.id === guildId);
	if (!guild) {
		notFound();
	}

	const user = {
		displayName: session.displayName,
		avatarUrl: session.avatarUrl || undefined,
		discordUsername: currentUser?.user.discordUsername,
		isInternalAdmin:
			(session.adminRoles?.length ?? 0) > 0 ||
			session.roles.includes("internal_admin"),
	};

	const iconSrc = guildIconUrl(guild.id, guild.iconHash) ?? undefined;

	return (
		<SidebarProvider
			style={
				{
					"--sidebar-width": "calc(var(--spacing) * 72)",
					"--header-height": "calc(var(--spacing) * 12)",
				} as React.CSSProperties
			}
		>
			<AppSidebar csrfToken={csrfToken} user={user} variant="inset" />
			<SidebarInset>
				<SiteHeader />
				<div className="flex flex-1 flex-col">
					<div className="@container/main flex flex-1 flex-col gap-2">
						<div className="flex flex-col gap-4 py-4 md:gap-6 md:py-6">
							<div className="flex flex-col gap-2 px-4 lg:px-6">
								<Link
									href="/dashboard"
									className="text-sm text-muted-foreground hover:underline"
								>
									← Back to guilds
								</Link>
								<div className="flex items-center gap-3">
									<Avatar size="lg" className="ring-2 ring-background">
										{iconSrc ? <AvatarImage src={iconSrc} alt="" /> : null}
										<AvatarFallback>{initials(guild.name)}</AvatarFallback>
									</Avatar>
									<h1 className="font-heading text-xl">{guild.name}</h1>
								</div>
								<p className="text-sm text-muted-foreground">
									Your Discord-to-Steam account links scoped to this guild.
								</p>
							</div>
							<div className="px-4 lg:px-6">
								<Suspense fallback={<LinksSkeleton />}>
									<LinksList guildId={guildId} />
								</Suspense>
							</div>
						</div>
					</div>
				</div>
			</SidebarInset>
		</SidebarProvider>
	);
}
