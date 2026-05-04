"use client";

import * as React from "react";
import { useRouter } from "next/navigation";
import { PlusIcon } from "lucide-react";

import { Button } from "@/components/ui/button";
import type { AdminPermissionItem, AdminRoleItem } from "@/lib/rustlens-api";

import {
	createRoleAction,
	deleteRoleAction,
	setRolePermissionsAction,
	updateRoleAction,
} from "../actions";
import { DeleteRoleDialog } from "./delete-role-dialog";
import { RoleDrawer } from "./role-drawer";
import { RolesTable } from "./roles-table";

type Props = {
	initialRoles: AdminRoleItem[];
	permissionsCatalog: AdminPermissionItem[];
};

type DrawerState =
	| { open: false }
	| { open: true; mode: "create"; role: null }
	| { open: true; mode: "edit"; role: AdminRoleItem };

export function RolesPage({ initialRoles, permissionsCatalog }: Props) {
	const router = useRouter();
	const [drawer, setDrawer] = React.useState<DrawerState>({ open: false });
	const [deleting, setDeleting] = React.useState<AdminRoleItem | null>(null);
	const [error, setError] = React.useState<string | null>(null);

	const onSubmit = async (
		input: Parameters<React.ComponentProps<typeof RoleDrawer>["onSubmit"]>[0],
	) => {
		setError(null);
		if (input.mode === "create") {
			const res = await createRoleAction(
				{
					key: input.next.key,
					name: input.next.name,
					description: input.next.description,
					permissions: input.next.permissions,
				},
				input.reason,
			);
			if (!res.ok) throw new Error(res.error);
			router.refresh();
			return;
		}
		// Edit: post update + permissions.set sequentially. Reuse one reason.
		if (!input.role) return;
		const role = input.role;
		const nameChanged = role.name !== input.next.name;
		const descChanged = (role.description ?? "") !== input.next.description;
		const oldPerms = [...role.permissions].sort().join(",");
		const newPerms = [...input.next.permissions].sort().join(",");
		const permsChanged = oldPerms !== newPerms;

		if (nameChanged || descChanged) {
			const r = await updateRoleAction(
				role.id,
				{ name: input.next.name, description: input.next.description },
				input.reason,
			);
			if (!r.ok) throw new Error(r.error);
		}
		if (permsChanged) {
			const r = await setRolePermissionsAction(role.id, input.next.permissions, input.reason);
			if (!r.ok) throw new Error(r.error);
		}
		router.refresh();
	};

	const onDeleteConfirm = async (reason: string) => {
		if (!deleting) return;
		const res = await deleteRoleAction(deleting.id, reason);
		if (!res.ok) {
			setError(res.error);
			return;
		}
		router.refresh();
	};

	return (
		<div className="flex flex-col gap-4">
			<div className="flex items-center justify-between">
				<div />
				<Button onClick={() => setDrawer({ open: true, mode: "create", role: null })}>
					<PlusIcon className="mr-2 size-4" />
					New role
				</Button>
			</div>

			{error ? <p className="text-sm text-destructive">{error}</p> : null}

			<RolesTable
				roles={initialRoles}
				onEdit={(role) => setDrawer({ open: true, mode: "edit", role })}
				onDelete={(role) => setDeleting(role)}
			/>

			<RoleDrawer
				open={drawer.open}
				mode={drawer.open ? drawer.mode : "create"}
				role={drawer.open && drawer.mode === "edit" ? drawer.role : null}
				catalog={permissionsCatalog}
				onOpenChange={(open) => setDrawer(open ? drawer : { open: false })}
				onSubmit={onSubmit}
			/>

			<DeleteRoleDialog
				role={deleting}
				open={deleting !== null}
				onOpenChange={(open) => {
					if (!open) setDeleting(null);
				}}
				onConfirm={onDeleteConfirm}
			/>
		</div>
	);
}
