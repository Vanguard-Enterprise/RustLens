"use client";

import * as React from "react";
import {
	ArrowDownIcon,
	ArrowUpDownIcon,
	ArrowUpIcon,
	EyeIcon,
	MoreHorizontalIcon,
	PencilIcon,
	Trash2Icon,
} from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
	Table,
	TableBody,
	TableCell,
	TableHead,
	TableHeader,
	TableRow,
} from "@/components/ui/table";
import { cn } from "@/lib/utils";
import type { AdminRoleItem } from "@/lib/rustlens-api";

type SortableField = "name" | "key" | "permissions" | "memberCount";

type Props = {
	roles: AdminRoleItem[];
	sort: string | undefined;
	onSortChange: (sort: string) => void;
	onEdit: (role: AdminRoleItem) => void;
	onDelete: (role: AdminRoleItem) => void;
};

function parseSort(sort: string | undefined): { field: string; desc: boolean } {
	const value = sort ?? "name";
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
	const nextSort = !isActive ? `-${field}` : desc ? field : `-${field}`;
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

function roleBadgeStyle(color?: string | null): React.CSSProperties | undefined {
	if (!color) return undefined;
	return {
		backgroundColor: color,
		borderColor: color,
		color: "#422006",
	};
}

export function RolesTable({ roles, sort, onSortChange, onEdit, onDelete }: Props) {
	return (
		<div className="overflow-hidden rounded-lg border">
			<Table>
				<TableHeader>
					<TableRow>
						<SortableHeader
							field="name"
							label="Name"
							sort={sort}
							onSortChange={onSortChange}
						/>
						<SortableHeader
							field="key"
							label="Key"
							sort={sort}
							onSortChange={onSortChange}
						/>
						<TableHead>Description</TableHead>
						<TableHead>System</TableHead>
						<SortableHeader
							field="permissions"
							label="Permissions"
							sort={sort}
							onSortChange={onSortChange}
							className="text-right"
						/>
						<SortableHeader
							field="memberCount"
							label="Members"
							sort={sort}
							onSortChange={onSortChange}
							className="text-right"
						/>
						<TableHead className="w-12" aria-label="Actions" />
					</TableRow>
				</TableHeader>
				<TableBody>
					{roles.length === 0 ? (
						<TableRow>
							<TableCell colSpan={7} className="h-24 text-center">
								No roles found.
							</TableCell>
						</TableRow>
					) : (
						roles.map((r) => (
							<TableRow key={r.id}>
								<TableCell className="font-medium">
									<Badge
										variant={r.color ? "outline" : "secondary"}
										style={roleBadgeStyle(r.color)}
									>
										{r.name}
									</Badge>
								</TableCell>
								<TableCell>
									<code className="text-xs font-mono">{r.key}</code>
								</TableCell>
								<TableCell className="max-w-[36ch] truncate" title={r.description ?? ""}>
									{r.description}
								</TableCell>
								<TableCell>
									{r.isSystem ? <Badge variant="secondary">System</Badge> : null}
								</TableCell>
								<TableCell className="text-right tabular-nums">
									{r.permissions.length}
								</TableCell>
								<TableCell className="text-right tabular-nums">{r.memberCount}</TableCell>
								<TableCell>
									<DropdownMenu>
										<DropdownMenuTrigger asChild>
											<Button variant="ghost" size="icon" aria-label={`Actions for ${r.name}`}>
												<MoreHorizontalIcon />
											</Button>
										</DropdownMenuTrigger>
										<DropdownMenuContent align="end">
											<DropdownMenuItem onClick={() => onEdit(r)}>
												{r.isSystem ? (
													<>
														<EyeIcon className="mr-2 size-4" /> View
													</>
												) : (
													<>
														<PencilIcon className="mr-2 size-4" /> Edit
													</>
												)}
											</DropdownMenuItem>
											{r.isSystem ? null : (
												<DropdownMenuItem
													onClick={() => onDelete(r)}
													className="text-destructive"
												>
													<Trash2Icon className="mr-2 size-4" /> Delete
												</DropdownMenuItem>
											)}
										</DropdownMenuContent>
									</DropdownMenu>
								</TableCell>
							</TableRow>
						))
					)}
				</TableBody>
			</Table>
		</div>
	);
}
