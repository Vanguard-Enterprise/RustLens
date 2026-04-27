"use client";

import { useState } from "react";
import { Button } from "@/components/ui/button";
import { ADMIN_ACTIONS, type AdminActionKey } from "./action-manifest";
import { ReasonDialog } from "./reason-dialog";
import { DangerousConfirmDialog } from "./dangerous-confirm-dialog";

export interface AdminActionButtonProps {
  action: AdminActionKey;
  params: Record<string, unknown>;
  csrfToken: string;
  expectedConfirmation?: string;
  onSuccess?: (result: unknown) => void;
  label?: string;
  variant?: "default" | "destructive" | "ghost";
}

export function AdminActionButton({
  action,
  params,
  csrfToken,
  expectedConfirmation,
  onSuccess,
  label,
  variant,
}: AdminActionButtonProps) {
  const meta = ADMIN_ACTIONS[action];
  const [open, setOpen] = useState(false);

  const dispatch = async (reason: string, typed?: string) => {
    const resp = await fetch(`/api/app/admin/actions/${action}`, {
      method: "POST",
      credentials: "include",
      headers: {
        "Content-Type": "application/json",
        "X-CSRF-Token": csrfToken,
      },
      body: JSON.stringify({
        params,
        reason,
        typedConfirmation: typed ?? "",
      }),
    });
    const data: unknown = await resp.json();
    if (!resp.ok) {
      throw new Error((data as { error?: string }).error ?? "Action failed");
    }
    onSuccess?.(data);
  };

  const buttonLabel = label ?? meta.label;
  const buttonVariant = variant ?? (meta.dangerous ? "destructive" : "default");

  return (
    <>
      <Button variant={buttonVariant} onClick={() => setOpen(true)}>
        {buttonLabel}
      </Button>

      {meta.dangerous ? (
        <DangerousConfirmDialog
          open={open}
          onOpenChange={setOpen}
          title={meta.label}
          description={`This action is irreversible. Please confirm you want to ${meta.label.toLowerCase()}.`}
          expectedConfirmation={expectedConfirmation ?? meta.label}
          onConfirm={dispatch}
        />
      ) : (
        <ReasonDialog
          open={open}
          onOpenChange={setOpen}
          title={meta.label}
          onConfirm={dispatch}
        />
      )}
    </>
  );
}
