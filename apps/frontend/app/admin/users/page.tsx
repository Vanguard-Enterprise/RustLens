import Link from "next/link";
import {
	Table,
	TableBody,
	TableCell,
	TableHead,
	TableHeader,
	TableRow,
} from "@/components/ui/table";
import { Badge } from "@/components/ui/badge";
import {
	getAdminRoles,
	getAdminUsers,
	type AdminUsersQuery,
} from "@/lib/rustlens-api";
import { getCsrfToken } from "@/lib/csrf";
import { requirePermission, requireAdminSession } from "@/lib/session";
import { UsersFilterBar } from "./_components/users-filter-bar";
import { UsersPagination } from "./_components/users-pagination";

type SP = Record<string, string | string[] | undefined>;

function first(v: string | string[] | undefined): string | undefined {
	return Array.isArray(v) ? v[0] : v;
}

function parseQuery(sp: SP): AdminUsersQuery {
	const page = Number(first(sp.page) ?? "1") || 1;
	const pageSize = Math.min(
		Math.max(Number(first(sp.pageSize) ?? "50") || 50, 1),
		200,
	);
	const status =
		(first(sp.status) as AdminUsersQuery["status"]) ?? "active";
	const discord =
		(first(sp.discord) as AdminUsersQuery["discord"]) ?? undefined;
	const steam = (first(sp.steam) as AdminUsersQuery["steam"]) ?? undefined;
	const emailVerified =
		(first(sp.emailVerified) as AdminUsersQuery["emailVerified"]) ?? undefined;
	const role = sp.role
		? Array.isArray(sp.role)
			? sp.role
			: [sp.role]
		: undefined;
	return {
		q: first(sp.q),
		role,
		status,
		discord,
		steam,
		emailVerified,
		sort: first(sp.sort),
		page,
		pageSize,
	};
}

export default async function AdminUsersPage({
	searchParams,
}: {
	searchParams: Promise<SP>;
}) {
	await requirePermission("users.read");
	const sp = await searchParams;
	const query = parseQuery(sp);
	const [page, roles, session, csrfToken] = await Promise.all([
		getAdminUsers(query),
		getAdminRoles(),
		requireAdminSession(),
		getCsrfToken(),
	]);
	const roleOptions = roles.map((r) => ({ value: r.key, label: r.name }));
	const canInvite = (session.permissions ?? []).includes("users.create");

	return (
		<div>
			<UsersFilterBar
				roleOptions={roleOptions}
				csrfToken={csrfToken}
				canInvite={canInvite}
			/>
			<Table>
				<TableHeader>
					<TableRow>
						<TableHead>User</TableHead>
						<TableHead>Email</TableHead>
						<TableHead>Roles</TableHead>
						<TableHead>Status</TableHead>
						<TableHead>Discord</TableHead>
						<TableHead>Steam</TableHead>
						<TableHead>Guilds</TableHead>
						<TableHead>Last login</TableHead>
					</TableRow>
				</TableHeader>
				<TableBody>
					{page.items.map((u) => (
						<TableRow key={u.user.id}>
							<TableCell>
								<Link
									href={`/admin/users/${u.user.id}`}
									className="underline-offset-2 hover:underline"
								>
									{u.user.displayName || u.user.id}
								</Link>
							</TableCell>
							<TableCell>
								{u.primaryEmail || (
									<span className="text-muted-foreground">—</span>
								)}
								{u.primaryEmail && !u.emailVerified && (
									<Badge variant="secondary" className="ml-2">
										unverified
									</Badge>
								)}
							</TableCell>
							<TableCell className="flex flex-wrap gap-1">
								{u.adminRoles.length === 0 ? (
									<span className="text-muted-foreground">—</span>
								) : (
									u.adminRoles.map((r) => <Badge key={r}>{r}</Badge>)
								)}
							</TableCell>
							<TableCell>
								{u.deletedAt ? (
									<Badge variant="destructive">deleted</Badge>
								) : u.suspendedAt ? (
									<Badge variant="destructive">suspended</Badge>
								) : (
									<Badge variant="secondary">active</Badge>
								)}
							</TableCell>
							<TableCell>
								{u.discordUsername || u.discordId || "—"}
							</TableCell>
							<TableCell>{u.steamConnectionCount}</TableCell>
							<TableCell>{u.guildCount}</TableCell>
							<TableCell>
								{u.user.lastLoginAt?.slice(0, 10) || "—"}
							</TableCell>
						</TableRow>
					))}
					{page.items.length === 0 && (
						<TableRow>
							<TableCell
								colSpan={8}
								className="text-center text-muted-foreground py-8"
							>
								No users match the current filters.
							</TableCell>
						</TableRow>
					)}
				</TableBody>
			</Table>
			<UsersPagination
				total={page.total}
				page={page.page}
				pageSize={page.pageSize}
			/>
		</div>
	);
}
