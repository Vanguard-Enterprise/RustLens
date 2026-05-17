"use client";

import Link from "next/link";
import { ArrowDownIcon, ArrowUpDownIcon, ArrowUpIcon } from "lucide-react";
import * as React from "react";

import { Badge } from "@/components/ui/badge";
import { Checkbox } from "@/components/ui/checkbox";
import { RelativeTime } from "@/components/admin/relative-time";
import {
	Table,
	TableBody,
	TableCell,
	TableHead,
	TableHeader,
	TableRow,
} from "@/components/ui/table";
import { cn } from "@/lib/utils";
import type { AdminUserItem } from "@/lib/rustlens-api";
import { userAvatarUrl } from "@/lib/discord-cdn";

type SortableField = "displayName" | "lastLoginAt" | "createdAt";

type Props = {
	items: AdminUserItem[];
	selected: Set<string>;
	onToggle: (userId: string) => void;
	onToggleAll: () => void;
	sort: string | undefined;
	onSortChange: (sort: string) => void;
};

function parseSort(sort: string | undefined): { field: string; desc: boolean } {
	const value = sort ?? "-lastLoginAt";
	if (value.startsWith("-")) return { field: value.slice(1), desc: true };
	return { field: value, desc: false };
}

function SortableHeader({
	field,
	label,
	sort,
	onSortChange,
	className,
}: {
	field: SortableField;
	label: string;
	sort: string | undefined;
	onSortChange: (sort: string) => void;
	className?: string;
}) {
	const { field: activeField, desc } = parseSort(sort);
	const isActive = activeField === field;
	const Icon = !isActive ? ArrowUpDownIcon : desc ? ArrowDownIcon : ArrowUpIcon;
	const nextSort = !isActive
		? `-${field}`
		: desc
			? field
			: `-${field}`;
	return (
		<TableHead className={className}>
			<button
				type="button"
				onClick={() => onSortChange(nextSort)}
				className={cn(
					"-mx-2 inline-flex items-center gap-1 rounded px-2 py-1 hover:bg-accent hover:text-accent-foreground",
					isActive ? "text-foreground" : "text-muted-foreground",
				)}
				aria-label={`Sort by ${label}`}
			>
				{label}
				<Icon className="size-3.5" />
			</button>
		</TableHead>
	);
}

export function UsersTable({ items, selected, onToggle, onToggleAll, sort, onSortChange }: Props) {
	const allOnPageSelected = items.length > 0 && items.every((it) => selected.has(it.user.id));
	return (
		<div className="overflow-hidden rounded-lg border">
			<Table>
				<TableHeader>
					<TableRow>
						<TableHead className="w-10">
							<Checkbox
								checked={allOnPageSelected}
								onCheckedChange={onToggleAll}
								aria-label="Select all on page"
							/>
						</TableHead>
						<SortableHeader
							field="displayName"
							label="User"
							sort={sort}
							onSortChange={onSortChange}
						/>
						<TableHead>Roles</TableHead>
						<TableHead>Status</TableHead>
						<SortableHeader
							field="lastLoginAt"
							label="Last login"
							sort={sort}
							onSortChange={onSortChange}
						/>
						<SortableHeader
							field="createdAt"
							label="Created"
							sort={sort}
							onSortChange={onSortChange}
						/>
					</TableRow>
				</TableHeader>
				<TableBody>
					{items.length === 0 ? (
						<TableRow>
							<TableCell colSpan={6} className="h-24 text-center">
								No users found.
							</TableCell>
						</TableRow>
					) : items.map((it) => {
						const status = it.deletedAt ? "deleted" : it.suspendedAt ? "suspended" : "active";
						return (
							<TableRow key={it.user.id}>
								<TableCell>
									<Checkbox
										checked={selected.has(it.user.id)}
										onCheckedChange={() => onToggle(it.user.id)}
										aria-label={`Select ${it.user.displayName}`}
									/>
								</TableCell>
								<TableCell>
									<Link
										href={`/admin/users/${it.user.id}`}
										className="flex items-center gap-2 hover:underline"
									>
										{(() => {
											const avatarSrc = it.user.avatarUrl || (it.discordId ? userAvatarUrl(it.discordId, it.discordAvatar ?? null) : null) || null;
											return avatarSrc ? (
												// eslint-disable-next-line @next/next/no-img-element
												<img
													src={avatarSrc}
													alt=""
													className="size-7 rounded-full object-cover"
												/>
											) : (
												<div className="size-7 rounded-full bg-muted" />
											);
										})()}
										<div className="flex flex-col">
											<span className="font-medium">{it.user.displayName}</span>
											<span className="text-xs text-muted-foreground">
												@{it.discordUsername || "—"}
											</span>
										</div>
									</Link>
								</TableCell>
								<TableCell>
									<div className="flex flex-wrap gap-1">
										{it.adminRoles.length === 0 ? (
											<span className="text-xs text-muted-foreground">—</span>
										) : (
											it.adminRoles.map((r) => (
												<Badge
													key={r}
													variant={r === "superadmin" ? "destructive" : r === "pro" ? "outline" : "secondary"}
													className={
														r === "pro"
															? "border-yellow-500/20 bg-yellow-500/10 text-yellow-700 hover:bg-yellow-500/15 dark:text-yellow-300"
															: undefined
													}
												>
													{r === "pro" ? "PRO" : r}
												</Badge>
											))
										)}
									</div>
								</TableCell>
								<TableCell>
									<Badge
										variant={
											status === "deleted"
												? "destructive"
												: status === "suspended"
													? "outline"
													: "secondary"
										}
									>
										{status}
									</Badge>
								</TableCell>
								<TableCell>
									<RelativeTime iso={it.user.lastLoginAt} />
								</TableCell>
								<TableCell>
									<RelativeTime iso={it.user.createdAt} />
								</TableCell>
							</TableRow>
						);
					})}
				</TableBody>
			</Table>
		</div>
	);
}
