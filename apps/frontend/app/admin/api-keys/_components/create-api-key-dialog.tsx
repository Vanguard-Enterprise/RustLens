"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";
import { ShieldCheckIcon } from "lucide-react";
import type { AdminPermission } from "@/lib/rustlens-api";
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
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { RolePermissionsGrid } from "@/app/admin/roles/_components/role-permissions-grid";
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
	csrfToken,
}: {
	open: boolean;
	onOpenChange: (open: boolean) => void;
	availablePermissions: AdminPermission[];
	csrfToken: string;
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
			const res = await fetch("/api/admin/api-keys", {
				method: "POST",
				headers: { "Content-Type": "application/json" },
				body: JSON.stringify({
					csrfToken,
					input: { name, permissions: Array.from(selected), expiresAt },
					reason,
				}),
			});
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

	function togglePerm(key: string, on: boolean) {
		setSelected((prev) => {
			const next = new Set(prev);
			if (on) next.add(key);
			else next.delete(key);
			return next;
		});
	}

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
						<div className="grid gap-2">
							<div className="flex items-center justify-between">
								<Label className="flex items-center gap-2">
									<ShieldCheckIcon className="size-4" />
									Permissions
								</Label>
								<span className="text-xs text-muted-foreground">
									{selected.size} selected
								</span>
							</div>
							<div className="max-h-72 overflow-y-auto rounded-lg border bg-muted/30 p-3">
								<RolePermissionsGrid
									catalog={availablePermissions}
									selected={selected}
									onToggle={togglePerm}
								/>
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
