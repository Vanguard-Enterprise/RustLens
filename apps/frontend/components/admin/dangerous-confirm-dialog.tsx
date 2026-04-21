"use client";

import { useState } from "react";
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
import { Textarea } from "@/components/ui/textarea";

export interface DangerousConfirmDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  description: string;
  expectedConfirmation: string;
  onConfirm: (reason: string, typed: string) => Promise<void>;
  submitLabel?: string;
}

export function DangerousConfirmDialog({
  open,
  onOpenChange,
  title,
  description,
  expectedConfirmation,
  onConfirm,
  submitLabel = "Confirm",
}: DangerousConfirmDialogProps) {
  const [reason, setReason] = useState("");
  const [typed, setTyped] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const disabled = submitting || typed !== expectedConfirmation || !reason.trim();

  const handleSubmit = async () => {
    setSubmitting(true);
    setError(null);
    try {
      await onConfirm(reason, typed);
      setReason("");
      setTyped("");
      onOpenChange(false);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Action failed.");
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>{description}</DialogDescription>
        </DialogHeader>
        <div className="space-y-3">
          <Textarea value={reason} onChange={(e) => setReason(e.target.value)} placeholder="Reason (required)" disabled={submitting} />
          <div>
            <p className="text-sm">
              Type <code className="bg-muted px-1 rounded">{expectedConfirmation}</code> to confirm:
            </p>
            <Input value={typed} onChange={(e) => setTyped(e.target.value)} disabled={submitting} />
          </div>
          {error ? <p className="text-destructive text-sm">{error}</p> : null}
        </div>
        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)} disabled={submitting}>
            Cancel
          </Button>
          <Button variant="destructive" onClick={handleSubmit} disabled={disabled}>
            {submitting ? "Working..." : submitLabel}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
