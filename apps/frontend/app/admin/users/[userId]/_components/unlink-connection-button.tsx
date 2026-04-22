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

export function UnlinkConnectionButton({
	userId,
	provider,
	providerAccountId,
	csrfToken,
}: {
	userId: string;
	provider: "discord" | "steam";
	providerAccountId: string;
	csrfToken: string;
}) {
	const router = useRouter();
	const [open, setOpen] = useState(false);
	const [typed, setTyped] = useState("");
	const [reason, setReason] = useState("");
	const [busy, setBusy] = useState(false);
	const [error, setError] = useState<string | null>(null);

	return (
		<>
			<Button
				size="sm"
				variant="destructive"
				onClick={() => setOpen(true)}
			>
				Unlink
			</Button>
			<Dialog open={open} onOpenChange={setOpen}>
				<DialogContent>
					<DialogHeader>
						<DialogTitle>
							Unlink {provider} connection
						</DialogTitle>
					</DialogHeader>
					<p className="text-sm text-muted-foreground">
						Type the user id <code>{userId}</code> to confirm.
					</p>
					<div className="grid gap-2">
						<Input
							value={typed}
							onChange={(e) => setTyped(e.target.value)}
							placeholder={userId}
							aria-label="Type user id to confirm"
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
								typed.trim() !== userId ||
								reason.trim().length === 0
							}
							onClick={async () => {
								setBusy(true);
								setError(null);
								try {
									const resp = await fetch(
										`/api/app/admin/actions/users.connections.unlink`,
										{
											method: "POST",
											credentials: "include",
											headers: {
												"Content-Type": "application/json",
												"X-CSRF-Token": csrfToken,
											},
											body: JSON.stringify({
												params: {
													userId,
													provider,
													providerAccountId,
												},
												reason,
												typedConfirmation: userId,
											}),
										},
									);
									const data: unknown = await resp.json();
									if (!resp.ok) {
										throw new Error(
											(data as { error?: string; message?: string })
												.message ||
												(data as { error?: string }).error ||
												"Unlink failed",
										);
									}
									setOpen(false);
									setReason("");
									setTyped("");
									router.refresh();
								} catch (e) {
									setError(
										e instanceof Error ? e.message : "Unlink failed",
									);
								} finally {
									setBusy(false);
								}
							}}
						>
							{busy ? "Unlinking…" : "Unlink"}
						</Button>
					</DialogFooter>
				</DialogContent>
			</Dialog>
		</>
	);
}
