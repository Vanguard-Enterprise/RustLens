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
import type { BulkBmServerAction } from "@/lib/rustlens-api";

import { bulkBmServerAction } from "../actions";

type Props = {
	open: boolean;
	onOpenChange: (open: boolean) => void;
	mode: BulkBmServerAction;
	bmIds: string[];
	onSuccess: () => void;
};

const TITLES: Record<BulkBmServerAction, string> = {
	set_high_freq: "Set high-frequency polling",
	set_standard: "Set standard polling",
	delete: "Delete tracked servers",
};

const DESCRIPTIONS: Record<BulkBmServerAction, string> = {
	set_high_freq:
		"Mark all selected servers as high-frequency. They will be polled 3× more often than standard servers on the next rolling tick.",
	set_standard:
		"Demote all selected servers back to the standard polling tier on the next rolling tick.",
	delete:
		"All selected tracked servers will be permanently removed. Per-guild selections, player presence state, and stored player-count history for these servers will cascade-delete. This cannot be undone.",
};

export function BulkBmServerDialog({
	open,
	onOpenChange,
	mode,
	bmIds,
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
		const res = await bulkBmServerAction(mode, bmIds, reason);
		setSubmitting(false);
		if (!res.ok) {
			setError(res.error);
			return;
		}
		if (res.result.failed > 0) {
			setError(
				`${res.result.failed} of ${bmIds.length} failed; ${res.result.succeeded} applied.`,
			);
			onSuccess();
			return;
		}
		onOpenChange(false);
		onSuccess();
	};

	const isDestructive = mode === "delete";

	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent>
				<DialogHeader>
					<DialogTitle>
						{TITLES[mode]} ({bmIds.length})
					</DialogTitle>
					<DialogDescription>{DESCRIPTIONS[mode]}</DialogDescription>
				</DialogHeader>
				<div className="flex flex-col gap-1">
					<Label htmlFor="bm-server-bulk-reason">Reason (optional)</Label>
					<Input
						id="bm-server-bulk-reason"
						placeholder="Audited"
						value={reason}
						onChange={(e) => setReason(e.target.value)}
					/>
				</div>
				{error ? <div className="text-sm text-destructive">{error}</div> : null}
				<DialogFooter>
					<Button
						variant="ghost"
						onClick={() => onOpenChange(false)}
						disabled={submitting}
					>
						Cancel
					</Button>
					<Button
						variant={isDestructive ? "destructive" : "default"}
						onClick={submit}
						disabled={submitting}
					>
						{submitting ? "Working…" : TITLES[mode]}
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
