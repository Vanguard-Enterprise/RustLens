"use client";

import Link from "next/link";
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
import type { AdminUserItem } from "@/lib/rustlens-api";

type Props = {
	items: AdminUserItem[];
	selected: Set<string>;
	onToggle: (userId: string) => void;
	onToggleAll: () => void;
};

export function UsersTable({ items, selected, onToggle, onToggleAll }: Props) {
	const allOnPageSelected = items.length > 0 && items.every((it) => selected.has(it.user.id));
	return (
		<div className="rounded-md border">
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
						<TableHead>User</TableHead>
						<TableHead>Roles</TableHead>
						<TableHead>Status</TableHead>
						<TableHead>Last login</TableHead>
						<TableHead>Created</TableHead>
					</TableRow>
				</TableHeader>
				<TableBody>
					{items.map((it) => {
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
										{it.user.avatarUrl || it.discordAvatar ? (
											// eslint-disable-next-line @next/next/no-img-element
											<img
												src={it.user.avatarUrl || it.discordAvatar}
												alt=""
												className="size-7 rounded-full object-cover"
											/>
										) : (
											<div className="size-7 rounded-full bg-muted" />
										)}
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
													variant={r === "superadmin" ? "destructive" : "secondary"}
												>
													{r}
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
