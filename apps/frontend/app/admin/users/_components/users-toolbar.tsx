"use client";

import { SearchIcon } from "lucide-react";
import * as React from "react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
	Select,
	SelectContent,
	SelectItem,
	SelectTrigger,
	SelectValue,
} from "@/components/ui/select";
import type { AdminRoleItem, AdminUsersQuery } from "@/lib/rustlens-api";

type Props = {
	query: AdminUsersQuery;
	roles: AdminRoleItem[];
	totalRows: number;
	exportUrl: string | null;
	onChange: (next: Partial<AdminUsersQuery>) => void;
};

const STATUSES: Array<NonNullable<AdminUsersQuery["status"]>> = [
	"active",
	"suspended",
	"deleted",
	"any",
];

const SORTS: Array<{ value: string; label: string }> = [
	{ value: "-lastLoginAt", label: "Last login (newest)" },
	{ value: "lastLoginAt", label: "Last login (oldest)" },
	{ value: "-createdAt", label: "Created (newest)" },
	{ value: "createdAt", label: "Created (oldest)" },
	{ value: "displayName", label: "Display name (A→Z)" },
	{ value: "-displayName", label: "Display name (Z→A)" },
];

export function UsersToolbar({ query, roles, totalRows, exportUrl, onChange }: Props) {
	const [text, setText] = React.useState(query.q ?? "");
	React.useEffect(() => {
		const id = setTimeout(() => {
			onChange({ q: text || undefined, page: 1 });
		}, 300);
		return () => clearTimeout(id);
		// eslint-disable-next-line react-hooks/exhaustive-deps
	}, [text]);

	const selectedRoles = new Set(query.role ?? []);
	const toggleRole = (key: string) => {
		const next = new Set(selectedRoles);
		if (next.has(key)) next.delete(key);
		else next.add(key);
		onChange({ role: Array.from(next), page: 1 });
	};

	return (
		<div className="flex flex-col gap-2">
			<div className="flex flex-wrap items-center gap-2">
				<div className="relative w-full max-w-sm">
					<SearchIcon className="absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
					<Input
						type="search"
						placeholder="Search by name, Discord, or ID"
						value={text}
						onChange={(e) => setText(e.target.value)}
						className="pl-8"
					/>
				</div>
				<div className="flex items-center gap-1">
					{STATUSES.map((s) => (
						<Button
							key={s}
							variant={query.status === s ? "default" : "outline"}
							size="sm"
							onClick={() => onChange({ status: s, page: 1 })}
						>
							{s}
						</Button>
					))}
				</div>
				<Select value={query.sort ?? "-lastLoginAt"} onValueChange={(v) => onChange({ sort: v })}>
					<SelectTrigger className="w-56">
						<SelectValue placeholder="Sort" />
					</SelectTrigger>
					<SelectContent>
						{SORTS.map((s) => (
							<SelectItem key={s.value} value={s.value}>
								{s.label}
							</SelectItem>
						))}
					</SelectContent>
				</Select>
				<div className="ml-auto">
					{exportUrl ? (
						<a href={exportUrl} download>
							<Button variant="outline" size="sm">
								Export filtered ({totalRows})
							</Button>
						</a>
					) : null}
				</div>
			</div>
			<div className="flex flex-wrap items-center gap-1">
				<span className="text-xs text-muted-foreground">Roles:</span>
				{roles.map((r) => (
					<Badge
						key={r.key}
						variant={selectedRoles.has(r.key) ? "default" : "outline"}
						className="cursor-pointer"
						onClick={() => toggleRole(r.key)}
					>
						{r.name}
					</Badge>
				))}
				{selectedRoles.size > 0 ? (
					<Button variant="ghost" size="sm" onClick={() => onChange({ role: [], page: 1 })}>
						Clear
					</Button>
				) : null}
			</div>
		</div>
	);
}
