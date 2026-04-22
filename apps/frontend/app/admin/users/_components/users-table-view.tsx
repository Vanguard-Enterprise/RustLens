"use client";

import Link from "next/link";
import { useMemo, useState } from "react";
import {
	Table,
	TableBody,
	TableCell,
	TableHead,
	TableHeader,
	TableRow,
} from "@/components/ui/table";
import { Badge } from "@/components/ui/badge";
import type { AdminUserItem } from "@/lib/rustlens-api";
import { UsersBulkToolbar } from "./users-bulk-toolbar";

export interface UsersTableViewProps {
	items: AdminUserItem[];
	roleOptions: { value: string; label: string }[];
	csrfToken: string;
	currentQuery: string; // querystring including current filters (no ids)
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
		<div>
			<UsersBulkToolbar
				selected={Array.from(selected)}
				roleOptions={roleOptions}
				csrfToken={csrfToken}
				currentQuery={currentQuery}
				onClear={() => setSelected(new Set())}
			/>
			<Table>
				<TableHeader>
					<TableRow>
						<TableHead className="w-[2rem]">
							<input
								type="checkbox"
								aria-label="Select all on page"
								checked={allChecked}
								ref={(el) => {
									if (el) el.indeterminate = !allChecked && someChecked;
								}}
								onChange={toggleAll}
							/>
						</TableHead>
						<TableHead>User</TableHead>
						<TableHead>Email</TableHead>
						<TableHead>Roles</TableHead>
						<TableHead>Status</TableHead>
						<TableHead>Discord</TableHead>
						<TableHead>Steam</TableHead>
						<TableHead>Guilds</TableHead>
						<TableHead>Last login</TableHead>
					</TableRow>
				</TableHeader>
				<TableBody>
					{items.map((u) => (
						<TableRow key={u.user.id}>
							<TableCell>
								<input
									type="checkbox"
									aria-label={`Select ${u.user.displayName || u.user.id}`}
									checked={selected.has(u.user.id)}
									onChange={() => toggleOne(u.user.id)}
								/>
							</TableCell>
							<TableCell>
								<Link
									href={`/admin/users/${u.user.id}`}
									className="underline-offset-2 hover:underline"
								>
									{u.user.displayName || u.user.id}
								</Link>
							</TableCell>
							<TableCell>
								{u.primaryEmail || (
									<span className="text-muted-foreground">—</span>
								)}
								{u.primaryEmail && !u.emailVerified && (
									<Badge variant="secondary" className="ml-2">
										unverified
									</Badge>
								)}
							</TableCell>
							<TableCell className="flex flex-wrap gap-1">
								{u.adminRoles.length === 0 ? (
									<span className="text-muted-foreground">—</span>
								) : (
									u.adminRoles.map((r) => <Badge key={r}>{r}</Badge>)
								)}
							</TableCell>
							<TableCell>
								{u.deletedAt ? (
									<Badge variant="destructive">deleted</Badge>
								) : u.suspendedAt ? (
									<Badge variant="destructive">suspended</Badge>
								) : (
									<Badge variant="secondary">active</Badge>
								)}
							</TableCell>
							<TableCell>
								{u.discordUsername || u.discordId || "—"}
							</TableCell>
							<TableCell>{u.steamConnectionCount}</TableCell>
							<TableCell>{u.guildCount}</TableCell>
							<TableCell>
								{u.user.lastLoginAt?.slice(0, 10) || "—"}
							</TableCell>
						</TableRow>
					))}
					{items.length === 0 && (
						<TableRow>
							<TableCell
								colSpan={9}
								className="text-center text-muted-foreground py-8"
							>
								No users match the current filters.
							</TableCell>
						</TableRow>
					)}
				</TableBody>
			</Table>
		</div>
	);
}
