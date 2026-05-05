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
import type { BulkUserResult, BulkUserSubAction } from "@/lib/rustlens-api";

type Mode = "suspend" | "unsuspend";

type Props = {
	open: boolean;
	onOpenChange: (open: boolean) => void;
	onSuccess?: () => void;
	mode: Mode;
	userIds: string[];
	onConfirm: (
		action: BulkUserSubAction,
		userIds: string[],
		params: Record<string, unknown>,
		reason: string,
	) => Promise<BulkUserResult>;
};

export function BulkSuspendDialog({ open, onOpenChange, onSuccess, mode, userIds, onConfirm }: Props) {
	const [reason, setReason] = React.useState("");
	const [submitting, setSubmitting] = React.useState(false);
	const [result, setResult] = React.useState<BulkUserResult | null>(null);
	const [error, setError] = React.useState<string | null>(null);

	React.useEffect(() => {
		if (!open) {
			setReason("");
			setResult(null);
			setError(null);
			setSubmitting(false);
		}
	}, [open]);

	const reasonRequired = mode === "suspend";
	const submit = async () => {
		if (reasonRequired && !reason.trim()) return;
		setSubmitting(true);
		setError(null);
		try {
			const res = await onConfirm(mode, userIds, {}, reason.trim());
			setResult(res);
			onSuccess?.();
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
						{mode === "suspend" ? "Suspend" : "Unsuspend"} {userIds.length} user{userIds.length === 1 ? "" : "s"}
					</DialogTitle>
					<DialogDescription>
						Each user is processed individually; failures don&apos;t stop the batch.
					</DialogDescription>
				</DialogHeader>

				{result ? (
					<div className="flex flex-col gap-2 text-sm">
						<div>{result.succeeded} succeeded · {result.failed} failed</div>
						<ul className="max-h-48 overflow-auto text-xs">
							{result.results.map((r) => (
								<li key={r.userId} className={r.ok ? "" : "text-destructive"}>
									{r.userId} — {r.ok ? "ok" : r.error}
								</li>
							))}
						</ul>
					</div>
				) : (
					<div className="flex flex-col gap-1">
						<Label htmlFor="bulk-suspend-reason">
							Reason{reasonRequired ? "" : " (optional)"}
						</Label>
						<Input
							id="bulk-suspend-reason"
							placeholder="Reason (audited)"
							value={reason}
							onChange={(e) => setReason(e.target.value)}
							maxLength={500}
						/>
						{error ? <div className="text-sm text-destructive">{error}</div> : null}
					</div>
				)}

				<DialogFooter>
					<Button variant="ghost" onClick={() => onOpenChange(false)} disabled={submitting}>
						{result ? "Close" : "Cancel"}
					</Button>
					{result ? (
						<Button onClick={() => onOpenChange(false)}>Done</Button>
					) : (
						<Button
							variant={mode === "suspend" ? "destructive" : "default"}
							onClick={submit}
							disabled={submitting || (reasonRequired && !reason.trim())}
						>
							{mode === "suspend" ? "Suspend" : "Unsuspend"}
						</Button>
					)}
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
