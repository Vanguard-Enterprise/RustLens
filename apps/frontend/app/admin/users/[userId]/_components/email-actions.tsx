"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import {
	Dialog,
	DialogContent,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@/components/ui/dialog";
import { OneTimeTokenModal } from "@/components/admin/one-time-token-modal";
import type {
	AdminActionResponse,
	ResendVerificationOutput,
} from "@/lib/rustlens-api";

async function dispatchAction<T = unknown>(
	action: string,
	csrfToken: string,
	body: Record<string, unknown>,
): Promise<AdminActionResponse<T>> {
	const resp = await fetch(`/api/app/admin/actions/${action}`, {
		method: "POST",
		credentials: "include",
		headers: {
			"Content-Type": "application/json",
			"X-CSRF-Token": csrfToken,
		},
		body: JSON.stringify(body),
	});
	const data: unknown = await resp.json();
	if (!resp.ok) {
		throw new Error(
			(data as { error?: string; message?: string }).message ||
				(data as { error?: string }).error ||
				"Action failed",
		);
	}
	return data as AdminActionResponse<T>;
}

export function SetPrimaryButton({
	userId,
	email,
	csrfToken,
}: {
	userId: string;
	email: string;
	csrfToken: string;
}) {
	const router = useRouter();
	const [busy, setBusy] = useState(false);
	const [error, setError] = useState<string | null>(null);
	return (
		<div className="inline-flex flex-col items-start gap-1">
			<Button
				size="sm"
				variant="outline"
				disabled={busy}
				onClick={async () => {
					setBusy(true);
					setError(null);
					try {
						await dispatchAction("users.email.setPrimary", csrfToken, {
							params: { userId, email },
						});
						router.refresh();
					} catch (e) {
						setError(e instanceof Error ? e.message : "Failed");
					} finally {
						setBusy(false);
					}
				}}
			>
				Set primary
			</Button>
			{error ? <span className="text-xs text-destructive">{error}</span> : null}
		</div>
	);
}

export function ResendVerificationButton({
	userId,
	email,
	csrfToken,
}: {
	userId: string;
	email: string;
	csrfToken: string;
}) {
	const [busy, setBusy] = useState(false);
	const [error, setError] = useState<string | null>(null);
	const [tokenUrl, setTokenUrl] = useState<string | null>(null);
	return (
		<>
			<div className="inline-flex flex-col items-start gap-1">
				<Button
					size="sm"
					variant="outline"
					disabled={busy}
					onClick={async () => {
						setBusy(true);
						setError(null);
						try {
							const envelope = await dispatchAction<ResendVerificationOutput>(
								"users.email.resendVerification",
								csrfToken,
								{ params: { userId, email } },
							);
							if (envelope.result?.tokenUrl) setTokenUrl(envelope.result.tokenUrl);
						} catch (e) {
							setError(e instanceof Error ? e.message : "Failed");
						} finally {
							setBusy(false);
						}
					}}
				>
					Send verification
				</Button>
				{error ? (
					<span className="text-xs text-destructive">{error}</span>
				) : null}
			</div>
			<OneTimeTokenModal
				open={tokenUrl !== null}
				url={tokenUrl}
				title={`Verification link for ${email}`}
				onClose={() => setTokenUrl(null)}
			/>
		</>
	);
}

export function RemoveEmailButton({
	userId,
	email,
	csrfToken,
}: {
	userId: string;
	email: string;
	csrfToken: string;
}) {
	const router = useRouter();
	const [open, setOpen] = useState(false);
	const [reason, setReason] = useState("");
	const [typed, setTyped] = useState("");
	const [busy, setBusy] = useState(false);
	const [error, setError] = useState<string | null>(null);
	return (
		<>
			<Button
				size="sm"
				variant="destructive"
				onClick={() => setOpen(true)}
			>
				Remove
			</Button>
			<Dialog open={open} onOpenChange={setOpen}>
				<DialogContent>
					<DialogHeader>
						<DialogTitle>Remove email</DialogTitle>
					</DialogHeader>
					<p className="text-sm text-muted-foreground">
						Type the email to confirm removal of <code>{email}</code>.
					</p>
					<div className="grid gap-2">
						<Input
							value={typed}
							onChange={(e) => setTyped(e.target.value)}
							placeholder={email}
							aria-label="Type email to confirm"
						/>
						<Textarea
							value={reason}
							onChange={(e) => setReason(e.target.value)}
							maxLength={500}
							placeholder="Reason"
							aria-label="Reason"
						/>
						{error ? (
							<p className="text-sm text-destructive">{error}</p>
						) : null}
					</div>
					<DialogFooter>
						<Button variant="ghost" onClick={() => setOpen(false)}>
							Cancel
						</Button>
						<Button
							variant="destructive"
							disabled={
								busy ||
								typed.trim().toLowerCase() !== email.trim().toLowerCase() ||
								reason.trim().length === 0
							}
							onClick={async () => {
								setBusy(true);
								setError(null);
								try {
									await dispatchAction("users.email.remove", csrfToken, {
										params: { userId, email },
										reason,
										typedConfirmation: email,
									});
									setOpen(false);
									setReason("");
									setTyped("");
									router.refresh();
								} catch (e) {
									setError(e instanceof Error ? e.message : "Failed");
								} finally {
									setBusy(false);
								}
							}}
						>
							{busy ? "Removing…" : "Remove email"}
						</Button>
					</DialogFooter>
				</DialogContent>
			</Dialog>
		</>
	);
}

export function AddEmailForm({
	userId,
	csrfToken,
}: {
	userId: string;
	csrfToken: string;
}) {
	const router = useRouter();
	const [email, setEmail] = useState("");
	const [busy, setBusy] = useState(false);
	const [error, setError] = useState<string | null>(null);

	const valid = /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email.trim());

	return (
		<form
			onSubmit={async (e) => {
				e.preventDefault();
				if (!valid) return;
				setBusy(true);
				setError(null);
				try {
					await dispatchAction("users.email.add", csrfToken, {
						params: { userId, email: email.trim().toLowerCase() },
					});
					setEmail("");
					router.refresh();
				} catch (err) {
					setError(err instanceof Error ? err.message : "Failed");
				} finally {
					setBusy(false);
				}
			}}
			className="flex items-start gap-2"
		>
			<Input
				type="email"
				value={email}
				onChange={(e) => setEmail(e.target.value)}
				placeholder="new-email@example.com"
				aria-label="New email"
				className="max-w-sm"
			/>
			<Button type="submit" disabled={!valid || busy}>
				{busy ? "Adding…" : "Add email"}
			</Button>
			{error ? (
				<span className="text-sm text-destructive self-center">{error}</span>
			) : null}
		</form>
	);
}
