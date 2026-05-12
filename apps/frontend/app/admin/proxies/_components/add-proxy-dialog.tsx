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

import { createProxyAction } from "../actions";

type Props = {
	open: boolean;
	onOpenChange: (open: boolean) => void;
	onSuccess: () => void;
};

export function AddProxyDialog({ open, onOpenChange, onSuccess }: Props) {
	const [endpoint, setEndpoint] = React.useState("");
	const [reason, setReason] = React.useState("");
	const [submitting, setSubmitting] = React.useState(false);
	const [error, setError] = React.useState<string | null>(null);
	const [prevOpen, setPrevOpen] = React.useState(open);

	if (prevOpen !== open) {
		setPrevOpen(open);
		if (!open) {
			setEndpoint("");
			setReason("");
			setError(null);
			setSubmitting(false);
		}
	}

	const submit = async () => {
		if (!endpoint.trim()) return;
		setSubmitting(true);
		setError(null);
		const res = await createProxyAction(endpoint, reason);
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
					<DialogTitle>Add proxy</DialogTitle>
					<DialogDescription>
						Provide the full proxy URL including scheme, credentials, host, and
						port. The endpoint is encrypted at rest and only the redacted form
						is shown afterwards.
					</DialogDescription>
				</DialogHeader>
				<div className="flex flex-col gap-3">
					<div className="flex flex-col gap-1">
						<Label htmlFor="proxy-endpoint">Endpoint</Label>
						<Input
							id="proxy-endpoint"
							placeholder="https://user:pass@proxy.example.com:8000"
							value={endpoint}
							onChange={(e) => setEndpoint(e.target.value)}
							autoComplete="off"
							spellCheck={false}
						/>
					</div>
					<div className="flex flex-col gap-1">
						<Label htmlFor="proxy-reason">Reason (optional)</Label>
						<Input
							id="proxy-reason"
							placeholder="Why is this being added?"
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
						onClick={submit}
						disabled={submitting || !endpoint.trim()}
					>
						{submitting ? "Adding…" : "Add"}
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
