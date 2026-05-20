"use client";

import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import type {
	AdminCommandUsageFilterOptions,
	AdminCommandUsagePage,
	AdminRoleItem,
	AdminUserAuditItem,
	AdminUserDetail,
} from "@/lib/rustlens-api";

import { AuditSection } from "./audit-section";
import { IdentityHeader } from "./identity-header";
import { ServersSection } from "./servers-section";
import { UsageSection } from "./usage-section";
import { UserActionsSection } from "./user-actions-section";

type Props = {
	detail: AdminUserDetail;
	roles: AdminRoleItem[];
	audit: AdminUserAuditItem[];
	commandUsage: AdminCommandUsagePage;
	commandUsageFilterOptions: AdminCommandUsageFilterOptions;
	permissions: string[];
};

export function UserDetailPage({ detail, roles, audit, commandUsage, commandUsageFilterOptions, permissions }: Props) {
	const canAssign = permissions.includes("users.roles.assign");
	const canGrantSuperadmin = permissions.includes("admins.grant");
	const canSuspend = permissions.includes("users.suspend");
	const canDelete = permissions.includes("users.delete");
	return (
		<div className="flex flex-col gap-6">
			<IdentityHeader detail={detail} />
			<Tabs defaultValue="servers">
				<TabsList>
					<TabsTrigger value="servers">Servers</TabsTrigger>
					<TabsTrigger value="audit">Audit</TabsTrigger>
					<TabsTrigger value="usage">Usage</TabsTrigger>
					<TabsTrigger value="actions">User Actions</TabsTrigger>
				</TabsList>
				<TabsContent value="servers">
					<ServersSection guilds={detail.guilds} />
				</TabsContent>
				<TabsContent value="audit">
					<AuditSection userId={detail.user.id} items={audit} />
				</TabsContent>
				<TabsContent value="usage">
					<UsageSection data={commandUsage} userId={detail.user.id} filterOptions={commandUsageFilterOptions} />
				</TabsContent>
				<TabsContent value="actions">
					<UserActionsSection
						detail={detail}
						roles={roles}
						canAssign={canAssign}
						canGrantSuperadmin={canGrantSuperadmin}
						canSuspend={canSuspend}
						canDelete={canDelete}
					/>
				</TabsContent>
			</Tabs>
		</div>
	);
}
