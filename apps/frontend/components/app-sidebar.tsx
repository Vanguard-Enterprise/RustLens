"use client";

import { Button } from "@/components/ui/button";
import {
	Sidebar,
	SidebarContent,
	SidebarFooter,
	SidebarGroup,
	SidebarHeader,
	SidebarMenu,
	SidebarMenuButton,
	SidebarMenuItem,
} from "@/components/ui/sidebar";
import { NavGroup } from "@/components/nav-group";
import {
	type AppShellVariant,
	getFooterNavLinks,
	getNavGroups,
} from "@/components/app-shared";
import { LatestChange } from "@/components/latest-change";
import Image from "next/image";
import Link from "next/link";
import { Link2Icon, SearchIcon } from "lucide-react";

type AppSidebarProps = {
	pathname: string;
	variant: AppShellVariant;
};

export function AppSidebar({ pathname, variant }: AppSidebarProps) {
	const navGroups = getNavGroups(variant, pathname);
	const footerNavLinks = getFooterNavLinks(variant, pathname);

	return (
		<Sidebar collapsible="icon" variant="floating">
			<SidebarHeader className="h-14 justify-center">
				<SidebarMenuButton asChild>
					<Link href={variant === "admin" ? "/admin" : "/dashboard"}>
						<Image
							alt="RustLens"
							className="h-8 w-auto"
							height={32}
							priority
							src="/RustLens.png"
							width={144}
						/>
						<span className="font-medium">RustLens</span>
					</Link>
				</SidebarMenuButton>
			</SidebarHeader>
			<SidebarContent>
				<SidebarGroup>
					<SidebarMenuItem className="flex items-center gap-2">
						<SidebarMenuButton
							className="min-w-8 bg-primary text-primary-foreground duration-200 ease-linear hover:bg-primary/90 hover:text-primary-foreground active:bg-primary/90 active:text-primary-foreground"
							asChild
							tooltip="Open connections"
						>
							<Link
								href={variant === "admin" ? "/admin/users" : "/dashboard/connections"}
							>
								<Link2Icon />
								<span>{variant === "admin" ? "View users" : "Manage links"}</span>
							</Link>
						</SidebarMenuButton>
						<Button
							aria-label="Search workspace"
							className="size-8 group-data-[collapsible=icon]:opacity-0"
							size="icon"
							variant="outline"
						>
							<SearchIcon />
							<span className="sr-only">Search workspace</span>
						</Button>
					</SidebarMenuItem>
				</SidebarGroup>
				{navGroups.map((group, index) => (
					<NavGroup key={`sidebar-group-${index}`} {...group} />
				))}
			</SidebarContent>
			<SidebarFooter>
				<LatestChange />
				<SidebarMenu className="mt-2">
					{footerNavLinks.map((item) => (
						<SidebarMenuItem key={item.title}>
							<SidebarMenuButton
								asChild
								className="text-muted-foreground"
								isActive={item.isActive}
								size="sm"
							>
								<Link href={item.path}>
									<item.icon />
									<span>{item.title}</span>
								</Link>
							</SidebarMenuButton>
						</SidebarMenuItem>
					))}
				</SidebarMenu>
			</SidebarFooter>
		</Sidebar>
	);
}
