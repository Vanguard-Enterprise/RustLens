"use client";

import type { AdminRoleItem, AdminUserAuditItem, AdminUserDetail } from "@/lib/rustlens-api";

import { IdentityHeader } from "./identity-header";

type Props = {
	detail: AdminUserDetail;
	roles: AdminRoleItem[];
	audit: AdminUserAuditItem[];
	actorUserId: string;
	permissions: string[];
};

export function UserDetailPage({ detail }: Props) {
	return (
		<div className="flex flex-col gap-6">
			<IdentityHeader detail={detail} />
			{/* Roles, profile, audit, danger zone added in later tasks */}
		</div>
	);
}
