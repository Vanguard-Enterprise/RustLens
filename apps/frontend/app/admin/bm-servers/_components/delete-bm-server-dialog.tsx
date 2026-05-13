"use client";

import * as React from "react";

import { Button } from "@/components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import type { AdminBmServerItem } from "@/lib/rustlens-api";

import { deleteBmServerAction } from "../actions";

type Props = {
	open: boolean;
	onOpenChange: (open: boolean) => void;
	server: AdminBmServerItem;
	onSuccess: () => void;
};

export function DeleteBmServerDialog({
	open,
	onOpenChange,
	server,
	onSuccess,
}: Props) {
	const [reason, setReason] = React.useState("");
	const [submitting, setSubmitting] = React.useState(false);
	const [error, setError] = React.useState<string | null>(null);
	const [prevOpen, setPrevOpen] = React.useState(open);

	if (prevOpen !== open) {
		setPrevOpen(open);
		if (!open) {
			setReason("");
			setError(null);
			setSubmitting(false);
		}
	}

	const submit = async () => {
		setSubmitting(true);
		setError(null);
		const res = await deleteBmServerAction(server.bmId, reason);
		setSubmitting(false);
		if (!res.ok) {
			setError(res.error);
			return;
		}
		onOpenChange(false);
		onSuccess();
	};

	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent>
				<DialogHeader>
					<DialogTitle>Delete tracked server</DialogTitle>
					<DialogDescription>
						This permanently removes the BattleMetrics server target. Per-guild
						selections, player presence state, and stored player-count history
						for this server will cascade-delete.
					</DialogDescription>
				</DialogHeader>
				<div className="flex flex-col gap-2 text-sm">
					<div>
						<span className="text-muted-foreground">Name: </span>
						<span className="font-medium">{server.name}</span>
					</div>
					<div>
						<span className="text-muted-foreground">BM ID: </span>
						<span className="font-mono">{server.bmId}</span>
					</div>
					<div className="flex flex-col gap-1">
						<Label htmlFor="bm-server-delete-reason">Reason (optional)</Label>
						<Input
							id="bm-server-delete-reason"
							placeholder="Why is this being deleted?"
							value={reason}
							onChange={(e) => setReason(e.target.value)}
						/>
					</div>
				</div>
				{error ? (
					<div className="text-sm text-destructive">{error}</div>
				) : null}
				<DialogFooter>
					<Button
						variant="ghost"
						onClick={() => onOpenChange(false)}
						disabled={submitting}
					>
						Cancel
					</Button>
					<Button
						variant="destructive"
						onClick={submit}
						disabled={submitting}
					>
						{submitting ? "Deleting…" : "Delete"}
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
