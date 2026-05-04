"use client";

import * as React from "react";

import {
	DashboardAppSidebar,
} from "@/components/dashboard-app-sidebar";
import type { SidebarNavUser } from "@/components/sidebar-nav-user";
import type { Sidebar } from "@/components/ui/sidebar";

type AppSidebarProps = Omit<React.ComponentProps<typeof Sidebar>, "variant"> & {
	user: SidebarNavUser;
	csrfToken: string;
	permissions?: string[];
	variant?: React.ComponentProps<typeof Sidebar>["variant"];
};

export function AppSidebar({ variant, ...props }: AppSidebarProps) {
	return <DashboardAppSidebar area="admin" sidebarVariant={variant} {...props} />;
}
