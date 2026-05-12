"use client";

import * as React from "react";
import Image from "next/image";
import Link from "next/link";
import { usePathname } from "next/navigation";

import { NavGroup } from "@/components/nav-group";
import { NavMain } from "@/components/nav-main";
import { NavSecondary } from "@/components/nav-secondary";
import { AccountSettingsDialog } from "@/components/account-settings-dialog";
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
	NetworkIcon,
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
	/** Discord OAuth URL that adds the bot to a guild with admin permissions. */
	installBotUrl?: string;
};

export function DashboardAppSidebar({
	area,
	csrfToken,
	installBotUrl,
	permissions = [],
	sidebarVariant,
	user,
	...props
}: DashboardAppSidebarProps) {
	const pathname = usePathname() ?? "";
	const navMain = buildNavMain(area);
	const homeHref = area === "admin" ? "/admin" : "/dashboard";

	const [settingsOpen, setSettingsOpen] = React.useState(false);

	const navSecondary = React.useMemo(
		() => [
			{
				title: "Settings",
				icon: <Settings2Icon />,
				onSelect: () => setSettingsOpen(true),
			},
			{ title: "Get Help", icon: <CircleHelpIcon />, url: "#" },
			{ title: "Search", icon: <SearchIcon />, url: "#" },
		],
		[],
	);

	return (
		<>
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
				<NavMain items={navMain} installBotUrl={installBotUrl} />
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
						{(() => {
							const infraItems: SidebarNavItem[] = [];
							if (permissions.includes("proxies.read")) {
								infraItems.push({
									title: "Proxies",
									path: "/admin/proxies",
									icon: NetworkIcon,
									isActive: pathname === "/admin/proxies" || pathname.startsWith("/admin/proxies/"),
								});
							}
							return infraItems.length > 0 ? (
								<NavGroup label="Infrastructure" items={infraItems} />
							) : null;
						})()}
					</>
				) : null}
				<NavSecondary items={navSecondary} className="mt-auto" />
			</SidebarContent>
			<SidebarFooter>
				<SidebarNavUser csrfToken={csrfToken} user={user} variant={area} />
			</SidebarFooter>
		</Sidebar>
		<AccountSettingsDialog open={settingsOpen} onOpenChange={setSettingsOpen} />
		</>
	);
}

export const DASHBOARD_NAV_CLOUDS = NAV_CLOUDS;
