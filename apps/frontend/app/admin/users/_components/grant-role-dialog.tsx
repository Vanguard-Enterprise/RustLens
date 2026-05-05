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
import type { AdminRoleItem } from "@/lib/rustlens-api";

type Props = {
	open: boolean;
	onOpenChange: (open: boolean) => void;
	userId: string;
	assignableRoles: AdminRoleItem[];
	canGrantSuperadmin: boolean;
	onConfirm: (userId: string, roleKey: string, reason: string) => Promise<void>;
};

export function GrantRoleDialog({
	open,
	onOpenChange,
	userId,
	assignableRoles,
	canGrantSuperadmin,
	onConfirm,
}: Props) {
	const [roleKey, setRoleKey] = React.useState<string>("");
	const [reason, setReason] = React.useState("");
	const [submitting, setSubmitting] = React.useState(false);
	const [error, setError] = React.useState<string | null>(null);

	React.useEffect(() => {
		if (!open) {
			setRoleKey("");
			setReason("");
			setError(null);
			setSubmitting(false);
		}
	}, [open]);

	const submit = async () => {
		if (!roleKey || !reason.trim()) return;
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
					<DialogTitle>Grant role</DialogTitle>
					<DialogDescription>
						Assign an admin role. Granting <code>superadmin</code> requires admins.grant.
					</DialogDescription>
				</DialogHeader>

				<div className="flex flex-col gap-3">
					<div className="flex flex-col gap-1">
						<Label htmlFor="role">Role</Label>
						<Select value={roleKey} onValueChange={setRoleKey}>
							<SelectTrigger id="role">
								<SelectValue placeholder="Pick a role" />
							</SelectTrigger>
							<SelectContent>
								{assignableRoles.map((r) => {
									const disabled = r.key === "superadmin" && !canGrantSuperadmin;
									return (
										<SelectItem key={r.key} value={r.key} disabled={disabled}>
											<div className="flex flex-col">
												<span>{r.name}</span>
												{r.description ? (
													<span className="text-xs text-muted-foreground">
														{r.description}
													</span>
												) : null}
												{disabled ? (
													<span className="text-xs text-amber-600">
														Requires admins.grant
													</span>
												) : null}
											</div>
										</SelectItem>
									);
								})}
							</SelectContent>
						</Select>
					</div>

					<div className="flex flex-col gap-1">
						<Label htmlFor="reason">Reason</Label>
						<Input
							id="reason"
							placeholder="Reason (required, audited)"
							value={reason}
							onChange={(e) => setReason(e.target.value)}
						/>
					</div>

					{error ? <div className="text-sm text-destructive">{error}</div> : null}
				</div>

				<DialogFooter>
					<Button variant="ghost" onClick={() => onOpenChange(false)} disabled={submitting}>
						Cancel
					</Button>
					<Button
						onClick={submit}
						disabled={submitting || !roleKey || !reason.trim()}
					>
						Grant role
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
