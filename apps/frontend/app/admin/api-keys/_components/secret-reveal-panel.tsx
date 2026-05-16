"use client";

import { CheckIcon, CopyIcon } from "lucide-react";
import { useState } from "react";
import { Button } from "@/components/ui/button";

export function SecretRevealPanel({
	secret,
	onAcknowledge,
}: {
	secret: string;
	onAcknowledge: () => void;
}) {
	const [copied, setCopied] = useState(false);
	return (
		<div className="flex flex-col gap-4">
			<div className="rounded-md border border-amber-500/30 bg-amber-500/5 p-3 text-sm">
				This is the only time the full secret will be shown. Store it in a password manager or
				secret store. If you lose it, rotate the key.
			</div>
			<div className="flex items-center gap-2 rounded-md bg-muted p-3 font-mono text-sm">
				<span className="flex-1 break-all">{secret}</span>
				<Button
					size="sm"
					variant="outline"
					onClick={async () => {
						await navigator.clipboard.writeText(secret);
						setCopied(true);
					}}
				>
					{copied ? <CheckIcon className="size-4" /> : <CopyIcon className="size-4" />}
					{copied ? "Copied" : "Copy"}
				</Button>
			</div>
			<div className="flex justify-end">
				<Button onClick={onAcknowledge}>I copied it</Button>
			</div>
		</div>
	);
}
