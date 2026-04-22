import type { ReactNode } from "react";
import { getAdminUser } from "@/lib/rustlens-api";
import { requirePermission } from "@/lib/session";
import { UserDetailHeader } from "./_components/user-detail-header";
import { TabLink } from "./_components/tab-link";

export default async function UserDetailLayout({
	children,
	params,
}: {
	children: ReactNode;
	params: Promise<{ userId: string }>;
}) {
	await requirePermission("users.read");
	const { userId } = await params;
	const user = await getAdminUser(userId);

	const base = `/admin/users/${userId}`;
	return (
		<div>
			<UserDetailHeader user={user} />
			<nav className="mb-4 flex gap-4 border-b text-sm">
				<TabLink href={base} exact>
					Overview
				</TabLink>
				<TabLink href={`${base}/roles`}>Roles</TabLink>
				<TabLink href={`${base}/emails`}>Emails</TabLink>
				<TabLink href={`${base}/connections`}>Connections</TabLink>
				<TabLink href={`${base}/audit`}>Audit</TabLink>
			</nav>
			{children}
		</div>
	);
}
