"use client";

import Link from "next/link";
import { useMemo, useState } from "react";
import { UserRoundIcon, UsersIcon } from "lucide-react";
import {
	Table,
	TableBody,
	TableCell,
	TableHead,
	TableHeader,
	TableRow,
} from "@/components/ui/table";
import { Badge } from "@/components/ui/badge";
import { Checkbox } from "@/components/ui/checkbox";
import {
	Avatar,
	AvatarFallback,
	AvatarImage,
} from "@/components/ui/avatar";
import {
	Empty,
	EmptyDescription,
	EmptyHeader,
	EmptyMedia,
	EmptyTitle,
} from "@/components/ui/empty";
import {
	Tooltip,
	TooltipContent,
	TooltipTrigger,
} from "@/components/ui/tooltip";
import type { AdminUserItem } from "@/lib/rustlens-api";
import { UsersBulkToolbar } from "./users-bulk-toolbar";

export interface UsersTableViewProps {
	items: AdminUserItem[];
	roleOptions: { value: string; label: string }[];
	csrfToken: string;
	currentQuery: string; // querystring including current filters (no ids)
}

function initialsFor(name: string): string {
	const trimmed = name.trim();
	if (!trimmed) return "?";
	const parts = trimmed.split(/\s+/).filter(Boolean);
	const letters = parts.length === 1 ? parts[0] : `${parts[0][0]}${parts[parts.length - 1][0]}`;
	return letters.slice(0, 2).toUpperCase();
}

function statusFor(u: AdminUserItem) {
	if (u.deletedAt) return { label: "Deleted", variant: "destructive" as const };
	if (u.suspendedAt) return { label: "Suspended", variant: "destructive" as const };
	return { label: "Active", variant: "secondary" as const };
}

export function UsersTableView({
	items,
	roleOptions,
	csrfToken,
	currentQuery,
}: UsersTableViewProps) {
	const [selected, setSelected] = useState<Set<string>>(new Set());

	const pageIds = useMemo(() => items.map((it) => it.user.id), [items]);
	const allChecked = pageIds.length > 0 && pageIds.every((id) => selected.has(id));
	const someChecked = pageIds.some((id) => selected.has(id));
	const headerState: boolean | "indeterminate" = allChecked
		? true
		: someChecked
			? "indeterminate"
			: false;

	const toggleAll = () => {
		const next = new Set(selected);
		if (allChecked) {
			for (const id of pageIds) next.delete(id);
		} else {
			for (const id of pageIds) next.add(id);
		}
		setSelected(next);
	};

	const toggleOne = (id: string) => {
		const next = new Set(selected);
		if (next.has(id)) next.delete(id);
		else next.add(id);
		setSelected(next);
	};

	return (
		<div className="space-y-3">
			<UsersBulkToolbar
				selected={Array.from(selected)}
				roleOptions={roleOptions}
				csrfToken={csrfToken}
				currentQuery={currentQuery}
				onClear={() => setSelected(new Set())}
			/>

			{items.length === 0 ? (
				<Empty>
					<EmptyHeader>
						<EmptyMedia variant="icon">
							<UsersIcon />
						</EmptyMedia>
						<EmptyTitle>No users match these filters</EmptyTitle>
						<EmptyDescription>
							Try adjusting the search query, status, or role filters above.
						</EmptyDescription>
					</EmptyHeader>
				</Empty>
			) : (
				<div className="overflow-hidden rounded-xl ring-1 ring-foreground/10">
					<Table>
						<TableHeader>
							<TableRow>
								<TableHead className="w-10">
									<Checkbox
										aria-label="Select all on page"
										checked={headerState}
										onCheckedChange={toggleAll}
									/>
								</TableHead>
								<TableHead>User</TableHead>
								<TableHead>Email</TableHead>
								<TableHead>Roles</TableHead>
								<TableHead>Status</TableHead>
								<TableHead>Discord</TableHead>
								<TableHead className="text-right">Steam</TableHead>
								<TableHead className="text-right">Guilds</TableHead>
								<TableHead>Last login</TableHead>
							</TableRow>
						</TableHeader>
						<TableBody>
							{items.map((u) => {
								const status = statusFor(u);
								const isSelected = selected.has(u.user.id);
								const displayName = u.user.displayName || u.user.id;
								return (
									<TableRow
										key={u.user.id}
										data-state={isSelected ? "selected" : undefined}
									>
										<TableCell className="w-10">
											<Checkbox
												aria-label={`Select ${displayName}`}
												checked={isSelected}
												onCheckedChange={() => toggleOne(u.user.id)}
											/>
										</TableCell>
										<TableCell>
											<div className="flex items-center gap-2.5 min-w-0">
												<Avatar size="sm">
													{u.user.avatarUrl ? (
														<AvatarImage
															src={u.user.avatarUrl}
															alt=""
														/>
													) : null}
													<AvatarFallback>
														{u.user.avatarUrl ? (
															<UserRoundIcon className="size-3" />
														) : (
															initialsFor(displayName)
														)}
													</AvatarFallback>
												</Avatar>
												<div className="flex min-w-0 flex-col">
													<Link
														href={`/admin/users/${u.user.id}`}
														className="truncate font-medium text-foreground underline-offset-2 hover:underline"
													>
														{displayName}
													</Link>
													<span
														className="truncate font-mono text-[11px] text-muted-foreground"
														title={u.user.id}
													>
														{u.user.id.slice(0, 8)}…
													</span>
												</div>
											</div>
										</TableCell>
										<TableCell>
											{u.primaryEmail ? (
												<div className="flex items-center gap-2">
													<span className="truncate">{u.primaryEmail}</span>
													{!u.emailVerified ? (
														<Badge variant="outline">unverified</Badge>
													) : null}
												</div>
											) : (
												<span className="text-muted-foreground">—</span>
											)}
										</TableCell>
										<TableCell>
											{u.adminRoles.length === 0 ? (
												<span className="text-muted-foreground">—</span>
											) : (
												<div className="flex flex-wrap gap-1">
													{u.adminRoles.map((r) => (
														<Badge key={r} variant="secondary">
															{r}
														</Badge>
													))}
												</div>
											)}
										</TableCell>
										<TableCell>
											<Badge variant={status.variant}>{status.label}</Badge>
										</TableCell>
										<TableCell>
											{u.discordUsername || u.discordId ? (
												<Tooltip>
													<TooltipTrigger asChild>
														<span className="block max-w-[12rem] truncate">
															{u.discordUsername || u.discordId}
														</span>
													</TooltipTrigger>
													<TooltipContent>
														{u.discordUsername
															? `${u.discordUsername} · ${u.discordId}`
															: u.discordId}
													</TooltipContent>
												</Tooltip>
											) : (
												<span className="text-muted-foreground">—</span>
											)}
										</TableCell>
										<TableCell className="text-right tabular-nums">
											{u.steamConnectionCount}
										</TableCell>
										<TableCell className="text-right tabular-nums">
											{u.guildCount}
										</TableCell>
										<TableCell className="whitespace-nowrap text-muted-foreground">
											{u.user.lastLoginAt?.slice(0, 10) || "—"}
										</TableCell>
									</TableRow>
								);
							})}
						</TableBody>
					</Table>
				</div>
			)}
		</div>
	);
}
