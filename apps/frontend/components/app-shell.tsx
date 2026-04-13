"use client";

import { SidebarInset, SidebarProvider } from "@/components/ui/sidebar";
import { AppHeader } from "@/components/app-header";
import { AppSidebar } from "@/components/app-sidebar";
import type { AppShellVariant } from "@/components/app-shared";
import { usePathname } from "next/navigation";

type AppShellProps = {
	children: React.ReactNode;
	variant: AppShellVariant;
	user: {
		displayName: string;
		email?: string;
		avatarUrl?: string;
		isInternalAdmin: boolean;
	};
	csrfToken: string;
};

export function AppShell({
	children,
	csrfToken,
	variant,
	user,
}: AppShellProps) {
	const pathname = usePathname();

	return (
		<SidebarProvider>
			<AppSidebar pathname={pathname} variant={variant} />
			<SidebarInset className="p-4 md:p-6">
				<AppHeader csrfToken={csrfToken} pathname={pathname} user={user} variant={variant} />
				<div className="flex flex-1 flex-col gap-4 overflow-y-auto">
					{children}
				</div>
			</SidebarInset>
		</SidebarProvider>
	);
}
