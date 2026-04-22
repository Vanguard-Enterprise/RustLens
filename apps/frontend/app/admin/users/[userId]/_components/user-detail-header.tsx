import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import type { AdminUserDetail } from "@/lib/rustlens-api";

export function UserDetailHeader({ user }: { user: AdminUserDetail }) {
	const u = user.user;
	const initials = (u.displayName || u.id).slice(0, 2).toUpperCase();
	return (
		<header className="mb-4 flex items-start gap-4">
			<Avatar className="size-16">
				{u.avatarUrl ? <AvatarImage src={u.avatarUrl} alt={u.displayName} /> : null}
				<AvatarFallback>{initials}</AvatarFallback>
			</Avatar>
			<div className="flex-1">
				<h1 className="text-2xl font-semibold">{u.displayName || u.id}</h1>
				<div className="text-sm text-muted-foreground">{u.id}</div>
				{user.primaryEmail ? (
					<div className="text-sm">{user.primaryEmail}</div>
				) : null}
				<div className="mt-2 flex flex-wrap gap-1">
					{user.deletedAt ? <Badge variant="destructive">deleted</Badge> : null}
					{user.suspendedAt ? <Badge variant="destructive">suspended</Badge> : null}
					{user.adminRoles.map((r) => (
						<Badge key={r} variant={r === "superadmin" ? "destructive" : "secondary"}>
							{r}
						</Badge>
					))}
				</div>
			</div>
		</header>
	);
}
