import type { LucideIcon } from "lucide-react";
import {
	ActivityIcon,
	BookOpenIcon,
	ChartNoAxesColumnIcon,
	GaugeIcon,
	KeyRoundIcon,
	Link2Icon,
	NetworkIcon,
	ScanEyeIcon,
	SettingsIcon,
	ServerIcon,
	ShieldIcon,
	UsersIcon,
	UserRoundIcon,
} from "lucide-react";

export type AppShellVariant = "dashboard" | "admin";

export type AdminNavItem = {
	href: string;
	label: string;
	icon: LucideIcon;
	permissions: string[];
};

export const ADMIN_NAV: AdminNavItem[] = [
	{ href: "/admin", label: "Overview", icon: ShieldIcon, permissions: [] },
	{ href: "/admin/users", label: "Users", icon: UsersIcon, permissions: ["users.read"] },
	{ href: "/admin/links", label: "Link Review", icon: ScanEyeIcon, permissions: ["links.review"] },
	{ href: "/admin/guilds", label: "Guilds", icon: UserRoundIcon, permissions: ["guilds.untrack_server"] },
	{ href: "/admin/user-installs", label: "User Apps", icon: UsersIcon, permissions: ["users.read"] },
	{ href: "/admin/proxies", label: "Proxies", icon: NetworkIcon, permissions: ["proxies.edit"] },
	{ href: "/admin/audit", label: "Audit", icon: ActivityIcon, permissions: ["audit.read"] },
	{ href: "/admin/roles", label: "Roles", icon: KeyRoundIcon, permissions: ["admins.grant"] },
];

export function filterNavByPermissions(
	nav: AdminNavItem[],
	perms: string[],
): AdminNavItem[] {
	const set = new Set(perms);
	return nav.filter(
		(item) => item.permissions.length === 0 || item.permissions.some((p) => set.has(p)),
	);
}

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

export function getNavGroups(
	variant: AppShellVariant,
	pathname: string,
	permissions: string[] = [],
): SidebarNavGroup[] {
	if (variant === "admin") {
		const filtered = filterNavByPermissions(ADMIN_NAV, permissions);
		const accessHrefs = new Set(["/admin/users", "/admin/roles", "/admin/audit"]);
		const toItem = (item: AdminNavItem) => ({
			title: item.label,
			path: item.href,
			icon: item.icon,
		});
		return [
			{
				label: "Operations",
				items: withActive(
					pathname,
					filtered.filter((item) => !accessHrefs.has(item.href)).map(toItem),
				),
			},
			{
				label: "Access",
				items: withActive(
					pathname,
					filtered.filter((item) => accessHrefs.has(item.href)).map(toItem),
				),
			},
			{
				label: "Admin",
				items: withActive(pathname, [
					{ title: "Servers", path: "/admin/servers", icon: ServerIcon },
					{ title: "Settings", path: "/admin/settings", icon: SettingsIcon },
				]),
			},
		].filter((group) => group.items.length > 0);
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
