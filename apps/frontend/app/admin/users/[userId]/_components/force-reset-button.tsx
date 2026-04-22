"use client";

import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { OneTimeTokenModal } from "@/components/admin/one-time-token-modal";
import type { AdminActionResponse, ForceResetOutput } from "@/lib/rustlens-api";

export interface ForceResetButtonProps {
	userId: string;
	csrfToken: string;
	selfTarget: boolean;
}

export function ForceResetButton({
	userId,
	csrfToken,
	selfTarget,
}: ForceResetButtonProps) {
	const [reason, setReason] = useState("");
	const [busy, setBusy] = useState(false);
	const [error, setError] = useState<string | null>(null);
	const [url, setUrl] = useState<string | null>(null);

	const submit = async () => {
		setBusy(true);
		setError(null);
		try {
			const resp = await fetch(
				`/api/app/admin/actions/users.password.forceReset`,
				{
					method: "POST",
					credentials: "include",
					headers: {
						"Content-Type": "application/json",
						"X-CSRF-Token": csrfToken,
					},
					body: JSON.stringify({
						params: { userId },
						reason,
						typedConfirmation: userId,
					}),
				},
			);
			const data: unknown = await resp.json();
			if (!resp.ok) {
				throw new Error(
					(data as { error?: string; message?: string }).message ||
						(data as { error?: string }).error ||
						"Action failed",
				);
			}
			const envelope = data as AdminActionResponse<ForceResetOutput>;
			if (envelope.result?.tokenUrl) {
				setUrl(envelope.result.tokenUrl);
			}
		} catch (e) {
			setError(e instanceof Error ? e.message : "Action failed");
		} finally {
			setBusy(false);
		}
	};

	return (
		<div className="grid gap-2">
			<label htmlFor="force-reset-reason" className="text-sm font-medium">
				Reason (required)
			</label>
			<Textarea
				id="force-reset-reason"
				value={reason}
				onChange={(e) => setReason(e.target.value)}
				maxLength={500}
				placeholder="Why are you forcing a password reset?"
			/>
			<div>
				<Button
					variant="destructive"
					disabled={busy || selfTarget || reason.trim().length === 0}
					title={selfTarget ? "You cannot act on your own account" : undefined}
					onClick={() => void submit()}
				>
					{busy ? "Generating…" : "Force password reset"}
				</Button>
			</div>
			{error ? <p className="text-sm text-destructive">{error}</p> : null}
			<OneTimeTokenModal
				open={url !== null}
				url={url}
				title="Password reset link"
				onClose={() => setUrl(null)}
			/>
		</div>
	);
}
