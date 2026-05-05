"use client";

import { useRouter } from "next/navigation";
import * as React from "react";

import { Button } from "@/components/ui/button";
import {
	Collapsible,
	CollapsibleContent,
	CollapsibleTrigger,
} from "@/components/ui/collapsible";
import type { AdminUserDetail } from "@/lib/rustlens-api";

import {
	deleteUserAction,
	purgeUserAction,
	restoreUserAction,
	suspendUserAction,
	unsuspendUserAction,
} from "../actions";
import { DeleteDialog } from "./delete-dialog";
import { PurgeDialog } from "./purge-dialog";
import { RestoreDialog } from "./restore-dialog";
import { SuspendDialog } from "./suspend-dialog";

type Props = {
	detail: AdminUserDetail;
	canSuspend: boolean;
	canDelete: boolean;
	canPurge: boolean;
};

type DialogKey = "suspend" | "unsuspend" | "delete" | "restore" | "purge" | null;

export function DangerZone({ detail, canSuspend, canDelete, canPurge }: Props) {
	const router = useRouter();
	const [dialog, setDialog] = React.useState<DialogKey>(null);
	const close = () => setDialog(null);

	const isSuspended = !!detail.suspendedAt;
	const isDeleted = !!detail.deletedAt;

	if (!canSuspend && !canDelete && !canPurge) return null;

	return (
		<section className="rounded-lg border border-destructive/40 bg-card p-4">
			<Collapsible>
				<CollapsibleTrigger asChild>
					<Button variant="outline" size="sm" className="border-destructive/50 text-destructive">
						Danger zone
					</Button>
				</CollapsibleTrigger>
				<CollapsibleContent className="mt-3 flex flex-col gap-2">
					{canSuspend && !isSuspended ? (
						<Button variant="destructive" size="sm" onClick={() => setDialog("suspend")}>
							Suspend user
						</Button>
					) : null}
					{canSuspend && isSuspended ? (
						<Button variant="outline" size="sm" onClick={() => setDialog("unsuspend")}>
							Unsuspend user
						</Button>
					) : null}
					{canDelete && !isDeleted ? (
						<Button variant="destructive" size="sm" onClick={() => setDialog("delete")}>
							Delete user
						</Button>
					) : null}
					{canDelete && isDeleted ? (
						<Button variant="outline" size="sm" onClick={() => setDialog("restore")}>
							Restore user
						</Button>
					) : null}
					{canPurge ? (
						<Button variant="destructive" size="sm" onClick={() => setDialog("purge")}>
							Purge user (permanent)
						</Button>
					) : null}
				</CollapsibleContent>
			</Collapsible>

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
					router.refresh();
				}}
			/>
			<RestoreDialog
				open={dialog === "restore"}
				onOpenChange={(o) => !o && close()}
				userId={detail.user.id}
				displayName={detail.user.displayName}
				onConfirm={async (userId, reason) => {
					const res = await restoreUserAction(userId, reason);
					if (!res.ok) throw new Error(res.error);
					router.refresh();
				}}
			/>
			<PurgeDialog
				open={dialog === "purge"}
				onOpenChange={(o) => !o && close()}
				userId={detail.user.id}
				displayName={detail.user.displayName}
				onConfirm={async (userId, reason, typed) => {
					const res = await purgeUserAction(userId, reason, typed);
					if (!res.ok) throw new Error(res.error);
					// On purge the user is gone; navigate back to the list.
					router.push("/admin/users");
				}}
			/>
		</section>
	);
}
