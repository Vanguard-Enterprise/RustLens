"use client";

import { useRouter } from "next/navigation";
import * as React from "react";

import { Button } from "@/components/ui/button";
import type { AdminRoleItem, AdminUserDetail } from "@/lib/rustlens-api";

import { deleteUserAction, suspendUserAction, unsuspendUserAction } from "../actions";
import { DeleteDialog } from "./delete-dialog";
import { RolesSection } from "./roles-section";
import { SuspendDialog } from "./suspend-dialog";

type Props = {
	detail: AdminUserDetail;
	roles: AdminRoleItem[];
	canAssign: boolean;
	canGrantSuperadmin: boolean;
	canSuspend: boolean;
	canDelete: boolean;
};

type DialogKey = "suspend" | "unsuspend" | "delete" | null;

export function UserActionsSection({
	detail,
	roles,
	canAssign,
	canGrantSuperadmin,
	canSuspend,
	canDelete,
}: Props) {
	const router = useRouter();
	const [dialog, setDialog] = React.useState<DialogKey>(null);
	const close = () => setDialog(null);

	const isSuspended = !!detail.suspendedAt;
	const showAccountActions = canSuspend || canDelete;

	return (
		<div className="flex flex-col gap-4">
			<RolesSection
				detail={detail}
				roles={roles}
				canAssign={canAssign}
				canGrantSuperadmin={canGrantSuperadmin}
			/>

			{showAccountActions ? (
				<section className="flex flex-col gap-3 rounded-lg border bg-card p-4">
					<div>
						<h2 className="text-lg font-semibold">Account status</h2>
						<p className="text-sm text-muted-foreground">
							Suspend, unsuspend, or permanently delete this user.
						</p>
					</div>
					<div className="flex flex-wrap gap-2">
						{canSuspend && !isSuspended ? (
							<Button
								variant="destructive"
								size="sm"
								onClick={() => setDialog("suspend")}
							>
								Suspend user
							</Button>
						) : null}
						{canSuspend && isSuspended ? (
							<Button
								variant="outline"
								size="sm"
								onClick={() => setDialog("unsuspend")}
							>
								Unsuspend user
							</Button>
						) : null}
						{canDelete ? (
							<Button
								variant="destructive"
								size="sm"
								onClick={() => setDialog("delete")}
							>
								Delete user (permanent)
							</Button>
						) : null}
					</div>

					<SuspendDialog
						open={dialog === "suspend" || dialog === "unsuspend"}
						onOpenChange={(o) => !o && close()}
						mode={dialog === "unsuspend" ? "unsuspend" : "suspend"}
						userId={detail.user.id}
						displayName={detail.user.displayName}
						onConfirm={async (userId, reason) => {
							const action = dialog === "unsuspend" ? unsuspendUserAction : suspendUserAction;
							const res = await action(userId, reason);
							if (!res.ok) throw new Error(res.error);
							router.refresh();
						}}
					/>
					<DeleteDialog
						open={dialog === "delete"}
						onOpenChange={(o) => !o && close()}
						userId={detail.user.id}
						displayName={detail.user.displayName}
						onConfirm={async (userId, reason, typed) => {
							const res = await deleteUserAction(userId, reason, typed);
							if (!res.ok) throw new Error(res.error);
							router.push("/admin/users");
						}}
					/>
				</section>
			) : null}
		</div>
	);
}
