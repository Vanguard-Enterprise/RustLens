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

type Props = {
	open: boolean;
	onOpenChange: (open: boolean) => void;
	userId: string;
	displayName: string;
	onConfirm: (userId: string, reason: string, typed: string) => Promise<void>;
};

export function DeleteDialog({ open, onOpenChange, userId, displayName, onConfirm }: Props) {
	const [reason, setReason] = React.useState("");
	const [typed, setTyped] = React.useState("");
	const [submitting, setSubmitting] = React.useState(false);
	const [error, setError] = React.useState<string | null>(null);
	const [prevOpen, setPrevOpen] = React.useState(open);

	if (prevOpen !== open) {
		setPrevOpen(open);
		if (!open) {
			setReason("");
			setTyped("");
			setError(null);
			setSubmitting(false);
		}
	}

	const submit = async () => {
		if (!reason.trim() || typed !== displayName) return;
		setSubmitting(true);
		try {
			await onConfirm(userId, reason.trim(), typed);
			onOpenChange(false);
		} catch (err) {
			setError((err as Error).message);
		} finally {
			setSubmitting(false);
		}
	};

	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent>
				<DialogHeader>
					<DialogTitle>Delete {displayName}</DialogTitle>
					<DialogDescription>
						This soft-deletes the account. Type the display name <code>{displayName}</code> to confirm.
					</DialogDescription>
				</DialogHeader>
				<div className="flex flex-col gap-3">
					<div className="flex flex-col gap-1">
						<Label htmlFor="delete-reason">Reason</Label>
						<Input
							id="delete-reason"
							placeholder="Reason (required, audited)"
							value={reason}
							onChange={(e) => setReason(e.target.value)}
						/>
					</div>
					<div className="flex flex-col gap-1">
						<Label htmlFor="delete-typed">Type the display name to confirm</Label>
						<Input
							id="delete-typed"
							placeholder={displayName}
							value={typed}
							onChange={(e) => setTyped(e.target.value)}
						/>
					</div>
					{error ? <div className="text-sm text-destructive">{error}</div> : null}
				</div>
				<DialogFooter>
					<Button variant="ghost" onClick={() => onOpenChange(false)} disabled={submitting}>
						Cancel
					</Button>
					<Button
						variant="destructive"
						onClick={submit}
						disabled={submitting || !reason.trim() || typed !== displayName}
					>
						Delete user
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
