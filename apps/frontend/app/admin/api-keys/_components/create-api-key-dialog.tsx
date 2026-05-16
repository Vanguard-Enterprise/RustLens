"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";
import type { AdminPermission } from "@/lib/rustlens-api";
import { createApiKeyAction } from "../actions";
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
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { SecretRevealPanel } from "./secret-reveal-panel";

const EXPIRY_PRESETS: Array<{ value: string; label: string; days: number | null }> = [
	{ value: "never", label: "Never", days: null },
	{ value: "30", label: "30 days", days: 30 },
	{ value: "90", label: "90 days", days: 90 },
	{ value: "365", label: "1 year", days: 365 },
];

export function CreateApiKeyDialog({
	open,
	onOpenChange,
	availablePermissions,
}: {
	open: boolean;
	onOpenChange: (open: boolean) => void;
	availablePermissions: AdminPermission[];
}) {
	const router = useRouter();
	const [name, setName] = useState("");
	const [selected, setSelected] = useState<Set<string>>(new Set());
	const [expiry, setExpiry] = useState("never");
	const [reason, setReason] = useState("");
	const [submitting, setSubmitting] = useState(false);
	const [secret, setSecret] = useState<string | null>(null);
	const [error, setError] = useState<string | null>(null);

	async function submit() {
		setSubmitting(true);
		setError(null);
		try {
			const days = EXPIRY_PRESETS.find((p) => p.value === expiry)?.days ?? null;
			const expiresAt = days
				? new Date(Date.now() + days * 86_400_000).toISOString()
				: undefined;
			const res = await createApiKeyAction(
				{ name, permissions: Array.from(selected), expiresAt },
				reason,
			);
			if (!res.ok) {
				setError(res.error);
				return;
			}
			setSecret(res.result.secret);
		} catch (e) {
			setError(e instanceof Error ? e.message : "Failed to create key");
		} finally {
			setSubmitting(false);
		}
	}

	function close() {
		setName("");
		setSelected(new Set());
		setExpiry("never");
		setReason("");
		setSecret(null);
		setError(null);
		onOpenChange(false);
		router.refresh();
	}

	const grouped = availablePermissions.reduce<Record<string, AdminPermission[]>>((acc, p) => {
		const ns = p.key.split(".")[0] ?? "other";
		(acc[ns] ??= []).push(p);
		return acc;
	}, {});

	return (
		<Dialog open={open} onOpenChange={(o) => (o ? onOpenChange(true) : close())}>
			<DialogContent className="max-w-2xl">
				<DialogHeader>
					<DialogTitle>{secret ? "Copy your key" : "New API key"}</DialogTitle>
					{!secret && (
						<DialogDescription>
							Creates a new API key. You will only see the secret once.
						</DialogDescription>
					)}
				</DialogHeader>
				{secret ? (
					<SecretRevealPanel secret={secret} onAcknowledge={close} />
				) : (
					<div className="flex flex-col gap-4">
						<div className="flex flex-col gap-1.5">
							<Label htmlFor="ak-name">Name</Label>
							<Input
								id="ak-name"
								value={name}
								maxLength={64}
								onChange={(e) => setName(e.target.value)}
								placeholder="deploy-bot"
							/>
						</div>
						<div className="flex flex-col gap-1.5">
							<Label>Permissions</Label>
							<div className="grid max-h-64 grid-cols-2 gap-2 overflow-y-auto rounded-md border p-3">
								{Object.entries(grouped).map(([ns, perms]) => (
									<div key={ns} className="col-span-2">
										<div className="text-xs font-semibold uppercase text-muted-foreground">
											{ns}
										</div>
										{perms.map((p) => (
											<label
												key={p.key}
												className="flex items-center gap-2 py-1 text-sm"
											>
												<Checkbox
													checked={selected.has(p.key)}
													onCheckedChange={(checked) => {
														setSelected((prev) => {
															const next = new Set(prev);
															if (checked) next.add(p.key);
															else next.delete(p.key);
															return next;
														});
													}}
												/>
												<span className="font-mono">{p.key}</span>
												<span className="text-muted-foreground">— {p.description}</span>
											</label>
										))}
									</div>
								))}
							</div>
						</div>
						<div className="flex flex-col gap-1.5">
							<Label>Expires</Label>
							<RadioGroup value={expiry} onValueChange={setExpiry} className="flex gap-4">
								{EXPIRY_PRESETS.map((p) => (
									<label key={p.value} className="flex items-center gap-1.5 text-sm">
										<RadioGroupItem value={p.value} />
										{p.label}
									</label>
								))}
							</RadioGroup>
						</div>
						<div className="flex flex-col gap-1.5">
							<Label htmlFor="ak-reason">Reason</Label>
							<Input
								id="ak-reason"
								value={reason}
								onChange={(e) => setReason(e.target.value)}
								placeholder="Why is this key being issued?"
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
						<Button
							onClick={submit}
							disabled={
								submitting || name.trim().length === 0 || reason.trim().length === 0
							}
						>
							{submitting ? "Creating…" : "Create key"}
						</Button>
					</DialogFooter>
				)}
			</DialogContent>
		</Dialog>
	);
}
