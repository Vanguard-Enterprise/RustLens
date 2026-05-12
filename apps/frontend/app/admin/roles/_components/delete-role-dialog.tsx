"use client";

import * as React from "react";
import {
	AlertTriangleIcon,
	HashIcon,
	MessageSquareTextIcon,
	UsersIcon,
} from "lucide-react";

import { Button } from "@/components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogTitle,
} from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";

type Role = { id: string; name: string; memberCount: number };

type Props = {
	role: Role | null;
	open: boolean;
	onOpenChange: (open: boolean) => void;
	onConfirm: (reason: string) => Promise<void>;
};

export function DeleteRoleDialog({ role, open, onOpenChange, onConfirm }: Props) {
	const [reason, setReason] = React.useState("");
	const [submitting, setSubmitting] = React.useState(false);
	const [error, setError] = React.useState<string | null>(null);
	const [prevOpen, setPrevOpen] = React.useState(open);
	if (open !== prevOpen) {
		setPrevOpen(open);
		if (open) {
			setReason("");
			setError(null);
		}
	}

	if (!role) return null;

	const submit = async () => {
		if (!reason.trim()) return;
		setError(null);
		setSubmitting(true);
		try {
			await onConfirm(reason.trim());
			onOpenChange(false);
		} catch (err) {
			setError((err as Error).message);
		} finally {
			setSubmitting(false);
		}
	};

	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent className="max-w-md gap-0 overflow-hidden p-0">
				<div className="flex flex-col">
					{/* Hero */}
					<div className="flex flex-col gap-2 border-b bg-gradient-to-b from-destructive/15 to-transparent px-6 pt-5 pb-4">
						<div className="flex min-w-0 flex-col gap-1">
							<DialogTitle className="text-lg font-semibold leading-tight">
								Delete role &ldquo;{role.name}&rdquo;?
							</DialogTitle>
							<DialogDescription>
								This action is permanent and cannot be undone.
							</DialogDescription>
						</div>
						<div className="flex flex-wrap items-center gap-x-3 gap-y-1.5">
							<div className="flex items-center gap-1.5 text-xs text-muted-foreground">
								<UsersIcon className="size-3.5" />
								{role.memberCount}{" "}
								{role.memberCount === 1 ? "member" : "members"}
							</div>
						</div>
					</div>

					{/* Body */}
					<div className="flex flex-col gap-4 px-6 py-4">
						{role.memberCount > 0 ? (
							<div className="flex gap-3 rounded-lg border border-destructive/30 bg-destructive/5 p-3">
								<AlertTriangleIcon className="mt-0.5 size-4 shrink-0 text-destructive" />
								<div className="flex min-w-0 flex-col gap-1">
									<div className="text-[10px] font-semibold uppercase tracking-wider text-destructive">
										Will affect users
									</div>
									<p className="text-sm">
										This will also remove the role from {role.memberCount} user
										{role.memberCount === 1 ? "" : "s"}.
									</p>
								</div>
							</div>
						) : null}

						<div className="flex flex-col gap-2">
							<div className="flex items-center gap-1.5 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">
								<MessageSquareTextIcon className="size-3.5" />
								Reason
							</div>
							<Label htmlFor="delete-role-reason" className="sr-only">
								Reason
							</Label>
							<Textarea
								id="delete-role-reason"
								placeholder="Required. Recorded in the admin audit log."
								value={reason}
								onChange={(e) => setReason(e.target.value)}
								autoFocus
							/>
							{error ? <p className="text-sm text-destructive">{error}</p> : null}
						</div>
					</div>

					{/* Footer */}
					<div className="flex items-center justify-between gap-2 border-t bg-muted/30 px-6 py-3">
						<div className="flex min-w-0 items-center gap-1.5 text-xs text-muted-foreground">
							<HashIcon className="size-3.5 shrink-0" />
							<code className="truncate font-mono">{role.id}</code>
						</div>
						<div className="flex items-center gap-2">
							<Button
								variant="outline"
								onClick={() => onOpenChange(false)}
								disabled={submitting}
							>
								Cancel
							</Button>
							<Button
								variant="destructive"
								disabled={!reason.trim() || submitting}
								onClick={submit}
							>
								{submitting ? "Deleting…" : "Delete"}
							</Button>
						</div>
					</div>
				</div>
			</DialogContent>
		</Dialog>
	);
}
