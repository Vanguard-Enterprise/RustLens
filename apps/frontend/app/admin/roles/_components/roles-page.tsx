"use client";

import * as React from "react";
import { useRouter } from "next/navigation";
import { PlusIcon, SearchIcon } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import type { AdminPermissionItem, AdminRoleItem } from "@/lib/rustlens-api";

import {
	createRoleAction,
	deleteRoleAction,
	setRolePermissionsAction,
	updateRoleAction,
} from "../actions";
import { DeleteRoleDialog } from "./delete-role-dialog";
import { RoleDialog } from "./role-dialog";
import { RolesTable } from "./roles-table";

type Props = {
	initialRoles: AdminRoleItem[];
	permissionsCatalog: AdminPermissionItem[];
};

type DialogState =
	| { open: false }
	| { open: true; mode: "create"; role: null }
	| { open: true; mode: "edit"; role: AdminRoleItem };

export function RolesPage({ initialRoles, permissionsCatalog }: Props) {
	const router = useRouter();
	const [dialog, setDialog] = React.useState<DialogState>({ open: false });
	const [deleting, setDeleting] = React.useState<AdminRoleItem | null>(null);
	const [search, setSearch] = React.useState("");

	const filteredRoles = React.useMemo(() => {
		const q = search.trim().toLowerCase();
		if (!q) return initialRoles;
		return initialRoles.filter(
			(r) =>
				r.name.toLowerCase().includes(q) ||
				r.key.toLowerCase().includes(q) ||
				(r.description ?? "").toLowerCase().includes(q),
		);
	}, [initialRoles, search]);

	const onSubmit = async (
		input: Parameters<React.ComponentProps<typeof RoleDialog>["onSubmit"]>[0],
	) => {
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
		// Backend exposes these as separate actions, so a failure between them
		// can leave the role partially updated; on any failure we refresh so
		// the table reflects actual persisted state and prefix the error to
		// make partial-success obvious to the admin.
		if (!input.role) return;
		const role = input.role;
		const nameChanged = role.name !== input.next.name;
		const descChanged = (role.description ?? "") !== input.next.description;
		const oldPerms = [...role.permissions].sort().join(",");
		const newPerms = [...input.next.permissions].sort().join(",");
		const permsChanged = oldPerms !== newPerms;

		let metadataApplied = false;
		try {
			if (nameChanged || descChanged) {
				const r = await updateRoleAction(
					role.id,
					{ name: input.next.name, description: input.next.description },
					input.reason,
				);
				if (!r.ok) throw new Error(r.error);
				metadataApplied = true;
			}
			if (permsChanged) {
				const r = await setRolePermissionsAction(
					role.id,
					input.next.permissions,
					input.reason,
				);
				if (!r.ok) {
					throw new Error(
						metadataApplied
							? `Name/description saved, but permissions update failed: ${r.error}. Reopen the role to retry.`
							: r.error,
					);
				}
			}
			router.refresh();
		} catch (err) {
			router.refresh();
			throw err;
		}
	};

	const onDeleteConfirm = async (reason: string) => {
		if (!deleting) return;
		const res = await deleteRoleAction(deleting.id, reason);
		if (!res.ok) throw new Error(res.error);
		router.refresh();
	};

	return (
		<div className="flex flex-col gap-4">
			<div className="flex items-center justify-between gap-4">
				<div className="relative w-full max-w-sm">
					<SearchIcon className="absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
					<Input
						type="search"
						placeholder="Search by name, key, or description"
						value={search}
						onChange={(e) => setSearch(e.target.value)}
						className="pl-8"
					/>
				</div>
				<Button onClick={() => setDialog({ open: true, mode: "create", role: null })}>
					<PlusIcon className="mr-2 size-4" />
					New role
				</Button>
			</div>

			<RolesTable
				roles={filteredRoles}
				onEdit={(role) => setDialog({ open: true, mode: "edit", role })}
				onDelete={(role) => setDeleting(role)}
			/>

			<RoleDialog
				open={dialog.open}
				mode={dialog.open ? dialog.mode : "create"}
				role={dialog.open && dialog.mode === "edit" ? dialog.role : null}
				catalog={permissionsCatalog}
				onOpenChange={(open) => setDialog(open ? dialog : { open: false })}
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
