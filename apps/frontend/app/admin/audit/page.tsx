import { ADMIN_ACTIONS } from "@/components/admin/action-manifest";
import { RelativeTime } from "@/components/admin/relative-time";
import { UserLink } from "@/components/admin/user-link";
import { Badge } from "@/components/ui/badge";
import {
	Table,
	TableBody,
	TableCell,
	TableHead,
	TableHeader,
	TableRow,
} from "@/components/ui/table";
import type { AuditEventItem } from "@/lib/rustlens-api";
import { getAuditEvents } from "@/lib/rustlens-api";
import { requirePermission } from "@/lib/session";

type BadgeVariant = "default" | "destructive" | "outline" | "secondary";

function prettify(eventType: string): string {
	return eventType
		.split(".")
		.map((segment) =>
			segment.length === 0
				? segment
				: segment[0].toUpperCase() + segment.slice(1).replace(/_/g, " "),
		)
		.join(" / ");
}

function resolveAction(event: AuditEventItem): {
	label: string;
	variant: BadgeVariant;
	detail: string | null;
	isError: boolean;
} {
	const entry = ADMIN_ACTIONS[event.eventType as keyof typeof ADMIN_ACTIONS];
	const isError = event.result === "error";

	let variant: BadgeVariant;
	let label: string;
	if (entry) {
		label = entry.label;
		if (isError) {
			variant = "destructive";
		} else if (entry.dangerous) {
			variant = "destructive";
		} else {
			variant = "default";
		}
	} else {
		label = prettify(event.eventType);
		variant = isError ? "destructive" : "outline";
	}

	const detail = event.reason ?? entry?.resourceType ?? null;
	return { label, variant, detail, isError };
}

export default async function AdminAuditPage() {
	await requirePermission("audit.read");
	const events = await getAuditEvents();

	return (
		<div className="space-y-4 p-4">
			<div>
				<h1 className="text-2xl font-semibold">Audit log</h1>
				<p className="text-sm text-muted-foreground">
					Most recent admin actions across the platform.
				</p>
			</div>
			<Table>
				<TableHeader>
					<TableRow>
						<TableHead>Event</TableHead>
						<TableHead>Actor</TableHead>
						<TableHead>Target</TableHead>
						<TableHead className="text-right">When</TableHead>
					</TableRow>
				</TableHeader>
				<TableBody>
					{events.length === 0 ? (
						<TableRow>
							<TableCell
								colSpan={4}
								className="text-center text-muted-foreground"
							>
								No audit events yet.
							</TableCell>
						</TableRow>
					) : (
						events.map((event) => {
							const action = resolveAction(event);
							return (
								<TableRow key={event.id}>
									<TableCell className="max-w-xs align-top">
										<div className="flex flex-col gap-1">
											<Badge variant={action.variant} className="w-fit">
												{action.isError ? "✗ " : ""}
												{action.label}
											</Badge>
											{action.detail ? (
												<span
													title={action.detail}
													className="line-clamp-1 text-xs text-muted-foreground"
												>
													{action.detail}
												</span>
											) : null}
										</div>
									</TableCell>
									<TableCell className="align-top">
										<UserLink
											userId={event.actorUserId || null}
											displayName={event.actorDisplayName}
											avatarUrl={event.actorAvatarUrl}
										/>
									</TableCell>
									<TableCell className="align-top">
										{event.targetUserId ? (
											<UserLink
												userId={event.targetUserId}
												displayName={event.targetDisplayName}
												avatarUrl={event.targetAvatarUrl}
											/>
										) : (
											<span className="text-muted-foreground">—</span>
										)}
									</TableCell>
									<TableCell className="align-top text-right text-sm text-muted-foreground">
										<RelativeTime iso={event.createdAt} />
									</TableCell>
								</TableRow>
							);
						})
					)}
				</TableBody>
			</Table>
		</div>
	);
}
