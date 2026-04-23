"use client";

import { useState } from "react";
import {
	ChevronDownIcon,
	DownloadIcon,
	PauseIcon,
	PlayIcon,
	ShieldMinusIcon,
	ShieldPlusIcon,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuLabel,
	DropdownMenuSeparator,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Separator } from "@/components/ui/separator";
import { Badge } from "@/components/ui/badge";

type BulkAction = "suspend" | "unsuspend" | "roles.grant" | "roles.revoke";

export interface UsersBulkToolbarProps {
	selected: string[];
	roleOptions: { value: string; label: string }[];
	csrfToken: string;
	/** Current filter query-string (without ids). Used to build export URL. */
	currentQuery: string;
	onClear: () => void;
}

interface BulkResponse {
	ok?: boolean;
	result?: {
		succeeded: number;
		failed: number;
		results: { userId: string; ok: boolean; error?: string }[];
	};
	error?: string;
}

export function UsersBulkToolbar({
	selected,
	roleOptions,
	csrfToken,
	currentQuery,
	onClear,
}: UsersBulkToolbarProps) {
	const [busy, setBusy] = useState(false);
	const [lastSummary, setLastSummary] = useState<string | null>(null);

	if (selected.length === 0) return null;

	const postBulk = async (
		action: BulkAction,
		params: Record<string, unknown> = {},
	) => {
		setBusy(true);
		setLastSummary(null);
		try {
			const resp = await fetch("/api/app/admin/actions/users.bulk", {
				method: "POST",
				credentials: "include",
				headers: {
					"Content-Type": "application/json",
					"X-CSRF-Token": csrfToken,
				},
				body: JSON.stringify({
					params: { action, userIds: selected, params },
					reason: `bulk ${action}`,
				}),
			});
			const data = (await resp.json()) as BulkResponse;
			if (!resp.ok) {
				setLastSummary(`Failed: ${data.error ?? "unknown error"}`);
				return;
			}
			const r = data.result;
			if (r) {
				setLastSummary(
					`${r.succeeded} succeeded, ${r.failed} failed (of ${selected.length})`,
				);
				if (r.failed === 0) onClear();
			}
		} finally {
			setBusy(false);
		}
	};

	const confirmAndRun = (action: BulkAction, params?: Record<string, unknown>) => {
		if (
			!window.confirm(
				`${action === "suspend" ? "Suspend" : action === "unsuspend" ? "Unsuspend" : action === "roles.grant" ? "Grant role to" : "Revoke role from"} ${selected.length} user${selected.length === 1 ? "" : "s"}?`,
			)
		) {
			return;
		}
		void postBulk(action, params);
	};

	const exportUrl = () => {
		const sp = new URLSearchParams(currentQuery);
		sp.set("ids", selected.join(","));
		return `/api/app/admin/users/export.csv?${sp.toString()}`;
	};

	return (
		<div className="sticky top-0 z-10 flex flex-wrap items-center gap-2 rounded-lg border bg-background/90 p-2 shadow-sm backdrop-blur">
			<div className="flex items-center gap-2">
				<Badge variant="default">{selected.length}</Badge>
				<span className="text-sm font-medium">
					user{selected.length === 1 ? "" : "s"} selected
				</span>
			</div>

			<Separator orientation="vertical" className="mx-1 h-6" />

			<DropdownMenu>
				<DropdownMenuTrigger asChild>
					<Button variant="outline" size="sm" disabled={busy}>
						<ShieldPlusIcon aria-hidden="true" />
						Grant role
						<ChevronDownIcon aria-hidden="true" />
					</Button>
				</DropdownMenuTrigger>
				<DropdownMenuContent align="start">
					<DropdownMenuLabel>Grant role to selection</DropdownMenuLabel>
					<DropdownMenuSeparator />
					{roleOptions.map((r) => (
						<DropdownMenuItem
							key={r.value}
							onSelect={() =>
								confirmAndRun("roles.grant", { roleKey: r.value })
							}
						>
							{r.label}
						</DropdownMenuItem>
					))}
				</DropdownMenuContent>
			</DropdownMenu>

			<DropdownMenu>
				<DropdownMenuTrigger asChild>
					<Button variant="outline" size="sm" disabled={busy}>
						<ShieldMinusIcon aria-hidden="true" />
						Revoke role
						<ChevronDownIcon aria-hidden="true" />
					</Button>
				</DropdownMenuTrigger>
				<DropdownMenuContent align="start">
					<DropdownMenuLabel>Revoke role from selection</DropdownMenuLabel>
					<DropdownMenuSeparator />
					{roleOptions.map((r) => (
						<DropdownMenuItem
							key={r.value}
							onSelect={() =>
								confirmAndRun("roles.revoke", { roleKey: r.value })
							}
						>
							{r.label}
						</DropdownMenuItem>
					))}
				</DropdownMenuContent>
			</DropdownMenu>

			<Button
				variant="destructive"
				size="sm"
				disabled={busy}
				onClick={() => confirmAndRun("suspend")}
			>
				<PauseIcon aria-hidden="true" />
				Suspend
			</Button>
			<Button
				variant="outline"
				size="sm"
				disabled={busy}
				onClick={() => confirmAndRun("unsuspend")}
			>
				<PlayIcon aria-hidden="true" />
				Unsuspend
			</Button>

			<Separator orientation="vertical" className="mx-1 h-6" />

			<Button asChild variant="outline" size="sm">
				<a href={exportUrl()}>
					<DownloadIcon aria-hidden="true" />
					Export CSV
				</a>
			</Button>
			<Button variant="ghost" size="sm" onClick={onClear}>
				Clear
			</Button>
			{lastSummary ? (
				<span className="ml-auto text-sm text-muted-foreground">
					{lastSummary}
				</span>
			) : null}
		</div>
	);
}
