"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";

export interface DeleteRestorePanelProps {
	userId: string;
	csrfToken: string;
	deleted: boolean;
	selfTarget: boolean;
}

export function DeleteRestorePanel({
	userId,
	csrfToken,
	deleted,
	selfTarget,
}: DeleteRestorePanelProps) {
	const router = useRouter();
	const [reason, setReason] = useState("");
	const [busy, setBusy] = useState(false);
	const [error, setError] = useState<string | null>(null);

	const submit = async (action: "users.delete" | "users.restore") => {
		setBusy(true);
		setError(null);
		try {
			const resp = await fetch(`/api/app/admin/actions/${action}`, {
				method: "POST",
				credentials: "include",
				headers: {
					"Content-Type": "application/json",
					"X-CSRF-Token": csrfToken,
				},
				body: JSON.stringify({
					params: { userId },
					reason: action === "users.delete" ? reason : "",
					typedConfirmation: action === "users.delete" ? userId : "",
				}),
			});
			const data: unknown = await resp.json();
			if (!resp.ok) {
				throw new Error(
					(data as { error?: string; message?: string }).message ||
						(data as { error?: string }).error ||
						"Action failed",
				);
			}
			router.refresh();
		} catch (e) {
			setError(e instanceof Error ? e.message : "Action failed");
		} finally {
			setBusy(false);
		}
	};

	if (deleted) {
		return (
			<div className="grid gap-2">
				<p className="text-sm">
					This account is <strong>soft-deleted</strong>.
				</p>
				<div>
					<Button
						disabled={busy || selfTarget}
						title={selfTarget ? "You cannot act on your own account" : undefined}
						onClick={() => void submit("users.restore")}
					>
						{busy ? "Working…" : "Restore"}
					</Button>
				</div>
				{error ? <p className="text-sm text-destructive">{error}</p> : null}
			</div>
		);
	}

	return (
		<div className="grid gap-2">
			<label htmlFor="delete-reason" className="text-sm font-medium">
				Reason (required)
			</label>
			<Textarea
				id="delete-reason"
				value={reason}
				onChange={(e) => setReason(e.target.value)}
				maxLength={500}
				placeholder="Why is this account being deleted?"
			/>
			<div>
				<Button
					variant="destructive"
					disabled={busy || selfTarget || reason.trim().length === 0}
					title={selfTarget ? "You cannot act on your own account" : undefined}
					onClick={() => void submit("users.delete")}
				>
					{busy ? "Working…" : "Soft delete"}
				</Button>
			</div>
			{error ? <p className="text-sm text-destructive">{error}</p> : null}
		</div>
	);
}
