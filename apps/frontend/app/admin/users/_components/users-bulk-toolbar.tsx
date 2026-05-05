"use client";

import * as React from "react";

import { Button } from "@/components/ui/button";
import {
	type AdminRoleItem,
	type BulkUserResult,
	type BulkUserSubAction,
	buildAdminUsersExportUrl,
} from "@/lib/rustlens-api";

import { bulkUserAction } from "../actions";
import { BulkRoleDialog } from "./bulk-role-dialog";
import { BulkSuspendDialog } from "./bulk-suspend-dialog";

type Props = {
	selectedIds: string[];
	roles: AdminRoleItem[];
	permissions: string[];
	onClear: () => void;
	onComplete: () => void;
};

export function UsersBulkToolbar({ selectedIds, roles, permissions, onClear, onComplete }: Props) {
	const [dialog, setDialog] = React.useState<
		| null
		| { kind: "suspend"; mode: "suspend" | "unsuspend" }
		| { kind: "role"; mode: "grant" | "revoke" }
	>(null);

	const canSuspend = permissions.includes("users.suspend");
	const canRoles = permissions.includes("users.roles.assign");
	const canExport = permissions.includes("users.export");

	const wrapBulk = async (
		action: BulkUserSubAction,
		userIds: string[],
		params: Record<string, unknown>,
		reason: string,
	): Promise<BulkUserResult> => {
		const res = await bulkUserAction(action, userIds, params, reason);
		if (!res.ok) throw new Error(res.error);
		return res.result;
	};

	if (selectedIds.length === 0) return null;

	const exportSelectedUrl = canExport ? buildAdminUsersExportUrl({ ids: selectedIds }) : null;

	return (
		<div className="sticky top-0 z-20 flex flex-wrap items-center gap-2 rounded-md border bg-card p-2 shadow-sm">
			<span className="text-sm font-medium">{selectedIds.length} selected</span>
			{canSuspend ? (
				<>
					<Button
						size="sm"
						variant="outline"
						onClick={() => setDialog({ kind: "suspend", mode: "suspend" })}
					>
						Suspend
					</Button>
					<Button
						size="sm"
						variant="outline"
						onClick={() => setDialog({ kind: "suspend", mode: "unsuspend" })}
					>
						Unsuspend
					</Button>
				</>
			) : null}
			{canRoles ? (
				<>
					<Button
						size="sm"
						variant="outline"
						onClick={() => setDialog({ kind: "role", mode: "grant" })}
					>
						Grant role…
					</Button>
					<Button
						size="sm"
						variant="outline"
						onClick={() => setDialog({ kind: "role", mode: "revoke" })}
					>
						Revoke role…
					</Button>
				</>
			) : null}
			{exportSelectedUrl ? (
				<a href={exportSelectedUrl} download>
					<Button size="sm" variant="outline">
						Export selected
					</Button>
				</a>
			) : null}
			<div className="ml-auto">
				<Button size="sm" variant="ghost" onClick={onClear}>
					Clear
				</Button>
			</div>

			{dialog?.kind === "suspend" ? (
				<BulkSuspendDialog
					open
					onOpenChange={(o) => {
						if (!o) {
							setDialog(null);
							onComplete();
						}
					}}
					mode={dialog.mode}
					userIds={selectedIds}
					onConfirm={wrapBulk}
				/>
			) : null}
			{dialog?.kind === "role" ? (
				<BulkRoleDialog
					open
					onOpenChange={(o) => {
						if (!o) {
							setDialog(null);
							onComplete();
						}
					}}
					mode={dialog.mode}
					userIds={selectedIds}
					roles={roles}
					onConfirm={wrapBulk}
				/>
			) : null}
		</div>
	);
}
