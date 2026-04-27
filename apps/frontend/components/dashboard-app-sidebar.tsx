"use client";

import * as React from "react";
import Image from "next/image";
import Link from "next/link";
import { usePathname } from "next/navigation";

import { NavDocuments } from "@/components/nav-documents";
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
	CameraIcon,
	ChartBarIcon,
	CircleHelpIcon,
	DatabaseIcon,
	FileChartColumnIcon,
	FileIcon,
	FileTextIcon,
	FolderIcon,
	LayoutDashboardIcon,
	ListIcon,
	SearchIcon,
	Settings2Icon,
	ShieldCheckIcon,
	UsersIcon,
} from "lucide-react";

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
		title: "Admin",
		url: "/admin",
		icon: <ShieldCheckIcon />,
		pathPrefix: "/admin",
	},
};

const SECONDARY_NAV_MAIN: NavItem[] = [
	{ title: "Lifecycle", url: "#", icon: <ListIcon />, pathPrefix: "" },
	{ title: "Analytics", url: "#", icon: <ChartBarIcon />, pathPrefix: "" },
	{ title: "Projects", url: "#", icon: <FolderIcon />, pathPrefix: "" },
	{ title: "Team", url: "#", icon: <UsersIcon />, pathPrefix: "" },
];

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

const DOCUMENTS = [
	{ name: "Data Library", url: "#", icon: <DatabaseIcon /> },
	{ name: "Reports", url: "#", icon: <FileChartColumnIcon /> },
	{ name: "Word Assistant", url: "#", icon: <FileIcon /> },
];

function buildNavMain(pathname: string, isAdmin: boolean): NavItem[] {
	const items: NavItem[] = [];
	for (const key of ["dashboard", "admin"] as const) {
		const item = PRIMARY_NAV[key];
		if (key === "admin" && !isAdmin) continue;
		if (pathname === item.pathPrefix || pathname.startsWith(`${item.pathPrefix}/`)) {
			continue;
		}
		items.push(item);
	}
	items.push(...SECONDARY_NAV_MAIN);
	return items;
}

type DashboardAppSidebarProps = Omit<
	React.ComponentProps<typeof Sidebar>,
	"variant"
> & {
	area: DashboardSidebarArea;
	user: SidebarNavUserData;
	csrfToken: string;
	/** Optional override for the underlying shadcn Sidebar visual variant. */
	sidebarVariant?: React.ComponentProps<typeof Sidebar>["variant"];
};

export function DashboardAppSidebar({
	area,
	csrfToken,
	sidebarVariant,
	user,
	...props
}: DashboardAppSidebarProps) {
	const pathname = usePathname() ?? "";
	const navMain = buildNavMain(pathname, user.isInternalAdmin);
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
				<NavDocuments items={DOCUMENTS} />
				<NavSecondary items={NAV_SECONDARY} className="mt-auto" />
			</SidebarContent>
			<SidebarFooter>
				<SidebarNavUser csrfToken={csrfToken} user={user} variant={area} />
			</SidebarFooter>
		</Sidebar>
	);
}

export const DASHBOARD_NAV_CLOUDS = NAV_CLOUDS;
