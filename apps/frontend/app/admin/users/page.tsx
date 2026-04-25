import {
	getAdminRoles,
	getAdminUsers,
	type AdminUsersQuery,
} from "@/lib/rustlens-api";
import { getCsrfToken } from "@/lib/csrf";
import { requirePermission } from "@/lib/session";
import { UsersFilterBar } from "./_components/users-filter-bar";
import { UsersPagination } from "./_components/users-pagination";
import { UsersTableView } from "./_components/users-table-view";

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
		(first(sp.status) as AdminUsersQuery["status"]) ?? undefined;
	const discord =
		(first(sp.discord) as AdminUsersQuery["discord"]) ?? undefined;
	const steam = (first(sp.steam) as AdminUsersQuery["steam"]) ?? undefined;
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
		sort: first(sp.sort),
		page,
		pageSize,
	};
}

function queryToString(query: AdminUsersQuery): string {
	const sp = new URLSearchParams();
	if (query.q) sp.set("q", query.q);
	for (const r of query.role ?? []) sp.append("role", r);
	if (query.status) sp.set("status", query.status);
	if (query.discord) sp.set("discord", query.discord);
	if (query.steam) sp.set("steam", query.steam);
	if (query.sort) sp.set("sort", query.sort);
	return sp.toString();
}

export default async function AdminUsersPage({
	searchParams,
}: {
	searchParams: Promise<SP>;
}) {
	await requirePermission("users.read");
	const sp = await searchParams;
	const query = parseQuery(sp);
	const [page, roles, csrfToken] = await Promise.all([
		getAdminUsers(query),
		getAdminRoles(),
		getCsrfToken(),
	]);
	const roleOptions = roles.map((r) => ({ value: r.key, label: r.name }));

	return (
		<div>
			<UsersFilterBar roleOptions={roleOptions} />
			<UsersTableView
				items={page.items}
				roleOptions={roleOptions}
				csrfToken={csrfToken}
				currentQuery={queryToString(query)}
			/>
			<UsersPagination
				total={page.total}
				page={page.page}
				pageSize={page.pageSize}
			/>
		</div>
	);
}
