"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";
import { Button } from "@/components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { SecretRevealPanel } from "./secret-reveal-panel";

export function RotateApiKeyDialog({
	keyId,
	keyName,
	csrfToken,
	open,
	onOpenChange,
}: {
	keyId: string;
	keyName: string;
	csrfToken: string;
	open: boolean;
	onOpenChange: (open: boolean) => void;
}) {
	const router = useRouter();
	const [reason, setReason] = useState("");
	const [submitting, setSubmitting] = useState(false);
	const [secret, setSecret] = useState<string | null>(null);
	const [error, setError] = useState<string | null>(null);

	async function submit() {
		setSubmitting(true);
		setError(null);
		try {
			const res = await fetch(
				`/admin/api-keys/${encodeURIComponent(keyId)}/rotate`,
				{
					method: "POST",
					headers: { "Content-Type": "application/json" },
					body: JSON.stringify({ csrfToken, reason }),
				},
			);
			const body = (await res.json().catch(() => ({}))) as {
				ok?: boolean;
				result?: { secret: string };
				error?: string;
			};
			if (!res.ok || !body.ok) {
				setError(body.error ?? `Request failed (${res.status})`);
				return;
			}
			setSecret(body.result?.secret ?? null);
		} catch (e) {
			setError(e instanceof Error ? e.message : "Failed to rotate key");
		} finally {
			setSubmitting(false);
		}
	}

	function close() {
		setReason("");
		setSecret(null);
		setError(null);
		onOpenChange(false);
		router.refresh();
	}

	return (
		<Dialog open={open} onOpenChange={(o) => (o ? onOpenChange(true) : close())}>
			<DialogContent>
				<DialogHeader>
					<DialogTitle>{secret ? "Copy your new key" : `Rotate ${keyName}`}</DialogTitle>
				</DialogHeader>
				{secret ? (
					<SecretRevealPanel secret={secret} onAcknowledge={close} />
				) : (
					<div className="flex flex-col gap-4">
						<p className="text-sm text-muted-foreground">
							Rotating generates a new secret in place. The old secret stops working
							immediately. The key id, name, permissions, and audit history are preserved.
						</p>
						<div className="flex flex-col gap-1.5">
							<Label htmlFor="rk-reason">Reason</Label>
							<Input
								id="rk-reason"
								value={reason}
								onChange={(e) => setReason(e.target.value)}
								placeholder="Why is this key being rotated?"
							/>
						</div>
						{error && <div className="text-sm text-destructive">{error}</div>}
					</div>
				)}
				{!secret && (
					<DialogFooter>
						<Button variant="outline" onClick={close} disabled={submitting}>
							Cancel
						</Button>
						<Button onClick={submit} disabled={submitting || reason.trim().length === 0}>
							{submitting ? "Rotating…" : "Rotate"}
						</Button>
					</DialogFooter>
				)}
			</DialogContent>
		</Dialog>
	);
}
