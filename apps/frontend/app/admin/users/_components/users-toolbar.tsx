"use client";

import { FilterIcon, SearchIcon, XIcon } from "lucide-react";
import * as React from "react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ButtonGroup } from "@/components/ui/button-group";
import { Input } from "@/components/ui/input";
import {
	Popover,
	PopoverContent,
	PopoverTrigger,
} from "@/components/ui/popover";
import type { AdminRoleItem, AdminUsersQuery } from "@/lib/rustlens-api";

type Props = {
	query: AdminUsersQuery;
	roles: AdminRoleItem[];
	onChange: (next: Partial<AdminUsersQuery>) => void;
};

const STATUSES: Array<NonNullable<AdminUsersQuery["status"]>> = [
	"active",
	"suspended",
	"deleted",
	"any",
];

export function UsersToolbar({ query, roles, onChange }: Props) {
	const [text, setText] = React.useState(query.q ?? "");
	React.useEffect(() => {
		const id = setTimeout(() => {
			onChange({ q: text || undefined, page: 1 });
		}, 300);
		return () => clearTimeout(id);
		// eslint-disable-next-line react-hooks/exhaustive-deps
	}, [text]);

	const [selectedRoles, setSelectedRoles] = React.useState<Set<string>>(
		() => new Set(query.role ?? []),
	);
	const queryRoleKey = (query.role ?? []).slice().sort().join(",");
	React.useEffect(() => {
		setSelectedRoles(new Set(query.role ?? []));
		// eslint-disable-next-line react-hooks/exhaustive-deps
	}, [queryRoleKey]);
	const toggleRole = (key: string) => {
		const next = new Set(selectedRoles);
		if (next.has(key)) next.delete(key);
		else next.add(key);
		setSelectedRoles(next);
		onChange({ role: Array.from(next), page: 1 });
	};

	const status = query.status ?? "active";
	const activeFilterCount = (status !== "active" ? 1 : 0) + selectedRoles.size;
	const hasActiveFilters = activeFilterCount > 0;

	return (
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
			<ButtonGroup>
				<Popover>
					<PopoverTrigger asChild>
						<Button variant="outline" size="sm" className="gap-2">
							<FilterIcon className="size-4" />
							Filters
							{hasActiveFilters ? (
								<Badge variant="secondary" className="ml-1 px-1.5">
									{activeFilterCount}
								</Badge>
							) : null}
						</Button>
					</PopoverTrigger>
				<PopoverContent align="start" className="w-80">
					<div className="flex flex-col gap-3">
						<div className="flex flex-col gap-1.5">
							<span className="text-xs font-medium uppercase text-muted-foreground">
								Status
							</span>
							<div className="flex flex-wrap gap-1">
								{STATUSES.map((s) => (
									<Button
										key={s}
										variant={status === s ? "default" : "outline"}
										size="sm"
										onClick={() => onChange({ status: s, page: 1 })}
									>
										{s}
									</Button>
								))}
							</div>
						</div>
						<div className="flex flex-col gap-1.5">
							<div className="flex items-center justify-between">
								<span className="text-xs font-medium uppercase text-muted-foreground">
									Roles
								</span>
								{selectedRoles.size > 0 ? (
									<Button
										variant="ghost"
										size="sm"
										className="h-auto px-1 py-0 text-xs"
										onClick={() => onChange({ role: [], page: 1 })}
									>
										Clear
									</Button>
								) : null}
							</div>
							<div className="flex flex-wrap gap-1">
								{roles.length === 0 ? (
									<span className="text-xs text-muted-foreground">No roles available.</span>
								) : (
									roles.map((r) => (
										<Button
											key={r.key}
											variant={selectedRoles.has(r.key) ? "default" : "outline"}
											size="sm"
											onClick={() => toggleRole(r.key)}
										>
											{r.name}
										</Button>
									))
								)}
							</div>
						</div>
					</div>
				</PopoverContent>
				</Popover>
				{hasActiveFilters ? (
					<Button
						variant="outline"
						size="sm"
						className="px-2"
						aria-label="Clear filters"
						title="Clear filters"
						onClick={() => onChange({ status: "active", role: [], page: 1 })}
					>
						<XIcon className="size-4" />
					</Button>
				) : null}
			</ButtonGroup>
		</div>
	);
}
