"use client";

import { ArrowDownIcon, ArrowUpDownIcon, ArrowUpIcon } from "lucide-react";

import { Button } from "@/components/ui/button";
import { TableHead } from "@/components/ui/table";

export type SortDir = "asc" | "desc";

type SortHeaderProps<K extends string> = {
	label:       string;
	sortKey:     K;
	currentKey:  K;
	currentDir:  SortDir;
	onSort:      (key: K) => void;
	className?:  string;
	align?:      "left" | "right";
};

export function SortHeader<K extends string>({
	label,
	sortKey,
	currentKey,
	currentDir,
	onSort,
	className,
	align = "left",
}: SortHeaderProps<K>) {
	const active = currentKey === sortKey;
	const Icon = active
		? currentDir === "asc"
			? ArrowUpIcon
			: ArrowDownIcon
		: ArrowUpDownIcon;
	return (
		<TableHead className={className}>
			<Button
				variant="ghost"
				size="sm"
				onClick={() => onSort(sortKey)}
				className={`-mx-2 h-auto px-2 py-1 font-medium ${
					align === "right" ? "ml-auto" : ""
				}`}
			>
				{label}
				<Icon
					className={`ml-1 size-3.5 ${active ? "" : "text-muted-foreground/60"}`}
				/>
			</Button>
		</TableHead>
	);
}
