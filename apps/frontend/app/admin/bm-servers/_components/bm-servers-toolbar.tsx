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
import type { AdminBmServersQuery } from "@/lib/rustlens-api";

type TriOption = "all" | "true" | "false";

const TRI: TriOption[] = ["all", "true", "false"];

function triLabel(v: TriOption, yes: string, no: string): string {
	if (v === "true") return yes;
	if (v === "false") return no;
	return "any";
}

function toTri(value: boolean | undefined): TriOption {
	if (value === undefined) return "all";
	return value ? "true" : "false";
}

function fromTri(value: TriOption): boolean | undefined {
	if (value === "all") return undefined;
	return value === "true";
}

type Props = {
	query: AdminBmServersQuery;
	onChange: (next: Partial<AdminBmServersQuery>) => void;
};

export function BmServersToolbar({ query, onChange }: Props) {
	const [text, setText] = React.useState(query.q ?? "");
	React.useEffect(() => {
		if ((text || undefined) === (query.q || undefined)) return;
		const id = setTimeout(() => {
			onChange({ q: text || undefined, page: 1 });
		}, 300);
		return () => clearTimeout(id);
		// eslint-disable-next-line react-hooks/exhaustive-deps
	}, [text]);

	const highFrequency = toTri(query.highFrequency);
	const hasError = toTri(query.hasError);

	const activeCount =
		(highFrequency !== "all" ? 1 : 0) + (hasError !== "all" ? 1 : 0);
	const hasActiveFilters = activeCount > 0;

	return (
		<>
			<div className="relative w-full max-w-sm">
				<SearchIcon className="absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
				<Input
					type="search"
					placeholder="Search by name or BM ID"
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
									{activeCount}
								</Badge>
							) : null}
						</Button>
					</PopoverTrigger>
					<PopoverContent align="start" className="w-72">
						<div className="flex flex-col gap-3">
							<div className="flex flex-col gap-1.5">
								<span className="text-xs font-medium uppercase text-muted-foreground">
									High frequency
								</span>
								<div className="flex flex-wrap gap-1">
									{TRI.map((v) => (
										<Button
											key={v}
											variant={highFrequency === v ? "default" : "outline"}
											size="sm"
											onClick={() =>
												onChange({
													highFrequency: fromTri(v),
													page: 1,
												})
											}
										>
											{triLabel(v, "high freq", "standard")}
										</Button>
									))}
								</div>
							</div>
							<div className="flex flex-col gap-1.5">
								<span className="text-xs font-medium uppercase text-muted-foreground">
									Poll status
								</span>
								<div className="flex flex-wrap gap-1">
									{TRI.map((v) => (
										<Button
											key={v}
											variant={hasError === v ? "default" : "outline"}
											size="sm"
											onClick={() =>
												onChange({
													hasError: fromTri(v),
													page: 1,
												})
											}
										>
											{triLabel(v, "error", "healthy")}
										</Button>
									))}
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
						onClick={() =>
							onChange({
								highFrequency: undefined,
								hasError: undefined,
								page: 1,
							})
						}
					>
						<XIcon className="size-4" />
					</Button>
				) : null}
			</ButtonGroup>
		</>
	);
}
