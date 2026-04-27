"use client";

import Link from "next/link";
import {
	Avatar,
	AvatarFallback,
	AvatarImage,
} from "@/components/ui/avatar";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuGroup,
	DropdownMenuItem,
	DropdownMenuLabel,
	DropdownMenuSeparator,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
	SidebarMenu,
	SidebarMenuButton,
	SidebarMenuItem,
	useSidebar,
} from "@/components/ui/sidebar";
import {
	EllipsisVerticalIcon,
	HouseIcon,
	LayoutDashboardIcon,
	LogOutIcon,
	ShieldCheckIcon,
} from "lucide-react";

export type SidebarNavUser = {
	displayName: string;
	discordUsername?: string;
	avatarUrl?: string;
	isInternalAdmin: boolean;
};

type SidebarNavUserProps = {
	user: SidebarNavUser;
	csrfToken: string;
	variant: "admin" | "dashboard";
};

function getInitials(displayName: string) {
	const parts = displayName.trim().split(/\s+/).filter(Boolean);
	if (parts.length === 0) return "?";
	const initials = parts.slice(0, 2).map((p) => p[0] ?? "").join("");
	return initials.toUpperCase() || "?";
}

export function SidebarNavUser({ csrfToken, user, variant }: SidebarNavUserProps) {
	const { isMobile } = useSidebar();
	const initials = getInitials(user.displayName);
	const secondary = user.discordUsername
		? `@${user.discordUsername}`
		: user.isInternalAdmin
			? "Internal admin"
			: "Member";
	const logoutFormId = "sidebar-nav-user-logout-form";

	return (
		<SidebarMenu>
			<SidebarMenuItem>
				<DropdownMenu>
					<DropdownMenuTrigger asChild>
						<SidebarMenuButton
							size="lg"
							className="data-[state=open]:bg-sidebar-accent data-[state=open]:text-sidebar-accent-foreground"
						>
							<Avatar className="h-8 w-8 rounded-lg">
								{user.avatarUrl ? (
									<AvatarImage src={user.avatarUrl} alt={user.displayName} />
								) : null}
								<AvatarFallback className="rounded-lg">{initials}</AvatarFallback>
							</Avatar>
							<div className="grid flex-1 text-left text-sm leading-tight">
								<span className="truncate font-medium">{user.displayName}</span>
								<span className="truncate text-xs text-muted-foreground">
									{secondary}
								</span>
							</div>
							<EllipsisVerticalIcon className="ml-auto size-4" />
						</SidebarMenuButton>
					</DropdownMenuTrigger>
					<DropdownMenuContent
						className="w-(--radix-dropdown-menu-trigger-width) min-w-56 rounded-lg"
						side={isMobile ? "bottom" : "right"}
						align="end"
						sideOffset={4}
					>
						<DropdownMenuLabel className="p-0 font-normal">
							<div className="flex items-center gap-2 px-1 py-1.5 text-left text-sm">
								<Avatar className="h-8 w-8 rounded-lg">
									{user.avatarUrl ? (
										<AvatarImage src={user.avatarUrl} alt={user.displayName} />
									) : null}
									<AvatarFallback className="rounded-lg">
										{initials}
									</AvatarFallback>
								</Avatar>
								<div className="grid flex-1 text-left text-sm leading-tight">
									<span className="truncate font-medium">
										{user.displayName}
									</span>
									<span className="truncate text-xs text-muted-foreground">
										{secondary}
									</span>
								</div>
							</div>
						</DropdownMenuLabel>
						<DropdownMenuSeparator />
						<DropdownMenuGroup>
							<DropdownMenuItem asChild>
								<Link href="/">
									<HouseIcon />
									Home
								</Link>
							</DropdownMenuItem>
							{variant === "admin" ? (
								<DropdownMenuItem asChild>
									<Link href="/dashboard">
										<LayoutDashboardIcon />
										Back to dashboard
									</Link>
								</DropdownMenuItem>
							) : user.isInternalAdmin ? (
								<DropdownMenuItem asChild>
									<Link href="/admin">
										<ShieldCheckIcon />
										Back to admin
									</Link>
								</DropdownMenuItem>
							) : null}
						</DropdownMenuGroup>
						<DropdownMenuSeparator />
						<form action="/logout" id={logoutFormId} method="post">
							<input name="csrfToken" type="hidden" value={csrfToken} />
							<DropdownMenuItem
								className="w-full cursor-pointer"
								onSelect={(event) => {
									event.preventDefault();
									const form = document.getElementById(
										logoutFormId,
									) as HTMLFormElement | null;
									form?.requestSubmit();
								}}
								variant="destructive"
							>
								<LogOutIcon />
								Log out
							</DropdownMenuItem>
						</form>
					</DropdownMenuContent>
				</DropdownMenu>
			</SidebarMenuItem>
		</SidebarMenu>
	);
}
