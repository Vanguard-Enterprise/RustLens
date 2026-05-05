"use client";

import type { AdminRoleItem, AdminUserAuditItem, AdminUserDetail } from "@/lib/rustlens-api";

import { IdentityHeader } from "./identity-header";
import { RolesSection } from "./roles-section";

type Props = {
	detail: AdminUserDetail;
	roles: AdminRoleItem[];
	audit: AdminUserAuditItem[];
	actorUserId: string;
	permissions: string[];
};

export function UserDetailPage({ detail, roles, actorUserId, permissions }: Props) {
	const canAssign = permissions.includes("users.roles.assign");
	const canGrantSuperadmin = permissions.includes("admins.grant");
	return (
		<div className="flex flex-col gap-6">
			<IdentityHeader detail={detail} />
			<RolesSection
				detail={detail}
				roles={roles}
				actorUserId={actorUserId}
				canAssign={canAssign}
				canGrantSuperadmin={canGrantSuperadmin}
			/>
			{/* Profile, audit, danger zone added in later tasks */}
		</div>
	);
}
