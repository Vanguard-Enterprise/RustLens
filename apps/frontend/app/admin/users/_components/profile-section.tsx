"use client";

import { useRouter } from "next/navigation";
import * as React from "react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import type { AdminUserDetail, EditUserPatch } from "@/lib/rustlens-api";

import { editUserAction } from "../actions";

type Props = {
	detail: AdminUserDetail;
	canEdit: boolean;
};

export function ProfileSection({ detail, canEdit }: Props) {
	const router = useRouter();
	const [editing, setEditing] = React.useState(false);
	const [displayName, setDisplayName] = React.useState(detail.user.displayName);
	const [avatarUrl, setAvatarUrl] = React.useState(detail.user.avatarUrl ?? "");
	const [submitting, setSubmitting] = React.useState(false);
	const [error, setError] = React.useState<string | null>(null);

	const cancel = () => {
		setDisplayName(detail.user.displayName);
		setAvatarUrl(detail.user.avatarUrl ?? "");
		setEditing(false);
		setError(null);
	};

	const save = async () => {
		const patch: EditUserPatch = {};
		if (displayName !== detail.user.displayName) patch.displayName = displayName;
		if (avatarUrl !== (detail.user.avatarUrl ?? "")) patch.avatarUrl = avatarUrl;
		if (Object.keys(patch).length === 0) {
			setEditing(false);
			return;
		}
		setSubmitting(true);
		setError(null);
		try {
			const res = await editUserAction(detail.user.id, patch, "");
			if (!res.ok) throw new Error(res.error);
			router.refresh();
			setEditing(false);
		} catch (err) {
			setError((err as Error).message);
		} finally {
			setSubmitting(false);
		}
	};

	return (
		<section className="flex flex-col gap-3 rounded-lg border bg-card p-4">
			<div className="flex items-center justify-between">
				<div>
					<h2 className="text-lg font-semibold">Profile</h2>
					<p className="text-sm text-muted-foreground">
						Display name and avatar shown across RustLens.
					</p>
				</div>
				{canEdit && !editing ? (
					<Button variant="outline" size="sm" onClick={() => setEditing(true)}>
						Edit profile
					</Button>
				) : null}
			</div>

			<dl className="grid grid-cols-1 gap-3 sm:grid-cols-2">
				<div className="flex flex-col gap-1">
					<Label htmlFor="display-name">Display name</Label>
					{editing ? (
						<Input
							id="display-name"
							value={displayName}
							onChange={(e) => setDisplayName(e.target.value)}
							maxLength={80}
						/>
					) : (
						<dd className="text-sm">{detail.user.displayName}</dd>
					)}
				</div>
				<div className="flex flex-col gap-1">
					<Label htmlFor="avatar-url">Avatar URL</Label>
					{editing ? (
						<Input
							id="avatar-url"
							value={avatarUrl}
							onChange={(e) => setAvatarUrl(e.target.value)}
							maxLength={512}
						/>
					) : (
						<dd className="truncate text-sm text-muted-foreground">
							{detail.user.avatarUrl || "—"}
						</dd>
					)}
				</div>
			</dl>

			{error ? <div className="text-sm text-destructive">{error}</div> : null}

			{editing ? (
				<div className="flex justify-end gap-2">
					<Button variant="ghost" onClick={cancel} disabled={submitting}>
						Cancel
					</Button>
					<Button onClick={save} disabled={submitting}>
						Save
					</Button>
				</div>
			) : null}
		</section>
	);
}
