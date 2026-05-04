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
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";

type Role = { id: string; name: string; memberCount: number };

type Props = {
    role: Role | null;
    open: boolean;
    onOpenChange: (open: boolean) => void;
    onConfirm: (reason: string) => Promise<void>;
};

export function DeleteRoleDialog({ role, open, onOpenChange, onConfirm }: Props) {
    const [reason, setReason] = React.useState("");
    const [submitting, setSubmitting] = React.useState(false);

    React.useEffect(() => {
        if (open) setReason("");
    }, [open]);

    if (!role) return null;

    const submit = async () => {
        if (!reason.trim()) return;
        setSubmitting(true);
        try {
            await onConfirm(reason.trim());
            onOpenChange(false);
        } finally {
            setSubmitting(false);
        }
    };

    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent>
                <DialogHeader>
                    <DialogTitle>Delete role &ldquo;{role.name}&rdquo;?</DialogTitle>
                    <DialogDescription>
                        {role.memberCount > 0
                            ? `This will also remove the role from ${role.memberCount} user${role.memberCount === 1 ? "" : "s"}.`
                            : "This role is not currently assigned to any user."}
                    </DialogDescription>
                </DialogHeader>
                <div className="flex flex-col gap-2">
                    <Label htmlFor="delete-role-reason">Reason</Label>
                    <Textarea
                        id="delete-role-reason"
                        placeholder="Required. Recorded in the admin audit log."
                        value={reason}
                        onChange={(e) => setReason(e.target.value)}
                    />
                </div>
                <DialogFooter>
                    <Button variant="outline" onClick={() => onOpenChange(false)}>
                        Cancel
                    </Button>
                    <Button
                        variant="destructive"
                        disabled={!reason.trim() || submitting}
                        onClick={submit}
                    >
                        {submitting ? "Deleting…" : "Delete"}
                    </Button>
                </DialogFooter>
            </DialogContent>
        </Dialog>
    );
}
