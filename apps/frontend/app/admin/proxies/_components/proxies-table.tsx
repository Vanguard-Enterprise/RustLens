"use client";

import * as React from "react";
import {
	AlertTriangleIcon,
	MoreHorizontalIcon,
	PauseIcon,
	PlayIcon,
	Trash2Icon,
} from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuSeparator,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { RelativeTime } from "@/components/admin/relative-time";
import {
	Table,
	TableBody,
	TableCell,
	TableHead,
	TableHeader,
	TableRow,
} from "@/components/ui/table";
import type { AdminProxyItem } from "@/lib/rustlens-api";
import { cn } from "@/lib/utils";

import { toggleProxyAction } from "../actions";
import { DeleteProxyDialog } from "./delete-proxy-dialog";

type Props = {
	items: AdminProxyItem[];
	selected: Set<number>;
	onToggle: (id: number) => void;
	onToggleAll: () => void;
	permissions: string[];
	onComplete: () => void;
};

function StatusBadge({ proxy }: { proxy: AdminProxyItem }) {
	if (proxy.status === "broken") {
		return (
			<Badge variant="destructive" className="gap-1">
				<AlertTriangleIcon className="size-3" />
				broken
			</Badge>
		);
	}
	if (proxy.status === "paused") {
		return <Badge variant="outline">paused</Badge>;
	}
	return <Badge variant="secondary">active</Badge>;
}

export function ProxiesTable({
	items,
	selected,
	onToggle,
	onToggleAll,
	permissions,
	onComplete,
}: Props) {
	const canEdit = permissions.includes("proxies.edit");
	const [deleteTarget, setDeleteTarget] = React.useState<AdminProxyItem | null>(
		null,
	);
	const [pendingId, setPendingId] = React.useState<number | null>(null);
	const [actionError, setActionError] = React.useState<string | null>(null);

	const allOnPageSelected =
		items.length > 0 && items.every((it) => selected.has(it.id));

	const runToggle = async (proxy: AdminProxyItem, next: boolean) => {
		setPendingId(proxy.id);
		setActionError(null);
		const res = await toggleProxyAction(proxy.id, next, "");
		setPendingId(null);
		if (!res.ok) {
			setActionError(res.error);
			return;
		}
		onComplete();
	};

	return (
		<div className="flex flex-col gap-2">
			{actionError ? (
				<div className="rounded-md border border-destructive/50 bg-destructive/10 px-3 py-2 text-sm text-destructive">
					{actionError}
				</div>
			) : null}
			<div className="overflow-hidden rounded-lg border">
				<Table>
					<TableHeader>
						<TableRow>
							<TableHead className="w-10">
								<Checkbox
									checked={allOnPageSelected}
									onCheckedChange={onToggleAll}
									aria-label="Select all on page"
									disabled={items.length === 0}
								/>
							</TableHead>
							<TableHead>Endpoint</TableHead>
							<TableHead>Status</TableHead>
							<TableHead>Created</TableHead>
							<TableHead>Updated</TableHead>
							<TableHead className="w-12" />
						</TableRow>
					</TableHeader>
					<TableBody>
						{items.length === 0 ? (
							<TableRow>
								<TableCell colSpan={6} className="h-24 text-center">
									No proxies found.
								</TableCell>
							</TableRow>
						) : (
							items.map((it) => {
								const isBroken = it.status === "broken";
								const isActive = it.status === "active";
								const isPending = pendingId === it.id;
								return (
									<TableRow
										key={it.id}
										className={cn(isBroken && "bg-destructive/5")}
									>
										<TableCell>
											<Checkbox
												checked={selected.has(it.id)}
												onCheckedChange={() => onToggle(it.id)}
												aria-label={`Select proxy ${it.id}`}
											/>
										</TableCell>
										<TableCell>
											<span className="font-mono text-sm">{it.endpoint}</span>
										</TableCell>
										<TableCell>
											<div className="flex flex-col gap-0.5">
												<StatusBadge proxy={it} />
												{isBroken && it.autoDisabledAt ? (
													<span className="text-xs text-muted-foreground">
														auto-disabled <RelativeTime iso={it.autoDisabledAt} />
													</span>
												) : null}
											</div>
										</TableCell>
										<TableCell>
											{it.createdAt ? <RelativeTime iso={it.createdAt} /> : "—"}
										</TableCell>
										<TableCell>
											{it.updatedAt ? <RelativeTime iso={it.updatedAt} /> : "—"}
										</TableCell>
										<TableCell className="text-right">
											{canEdit ? (
												<DropdownMenu>
													<DropdownMenuTrigger asChild>
														<Button
															variant="ghost"
															size="icon"
															className="size-8"
															aria-label={`Actions for proxy ${it.id}`}
															disabled={isPending}
														>
															<MoreHorizontalIcon className="size-4" />
														</Button>
													</DropdownMenuTrigger>
													<DropdownMenuContent align="end">
														<DropdownMenuItem
															disabled={isActive}
															onSelect={() => runToggle(it, true)}
														>
															<PlayIcon className="size-4" />
															Activate
														</DropdownMenuItem>
														<DropdownMenuItem
															disabled={it.status === "paused"}
															onSelect={() => runToggle(it, false)}
														>
															<PauseIcon className="size-4" />
															Pause
														</DropdownMenuItem>
														<DropdownMenuSeparator />
														<DropdownMenuItem
															variant="destructive"
															onSelect={() => setDeleteTarget(it)}
														>
															<Trash2Icon className="size-4" />
															Delete
														</DropdownMenuItem>
													</DropdownMenuContent>
												</DropdownMenu>
											) : null}
										</TableCell>
									</TableRow>
								);
							})
						)}
					</TableBody>
				</Table>
			</div>
			{deleteTarget ? (
				<DeleteProxyDialog
					open
					onOpenChange={(o) => {
						if (!o) setDeleteTarget(null);
					}}
					proxy={deleteTarget}
					onSuccess={() => {
						setDeleteTarget(null);
						onComplete();
					}}
				/>
			) : null}
		</div>
	);
}
