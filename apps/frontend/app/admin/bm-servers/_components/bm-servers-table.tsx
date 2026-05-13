"use client";

import * as React from "react";
import {
	AlertTriangleIcon,
	ArrowDownIcon,
	ArrowUpDownIcon,
	ArrowUpIcon,
	MoreHorizontalIcon,
	PencilIcon,
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
import {
	Tooltip,
	TooltipContent,
	TooltipTrigger,
} from "@/components/ui/tooltip";
import type {
	AdminBmServerItem,
	AdminBmServerSortKey,
	AdminBmServerSortOrder,
	AdminBmServersQuery,
} from "@/lib/rustlens-api";
import { cn } from "@/lib/utils";

import { BmServerDialog } from "./bm-server-dialog";
import { DeleteBmServerDialog } from "./delete-bm-server-dialog";

type Props = {
	items: AdminBmServerItem[];
	query: AdminBmServersQuery;
	onSortChange: (
		sortBy: AdminBmServerSortKey | undefined,
		sortOrder: AdminBmServerSortOrder | undefined,
	) => void;
	selected: Set<string>;
	onToggle: (bmId: string) => void;
	onToggleAll: () => void;
	permissions: string[];
	onComplete: () => void;
};

const CATEGORY_KEYS = [
	["isVitality", "Vitality"],
	["isAtlas", "Atlas"],
	["isVital", "Vital"],
	["isModded", "Modded"],
	["isOfficial", "Official"],
	["isBuild", "Build"],
] as const;

function CategoryBadges({ server }: { server: AdminBmServerItem }) {
	const active = CATEGORY_KEYS.filter(([k]) => server[k as keyof AdminBmServerItem]);
	if (active.length === 0) {
		return <span className="text-xs text-muted-foreground">—</span>;
	}
	return (
		<div className="flex flex-wrap gap-1">
			{active.map(([, label]) => (
				<Badge key={label} variant="outline" className="text-xs">
					{label}
				</Badge>
			))}
		</div>
	);
}

type SortableHeaderProps = {
	label: string;
	sortKey: AdminBmServerSortKey;
	activeKey: AdminBmServerSortKey;
	activeOrder: AdminBmServerSortOrder;
	defaultOrder?: AdminBmServerSortOrder;
	onSortChange: Props["onSortChange"];
	className?: string;
};

function SortableHeader({
	label,
	sortKey,
	activeKey,
	activeOrder,
	defaultOrder = "asc",
	onSortChange,
	className,
}: SortableHeaderProps) {
	const isActive = activeKey === sortKey;
	const Icon = !isActive
		? ArrowUpDownIcon
		: activeOrder === "asc"
			? ArrowUpIcon
			: ArrowDownIcon;
	const handleClick = () => {
		if (!isActive) {
			onSortChange(sortKey, defaultOrder);
			return;
		}
		const next: AdminBmServerSortOrder =
			activeOrder === "asc" ? "desc" : "asc";
		onSortChange(sortKey, next);
	};
	return (
		<TableHead className={className}>
			<button
				type="button"
				onClick={handleClick}
				className={cn(
					"inline-flex items-center gap-1 text-left text-xs font-semibold uppercase tracking-wide text-muted-foreground hover:text-foreground",
					isActive && "text-foreground",
				)}
				aria-label={`Sort by ${label}`}
			>
				{label}
				<Icon className="size-3.5 opacity-70" />
			</button>
		</TableHead>
	);
}

export function BmServersTable({
	items,
	query,
	onSortChange,
	selected,
	onToggle,
	onToggleAll,
	permissions,
	onComplete,
}: Props) {
	const canEdit = permissions.includes("bm_servers.edit");
	const [editTarget, setEditTarget] = React.useState<AdminBmServerItem | null>(
		null,
	);
	const [deleteTarget, setDeleteTarget] = React.useState<AdminBmServerItem | null>(
		null,
	);

	const activeKey: AdminBmServerSortKey = query.sortBy ?? "name";
	const activeOrder: AdminBmServerSortOrder = query.sortOrder ?? "asc";

	const allOnPageSelected =
		items.length > 0 && items.every((it) => selected.has(it.bmId));

	return (
		<div className="flex flex-col gap-2">
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
							<SortableHeader
								label="Name"
								sortKey="name"
								activeKey={activeKey}
								activeOrder={activeOrder}
								onSortChange={onSortChange}
							/>
							<SortableHeader
								label="BM ID"
								sortKey="bmId"
								activeKey={activeKey}
								activeOrder={activeOrder}
								onSortChange={onSortChange}
							/>
							<TableHead>Categories</TableHead>
							<SortableHeader
								label="Frequency"
								sortKey="highFrequency"
								activeKey={activeKey}
								activeOrder={activeOrder}
								defaultOrder="desc"
								onSortChange={onSortChange}
							/>
							<SortableHeader
								label="Last polled"
								sortKey="lastPolledAt"
								activeKey={activeKey}
								activeOrder={activeOrder}
								defaultOrder="desc"
								onSortChange={onSortChange}
							/>
							<SortableHeader
								label="Poll status"
								sortKey="pollStatus"
								activeKey={activeKey}
								activeOrder={activeOrder}
								defaultOrder="desc"
								onSortChange={onSortChange}
							/>
							<TableHead className="w-12" />
						</TableRow>
					</TableHeader>
					<TableBody>
						{items.length === 0 ? (
							<TableRow>
								<TableCell colSpan={8} className="h-24 text-center">
									No tracked servers found.
								</TableCell>
							</TableRow>
						) : (
							items.map((it) => {
								const hasError = it.lastPollError !== "";
								return (
									<TableRow
										key={it.bmId}
										className={cn(hasError && "bg-destructive/5")}
									>
										<TableCell>
											<Checkbox
												checked={selected.has(it.bmId)}
												onCheckedChange={() => onToggle(it.bmId)}
												aria-label={`Select ${it.name}`}
											/>
										</TableCell>
										<TableCell className="font-medium">{it.name}</TableCell>
										<TableCell>
											<span className="font-mono text-sm">{it.bmId}</span>
										</TableCell>
										<TableCell>
											<CategoryBadges server={it} />
										</TableCell>
										<TableCell>
											{it.highFrequency ? (
												<Badge variant="secondary">high freq</Badge>
											) : (
												<Badge variant="outline">standard</Badge>
											)}
										</TableCell>
										<TableCell>
											{it.lastPolledAt ? (
												<RelativeTime iso={it.lastPolledAt} />
											) : (
												<span className="text-xs text-muted-foreground">
													never
												</span>
											)}
										</TableCell>
										<TableCell>
											{hasError ? (
												<Tooltip>
													<TooltipTrigger asChild>
														<Badge variant="destructive" className="gap-1">
															<AlertTriangleIcon className="size-3" />
															error
														</Badge>
													</TooltipTrigger>
													<TooltipContent className="max-w-sm">
														<span className="font-mono text-xs">
															{it.lastPollError}
														</span>
													</TooltipContent>
												</Tooltip>
											) : (
												<Badge variant="secondary">healthy</Badge>
											)}
										</TableCell>
										<TableCell className="text-right">
											{canEdit ? (
												<DropdownMenu>
													<DropdownMenuTrigger asChild>
														<Button
															variant="ghost"
															size="icon"
															className="size-8"
															aria-label={`Actions for ${it.name}`}
														>
															<MoreHorizontalIcon className="size-4" />
														</Button>
													</DropdownMenuTrigger>
													<DropdownMenuContent align="end">
														<DropdownMenuItem onSelect={() => setEditTarget(it)}>
															<PencilIcon className="size-4" />
															Edit
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
			{editTarget ? (
				<BmServerDialog
					mode="edit"
					server={editTarget}
					open
					onOpenChange={(o) => {
						if (!o) setEditTarget(null);
					}}
					onSuccess={() => {
						setEditTarget(null);
						onComplete();
					}}
				/>
			) : null}
			{deleteTarget ? (
				<DeleteBmServerDialog
					server={deleteTarget}
					open
					onOpenChange={(o) => {
						if (!o) setDeleteTarget(null);
					}}
					onSuccess={() => {
						setDeleteTarget(null);
						onComplete();
					}}
				/>
			) : null}
		</div>
	);
}
