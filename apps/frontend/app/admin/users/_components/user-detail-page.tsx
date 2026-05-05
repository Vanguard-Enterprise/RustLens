"use client";

import type { AdminRoleItem, AdminUserAuditItem, AdminUserDetail } from "@/lib/rustlens-api";

import { AuditSection } from "./audit-section";
import { IdentityHeader } from "./identity-header";
import { ProfileSection } from "./profile-section";
import { RolesSection } from "./roles-section";

type Props = {
	detail: AdminUserDetail;
	roles: AdminRoleItem[];
	audit: AdminUserAuditItem[];
	actorUserId: string;
	permissions: string[];
};

export function UserDetailPage({ detail, roles, audit, actorUserId, permissions }: Props) {
	const canAssign = permissions.includes("users.roles.assign");
	const canGrantSuperadmin = permissions.includes("admins.grant");
	const canEdit = permissions.includes("users.edit");
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
			<ProfileSection detail={detail} canEdit={canEdit} />
			<AuditSection userId={detail.user.id} items={audit} />
			{/* Danger zone in next task */}
		</div>
	);
}
