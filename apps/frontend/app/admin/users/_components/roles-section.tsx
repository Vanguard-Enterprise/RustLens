"use client";

import { useRouter } from "next/navigation";
import { PlusIcon, XIcon } from "lucide-react";
import * as React from "react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
	Tooltip,
	TooltipContent,
	TooltipProvider,
	TooltipTrigger,
} from "@/components/ui/tooltip";
import type { AdminRoleItem, AdminUserDetail } from "@/lib/rustlens-api";



import { grantRoleAction, revokeRoleAction } from "../actions";
import { GrantRoleDialog } from "./grant-role-dialog";
import { RevokeRoleDialog } from "./revoke-role-dialog";

type Props = {
	detail: AdminUserDetail;
	roles: AdminRoleItem[];
	canAssign: boolean;
	canGrantSuperadmin: boolean;
};

export function RolesSection({
	detail,
	roles,
	canAssign,
	canGrantSuperadmin,
}: Props) {
	const router = useRouter();
	const [grantOpen, setGrantOpen] = React.useState(false);
	const [revoking, setRevoking] = React.useState<{ key: string; name: string } | null>(null);

	if (!canAssign) return null;

	const currentRoles = detail.adminRoles;
	const assignableRoles = roles.filter((r) => !currentRoles.includes(r.key));

	const handleGrant = async (userId: string, roleKey: string, reason: string) => {
		const res = await grantRoleAction(userId, roleKey, reason);
		if (!res.ok) throw new Error(res.error);
		router.refresh();
	};

	const handleRevoke = async (userId: string, roleKey: string, reason: string) => {
		const res = await revokeRoleAction(userId, roleKey, reason);
		if (!res.ok) throw new Error(res.error);
		router.refresh();
	};

	return (
		<section className="flex flex-col gap-3 rounded-lg border bg-card p-4">
			<div>
				<h2 className="text-lg font-semibold">Roles</h2>
				<p className="text-sm text-muted-foreground">
					Admin roles control which actions this user can perform.
				</p>
			</div>

			<TooltipProvider>
				<div className="flex flex-wrap gap-2">
					{currentRoles.length === 0 ? (
						<span className="text-sm text-muted-foreground">No admin roles assigned.</span>
					) : (
						currentRoles.map((roleKey) => {
							const role = roles.find((r) => r.key === roleKey);
							const name = role?.name ?? roleKey;
							const description = role?.description ?? null;
							const isSuperadmin = roleKey === "superadmin";
							return (
								<Badge
									key={roleKey}
									variant={isSuperadmin ? "destructive" : "secondary"}
									className="gap-1"
								>
									{description ? (
										<Tooltip>
											<TooltipTrigger asChild>
												<span>{name}</span>
											</TooltipTrigger>
											<TooltipContent>{description}</TooltipContent>
										</Tooltip>
									) : (
										<span>{name}</span>
									)}
									<button
										type="button"
										onClick={() => setRevoking({ key: roleKey, name })}
										className="rounded hover:bg-foreground/10"
										aria-label={`Revoke ${name}`}
									>
										<XIcon className="size-3" />
									</button>
								</Badge>
							);
						})
					)}
				</div>
			</TooltipProvider>

			<div>
				<Button
					variant="outline"
					size="sm"
					onClick={() => setGrantOpen(true)}
					disabled={assignableRoles.length === 0}
				>
					<PlusIcon className="mr-2 size-4" />
					Add role…
				</Button>
			</div>

			<GrantRoleDialog
				open={grantOpen}
				onOpenChange={setGrantOpen}
				userId={detail.user.id}
				assignableRoles={assignableRoles}
				canGrantSuperadmin={canGrantSuperadmin}
				onConfirm={handleGrant}
			/>
			<RevokeRoleDialog
				open={revoking !== null}
				onOpenChange={(open) => {
					if (!open) setRevoking(null);
				}}
				userId={detail.user.id}
				roleKey={revoking?.key ?? ""}
				roleName={revoking?.name ?? ""}
				onConfirm={handleRevoke}
			/>
		</section>
	);
}
