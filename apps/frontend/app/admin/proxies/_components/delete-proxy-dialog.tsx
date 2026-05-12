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
import type { AdminProxyItem } from "@/lib/rustlens-api";

import { deleteProxyAction } from "../actions";

type Props = {
	open: boolean;
	onOpenChange: (open: boolean) => void;
	proxy: AdminProxyItem;
	onSuccess: () => void;
};

export function DeleteProxyDialog({
	open,
	onOpenChange,
	proxy,
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
		const res = await deleteProxyAction(proxy.id, reason);
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
					<DialogTitle>Delete proxy</DialogTitle>
					<DialogDescription>
						This permanently removes the proxy and reloads the runtime
						provider. Outbound traffic will fall back to other enabled proxies.
					</DialogDescription>
				</DialogHeader>
				<div className="flex flex-col gap-2 text-sm">
					<div>
						<span className="text-muted-foreground">Endpoint: </span>
						<span className="font-mono">{proxy.endpoint}</span>
					</div>
					<div className="flex flex-col gap-1">
						<Label htmlFor="proxy-delete-reason">Reason (optional)</Label>
						<Input
							id="proxy-delete-reason"
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
