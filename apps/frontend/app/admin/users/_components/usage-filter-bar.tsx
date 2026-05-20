"use client";

import * as React from "react";
import { useRouter, useSearchParams, usePathname } from "next/navigation";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
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

function setOrDelete(sp: URLSearchParams, key: string, value: string | undefined) {
	if (value && value.length > 0) sp.set(key, value);
	else sp.delete(key);
	// Reset pagination on filter change.
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

	const activeChips: { key: string; label: string; clear: () => void }[] = [];
	if (outcomes.length > 0) {
		activeChips.push({
			key: "usageOutcome",
			label: `outcome: ${outcomes.join("|")}`,
			clear: () => apply((sp) => setOrDelete(sp, "usageOutcome", undefined)),
		});
	}
	if (interactionTypes.length > 0) {
		activeChips.push({
			key: "usageType",
			label: `type: ${interactionTypes.join("|")}`,
			clear: () => apply((sp) => setOrDelete(sp, "usageType", undefined)),
		});
	}
	if (command) activeChips.push({
		key: "usageCommand",
		label: `command: ${command}`,
		clear: () => apply((sp) => setOrDelete(sp, "usageCommand", undefined)),
	});
	if (guildId) {
		const g = filterOptions.guilds.find((x) => x.guildId === guildId);
		activeChips.push({
			key: "usageGuildId",
			label: `guild: ${g?.name || guildId}`,
			clear: () => apply((sp) => setOrDelete(sp, "usageGuildId", undefined)),
		});
	}
	if (windowDays !== "90") activeChips.push({
		key: "usageWindowDays",
		label: `last ${windowDays}d`,
		clear: () => apply((sp) => setOrDelete(sp, "usageWindowDays", undefined)),
	});

	return (
		<div className="flex flex-col gap-2">
			<div className="flex flex-wrap items-center gap-2">
				<Input
					value={draftQ}
					onChange={(e) => setDraftQ(e.target.value)}
					placeholder="Search command, target, recipient…"
					className="max-w-md"
					aria-label="Search usage"
				/>

				<ToggleGroup
					type="multiple"
					value={Array.from(outcomes)}
					onValueChange={(v) => apply((sp) => setOrDelete(sp, "usageOutcome", v.join(",") || undefined))}
					aria-label="Outcome filter"
				>
					{Array.from(OUTCOMES).map((o) => (
						<ToggleGroupItem key={o} value={o} className="capitalize">
							{o}
						</ToggleGroupItem>
					))}
				</ToggleGroup>

				<Select
					value={interactionTypes[0] ?? "all"}
					onValueChange={(v) => apply((sp) => setOrDelete(sp, "usageType", v === "all" ? undefined : v))}
				>
					<SelectTrigger className="w-[160px]" aria-label="Interaction type">
						<SelectValue placeholder="Type" />
					</SelectTrigger>
					<SelectContent>
						<SelectItem value="all">All types</SelectItem>
						{Array.from(INTERACTION_TYPES).map((t) => (
							<SelectItem key={t} value={t}>{t}</SelectItem>
						))}
					</SelectContent>
				</Select>

				<Select
					value={command || "all"}
					onValueChange={(v) => apply((sp) => setOrDelete(sp, "usageCommand", v === "all" ? undefined : v))}
				>
					<SelectTrigger className="w-[200px]" aria-label="Command">
						<SelectValue placeholder="Command" />
					</SelectTrigger>
					<SelectContent>
						<SelectItem value="all">All commands</SelectItem>
						{filterOptions.commands.map((c) => (
							<SelectItem key={c} value={c}>/{c}</SelectItem>
						))}
					</SelectContent>
				</Select>

				<Select
					value={guildId || "all"}
					onValueChange={(v) => apply((sp) => setOrDelete(sp, "usageGuildId", v === "all" ? undefined : v))}
				>
					<SelectTrigger className="w-[220px]" aria-label="Guild">
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

				<Select
					value={windowDays}
					onValueChange={(v) => apply((sp) => setOrDelete(sp, "usageWindowDays", v === "90" ? undefined : v))}
				>
					<SelectTrigger className="w-[120px]" aria-label="Window">
						<SelectValue />
					</SelectTrigger>
					<SelectContent>
						{Array.from(WINDOWS).map((w) => (
							<SelectItem key={w} value={w}>Last {w}d</SelectItem>
						))}
					</SelectContent>
				</Select>
			</div>

			{activeChips.length > 0 && (
				<div className="flex flex-wrap items-center gap-2 text-xs">
					{activeChips.map((c) => (
						<button
							key={c.key}
							onClick={c.clear}
							className="inline-flex items-center gap-1 rounded-full bg-muted px-2 py-0.5 hover:bg-muted/80"
							type="button"
						>
							{c.label} <span aria-hidden>×</span>
						</button>
					))}
					<Button
						variant="link"
						size="sm"
						className="h-auto px-1 text-xs"
						onClick={() => apply((sp) => {
							for (const k of ["usageQ","usageOutcome","usageType","usageCommand","usageGuildId","usageWindowDays"]) {
								sp.delete(k);
							}
							sp.delete("usagePage");
						})}
					>
						Clear all
					</Button>
				</div>
			)}
		</div>
	);
}
