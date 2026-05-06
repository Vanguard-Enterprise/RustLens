import { AppSidebar } from "../_components/app-sidebar";
import { AuditPage } from "./_components/audit-page";
import { SiteHeader } from "@/components/site-header";
import { SidebarInset, SidebarProvider } from "@/components/ui/sidebar";
import { getCsrfToken } from "@/lib/csrf";
import {
	getAdminAudit,
	getAdminAuditEvent,
	getAdminAuditEventTypes,
	getAdminUser,
	getCurrentUserSafe,
	type AdminAuditQuery,
	type AuditEventItem,
} from "@/lib/rustlens-api";
import { requirePermission } from "@/lib/session";

const DEFAULT_PAGE_SIZE = 50;
const PAGE_SIZE_OPTIONS = [50, 100, 200];

type SincePreset = "24h" | "7d" | "30d" | "all";

function pickStr(v: string | string[] | undefined): string | undefined {
	if (Array.isArray(v)) return v[0];
	return v;
}
function pickArr(v: string | string[] | undefined): string[] {
	if (Array.isArray(v)) return v;
	if (typeof v === "string" && v !== "") return [v];
	return [];
}
function parsePositiveInt(v: string | undefined, fallback: number): number {
	if (!v) return fallback;
	const n = Number.parseInt(v, 10);
	return Number.isFinite(n) && n > 0 ? n : fallback;
}

function presetToSince(preset: SincePreset): string | undefined {
	if (preset === "all") return undefined;
	const now = Date.now();
	const ms =
		preset === "24h" ? 24 * 60 * 60 * 1000 :
		preset === "7d" ? 7 * 24 * 60 * 60 * 1000 :
		30 * 24 * 60 * 60 * 1000;
	return new Date(now - ms).toISOString();
}

async function tryGetUserChip(id: string | undefined) {
	if (!id) return null;
	try {
		const d = await getAdminUser(id);
		return {
			id: d.user.id,
			displayName: d.user.displayName,
			avatarUrl: d.user.avatarUrl ?? null,
		};
	} catch {
		return null;
	}
}

export default async function AdminAuditRoute({
	searchParams,
}: {
	searchParams: Promise<Record<string, string | string[] | undefined>>;
}) {
	const session = await requirePermission("audit.read");
	const sp = await searchParams;

	const presetRaw = pickStr(sp.since);
	const preset: SincePreset =
		presetRaw === "24h" || presetRaw === "30d" || presetRaw === "all" ? presetRaw : "7d";
	const result = pickStr(sp.result);
	const resultFiltered: "success" | "error" | undefined =
		result === "success" || result === "error" ? result : undefined;

	const requestedSize = parsePositiveInt(pickStr(sp.pageSize), DEFAULT_PAGE_SIZE);
	const pageSize = PAGE_SIZE_OPTIONS.includes(requestedSize) ? requestedSize : DEFAULT_PAGE_SIZE;

	const initialQuery = {
		q: pickStr(sp.q),
		since: preset,
		result: resultFiltered,
		eventType: pickArr(sp.eventType),
		actorUserId: pickStr(sp.actorUserId),
		targetUserId: pickStr(sp.targetUserId),
		pageSize,
		before: pickStr(sp.before),
		cursors: pickArr(sp.cursors).filter((c) => c.length > 0),
		event: pickStr(sp.event),
	};

	const apiQuery: AdminAuditQuery = {
		targetUserId: initialQuery.targetUserId,
		actorUserId: initialQuery.actorUserId,
		eventType: initialQuery.eventType,
		result: initialQuery.result,
		since: presetToSince(preset),
		before: initialQuery.before,
		pageSize: initialQuery.pageSize,
	};

	const [csrfToken, currentUser, page, eventTypes, actorChip, targetChip] = await Promise.all([
		getCsrfToken(),
		getCurrentUserSafe(),
		getAdminAudit(apiQuery),
		getAdminAuditEventTypes(),
		tryGetUserChip(initialQuery.actorUserId),
		tryGetUserChip(initialQuery.targetUserId),
	]);

	let initialEvent: AuditEventItem | null = null;
	let initialEventStatus: "ok" | "not-found" | "absent" = "absent";
	if (initialQuery.event) {
		const inList = page.items.find((it) => it.id === initialQuery.event);
		if (inList) {
			initialEvent = inList;
			initialEventStatus = "ok";
		} else {
			try {
				initialEvent = await getAdminAuditEvent(initialQuery.event);
				initialEventStatus = "ok";
			} catch {
				initialEventStatus = "not-found";
			}
		}
	}

	const user = {
		displayName: session.displayName,
		avatarUrl: session.avatarUrl || undefined,
		discordUsername: currentUser?.user.discordUsername,
		isInternalAdmin:
			(session.adminRoles?.length ?? 0) > 0 ||
			session.roles.includes("internal_admin"),
	};

	return (
		<SidebarProvider
			style={
				{
					"--sidebar-width": "calc(var(--spacing) * 72)",
					"--header-height": "calc(var(--spacing) * 12)",
				} as React.CSSProperties
			}
		>
			<AppSidebar
				csrfToken={csrfToken}
				user={user}
				permissions={session.permissions ?? []}
				variant="inset"
			/>
			<SidebarInset>
				<SiteHeader />
				<div className="flex flex-1 flex-col">
					<div className="@container/main flex flex-1 flex-col gap-2">
						<div className="flex flex-col gap-4 py-4 md:gap-6 md:py-6 px-4 lg:px-6">
							<div>
								<h1 className="text-2xl font-semibold">Audit</h1>
								<p className="text-sm text-muted-foreground">
									Browse admin actions and system audit events.
								</p>
							</div>
							<AuditPage
								initialPage={page}
								eventTypes={eventTypes}
								initialEvent={initialEvent}
								initialEventStatus={initialEventStatus}
								initialQuery={initialQuery}
								initialActor={actorChip}
								initialTarget={targetChip}
							/>
						</div>
					</div>
				</div>
			</SidebarInset>
		</SidebarProvider>
	);
}
