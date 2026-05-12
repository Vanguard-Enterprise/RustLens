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
import type {
	AdminProxiesQuery,
	AdminProxyStatus,
} from "@/lib/rustlens-api";

type StatusOption = AdminProxyStatus | "all";

const STATUSES: StatusOption[] = ["all", "active", "paused", "broken"];

type Props = {
	query: AdminProxiesQuery;
	onChange: (next: Partial<AdminProxiesQuery>) => void;
};

export function ProxiesToolbar({ query, onChange }: Props) {
	const [text, setText] = React.useState(query.q ?? "");
	React.useEffect(() => {
		if ((text || undefined) === (query.q || undefined)) return;
		const id = setTimeout(() => {
			onChange({ q: text || undefined, page: 1 });
		}, 300);
		return () => clearTimeout(id);
		// eslint-disable-next-line react-hooks/exhaustive-deps
	}, [text]);

	const status: StatusOption = query.status ?? "all";
	const hasActiveFilters = status !== "all";

	return (
		<>
			<div className="relative w-full max-w-sm">
				<SearchIcon className="absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
				<Input
					type="search"
					placeholder="Search by endpoint"
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
									1
								</Badge>
							) : null}
						</Button>
					</PopoverTrigger>
					<PopoverContent align="start" className="w-72">
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
											onClick={() =>
												onChange({
													status: s === "all" ? undefined : s,
													page: 1,
												})
											}
										>
											{s}
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
						onClick={() => onChange({ status: undefined, page: 1 })}
					>
						<XIcon className="size-4" />
					</Button>
				) : null}
			</ButtonGroup>
		</>
	);
}
