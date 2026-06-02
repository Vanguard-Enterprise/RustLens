"use client";

import * as React from "react";

import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
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
import { ADMIN_BAN_CATEGORIES, type AdminBanCategory } from "@/lib/rustlens-api";

type Props = {
	open: boolean;
	onOpenChange: (open: boolean) => void;
	userId: string;
	displayName: string;
	initialCategories: AdminBanCategory[];
	onConfirm: (userId: string, categories: AdminBanCategory[], reason: string) => Promise<void>;
};

export function UserBanDialog({
	open,
	onOpenChange,
	userId,
	displayName,
	initialCategories,
	onConfirm,
}: Props) {
	const [selected, setSelected] = React.useState<Set<AdminBanCategory>>(
		() => new Set(initialCategories),
	);
	const [reason, setReason] = React.useState("");
	const [submitting, setSubmitting] = React.useState(false);
	const [error, setError] = React.useState<string | null>(null);
	const [prevOpen, setPrevOpen] = React.useState(open);

	if (prevOpen !== open) {
		setPrevOpen(open);
		setSelected(new Set(initialCategories));
		if (!open) {
			setReason("");
			setError(null);
			setSubmitting(false);
		}
	}

	const toggle = (category: AdminBanCategory, checked: boolean) => {
		setSelected((prev) => {
			const next = new Set(prev);
			if (checked) next.add(category);
			else next.delete(category);
			return next;
		});
	};

	const submit = async () => {
		if (!reason.trim()) return;
		setSubmitting(true);
		try {
			await onConfirm(userId, Array.from(selected), reason.trim());
			onOpenChange(false);
		} catch (err) {
			setError((err as Error).message);
		} finally {
			setSubmitting(false);
		}
	};

	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent className="sm:max-w-md">
				<DialogHeader>
					<DialogTitle>Restrict {displayName}</DialogTitle>
					<DialogDescription>
						Block selected RustLens bot feature categories for this Discord user.
					</DialogDescription>
				</DialogHeader>
				<div className="flex flex-col gap-3">
					<div className="flex flex-col gap-2">
						{ADMIN_BAN_CATEGORIES.map((category) => (
							<label
								key={category.value}
								className="flex items-center gap-2 rounded-md border px-3 py-2 text-sm"
							>
								<Checkbox
									checked={selected.has(category.value)}
									onCheckedChange={(value) => toggle(category.value, value === true)}
								/>
								<span>{category.label}</span>
							</label>
						))}
					</div>
					<div className="flex flex-col gap-1">
						<Label htmlFor="ban-reason">Reason</Label>
						<Input
							id="ban-reason"
							placeholder="Reason (audited)"
							value={reason}
							onChange={(e) => setReason(e.target.value)}
						/>
					</div>
				</div>
				{error ? <div className="text-sm text-destructive">{error}</div> : null}
				<DialogFooter>
					<Button variant="ghost" onClick={() => onOpenChange(false)} disabled={submitting}>
						Cancel
					</Button>
					<Button onClick={submit} disabled={submitting || !reason.trim()}>
						Save restrictions
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
