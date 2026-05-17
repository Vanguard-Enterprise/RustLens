"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";
import {
	ClockIcon,
	HashIcon,
	MessageSquareTextIcon,
	ShieldCheckIcon,
} from "lucide-react";
import type { AdminPermission } from "@/lib/rustlens-api";
import { Button } from "@/components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { RolePermissionsGrid } from "@/app/admin/roles/_components/role-permissions-grid";
import { SecretRevealPanel } from "./secret-reveal-panel";

const EXPIRY_PRESETS: Array<{ value: string; label: string; days: number | null }> = [
	{ value: "never", label: "Never", days: null },
	{ value: "30", label: "30 days", days: 30 },
	{ value: "90", label: "90 days", days: 90 },
	{ value: "365", label: "1 year", days: 365 },
];

function SectionLabel({
	icon: Icon,
	children,
}: {
	icon: React.ComponentType<{ className?: string }>;
	children: React.ReactNode;
}) {
	return (
		<div className="flex items-center gap-1.5 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">
			<Icon className="size-3.5" />
			{children}
		</div>
	);
}

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
			const res = await fetch("/admin/api-keys/create", {
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
			<DialogContent className="max-w-2xl gap-0 overflow-hidden p-0 sm:max-w-2xl">
				<div className="flex max-h-[85vh] flex-col">
					{/* Hero */}
					<div className="flex flex-col gap-2 border-b bg-gradient-to-b from-muted/40 to-transparent px-6 pt-5 pb-4 pr-14">
						<div className="flex min-w-0 flex-col gap-1">
							<DialogTitle className="text-lg font-semibold leading-tight">
								{secret ? "Copy your key" : "New API key"}
							</DialogTitle>
							{!secret ? (
								<DialogDescription>
									Creates a new API key. You will only see the secret once.
								</DialogDescription>
							) : null}
						</div>
					</div>

					{/* Body */}
					{secret ? (
						<div className="overflow-y-auto px-6 py-4">
							<SecretRevealPanel secret={secret} onAcknowledge={close} />
						</div>
					) : (
						<>
							<div className="flex flex-col gap-4 overflow-y-auto px-6 py-4">
								<div className="grid gap-2">
									<SectionLabel icon={HashIcon}>Name</SectionLabel>
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
										<SectionLabel icon={ShieldCheckIcon}>Permissions</SectionLabel>
										<span className="text-xs text-muted-foreground">
											{selected.size} selected
										</span>
									</div>
									<div className="rounded-lg border bg-muted/30 p-3">
										<RolePermissionsGrid
											catalog={availablePermissions}
											selected={selected}
											onToggle={togglePerm}
										/>
									</div>
								</div>

								<div className="grid gap-2">
									<SectionLabel icon={ClockIcon}>Expires</SectionLabel>
									<RadioGroup
										value={expiry}
										onValueChange={setExpiry}
										className="flex flex-wrap gap-4"
									>
										{EXPIRY_PRESETS.map((p) => (
											<label
												key={p.value}
												htmlFor={`ak-expiry-${p.value}`}
												className="flex items-center gap-1.5 text-sm"
											>
												<RadioGroupItem id={`ak-expiry-${p.value}`} value={p.value} />
												{p.label}
											</label>
										))}
									</RadioGroup>
								</div>

								<div className="grid gap-2">
									<SectionLabel icon={MessageSquareTextIcon}>Reason</SectionLabel>
									<Input
										id="ak-reason"
										value={reason}
										onChange={(e) => setReason(e.target.value)}
										placeholder="Why is this key being issued?"
									/>
								</div>

								{error ? <p className="text-sm text-destructive">{error}</p> : null}
							</div>

							{/* Footer */}
							<div className="flex items-center justify-end gap-2 border-t bg-muted/20 px-6 py-3">
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
							</div>
						</>
					)}
				</div>
			</DialogContent>
		</Dialog>
	);
}
