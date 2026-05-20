"use client";

import { CheckIcon, CopyIcon } from "lucide-react";
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
	const [copied, setCopied] = React.useState(false);
	const [prevOpen, setPrevOpen] = React.useState(open);

	if (prevOpen !== open) {
		setPrevOpen(open);
		if (!open) {
			setReason("");
			setTyped("");
			setError(null);
			setCopied(false);
			setSubmitting(false);
		}
	}

	const copyUserId = async () => {
		await navigator.clipboard.writeText(userId);
		setCopied(true);
		setTimeout(() => setCopied(false), 1500);
	};

	const submit = async () => {
		if (!reason.trim() || typed !== userId) return;
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
					<DialogDescription className="flex flex-col gap-2">
						<span>
							This permanently removes the user and any data tied to them via FK cascade.
						</span>
						<span className="flex flex-wrap items-center gap-1">
							<span>Type the user ID</span>
							<button
								type="button"
								onClick={copyUserId}
								className="inline-flex items-center gap-1 rounded bg-muted px-1.5 py-0.5 font-mono text-xs text-foreground hover:bg-muted/70"
								aria-label="Copy user ID"
							>
								<code>{userId}</code>
								{copied ? (
									<CheckIcon className="size-3" />
								) : (
									<CopyIcon className="size-3" />
								)}
							</button>
							<span>to confirm.</span>
						</span>
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
						<Label htmlFor="delete-typed">Type the user ID to confirm</Label>
						<Input
							id="delete-typed"
							placeholder={userId}
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
						disabled={submitting || !reason.trim() || typed !== userId}
					>
						Delete user
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
