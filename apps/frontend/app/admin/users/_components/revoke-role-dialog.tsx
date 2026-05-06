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
	roleKey: string;
	roleName: string;
	onConfirm: (userId: string, roleKey: string, reason: string) => Promise<void>;
};

export function RevokeRoleDialog({
	open,
	onOpenChange,
	userId,
	roleKey,
	roleName,
	onConfirm,
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
		if (!reason.trim()) return;
		setSubmitting(true);
		setError(null);
		try {
			await onConfirm(userId, roleKey, reason.trim());
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
					<DialogTitle>Revoke {roleName}</DialogTitle>
					<DialogDescription>
						This removes the <code>{roleKey}</code> role from this user. The action is audited.
					</DialogDescription>
				</DialogHeader>
				<div className="flex flex-col gap-1">
					<Label htmlFor="revoke-reason">Reason</Label>
					<Input
						id="revoke-reason"
						placeholder="Reason (required, audited)"
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
						variant="destructive"
						onClick={submit}
						disabled={submitting || !reason.trim()}
					>
						Revoke role
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
