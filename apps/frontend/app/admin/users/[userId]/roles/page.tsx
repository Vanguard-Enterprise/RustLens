import { getCsrfToken } from "@/lib/csrf";
import { getAdminRoles, getAdminUser } from "@/lib/rustlens-api";
import { requirePermission } from "@/lib/session";
import { RoleGrantControls } from "../_components/role-grant-controls";

export default async function UserRolesPage({
	params,
}: {
	params: Promise<{ userId: string }>;
}) {
	const session = await requirePermission("users.read");
	const { userId } = await params;
	const [user, roles, csrfToken] = await Promise.all([
		getAdminUser(userId),
		getAdminRoles(),
		getCsrfToken(),
	]);

	const perms = session.permissions ?? [];
	const canAssign = perms.includes("users.roles.assign");
	const canGrantSuperadmin = perms.includes("admins.grant");

	return (
		<RoleGrantControls
			userId={user.user.id}
			currentRoles={user.adminRoles}
			allRoles={roles}
			canAssign={canAssign}
			canGrantSuperadmin={canGrantSuperadmin}
			csrfToken={csrfToken}
		/>
	);
}
