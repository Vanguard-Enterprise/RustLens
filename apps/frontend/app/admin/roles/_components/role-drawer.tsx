"use client";

import * as React from "react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
    Sheet,
    SheetContent,
    SheetDescription,
    SheetFooter,
    SheetHeader,
    SheetTitle,
} from "@/components/ui/sheet";
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

export function RoleDrawer({ open, onOpenChange, mode, role, catalog, onSubmit }: Props) {
    const isSystem = mode === "edit" && role?.isSystem === true;
    const readOnly = isSystem;

    const [key, setKey] = React.useState("");
    const [name, setName] = React.useState("");
    const [description, setDescription] = React.useState("");
    const [perms, setPerms] = React.useState<Set<string>>(new Set());
    const [reason, setReason] = React.useState("");
    const [submitting, setSubmitting] = React.useState(false);
    const [error, setError] = React.useState<string | null>(null);

    React.useEffect(() => {
        if (!open) return;
        setError(null);
        setReason("");
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
    }, [open, mode, role]);

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
        if (!reason.trim()) return "Reason is required";
        return null;
    };

    const submit = async () => {
        const v = validate();
        if (v) {
            setError(v);
            return;
        }
        setError(null);
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
            onOpenChange(false);
        } catch (err) {
            setError((err as Error).message);
        } finally {
            setSubmitting(false);
        }
    };

    return (
        <Sheet open={open} onOpenChange={onOpenChange}>
            <SheetContent className="flex w-full flex-col gap-4 sm:max-w-2xl">
                <SheetHeader>
                    <SheetTitle>
                        {mode === "create" ? "New role" : isSystem ? `${role?.name} (system)` : `Edit ${role?.name}`}
                    </SheetTitle>
                    {isSystem ? (
                        <SheetDescription>
                            System role. Read-only — system roles are part of the application&apos;s
                            built-in trust model and cannot be modified.
                        </SheetDescription>
                    ) : null}
                </SheetHeader>

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

                    {!readOnly ? (
                        <div className="grid gap-2 border-t pt-4">
                            <Label htmlFor="role-reason">Reason</Label>
                            <Textarea
                                id="role-reason"
                                placeholder="Required. Recorded in the admin audit log."
                                value={reason}
                                onChange={(e) => setReason(e.target.value)}
                            />
                        </div>
                    ) : null}

                    {error ? <p className="text-sm text-destructive">{error}</p> : null}
                </div>

                <SheetFooter>
                    <Button variant="outline" onClick={() => onOpenChange(false)}>
                        Close
                    </Button>
                    {!readOnly ? (
                        <Button onClick={submit} disabled={submitting}>
                            {submitting ? "Saving…" : "Save"}
                        </Button>
                    ) : null}
                </SheetFooter>
            </SheetContent>
        </Sheet>
    );
}
