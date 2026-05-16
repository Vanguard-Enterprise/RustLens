"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";
import { revokeApiKeyAction } from "../actions";
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

export function RevokeApiKeyDialog({
	keyId,
	keyName,
	open,
	onOpenChange,
}: {
	keyId: string;
	keyName: string;
	open: boolean;
	onOpenChange: (open: boolean) => void;
}) {
	const router = useRouter();
	const [reason, setReason] = useState("");
	const [submitting, setSubmitting] = useState(false);
	const [error, setError] = useState<string | null>(null);

	async function submit() {
		setSubmitting(true);
		setError(null);
		try {
			const res = await revokeApiKeyAction(keyId, reason);
			if (!res.ok) {
				setError(res.error);
				return;
			}
			onOpenChange(false);
			router.refresh();
		} catch (e) {
			setError(e instanceof Error ? e.message : "Failed to revoke");
		} finally {
			setSubmitting(false);
		}
	}

	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent>
				<DialogHeader>
					<DialogTitle>Revoke {keyName}</DialogTitle>
				</DialogHeader>
				<div className="flex flex-col gap-3 text-sm">
					<p>This will permanently revoke the API key. Any clients using it will start failing.</p>
					<div className="flex flex-col gap-1.5">
						<Label htmlFor="rv-reason">Reason</Label>
						<Input
							id="rv-reason"
							value={reason}
							onChange={(e) => setReason(e.target.value)}
							placeholder="Why is this key being revoked?"
						/>
					</div>
					{error && <div className="text-destructive">{error}</div>}
				</div>
				<DialogFooter>
					<Button variant="outline" onClick={() => onOpenChange(false)} disabled={submitting}>
						Cancel
					</Button>
					<Button
						variant="destructive"
						onClick={submit}
						disabled={submitting || reason.trim().length === 0}
					>
						{submitting ? "Revoking…" : "Revoke"}
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
