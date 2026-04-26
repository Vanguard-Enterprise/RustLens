"use client";

import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { Separator } from "@/components/ui/separator";
import { AppBreadcrumbs } from "@/components/app-breadcrumbs";
import { CustomSidebarTrigger } from "@/components/custom-sidebar-trigger";
import {
	type AppShellVariant,
	getCurrentNavItem,
} from "@/components/app-shared";
import { NavUser } from "@/components/nav-user";
import { BellIcon } from "lucide-react";

type AppHeaderProps = {
	pathname: string;
	variant: AppShellVariant;
	user: {
		displayName: string;
		discordGlobalName?: string;
		discordUsername?: string;
		avatarUrl?: string;
		isInternalAdmin: boolean;
	};
	csrfToken: string;
};

export function AppHeader({ csrfToken, pathname, variant, user }: AppHeaderProps) {
	const activeItem = getCurrentNavItem(variant, pathname);
	const activePage = activeItem
		? {
				title: activeItem.title,
				icon: <activeItem.icon />,
			}
		: null;

	return (
		<header
			className={cn(
				"mb-6 flex items-center justify-between gap-2 px-2"
			)}
		>
			<div className="flex items-center gap-3">
				<CustomSidebarTrigger />
				<Separator
					className="mr-2 h-4 data-[orientation=vertical]:self-center"
					orientation="vertical"
				/>
				<AppBreadcrumbs page={activePage} />
			</div>
			<div className="flex items-center gap-3">
				<Button aria-label="Notifications" size="icon" variant="ghost">
					<BellIcon
					/>
				</Button>
				<Separator
					className="h-4 data-[orientation=vertical]:self-center"
					orientation="vertical"
				/>
				<NavUser csrfToken={csrfToken} user={user} />
			</div>
		</header>
	);
}
