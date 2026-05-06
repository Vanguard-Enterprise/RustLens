"use client";

import * as React from "react";
import Image from "next/image";
import Link from "next/link";
import { usePathname } from "next/navigation";

import { NavGroup } from "@/components/nav-group";
import { NavMain } from "@/components/nav-main";
import { NavSecondary } from "@/components/nav-secondary";
import {
	SidebarNavUser,
	type SidebarNavUser as SidebarNavUserData,
} from "@/components/sidebar-nav-user";
import {
	Sidebar,
	SidebarContent,
	SidebarFooter,
	SidebarHeader,
	SidebarMenu,
	SidebarMenuButton,
	SidebarMenuItem,
} from "@/components/ui/sidebar";
import {
	ActivityIcon,
	CameraIcon,
	CircleHelpIcon,
	FileTextIcon,
	KeyRoundIcon,
	LayoutDashboardIcon,
	SearchIcon,
	Settings2Icon,
	UsersIcon,
	UsersRoundIcon,
} from "lucide-react";

import type { SidebarNavItem } from "@/components/app-shared";

export type DashboardSidebarArea = "admin" | "dashboard";

type NavItem = {
	title: string;
	url: string;
	icon?: React.ReactNode;
	pathPrefix: string;
};

const PRIMARY_NAV: Record<DashboardSidebarArea, NavItem> = {
	dashboard: {
		title: "Dashboard",
		url: "/dashboard",
		icon: <LayoutDashboardIcon />,
		pathPrefix: "/dashboard",
	},
	admin: {
		title: "Dashboard",
		url: "/admin",
		icon: <LayoutDashboardIcon />,
		pathPrefix: "/admin",
	},
};

const NAV_CLOUDS = [
	{
		title: "Capture",
		icon: <CameraIcon />,
		isActive: true,
		url: "#",
		items: [
			{ title: "Active Proposals", url: "#" },
			{ title: "Archived", url: "#" },
		],
	},
	{
		title: "Proposal",
		icon: <FileTextIcon />,
		url: "#",
		items: [
			{ title: "Active Proposals", url: "#" },
			{ title: "Archived", url: "#" },
		],
	},
	{
		title: "Prompts",
		icon: <FileTextIcon />,
		url: "#",
		items: [
			{ title: "Active Proposals", url: "#" },
			{ title: "Archived", url: "#" },
		],
	},
];

const NAV_SECONDARY = [
	{ title: "Settings", url: "#", icon: <Settings2Icon /> },
	{ title: "Get Help", url: "#", icon: <CircleHelpIcon /> },
	{ title: "Search", url: "#", icon: <SearchIcon /> },
];

function buildNavMain(area: DashboardSidebarArea): NavItem[] {
	return [PRIMARY_NAV[area]];
}

type DashboardAppSidebarProps = Omit<
	React.ComponentProps<typeof Sidebar>,
	"variant"
> & {
	area: DashboardSidebarArea;
	user: SidebarNavUserData;
	csrfToken: string;
	/** Permission keys held by the current session — used to gate admin nav groups. */
	permissions?: string[];
	/** Optional override for the underlying shadcn Sidebar visual variant. */
	sidebarVariant?: React.ComponentProps<typeof Sidebar>["variant"];
};

export function DashboardAppSidebar({
	area,
	csrfToken,
	permissions = [],
	sidebarVariant,
	user,
	...props
}: DashboardAppSidebarProps) {
	const pathname = usePathname() ?? "";
	const navMain = buildNavMain(area);
	const homeHref = area === "admin" ? "/admin" : "/dashboard";

	return (
		<Sidebar collapsible="icon" variant={sidebarVariant} {...props}>
			<SidebarHeader>
				<SidebarMenu>
					<SidebarMenuItem>
						<SidebarMenuButton
							asChild
							className="data-[slot=sidebar-menu-button]:p-1.5!"
						>
							<Link href={homeHref}>
								<Image
									alt="RustLens"
									className="size-5! max-w-none shrink-0"
									height={64}
									priority
									src="/RustLens.png"
									width={64}
								/>
								<span className="text-base font-semibold whitespace-nowrap transition-opacity duration-100 delay-150 ease-out group-data-[collapsible=icon]:opacity-0 group-data-[collapsible=icon]:delay-0">
									RustLens
								</span>
							</Link>
						</SidebarMenuButton>
					</SidebarMenuItem>
				</SidebarMenu>
			</SidebarHeader>
			<SidebarContent>
				<NavMain items={navMain} />
				{area === "admin" ? (
					<>
						<NavGroup
							label="Discord"
							items={[
								{
									title: "Guilds",
									path: "/admin/guilds",
									icon: UsersRoundIcon,
									isActive: pathname === "/admin/guilds" || pathname.startsWith("/admin/guilds/"),
								},
							]}
						/>
						{(() => {
							const accessItems: SidebarNavItem[] = [];
							if (permissions.includes("users.read")) {
								accessItems.push({
									title: "Users",
									path: "/admin/users",
									icon: UsersIcon,
									isActive: pathname === "/admin/users" || pathname.startsWith("/admin/users/"),
								});
							}
							if (permissions.includes("admins.grant")) {
								accessItems.push({
									title: "Roles",
									path: "/admin/roles",
									icon: KeyRoundIcon,
									isActive: pathname === "/admin/roles" || pathname.startsWith("/admin/roles/"),
								});
							}
							if (permissions.includes("audit.read")) {
								accessItems.push({
									title: "Audit",
									path: "/admin/audit",
									icon: ActivityIcon,
									isActive: pathname === "/admin/audit" || pathname.startsWith("/admin/audit/"),
								});
							}
							return accessItems.length > 0 ? (
								<NavGroup label="Access" items={accessItems} />
							) : null;
						})()}
					</>
				) : (
					<NavSecondary items={NAV_SECONDARY} className="mt-auto" />
				)}
			</SidebarContent>
			<SidebarFooter>
				<SidebarNavUser csrfToken={csrfToken} user={user} variant={area} />
			</SidebarFooter>
		</Sidebar>
	);
}

export const DASHBOARD_NAV_CLOUDS = NAV_CLOUDS;
