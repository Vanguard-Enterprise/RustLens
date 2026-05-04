"use client";

import * as React from "react";
import { MoreHorizontalIcon, PencilIcon, EyeIcon, Trash2Icon } from "lucide-react";

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
import type { AdminRoleItem } from "@/lib/rustlens-api";

type Props = {
	roles: AdminRoleItem[];
	onEdit: (role: AdminRoleItem) => void;
	onDelete: (role: AdminRoleItem) => void;
};

export function RolesTable({ roles, onEdit, onDelete }: Props) {
	return (
		<div className="rounded-md border">
			<Table>
				<TableHeader>
					<TableRow>
						<TableHead>Name</TableHead>
						<TableHead>Key</TableHead>
						<TableHead>Description</TableHead>
						<TableHead>System</TableHead>
						<TableHead className="text-right">Permissions</TableHead>
						<TableHead className="text-right">Members</TableHead>
						<TableHead className="w-12" aria-label="Actions" />
					</TableRow>
				</TableHeader>
				<TableBody>
					{roles.map((r) => (
						<TableRow key={r.id}>
							<TableCell className="font-medium">{r.name}</TableCell>
							<TableCell>
								<code className="text-xs font-mono">{r.key}</code>
							</TableCell>
							<TableCell className="max-w-[36ch] truncate" title={r.description ?? ""}>
								{r.description}
							</TableCell>
							<TableCell>{r.isSystem ? <Badge variant="secondary">System</Badge> : null}</TableCell>
							<TableCell className="text-right tabular-nums">{r.permissions.length}</TableCell>
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
												<><EyeIcon className="mr-2 size-4" /> View</>
											) : (
												<><PencilIcon className="mr-2 size-4" /> Edit</>
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
					))}
				</TableBody>
			</Table>
		</div>
	);
}
