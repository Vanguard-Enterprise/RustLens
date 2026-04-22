"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";

export interface PurgePanelProps {
	userId: string;
	csrfToken: string;
	selfTarget: boolean;
}

export function PurgePanel({ userId, csrfToken, selfTarget }: PurgePanelProps) {
	const router = useRouter();
	const [typed, setTyped] = useState("");
	const [reason, setReason] = useState("");
	const [busy, setBusy] = useState(false);
	const [error, setError] = useState<string | null>(null);
	const matches = typed === userId;

	const submit = async () => {
		setBusy(true);
		setError(null);
		try {
			const resp = await fetch(`/api/app/admin/actions/users.purge`, {
				method: "POST",
				credentials: "include",
				headers: {
					"Content-Type": "application/json",
					"X-CSRF-Token": csrfToken,
				},
				body: JSON.stringify({
					params: { userId, confirmId: typed },
					reason,
					typedConfirmation: typed,
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
			router.push("/admin/users");
		} catch (e) {
			setError(e instanceof Error ? e.message : "Action failed");
			setBusy(false);
		}
	};

	return (
		<div className="grid gap-2 rounded border border-destructive/40 bg-destructive/5 p-3">
			<p className="text-sm">
				<strong>Purge</strong> permanently deletes this user and all linked
				records (emails, credentials, connections, role assignments). This
				cannot be undone. Type the user id <code>{userId}</code> below to
				enable the button.
			</p>
			<Input
				aria-label="Confirm user id"
				value={typed}
				onChange={(e) => setTyped(e.target.value)}
				placeholder={userId}
			/>
			<label htmlFor="purge-reason" className="text-sm font-medium">
				Reason (required)
			</label>
			<Textarea
				id="purge-reason"
				value={reason}
				onChange={(e) => setReason(e.target.value)}
				maxLength={500}
			/>
			<div>
				<Button
					variant="destructive"
					disabled={busy || selfTarget || !matches || reason.trim().length === 0}
					title={selfTarget ? "You cannot act on your own account" : undefined}
					onClick={() => void submit()}
				>
					{busy ? "Purging…" : "Purge user"}
				</Button>
			</div>
			{error ? <p className="text-sm text-destructive">{error}</p> : null}
		</div>
	);
}
