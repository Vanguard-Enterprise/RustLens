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
	mode: "suspend" | "unsuspend";
	userId: string;
	displayName: string;
	onConfirm: (userId: string, reason: string) => Promise<void>;
};

export function SuspendDialog({ open, onOpenChange, mode, userId, displayName, onConfirm }: Props) {
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

	const reasonRequired = mode === "suspend";
	const submit = async () => {
		if (reasonRequired && !reason.trim()) return;
		setSubmitting(true);
		try {
			await onConfirm(userId, reason.trim());
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
					<DialogTitle>
						{mode === "suspend" ? `Suspend ${displayName}` : `Unsuspend ${displayName}`}
					</DialogTitle>
					<DialogDescription>
						{mode === "suspend"
							? "The user will be locked out of authenticated routes until unsuspended."
							: "The user regains access immediately."}
					</DialogDescription>
				</DialogHeader>
				<div className="flex flex-col gap-1">
					<Label htmlFor="suspend-reason">Reason{reasonRequired ? "" : " (optional)"}</Label>
					<Input
						id="suspend-reason"
						placeholder="Reason (audited)"
						value={reason}
						onChange={(e) => setReason(e.target.value)}
					/>
				</div>
				{error ? <div className="text-sm text-destructive">{error}</div> : null}
				<DialogFooter>
					<Button variant="ghost" onClick={() => onOpenChange(false)} disabled={submitting}>
						Cancel
					</Button>
					<Button
						variant={mode === "suspend" ? "destructive" : "default"}
						onClick={submit}
						disabled={submitting || (reasonRequired && !reason.trim())}
					>
						{mode === "suspend" ? "Suspend" : "Unsuspend"}
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
