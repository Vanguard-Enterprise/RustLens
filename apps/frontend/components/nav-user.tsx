"use client";

import Link from "next/link";
import { useState } from "react";
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
import { useLogout } from "@/lib/use-logout";
import { cn } from "@/lib/utils";
import { LayoutDashboardIcon, LogOutIcon, ShieldCheckIcon } from "lucide-react";

type NavUserProps = {
	user: {
		displayName: string;
		discordUsername?: string;
		avatarUrl?: string;
		isInternalAdmin: boolean;
	};
	csrfToken: string;
};

function SimpleAvatar({
	avatarUrl,
	fallback,
	className,
}: {
	avatarUrl?: string;
	fallback: string;
	className?: string;
}) {
	const [errored, setErrored] = useState(false);
	const showImage = Boolean(avatarUrl) && !errored;
	return (
		<span
			className={cn(
				"relative flex shrink-0 items-center justify-center overflow-hidden rounded-full bg-muted text-muted-foreground text-sm select-none",
				className,
			)}
		>
			{showImage ? (
				<img
					alt=""
					className="size-full object-cover"
					onError={() => setErrored(true)}
					src={avatarUrl}
				/>
			) : (
				<span>{fallback}</span>
			)}
		</span>
	);
}

export function NavUser({ csrfToken, user }: NavUserProps) {
	const fallback = user.displayName.charAt(0).toUpperCase();
	const { logout, isLoggingOut } = useLogout(csrfToken);

	return (
		<DropdownMenu>
			<DropdownMenuTrigger asChild>
				<button
					className="flex items-center gap-2 rounded-full border border-border/50 bg-background/40 py-1 pr-1 pl-3 shadow-sm outline-none backdrop-blur-md focus-visible:ring-2 focus-visible:ring-ring"
					type="button"
				>
					<span className="font-medium text-foreground text-sm">
						{user.displayName}
					</span>
					<SimpleAvatar
						avatarUrl={user.avatarUrl}
						className="pointer-events-none size-7"
						fallback={fallback}
					/>
				</button>
			</DropdownMenuTrigger>
			<DropdownMenuContent align="end" className="w-60">
				<DropdownMenuItem className="flex items-center justify-start gap-2">
					<DropdownMenuLabel className="flex items-center gap-3">
						<Avatar className="size-10">
							{user.avatarUrl ? <AvatarImage src={user.avatarUrl} /> : null}
							<AvatarFallback>{fallback}</AvatarFallback>
						</Avatar>
						<div>
							<span className="font-medium text-foreground">
								{user.displayName}
							</span>{" "}
							<br />
							{user.discordUsername ? (
								<div className="truncate text-muted-foreground text-xs">
									@{user.discordUsername}
								</div>
							) : null}
							<div className="mt-0.5 text-[10px] text-muted-foreground">
								{user.isInternalAdmin ? "Internal admin" : "Product user"}
							</div>
						</div>
					</DropdownMenuLabel>
				</DropdownMenuItem>
				<DropdownMenuSeparator />
				<DropdownMenuGroup>
					<DropdownMenuItem asChild>
						<Link href="/dashboard">
							<LayoutDashboardIcon />
							Open Dashboard
						</Link>
					</DropdownMenuItem>
					{user.isInternalAdmin ? (
						<DropdownMenuItem asChild>
							<Link href="/admin">
								<ShieldCheckIcon />
								Open Admin
							</Link>
						</DropdownMenuItem>
					) : null}
				</DropdownMenuGroup>
				<DropdownMenuSeparator />
				<DropdownMenuGroup>
					<DropdownMenuItem
						className="w-full cursor-pointer"
						disabled={isLoggingOut}
						onSelect={(event) => {
							event.preventDefault();
							logout();
						}}
						variant="destructive"
					>
						<LogOutIcon />
						{isLoggingOut ? "Logging out…" : "Log out"}
					</DropdownMenuItem>
				</DropdownMenuGroup>
			</DropdownMenuContent>
		</DropdownMenu>
	);
}
