"use client";

import { useState, type FormEvent } from "react";
import { useRouter } from "next/navigation";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";

export interface EditIdentityFormProps {
	userId: string;
	initialDisplayName: string;
	initialAvatarUrl: string;
	csrfToken: string;
}

export function EditIdentityForm({
	userId,
	initialDisplayName,
	initialAvatarUrl,
	csrfToken,
}: EditIdentityFormProps) {
	const router = useRouter();
	const [displayName, setDisplayName] = useState(initialDisplayName);
	const [avatarUrl, setAvatarUrl] = useState(initialAvatarUrl);
	const [busy, setBusy] = useState(false);
	const [error, setError] = useState<string | null>(null);
	const [success, setSuccess] = useState(false);

	const onSubmit = async (e: FormEvent<HTMLFormElement>) => {
		e.preventDefault();
		setError(null);
		setSuccess(false);

		const trimmedName = displayName.trim();
		const trimmedAvatar = avatarUrl.trim();
		if (trimmedName.length > 80) {
			setError("Display name must be 80 characters or fewer.");
			return;
		}
		if (trimmedAvatar.length > 512) {
			setError("Avatar URL must be 512 characters or fewer.");
			return;
		}

		const params: Record<string, string> = { userId };
		if (trimmedName !== initialDisplayName) params.displayName = trimmedName;
		if (trimmedAvatar !== initialAvatarUrl) params.avatarUrl = trimmedAvatar;
		if (!("displayName" in params) && !("avatarUrl" in params)) {
			setError("Nothing to update.");
			return;
		}

		setBusy(true);
		try {
			const resp = await fetch("/api/app/admin/actions/users.edit", {
				method: "POST",
				credentials: "include",
				headers: {
					"Content-Type": "application/json",
					"X-CSRF-Token": csrfToken,
				},
				body: JSON.stringify({
					params,
					reason: "",
					typedConfirmation: "",
				}),
			});
			const data: unknown = await resp.json();
			if (!resp.ok) {
				throw new Error((data as { error?: string }).error ?? "Action failed");
			}
			setSuccess(true);
			router.refresh();
		} catch (err) {
			setError(err instanceof Error ? err.message : "Action failed");
		} finally {
			setBusy(false);
		}
	};

	return (
		<form onSubmit={onSubmit} className="grid max-w-md gap-3">
			<label className="grid gap-1 text-sm">
				<span>Display name</span>
				<Input
					value={displayName}
					maxLength={80}
					onChange={(e) => setDisplayName(e.target.value)}
					placeholder="Display name"
					aria-label="Display name"
				/>
			</label>
			<label className="grid gap-1 text-sm">
				<span>Avatar URL</span>
				<Input
					value={avatarUrl}
					maxLength={512}
					onChange={(e) => setAvatarUrl(e.target.value)}
					placeholder="Avatar URL"
					aria-label="Avatar URL"
				/>
			</label>
			{error ? <p className="text-sm text-destructive">{error}</p> : null}
			{success ? <p className="text-sm text-green-600">Saved.</p> : null}
			<div>
				<Button type="submit" disabled={busy}>
					{busy ? "Saving..." : "Save"}
				</Button>
			</div>
		</form>
	);
}
