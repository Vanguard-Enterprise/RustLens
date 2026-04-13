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
	BookOpenIcon,
	LogOutIcon,
	ShieldCheckIcon,
	UserIcon,
} from "lucide-react";

type NavUserProps = {
	user: {
		displayName: string;
		email?: string;
		avatarUrl?: string;
		isInternalAdmin: boolean;
	};
	csrfToken: string;
};

export function NavUser({ csrfToken, user }: NavUserProps) {
	const fallback = user.displayName.charAt(0).toUpperCase();
	const logoutFormId = "nav-user-logout-form";

	return (
		<DropdownMenu>
			<DropdownMenuTrigger asChild>
				<Avatar className="size-8">
					<AvatarImage src={user.avatarUrl} />
					<AvatarFallback>{fallback}</AvatarFallback>
				</Avatar>
			</DropdownMenuTrigger>
			<DropdownMenuContent align="end" className="w-60">
				<DropdownMenuItem className="flex items-center justify-start gap-2">
					<DropdownMenuLabel className="flex items-center gap-3">
						<Avatar className="size-10">
							<AvatarImage src={user.avatarUrl} />
							<AvatarFallback>{fallback}</AvatarFallback>
						</Avatar>
						<div>
							<span className="font-medium text-foreground">
								{user.displayName}
							</span>{" "}
							<br />
							<div className="truncate text-muted-foreground text-xs">
								{user.email}
							</div>
							<div className="mt-0.5 text-[10px] text-muted-foreground">
								{user.isInternalAdmin ? "Internal admin" : "Product user"}
							</div>
						</div>
					</DropdownMenuLabel>
				</DropdownMenuItem>
				<DropdownMenuSeparator />
				<DropdownMenuGroup>
					<DropdownMenuItem asChild>
						<Link href="/dashboard/profile">
							<UserIcon />
							Profile
						</Link>
					</DropdownMenuItem>
				</DropdownMenuGroup>
				<DropdownMenuSeparator />
				<DropdownMenuGroup>
					<DropdownMenuItem asChild>
						<Link href="/">
							<BookOpenIcon />
							Marketing site
						</Link>
					</DropdownMenuItem>
					{user.isInternalAdmin ? (
						<DropdownMenuItem asChild>
							<Link href="/admin">
								<ShieldCheckIcon />
								Admin panel
							</Link>
						</DropdownMenuItem>
					) : null}
				</DropdownMenuGroup>
				<DropdownMenuSeparator />
				<DropdownMenuGroup>
					<form action="/logout" id={logoutFormId} method="post">
						<input name="csrfToken" type="hidden" value={csrfToken} />
						<DropdownMenuItem
							className="w-full cursor-pointer"
							onSelect={(event) => {
								event.preventDefault();
								const form = document.getElementById(logoutFormId) as HTMLFormElement | null;
								form?.requestSubmit();
							}}
							variant="destructive"
						>
							<LogOutIcon />
							Log out
						</DropdownMenuItem>
					</form>
				</DropdownMenuGroup>
			</DropdownMenuContent>
		</DropdownMenu>
	);
}
