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
import type { BulkProxyAction } from "@/lib/rustlens-api";

import { bulkProxyAction } from "../actions";

type Props = {
	open: boolean;
	onOpenChange: (open: boolean) => void;
	mode: BulkProxyAction;
	ids: number[];
	onSuccess: () => void;
};

const TITLES: Record<BulkProxyAction, string> = {
	enable: "Activate proxies",
	disable: "Pause proxies",
	clear_broken: "Clear broken state",
	delete: "Delete proxies",
};

const DESCRIPTIONS: Record<BulkProxyAction, string> = {
	enable:
		"All selected proxies will start serving outbound traffic on the next reload. Broken state, if any, is cleared.",
	disable:
		"All selected proxies will stop serving outbound traffic on the next reload.",
	clear_broken:
		"Clears the broken (auto-disabled) marker and re-enables all selected proxies. Useful after fixing upstream issues.",
	delete:
		"All selected proxies will be permanently removed. This cannot be undone.",
};

export function BulkProxyDialog({
	open,
	onOpenChange,
	mode,
	ids,
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
		const res = await bulkProxyAction(mode, ids, reason);
		setSubmitting(false);
		if (!res.ok) {
			setError(res.error);
			return;
		}
		if (res.result.failed > 0) {
			setError(
				`${res.result.failed} of ${ids.length} failed; ${res.result.succeeded} applied.`,
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
						{TITLES[mode]} ({ids.length})
					</DialogTitle>
					<DialogDescription>{DESCRIPTIONS[mode]}</DialogDescription>
				</DialogHeader>
				<div className="flex flex-col gap-1">
					<Label htmlFor="proxy-bulk-reason">Reason (optional)</Label>
					<Input
						id="proxy-bulk-reason"
						placeholder="Audited"
						value={reason}
						onChange={(e) => setReason(e.target.value)}
					/>
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
