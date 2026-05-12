"use client";

import * as React from "react";

import { Button } from "@/components/ui/button";
import type { BulkProxyAction } from "@/lib/rustlens-api";

import { BulkProxyDialog } from "./bulk-proxy-dialog";

type Props = {
	selectedIds: number[];
	permissions: string[];
	onClear: () => void;
	onComplete: () => void;
};

export function ProxiesBulkToolbar({
	selectedIds,
	permissions,
	onClear,
	onComplete,
}: Props) {
	const [mode, setMode] = React.useState<BulkProxyAction | null>(null);
	const canEdit = permissions.includes("proxies.edit");

	if (selectedIds.length === 0 || !canEdit) return null;

	return (
		<div className="flex flex-wrap items-center gap-2 rounded-md border bg-card p-2 shadow-sm">
			<span className="text-sm font-medium">
				{selectedIds.length} selected
			</span>
			<Button size="sm" variant="outline" onClick={() => setMode("enable")}>
				Activate
			</Button>
			<Button size="sm" variant="outline" onClick={() => setMode("disable")}>
				Pause
			</Button>
			<Button
				size="sm"
				variant="outline"
				onClick={() => setMode("clear_broken")}
			>
				Clear broken state
			</Button>
			<Button
				size="sm"
				variant="outline"
				className="text-destructive hover:text-destructive"
				onClick={() => setMode("delete")}
			>
				Delete…
			</Button>
			<div className="ml-auto">
				<Button size="sm" variant="ghost" onClick={onClear}>
					Clear
				</Button>
			</div>

			{mode ? (
				<BulkProxyDialog
					open
					mode={mode}
					ids={selectedIds}
					onOpenChange={(o) => {
						if (!o) setMode(null);
					}}
					onSuccess={() => {
						setMode(null);
						onComplete();
					}}
				/>
			) : null}
		</div>
	);
}
