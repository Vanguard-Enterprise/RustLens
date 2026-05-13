"use client";

import type { AdminRoleItem, AdminUserAuditItem, AdminUserDetail } from "@/lib/rustlens-api";

import { AuditSection } from "./audit-section";
import { DangerZone } from "./danger-zone";
import { IdentityHeader } from "./identity-header";
import { RolesSection } from "./roles-section";

type Props = {
	detail: AdminUserDetail;
	roles: AdminRoleItem[];
	audit: AdminUserAuditItem[];
	permissions: string[];
};

export function UserDetailPage({ detail, roles, audit, permissions }: Props) {
	const canAssign = permissions.includes("users.roles.assign");
	const canGrantSuperadmin = permissions.includes("admins.grant");
	const canSuspend = permissions.includes("users.suspend");
	const canDelete = permissions.includes("users.delete");
	return (
		<div className="flex flex-col gap-6">
			<IdentityHeader detail={detail} />
			<RolesSection
				detail={detail}
				roles={roles}
				canAssign={canAssign}
				canGrantSuperadmin={canGrantSuperadmin}
			/>
			<AuditSection userId={detail.user.id} items={audit} />
			<DangerZone
				detail={detail}
				canSuspend={canSuspend}
				canDelete={canDelete}
			/>
		</div>
	);
}
