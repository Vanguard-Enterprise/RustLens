"use client";

import * as React from "react";
import { FilterIcon, SearchIcon, XIcon } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ButtonGroup } from "@/components/ui/button-group";
import { Input } from "@/components/ui/input";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";

import { UserCombobox, type UserComboboxValue } from "./user-combobox";

export type SincePreset = "24h" | "7d" | "30d" | "all";

export type AuditToolbarQuery = {
	q: string | undefined;
	since: SincePreset;
	result: "success" | "error" | undefined;
	eventType: string[];
	actor: UserComboboxValue | null;
	target: UserComboboxValue | null;
};

export type AuditToolbarPatch = Partial<{
	q: string | undefined;
	since: SincePreset;
	result: "success" | "error" | undefined;
	eventType: string[];
	actorUserId: string | undefined;
	targetUserId: string | undefined;
	actorLabel: UserComboboxValue | null;
	targetLabel: UserComboboxValue | null;
}>;

type Props = {
	query: AuditToolbarQuery;
	eventTypes: string[];
	onChange: (patch: AuditToolbarPatch) => void;
};

const PRESETS: { key: SincePreset; label: string }[] = [
	{ key: "24h", label: "Last 24h" },
	{ key: "7d", label: "Last 7d" },
	{ key: "30d", label: "Last 30d" },
	{ key: "all", label: "All time" },
];

const RESULTS: Array<{ key: "any" | "success" | "error"; label: string }> = [
	{ key: "any", label: "Any" },
	{ key: "success", label: "Success" },
	{ key: "error", label: "Error" },
];

export function AuditToolbar({ query, eventTypes, onChange }: Props) {
	const [text, setText] = React.useState(query.q ?? "");
	React.useEffect(() => {
		const id = setTimeout(() => onChange({ q: text || undefined }), 300);
		return () => clearTimeout(id);
		// eslint-disable-next-line react-hooks/exhaustive-deps
	}, [text]);

	const activeFilterCount =
		(query.since !== "7d" ? 1 : 0) +
		(query.result ? 1 : 0) +
		(query.actor ? 1 : 0) +
		(query.target ? 1 : 0) +
		query.eventType.length;
	const hasActiveFilters = activeFilterCount > 0;

	const toggleEventType = (t: string) => {
		const set = new Set(query.eventType);
		if (set.has(t)) set.delete(t);
		else set.add(t);
		onChange({ eventType: Array.from(set) });
	};

	const currentResult = query.result ?? "any";

	return (
		<div className="flex flex-wrap items-center gap-2">
			<div className="relative w-full max-w-sm">
				<SearchIcon className="absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
				<Input
					type="search"
					placeholder="Search reason or event type"
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
					<PopoverContent align="start" className="w-96">
						<div className="flex flex-col gap-3">
							<div className="flex flex-col gap-1.5">
								<span className="text-xs font-medium uppercase text-muted-foreground">Time</span>
								<div className="flex flex-wrap gap-1">
									{PRESETS.map((p) => (
										<Button
											key={p.key}
											variant={query.since === p.key ? "default" : "outline"}
											size="sm"
											onClick={() => onChange({ since: p.key })}
										>
											{p.label}
										</Button>
									))}
								</div>
							</div>

							<div className="flex flex-col gap-1.5">
								<span className="text-xs font-medium uppercase text-muted-foreground">Result</span>
								<div className="flex flex-wrap gap-1">
									{RESULTS.map((r) => (
										<Button
											key={r.key}
											variant={currentResult === r.key ? "default" : "outline"}
											size="sm"
											onClick={() => onChange({ result: r.key === "any" ? undefined : r.key })}
										>
											{r.label}
										</Button>
									))}
								</div>
							</div>

							<div className="flex flex-col gap-1.5">
								<span className="text-xs font-medium uppercase text-muted-foreground">Actor</span>
								<UserCombobox
									label="Actor"
									value={query.actor}
									onSelect={(v) => onChange({ actorUserId: v.id, actorLabel: v })}
									onClear={() => onChange({ actorUserId: undefined, actorLabel: null })}
								/>
							</div>

							<div className="flex flex-col gap-1.5">
								<span className="text-xs font-medium uppercase text-muted-foreground">Target</span>
								<UserCombobox
									label="Target"
									value={query.target}
									onSelect={(v) => onChange({ targetUserId: v.id, targetLabel: v })}
									onClear={() => onChange({ targetUserId: undefined, targetLabel: null })}
								/>
							</div>

							<div className="flex flex-col gap-1.5">
								<div className="flex items-center justify-between">
									<span className="text-xs font-medium uppercase text-muted-foreground">Event types</span>
									{query.eventType.length > 0 ? (
										<Button
											variant="ghost"
											size="sm"
											className="h-auto px-1 py-0 text-xs"
											onClick={() => onChange({ eventType: [] })}
										>
											Clear
										</Button>
									) : null}
								</div>
								<div className="flex flex-wrap gap-1">
									{eventTypes.length === 0 ? (
										<span className="text-xs text-muted-foreground">No event types yet.</span>
									) : (
										eventTypes.map((t) => (
											<Button
												key={t}
												variant={query.eventType.includes(t) ? "default" : "outline"}
												size="sm"
												onClick={() => toggleEventType(t)}
											>
												{t}
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
						onClick={() =>
							onChange({
								since: "7d",
								result: undefined,
								eventType: [],
								actorUserId: undefined,
								targetUserId: undefined,
								actorLabel: null,
								targetLabel: null,
							})
						}
					>
						<XIcon className="size-4" />
					</Button>
				) : null}
			</ButtonGroup>
		</div>
	);
}
