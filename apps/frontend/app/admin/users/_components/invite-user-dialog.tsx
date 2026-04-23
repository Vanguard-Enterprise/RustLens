"use client";

import { useId, useState } from "react";
import { UserPlusIcon } from "lucide-react";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogTitle,
	DialogTrigger,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { OneTimeTokenModal } from "@/components/admin/one-time-token-modal";
import type { AdminActionResponse, InviteUserOutput } from "@/lib/rustlens-api";

export interface InviteUserDialogProps {
	csrfToken: string;
}

export function InviteUserDialog({ csrfToken }: InviteUserDialogProps) {
	const [open, setOpen] = useState(false);
	const [email, setEmail] = useState("");
	const [displayName, setDisplayName] = useState("");
	const [reason, setReason] = useState("");
	const [busy, setBusy] = useState(false);
	const [error, setError] = useState<string | null>(null);
	const [tokenUrl, setTokenUrl] = useState<string | null>(null);
	const [invitedEmail, setInvitedEmail] = useState<string>("");

	const emailId = useId();
	const nameId = useId();
	const reasonId = useId();

	const reset = () => {
		setEmail("");
		setDisplayName("");
		setReason("");
		setError(null);
	};

	const submit = async () => {
		setBusy(true);
		setError(null);
		try {
			const normalised = email.trim().toLowerCase();
			const resp = await fetch(`/api/app/admin/actions/users.invite`, {
				method: "POST",
				credentials: "include",
				headers: {
					"Content-Type": "application/json",
					"X-CSRF-Token": csrfToken,
				},
				body: JSON.stringify({
					params: { email: normalised, displayName: displayName.trim() },
					reason,
					typedConfirmation: normalised,
				}),
			});
			const data: unknown = await resp.json();
			if (!resp.ok) {
				throw new Error(
					(data as { error?: string; message?: string }).message ||
						(data as { error?: string }).error ||
						"Invite failed",
				);
			}
			const envelope = data as AdminActionResponse<InviteUserOutput>;
			if (envelope.result?.tokenUrl) {
				setInvitedEmail(normalised);
				setTokenUrl(envelope.result.tokenUrl);
				setOpen(false);
				reset();
			}
		} catch (e) {
			setError(e instanceof Error ? e.message : "Invite failed");
		} finally {
			setBusy(false);
		}
	};

	const canSubmit =
		!busy &&
		email.trim().length > 0 &&
		reason.trim().length > 0 &&
		/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email.trim());

	return (
		<>
			<Dialog
				open={open}
				onOpenChange={(v) => {
					setOpen(v);
					if (!v) reset();
				}}
			>
				<DialogTrigger asChild>
					<Button size="sm">
						<UserPlusIcon aria-hidden="true" />
						Invite user
					</Button>
				</DialogTrigger>
				<DialogContent>
					<DialogHeader>
						<DialogTitle>Invite user</DialogTitle>
						<DialogDescription>
							Send an invite link. The user will be created on first
							acceptance.
						</DialogDescription>
					</DialogHeader>
					<div className="grid gap-4">
						<div className="grid gap-1.5">
							<Label htmlFor={emailId}>Email</Label>
							<Input
								id={emailId}
								type="email"
								value={email}
								required
								onChange={(e) => setEmail(e.target.value)}
								placeholder="person@example.com"
								aria-label="Email"
							/>
						</div>
						<div className="grid gap-1.5">
							<Label htmlFor={nameId}>
								Display name{" "}
								<span className="font-normal text-muted-foreground">
									(optional)
								</span>
							</Label>
							<Input
								id={nameId}
								value={displayName}
								onChange={(e) => setDisplayName(e.target.value)}
								aria-label="Display name"
							/>
						</div>
						<div className="grid gap-1.5">
							<Label htmlFor={reasonId}>Reason</Label>
							<Textarea
								id={reasonId}
								value={reason}
								onChange={(e) => setReason(e.target.value)}
								maxLength={500}
								placeholder="Why is this user being invited?"
								aria-label="Reason"
							/>
						</div>
						{error ? (
							<p className="text-sm text-destructive">{error}</p>
						) : null}
					</div>
					<DialogFooter>
						<Button
							variant="ghost"
							onClick={() => {
								setOpen(false);
								reset();
							}}
						>
							Cancel
						</Button>
						<Button disabled={!canSubmit} onClick={() => void submit()}>
							{busy ? "Sending…" : "Send invite"}
						</Button>
					</DialogFooter>
				</DialogContent>
			</Dialog>
			<OneTimeTokenModal
				open={tokenUrl !== null}
				url={tokenUrl}
				title={invitedEmail ? `Invite link for ${invitedEmail}` : "Invite link"}
				onClose={() => setTokenUrl(null)}
			/>
		</>
	);
}
