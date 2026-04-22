"use client";

import { useState } from "react";
import {
	Dialog,
	DialogContent,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";

export function OneTimeTokenModal({
	open,
	url,
	title,
	onClose,
}: {
	open: boolean;
	url: string | null;
	title: string;
	onClose: () => void;
}) {
	const [copied, setCopied] = useState(false);
	if (!url) return null;
	return (
		<Dialog
			open={open}
			onOpenChange={(v) => {
				// Only the explicit "I've saved this link" button is allowed to
				// close the dialog — Radix can try to close on Esc / outside-click
				// which we block below on DialogContent.
				if (!v) return;
			}}
		>
			<DialogContent
				showCloseButton={false}
				onEscapeKeyDown={(e) => e.preventDefault()}
				onPointerDownOutside={(e) => e.preventDefault()}
				onInteractOutside={(e) => e.preventDefault()}
			>
				<DialogHeader>
					<DialogTitle>{title}</DialogTitle>
				</DialogHeader>
				<p className="text-sm text-muted-foreground">
					This link is shown <strong>once</strong>. Copy it before closing —
					it cannot be retrieved again.
				</p>
				<Input
					readOnly
					value={url}
					aria-label="One-time URL"
					onFocus={(e) => e.currentTarget.select()}
				/>
				<DialogFooter className="gap-2">
					<Button
						variant="outline"
						onClick={() => {
							void navigator.clipboard.writeText(url);
							setCopied(true);
						}}
					>
						{copied ? "Copied" : "Copy link"}
					</Button>
					<Button onClick={onClose}>I&apos;ve saved this link</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
