import type { LucideIcon } from "lucide-react";
import {
	ActivityIcon,
	BookOpenIcon,
	ChartNoAxesColumnIcon,
	GaugeIcon,
	Link2Icon,
	SettingsIcon,
	ServerIcon,
	ShieldIcon,
	UsersIcon,
	UserRoundIcon,
} from "lucide-react";

export type AppShellVariant = "dashboard" | "admin";

export type SidebarNavItem = {
	title: string;
	path: string;
	icon: LucideIcon;
	isActive?: boolean;
};

export type SidebarNavGroup = {
	label: string;
	items: SidebarNavItem[];
};

function withActive(pathname: string, items: Omit<SidebarNavItem, "isActive">[]) {
	return items.map((item) => ({
		...item,
		isActive: pathname === item.path,
	}));
}

export function getNavGroups(variant: AppShellVariant, pathname: string): SidebarNavGroup[] {
	if (variant === "admin") {
		return [
			{
				label: "Operations",
				items: withActive(pathname, [
					{ title: "Overview", path: "/admin", icon: ShieldIcon },
					{ title: "Users", path: "/admin/users", icon: UsersIcon },
					{ title: "Guilds", path: "/admin/guilds", icon: UserRoundIcon },
					{ title: "Servers", path: "/admin/servers", icon: ServerIcon },
					{ title: "Audit", path: "/admin/audit", icon: ActivityIcon },
				]),
			},
			{
				label: "Admin",
				items: withActive(pathname, [
					{ title: "Settings", path: "/admin/settings", icon: SettingsIcon },
				]),
			},
		];
	}

	return [
		{
			label: "Workspace",
			items: withActive(pathname, [
				{ title: "Overview", path: "/dashboard", icon: GaugeIcon },
				{ title: "Connections", path: "/dashboard/connections", icon: Link2Icon },
				{ title: "Guilds", path: "/dashboard/guilds", icon: UsersIcon },
			]),
		},
		{
			label: "Account",
			items: withActive(pathname, [
				{ title: "Profile", path: "/dashboard/profile", icon: UserRoundIcon },
				{ title: "Settings", path: "/dashboard/settings", icon: SettingsIcon },
			]),
		},
	];
}

export function getFooterNavLinks(variant: AppShellVariant, pathname: string) {
	return withActive(pathname, [
		{
			title: variant === "admin" ? "Dashboard" : "Docs",
			path: variant === "admin" ? "/dashboard" : "/",
			icon: variant === "admin" ? GaugeIcon : BookOpenIcon,
		},
		{
			title: "System status",
			path: variant === "admin" ? "/admin/servers" : "/dashboard/guilds",
			icon: ChartNoAxesColumnIcon,
		},
	]);
}

export function getCurrentNavItem(variant: AppShellVariant, pathname: string) {
	const items = [
		...getNavGroups(variant, pathname).flatMap((group) => group.items),
		...getFooterNavLinks(variant, pathname),
	];
	return items.find((item) => item.path === pathname) ?? items[0] ?? null;
}
