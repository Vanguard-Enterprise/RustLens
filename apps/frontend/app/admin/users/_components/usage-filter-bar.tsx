"use client";

import { FilterIcon, SearchIcon, XIcon } from "lucide-react";
import * as React from "react";
import { useRouter, useSearchParams, usePathname } from "next/navigation";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ButtonGroup } from "@/components/ui/button-group";
import { Input } from "@/components/ui/input";
import {
	Popover, PopoverContent, PopoverTrigger,
} from "@/components/ui/popover";
import {
	Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@/components/ui/select";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import type { AdminCommandUsageFilterOptions } from "@/lib/rustlens-api";

type Props = {
	filterOptions: AdminCommandUsageFilterOptions;
};

const OUTCOMES = ["success", "error", "banned", "denied"] as const;
const INTERACTION_TYPES = ["command", "button", "modal", "string_select"] as const;
const WINDOWS = ["1", "7", "30", "90"] as const;

const FILTER_KEYS = [
	"usageQ",
	"usageOutcome",
	"usageType",
	"usageCommand",
	"usageGuildId",
	"usageWindowDays",
] as const;

function setOrDelete(sp: URLSearchParams, key: string, value: string | undefined) {
	if (value && value.length > 0) sp.set(key, value);
	else sp.delete(key);
	sp.delete("usagePage");
}

export function UsageFilterBar({ filterOptions }: Props) {
	const router = useRouter();
	const pathname = usePathname();
	const params = useSearchParams();
	const [draftQ, setDraftQ] = React.useState(params.get("usageQ") ?? "");

	const apply = React.useCallback(
		(updater: (sp: URLSearchParams) => void) => {
			const sp = new URLSearchParams(params.toString());
			updater(sp);
			router.replace(`${pathname}?${sp.toString()}#usage`, { scroll: false });
		},
		[params, pathname, router],
	);

	React.useEffect(() => {
		const t = setTimeout(() => {
			if ((params.get("usageQ") ?? "") === draftQ) return;
			apply((sp) => setOrDelete(sp, "usageQ", draftQ || undefined));
		}, 250);
		return () => clearTimeout(t);
	}, [draftQ, apply, params]);

	const outcomeRaw = params.get("usageOutcome");
	const outcomes = outcomeRaw ? outcomeRaw.split(",") : [];
	const interactionRaw = params.get("usageType");
	const interactionTypes = interactionRaw ? interactionRaw.split(",") : [];
	const command = params.get("usageCommand") ?? "";
	const guildId = params.get("usageGuildId") ?? "";
	const windowDays = params.get("usageWindowDays") ?? "90";

	const activeFilterCount =
		(outcomes.length > 0 ? 1 : 0) +
		(interactionTypes.length > 0 ? 1 : 0) +
		(command ? 1 : 0) +
		(guildId ? 1 : 0) +
		(windowDays !== "90" ? 1 : 0);
	const hasActiveFilters = activeFilterCount > 0;

	const clearAll = () => {
		setDraftQ("");
		apply((sp) => {
			for (const k of FILTER_KEYS) sp.delete(k);
			sp.delete("usagePage");
		});
	};

	return (
		<div className="flex flex-1 items-center gap-2">
			<div className="relative w-full max-w-xs flex-1">
				<SearchIcon className="absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
				<Input
					type="search"
					value={draftQ}
					onChange={(e) => setDraftQ(e.target.value)}
					placeholder="Search command, target, recipient…"
					className="pl-8"
					aria-label="Search usage"
				/>
			</div>
			<ButtonGroup>
				<Popover>
					<PopoverTrigger asChild>
						<Button variant="outline" className="gap-2">
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
								<span className="text-xs font-medium uppercase text-muted-foreground">
									Outcome
								</span>
								<ToggleGroup
									type="multiple"
									value={Array.from(outcomes)}
									onValueChange={(v) =>
										apply((sp) => setOrDelete(sp, "usageOutcome", v.join(",") || undefined))
									}
									aria-label="Outcome filter"
									className="justify-start"
								>
									{Array.from(OUTCOMES).map((o) => (
										<ToggleGroupItem key={o} value={o} className="capitalize">
											{o}
										</ToggleGroupItem>
									))}
								</ToggleGroup>
							</div>

							<div className="flex flex-col gap-1.5">
								<span className="text-xs font-medium uppercase text-muted-foreground">
									Type
								</span>
								<Select
									value={interactionTypes[0] ?? "all"}
									onValueChange={(v) =>
										apply((sp) => setOrDelete(sp, "usageType", v === "all" ? undefined : v))
									}
								>
									<SelectTrigger aria-label="Interaction type">
										<SelectValue placeholder="Type" />
									</SelectTrigger>
									<SelectContent>
										<SelectItem value="all">All types</SelectItem>
										{Array.from(INTERACTION_TYPES).map((t) => (
											<SelectItem key={t} value={t}>{t}</SelectItem>
										))}
									</SelectContent>
								</Select>
							</div>

							<div className="flex flex-col gap-1.5">
								<span className="text-xs font-medium uppercase text-muted-foreground">
									Command
								</span>
								<Select
									value={command || "all"}
									onValueChange={(v) =>
										apply((sp) => setOrDelete(sp, "usageCommand", v === "all" ? undefined : v))
									}
								>
									<SelectTrigger aria-label="Command">
										<SelectValue placeholder="Command" />
									</SelectTrigger>
									<SelectContent>
										<SelectItem value="all">All commands</SelectItem>
										{filterOptions.commands.map((c) => (
											<SelectItem key={c} value={c}>/{c}</SelectItem>
										))}
									</SelectContent>
								</Select>
							</div>

							<div className="flex flex-col gap-1.5">
								<span className="text-xs font-medium uppercase text-muted-foreground">
									Guild
								</span>
								<Select
									value={guildId || "all"}
									onValueChange={(v) =>
										apply((sp) => setOrDelete(sp, "usageGuildId", v === "all" ? undefined : v))
									}
								>
									<SelectTrigger aria-label="Guild">
										<SelectValue placeholder="Guild" />
									</SelectTrigger>
									<SelectContent>
										<SelectItem value="all">All guilds</SelectItem>
										{filterOptions.guilds.map((g) => (
											<SelectItem key={g.guildId} value={g.guildId}>
												{g.name || g.guildId}
											</SelectItem>
										))}
									</SelectContent>
								</Select>
							</div>

							<div className="flex flex-col gap-1.5">
								<span className="text-xs font-medium uppercase text-muted-foreground">
									Window
								</span>
								<Select
									value={windowDays}
									onValueChange={(v) =>
										apply((sp) => setOrDelete(sp, "usageWindowDays", v === "90" ? undefined : v))
									}
								>
									<SelectTrigger aria-label="Window">
										<SelectValue />
									</SelectTrigger>
									<SelectContent>
										{Array.from(WINDOWS).map((w) => (
											<SelectItem key={w} value={w}>Last {w}d</SelectItem>
										))}
									</SelectContent>
								</Select>
							</div>
						</div>
					</PopoverContent>
				</Popover>
				{hasActiveFilters ? (
					<Button
						variant="outline"
						className="px-2"
						aria-label="Clear filters"
						title="Clear filters"
						onClick={clearAll}
					>
						<XIcon className="size-4" />
					</Button>
				) : null}
			</ButtonGroup>
		</div>
	);
}
