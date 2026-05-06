"use client";

import * as React from "react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import {
	ChevronLeftIcon,
	ChevronRightIcon,
	FileIcon,
} from "lucide-react";

import { Button } from "@/components/ui/button";
import {
	Select,
	SelectContent,
	SelectGroup,
	SelectItem,
	SelectTrigger,
	SelectValue,
} from "@/components/ui/select";
import {
	getAdminAuditEvent,
	type AdminAuditPage as AdminAuditPageData,
	type AuditEventItem,
} from "@/lib/rustlens-api";

import { AuditDetailSheet } from "./audit-detail-sheet";
import { AuditTable } from "./audit-table";
import {
	AuditToolbar,
	type AuditToolbarPatch,
	type AuditToolbarQuery,
	type SincePreset,
} from "./audit-toolbar";
import type { UserComboboxValue } from "./user-combobox";

const PAGE_SIZE_OPTIONS = [50, 100, 200];

export type AuditInitialQuery = {
	q: string | undefined;
	since: SincePreset;
	result: "success" | "error" | undefined;
	eventType: string[];
	actorUserId: string | undefined;
	targetUserId: string | undefined;
	pageSize: number;
	before: string | undefined;
	event: string | undefined;
};

type Props = {
	initialPage: AdminAuditPageData;
	eventTypes: string[];
	initialEvent: AuditEventItem | null;
	initialEventStatus: "ok" | "not-found" | "absent";
	initialQuery: AuditInitialQuery;
	initialActor: UserComboboxValue | null;
	initialTarget: UserComboboxValue | null;
};

function applyClientSearch(items: AuditEventItem[], q: string | undefined): AuditEventItem[] {
	if (!q) return items;
	const needle = q.toLowerCase();
	return items.filter((it) =>
		it.eventType.toLowerCase().includes(needle) ||
		(it.reason ?? "").toLowerCase().includes(needle),
	);
}

export function AuditPage({
	initialPage,
	eventTypes,
	initialEvent,
	initialEventStatus,
	initialQuery,
	initialActor,
	initialTarget,
}: Props) {
	const router = useRouter();
	const pathname = usePathname();
	const searchParams = useSearchParams();

	const [actor, setActor] = React.useState<UserComboboxValue | null>(initialActor);
	const [target, setTarget] = React.useState<UserComboboxValue | null>(initialTarget);
	const [sheetEvent, setSheetEvent] = React.useState<AuditEventItem | null>(initialEvent);
	const [sheetStatus, setSheetStatus] = React.useState<"ok" | "not-found" | "loading" | "closed">(
		initialQuery.event ? (initialEventStatus === "ok" ? "ok" : "not-found") : "closed",
	);

	const updateQuery = React.useCallback(
		(patch: Record<string, unknown>) => {
			const sp = new URLSearchParams(searchParams.toString());
			const apply = (key: string, val: unknown) => {
				if (Array.isArray(val)) {
					sp.delete(key);
					for (const v of val) sp.append(key, String(v));
				} else if (val === undefined || val === null || val === "") {
					sp.delete(key);
				} else {
					sp.set(key, String(val));
				}
			};
			for (const [k, v] of Object.entries(patch)) apply(k, v);
			router.push(`${pathname}?${sp.toString()}`);
		},
		[pathname, router, searchParams],
	);

	const handleToolbarChange = (patch: AuditToolbarPatch) => {
		const out: Record<string, unknown> = { before: undefined };
		if ("q" in patch) out.q = patch.q;
		if ("since" in patch) out.since = patch.since;
		if ("result" in patch) out.result = patch.result;
		if ("eventType" in patch) out.eventType = patch.eventType;
		if ("actorUserId" in patch) out.actorUserId = patch.actorUserId;
		if ("targetUserId" in patch) out.targetUserId = patch.targetUserId;
		updateQuery(out);
		if ("actorLabel" in patch) setActor(patch.actorLabel ?? null);
		if ("targetLabel" in patch) setTarget(patch.targetLabel ?? null);
	};

	const toolbarQuery: AuditToolbarQuery = {
		q: initialQuery.q,
		since: initialQuery.since,
		result: initialQuery.result,
		eventType: initialQuery.eventType,
		actor,
		target,
	};

	const items = applyClientSearch(initialPage.items, initialQuery.q);
	const hasFilters =
		initialQuery.since !== "7d" ||
		!!initialQuery.result ||
		initialQuery.eventType.length > 0 ||
		!!initialQuery.actorUserId ||
		!!initialQuery.targetUserId ||
		!!initialQuery.q;

	const openSheet = async (id: string) => {
		const inList = initialPage.items.find((it) => it.id === id) ?? null;
		if (inList) {
			setSheetEvent(inList);
			setSheetStatus("ok");
		} else {
			setSheetEvent(null);
			setSheetStatus("loading");
			try {
				const e = await getAdminAuditEvent(id);
				setSheetEvent(e);
				setSheetStatus("ok");
			} catch {
				setSheetStatus("not-found");
			}
		}
		updateQuery({ event: id });
	};

	const closeSheet = () => {
		setSheetEvent(null);
		setSheetStatus("closed");
		updateQuery({ event: undefined });
	};

	const clearFilters = () => {
		updateQuery({
			q: undefined,
			since: "7d",
			result: undefined,
			eventType: [],
			actorUserId: undefined,
			targetUserId: undefined,
			before: undefined,
		});
		setActor(null);
		setTarget(null);
	};

	const goNewer = () => updateQuery({ before: undefined });
	const goOlder = () => {
		if (initialPage.nextCursor) updateQuery({ before: initialPage.nextCursor });
	};
	const canNewer = !!initialQuery.before;
	const canOlder = !!initialPage.nextCursor;

	return (
		<div className="flex flex-col gap-4">
			<AuditToolbar query={toolbarQuery} eventTypes={eventTypes} onChange={handleToolbarChange} />
			<AuditTable
				items={items}
				selectedId={initialQuery.event ?? null}
				onSelect={openSheet}
				hasFilters={hasFilters}
				onClearFilters={clearFilters}
			/>
			<div className="flex items-center gap-4">
				<div className="flex flex-1 items-center">
					<Select
						value={String(initialQuery.pageSize)}
						onValueChange={(value) =>
							updateQuery({ pageSize: Number.parseInt(value, 10), before: undefined })
						}
					>
						<SelectTrigger size="sm" className="w-20" id="rows-per-page" aria-label="Rows per page">
							<FileIcon className="size-4 text-muted-foreground" />
							<SelectValue placeholder={initialQuery.pageSize} />
						</SelectTrigger>
						<SelectContent side="top">
							<SelectGroup>
								{PAGE_SIZE_OPTIONS.map((size) => (
									<SelectItem key={size} value={String(size)}>
										{size}
									</SelectItem>
								))}
							</SelectGroup>
						</SelectContent>
					</Select>
				</div>
				<div className="flex items-center gap-2">
					<Button variant="outline" size="sm" onClick={goNewer} disabled={!canNewer}>
						<ChevronLeftIcon className="size-4" />
						Newer
					</Button>
					<Button variant="outline" size="sm" onClick={goOlder} disabled={!canOlder}>
						Older
						<ChevronRightIcon className="size-4" />
					</Button>
				</div>
				<div className="flex flex-1 justify-end text-sm text-muted-foreground">
					Showing {items.length} {items.length === 1 ? "event" : "events"}
				</div>
			</div>
			<AuditDetailSheet
				event={sheetEvent}
				status={sheetStatus === "closed" ? "loading" : sheetStatus}
				onClose={closeSheet}
			/>
		</div>
	);
}
