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
import {
	Select,
	SelectContent,
	SelectItem,
	SelectTrigger,
	SelectValue,
} from "@/components/ui/select";
import type { AdminRoleItem, BulkUserResult, BulkUserSubAction } from "@/lib/rustlens-api";

type Mode = "grant" | "revoke";

type Props = {
	open: boolean;
	onOpenChange: (open: boolean) => void;
	onSuccess?: () => void;
	mode: Mode;
	userIds: string[];
	roles: AdminRoleItem[];
	onConfirm: (
		action: BulkUserSubAction,
		userIds: string[],
		params: Record<string, unknown>,
		reason: string,
	) => Promise<BulkUserResult>;
};

export function BulkRoleDialog({ open, onOpenChange, onSuccess, mode, userIds, roles, onConfirm }: Props) {
	const [roleKey, setRoleKey] = React.useState("");
	const [reason, setReason] = React.useState("");
	const [submitting, setSubmitting] = React.useState(false);
	const [result, setResult] = React.useState<BulkUserResult | null>(null);
	const [error, setError] = React.useState<string | null>(null);

	React.useEffect(() => {
		if (!open) {
			setRoleKey("");
			setReason("");
			setResult(null);
			setError(null);
			setSubmitting(false);
		}
	}, [open]);

	const submit = async () => {
		if (!roleKey || !reason.trim()) return;
		setSubmitting(true);
		setError(null);
		try {
			const res = await onConfirm(
				mode === "grant" ? "roles.grant" : "roles.revoke",
				userIds,
				{ roleKey },
				reason.trim(),
			);
			setResult(res);
			onSuccess?.();
		} catch (err) {
			setError((err as Error).message);
		} finally {
			setSubmitting(false);
		}
	};

	const assignableRoles = roles.filter((r) => r.key !== "superadmin");
	const verb = mode === "grant" ? "Grant role to" : "Revoke role from";

	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent>
				<DialogHeader>
					<DialogTitle>
						{verb} {userIds.length} user{userIds.length === 1 ? "" : "s"}
					</DialogTitle>
					<DialogDescription>
						Each user is processed individually; failures don&apos;t stop the batch.
					</DialogDescription>
				</DialogHeader>

				{result ? (
					<div className="flex flex-col gap-2 text-sm">
						<div>
							{result.succeeded} succeeded · {result.failed} failed
						</div>
						<ul className="max-h-48 overflow-auto text-xs">
							{result.results.map((r) => (
								<li key={r.userId} className={r.ok ? "" : "text-destructive"}>
									{r.userId} — {r.ok ? "ok" : r.error}
								</li>
							))}
						</ul>
					</div>
				) : (
					<div className="flex flex-col gap-3">
						<div className="flex flex-col gap-1">
							<Label htmlFor="bulk-role">Role</Label>
							<Select value={roleKey} onValueChange={setRoleKey}>
								<SelectTrigger id="bulk-role">
									<SelectValue placeholder="Pick a role" />
								</SelectTrigger>
								<SelectContent>
									{assignableRoles.map((r) => (
										<SelectItem key={r.key} value={r.key}>
											{r.name}
										</SelectItem>
									))}
								</SelectContent>
							</Select>
						</div>
						<div className="flex flex-col gap-1">
							<Label htmlFor="bulk-reason">Reason</Label>
							<Input
								id="bulk-reason"
								placeholder="Reason (required, audited)"
								value={reason}
								onChange={(e) => setReason(e.target.value)}
								maxLength={500}
							/>
						</div>
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
							onClick={submit}
							disabled={submitting || !roleKey || !reason.trim()}
						>
							{verb}
						</Button>
					)}
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
