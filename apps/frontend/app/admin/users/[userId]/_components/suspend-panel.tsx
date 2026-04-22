"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";

export interface SuspendPanelProps {
	userId: string;
	csrfToken: string;
	suspended: boolean;
	suspendReason?: string;
	selfTarget: boolean;
}

export function SuspendPanel({
	userId,
	csrfToken,
	suspended,
	suspendReason,
	selfTarget,
}: SuspendPanelProps) {
	const router = useRouter();
	const [reason, setReason] = useState("");
	const [busy, setBusy] = useState(false);
	const [error, setError] = useState<string | null>(null);

	const submit = async (action: "users.suspend" | "users.unsuspend") => {
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
					reason: action === "users.suspend" ? reason : "",
					typedConfirmation: action === "users.suspend" ? userId : "",
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

	if (suspended) {
		return (
			<div className="grid gap-2">
				<p className="text-sm">
					This account is <strong>suspended</strong>.
				</p>
				{suspendReason ? (
					<p className="text-sm text-muted-foreground">
						Reason: {suspendReason}
					</p>
				) : null}
				<div>
					<Button
						disabled={busy || selfTarget}
						title={selfTarget ? "You cannot act on your own account" : undefined}
						onClick={() => void submit("users.unsuspend")}
					>
						{busy ? "Working…" : "Unsuspend"}
					</Button>
				</div>
				{error ? <p className="text-sm text-destructive">{error}</p> : null}
			</div>
		);
	}

	return (
		<div className="grid gap-2">
			<label htmlFor="suspend-reason" className="text-sm font-medium">
				Reason (required)
			</label>
			<Textarea
				id="suspend-reason"
				value={reason}
				onChange={(e) => setReason(e.target.value)}
				maxLength={500}
				placeholder="Why is this account being suspended?"
			/>
			<div>
				<Button
					variant="destructive"
					disabled={busy || selfTarget || reason.trim().length === 0}
					title={selfTarget ? "You cannot act on your own account" : undefined}
					onClick={() => void submit("users.suspend")}
				>
					{busy ? "Working…" : "Suspend"}
				</Button>
			</div>
			{error ? <p className="text-sm text-destructive">{error}</p> : null}
		</div>
	);
}
