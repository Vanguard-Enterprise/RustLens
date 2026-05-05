"use client";

import * as React from "react";

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

    return (
        <>
            <Dialog open={open} onOpenChange={onOpenChange}>
                <DialogContent className="flex max-h-[85vh] flex-col gap-4 text-left sm:max-w-2xl">
                    <DialogHeader>
                        <DialogTitle>
                            {mode === "create" ? "New role" : isSystem ? `${role?.name} (system)` : `Edit ${role?.name}`}
                        </DialogTitle>
                        {isSystem ? (
                            <DialogDescription>
                                System role. Read-only — system roles are part of the application&apos;s
                                built-in trust model and cannot be modified.
                            </DialogDescription>
                        ) : null}
                    </DialogHeader>

                    <div className="flex flex-col gap-4 overflow-y-auto pr-2">
                        <div className="grid gap-2">
                            <Label htmlFor="role-key">Key</Label>
                            <Input
                                id="role-key"
                                value={key}
                                disabled={readOnly || mode === "edit"}
                                onChange={(e) => setKey(e.target.value)}
                                placeholder="snake_case_only"
                            />
                            {mode === "edit" ? (
                                <p className="text-xs text-muted-foreground">Key is immutable post-create.</p>
                            ) : null}
                        </div>
                        <div className="grid gap-2">
                            <Label htmlFor="role-name">Name</Label>
                            <Input
                                id="role-name"
                                value={name}
                                disabled={readOnly}
                                onChange={(e) => setName(e.target.value)}
                            />
                        </div>
                        <div className="grid gap-2">
                            <Label htmlFor="role-description">Description</Label>
                            <Textarea
                                id="role-description"
                                value={description}
                                disabled={readOnly}
                                onChange={(e) => setDescription(e.target.value)}
                            />
                        </div>

                        <div className="grid gap-2">
                            <Label>Permissions</Label>
                            <RolePermissionsGrid
                                catalog={catalog}
                                selected={perms}
                                onToggle={togglePerm}
                                disabled={readOnly}
                            />
                        </div>

                        {error ? <p className="text-sm text-destructive">{error}</p> : null}
                    </div>

                    <DialogFooter>
                        <Button variant="outline" onClick={() => onOpenChange(false)}>
                            Close
                        </Button>
                        {!readOnly ? (
                            <Button onClick={onSaveClick} disabled={submitting}>
                                {submitting ? "Saving…" : "Save"}
                            </Button>
                        ) : null}
                    </DialogFooter>
                </DialogContent>
            </Dialog>

            <Dialog open={reasonOpen} onOpenChange={(o) => (submitting ? null : setReasonOpen(o))}>
                <DialogContent className="text-left">
                    <DialogHeader>
                        <DialogTitle>Reason for change</DialogTitle>
                        <DialogDescription>
                            Required. Recorded in the admin audit log.
                        </DialogDescription>
                    </DialogHeader>
                    <div className="flex flex-col gap-2">
                        <Label htmlFor="role-reason">Reason</Label>
                        <Textarea
                            id="role-reason"
                            placeholder="Why are you making this change?"
                            value={reason}
                            onChange={(e) => setReason(e.target.value)}
                            autoFocus
                        />
                    </div>
                    <DialogFooter>
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
                    </DialogFooter>
                </DialogContent>
            </Dialog>
        </>
    );
}
