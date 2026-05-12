"use client";

import * as React from "react";
import {
	HashIcon,
	KeyRoundIcon,
	LockIcon,
	MessageSquareTextIcon,
	ShieldCheckIcon,
	UsersIcon,
} from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogHeader,
	DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import type { AdminPermissionItem, AdminRoleItem } from "@/lib/rustlens-api";

import { RolePermissionsGrid } from "./role-permissions-grid";

type Mode = "create" | "edit";

type SubmitInput = {
	mode: Mode;
	role: AdminRoleItem | null;
	next: { key: string; name: string; description: string; permissions: string[] };
	reason: string;
};

type Props = {
	open: boolean;
	onOpenChange: (open: boolean) => void;
	mode: Mode;
	role: AdminRoleItem | null;
	catalog: AdminPermissionItem[];
	onSubmit: (input: SubmitInput) => Promise<void>;
};

const KEY_RE = /^[a-z][a-z0-9_]{1,63}$/;

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

export function RoleDialog({ open, onOpenChange, mode, role, catalog, onSubmit }: Props) {
	const isSystem = mode === "edit" && role?.isSystem === true;
	const readOnly = isSystem;

	const [key, setKey] = React.useState("");
	const [name, setName] = React.useState("");
	const [description, setDescription] = React.useState("");
	const [perms, setPerms] = React.useState<Set<string>>(new Set());
	const [reason, setReason] = React.useState("");
	const [reasonOpen, setReasonOpen] = React.useState(false);
	const [submitting, setSubmitting] = React.useState(false);
	const [error, setError] = React.useState<string | null>(null);
	const [prevOpen, setPrevOpen] = React.useState(open);
	if (open !== prevOpen) {
		setPrevOpen(open);
		if (open) {
			setError(null);
			setReason("");
			setReasonOpen(false);
			if (mode === "create") {
				setKey("");
				setName("");
				setDescription("");
				setPerms(new Set());
			} else if (role) {
				setKey(role.key);
				setName(role.name);
				setDescription(role.description ?? "");
				setPerms(new Set(role.permissions));
			}
		}
	}

	const togglePerm = (k: string, on: boolean) => {
		setPerms((cur) => {
			const next = new Set(cur);
			if (on) next.add(k);
			else next.delete(k);
			return next;
		});
	};

	const validate = (): string | null => {
		if (mode === "create" && !KEY_RE.test(key)) {
			return "Key must match ^[a-z][a-z0-9_]{1,63}$";
		}
		if (!name.trim() || name.length > 80) return "Name is required (max 80 chars)";
		if (description.length > 500) return "Description too long (max 500 chars)";
		return null;
	};

	const onSaveClick = () => {
		const v = validate();
		if (v) {
			setError(v);
			return;
		}
		setError(null);
		setReason("");
		setReasonOpen(true);
	};

	const confirmSubmit = async () => {
		if (!reason.trim()) return;
		setSubmitting(true);
		try {
			await onSubmit({
				mode,
				role,
				next: {
					key: key.trim(),
					name: name.trim(),
					description: description.trim(),
					permissions: [...perms],
				},
				reason: reason.trim(),
			});
			setReasonOpen(false);
			onOpenChange(false);
		} catch (err) {
			setError((err as Error).message);
			setReasonOpen(false);
		} finally {
			setSubmitting(false);
		}
	};

	const heroTitle =
		mode === "create"
			? "New role"
			: isSystem
				? `${role?.name} (system)`
				: `Edit ${role?.name}`;

	const heroDescription =
		mode === "edit" && role ? (
			<span className="font-mono text-xs">{role.key}</span>
		) : mode === "create" ? (
			"Define a new admin role and its permissions."
		) : null;

	return (
		<>
			<Dialog open={open} onOpenChange={onOpenChange}>
				<DialogContent className="max-w-2xl gap-0 overflow-hidden p-0 sm:max-w-2xl">
					<div className="flex max-h-[85vh] flex-col">
						{/* Hero */}
						<div className="flex flex-col gap-2 border-b bg-gradient-to-b from-muted/40 to-transparent px-6 pt-5 pb-4 pr-14">
							<div className="flex min-w-0 flex-col gap-1">
								<DialogTitle className="text-lg font-semibold leading-tight">
									{heroTitle}
								</DialogTitle>
								{heroDescription ? (
									<DialogDescription>{heroDescription}</DialogDescription>
								) : null}
							</div>
							<div className="flex flex-wrap items-center gap-x-3 gap-y-1.5">
								{isSystem ? (
									<Badge variant="secondary" className="gap-1">
										<LockIcon className="size-3" />
										System role
									</Badge>
								) : null}
								{mode === "edit" && role ? (
									<>
										<div className="flex items-center gap-1.5 text-xs text-muted-foreground">
											<ShieldCheckIcon className="size-3.5" />
											{role.permissions.length}{" "}
											{role.permissions.length === 1 ? "permission" : "permissions"}
										</div>
										<div className="flex items-center gap-1.5 text-xs text-muted-foreground">
											<UsersIcon className="size-3.5" />
											{role.memberCount}{" "}
											{role.memberCount === 1 ? "member" : "members"}
										</div>
									</>
								) : null}
							</div>
						</div>

						{/* Body */}
						<div className="flex flex-col gap-4 overflow-y-auto px-6 py-4">
							{isSystem ? (
								<div className="flex gap-3 rounded-lg border border-amber-500/30 bg-amber-500/5 p-3">
									<LockIcon className="mt-0.5 size-4 shrink-0 text-amber-600 dark:text-amber-400" />
									<div className="flex min-w-0 flex-col gap-1">
										<div className="text-[10px] font-semibold uppercase tracking-wider text-amber-700 dark:text-amber-300">
											Read-only
										</div>
										<p className="text-sm">
											System roles are part of the application&apos;s built-in trust
											model and cannot be modified.
										</p>
									</div>
								</div>
							) : null}

							<div className="grid gap-2">
								<SectionLabel icon={KeyRoundIcon}>Key</SectionLabel>
								<Input
									id="role-key"
									value={key}
									disabled={readOnly || mode === "edit"}
									onChange={(e) => setKey(e.target.value)}
									placeholder="snake_case_only"
									className="font-mono text-sm"
								/>
								{mode === "edit" ? (
									<p className="text-xs text-muted-foreground">
										Key is immutable post-create.
									</p>
								) : null}
							</div>

							<div className="grid gap-2">
								<SectionLabel icon={HashIcon}>Name</SectionLabel>
								<Input
									id="role-name"
									value={name}
									disabled={readOnly}
									onChange={(e) => setName(e.target.value)}
								/>
							</div>

							<div className="grid gap-2">
								<SectionLabel icon={MessageSquareTextIcon}>Description</SectionLabel>
								<Textarea
									id="role-description"
									value={description}
									disabled={readOnly}
									onChange={(e) => setDescription(e.target.value)}
								/>
							</div>

							<div className="grid gap-2">
								<div className="flex items-center justify-between">
									<SectionLabel icon={ShieldCheckIcon}>Permissions</SectionLabel>
									<span className="text-xs text-muted-foreground">
										{perms.size} selected
									</span>
								</div>
								<div className="rounded-lg border bg-muted/30 p-3">
									<RolePermissionsGrid
										catalog={catalog}
										selected={perms}
										onToggle={togglePerm}
										disabled={readOnly}
									/>
								</div>
							</div>

							{error ? (
								<p className="text-sm text-destructive">{error}</p>
							) : null}
						</div>

						{/* Footer */}
						<div className="flex items-center justify-between gap-2 border-t bg-muted/30 px-6 py-3">
							<div className="flex min-w-0 items-center gap-1.5 text-xs text-muted-foreground">
								{role ? (
									<>
										<HashIcon className="size-3.5 shrink-0" />
										<code className="truncate font-mono">{role.id}</code>
									</>
								) : null}
							</div>
							<div className="flex items-center gap-2">
								<Button variant="outline" onClick={() => onOpenChange(false)}>
									Close
								</Button>
								{!readOnly ? (
									<Button onClick={onSaveClick} disabled={submitting}>
										{submitting ? "Saving…" : "Save"}
									</Button>
								) : null}
							</div>
						</div>
					</div>
				</DialogContent>
			</Dialog>

			<Dialog open={reasonOpen} onOpenChange={(o) => (submitting ? null : setReasonOpen(o))}>
				<DialogContent className="max-w-md gap-0 overflow-hidden p-0">
					<div className="flex flex-col">
						<div className="flex flex-col gap-1 border-b bg-gradient-to-b from-muted/40 to-transparent px-6 pt-5 pb-4">
							<DialogTitle className="text-lg font-semibold leading-tight">
								Reason for change
							</DialogTitle>
							<DialogDescription>
								Required. Recorded in the admin audit log.
							</DialogDescription>
						</div>
						<div className="flex flex-col gap-2 px-6 py-4">
							<Label htmlFor="role-reason">Reason</Label>
							<Textarea
								id="role-reason"
								placeholder="Why are you making this change?"
								value={reason}
								onChange={(e) => setReason(e.target.value)}
								autoFocus
							/>
						</div>
						<div className="flex items-center justify-end gap-2 border-t bg-muted/30 px-6 py-3">
							<Button
								variant="outline"
								onClick={() => setReasonOpen(false)}
								disabled={submitting}
							>
								Cancel
							</Button>
							<Button onClick={confirmSubmit} disabled={!reason.trim() || submitting}>
								{submitting ? "Saving…" : "Confirm"}
							</Button>
						</div>
					</div>
				</DialogContent>
			</Dialog>
		</>
	);
}
