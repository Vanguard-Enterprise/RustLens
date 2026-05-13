"use client";

import * as React from "react";

import { Button } from "@/components/ui/button";
import type { BulkBmServerAction } from "@/lib/rustlens-api";

import { BulkBmServerDialog } from "./bulk-bm-server-dialog";

type Props = {
	selectedIds: string[];
	permissions: string[];
	onClear: () => void;
	onComplete: () => void;
};

export function BmServersBulkToolbar({
	selectedIds,
	permissions,
	onClear,
	onComplete,
}: Props) {
	const [mode, setMode] = React.useState<BulkBmServerAction | null>(null);
	const canEdit = permissions.includes("bm_servers.edit");

	if (selectedIds.length === 0 || !canEdit) return null;

	return (
		<div className="flex flex-wrap items-center gap-2 rounded-md border bg-card p-2 shadow-sm">
			<span className="text-sm font-medium">
				{selectedIds.length} selected
			</span>
			<Button
				size="sm"
				variant="outline"
				onClick={() => setMode("set_high_freq")}
			>
				Set high frequency
			</Button>
			<Button
				size="sm"
				variant="outline"
				onClick={() => setMode("set_standard")}
			>
				Set standard
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
				<BulkBmServerDialog
					open
					mode={mode}
					bmIds={selectedIds}
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
