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

import { deleteUserAction, suspendUserAction, unsuspendUserAction } from "../actions";
import { DeleteDialog } from "./delete-dialog";
import { SuspendDialog } from "./suspend-dialog";

type Props = {
	detail: AdminUserDetail;
	canSuspend: boolean;
	canDelete: boolean;
};

type DialogKey = "suspend" | "unsuspend" | "delete" | null;

export function DangerZone({ detail, canSuspend, canDelete }: Props) {
	const router = useRouter();
	const [dialog, setDialog] = React.useState<DialogKey>(null);
	const close = () => setDialog(null);

	const isSuspended = !!detail.suspendedAt;

	if (!canSuspend && !canDelete) return null;

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
					{canDelete ? (
						<Button variant="destructive" size="sm" onClick={() => setDialog("delete")}>
							Delete user (permanent)
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
					// On delete the user is gone; navigate back to the list.
					router.push("/admin/users");
				}}
			/>
		</section>
	);
}
