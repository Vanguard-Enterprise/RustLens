import {
	Table,
	TableBody,
	TableCell,
	TableHead,
	TableHeader,
	TableRow,
} from "@/components/ui/table";
import { getAuditEvents } from "@/lib/rustlens-api";
import { requirePermission } from "@/lib/session";

export default async function AdminAuditPage() {
	await requirePermission("audit.read");
	const events = await getAuditEvents();

	return (
		<Table>
			<TableHeader>
				<TableRow>
					<TableHead>Event</TableHead>
					<TableHead>Actor</TableHead>
					<TableHead>Target</TableHead>
					<TableHead>Created</TableHead>
				</TableRow>
			</TableHeader>
			<TableBody>
				{events.map((event) => (
					<TableRow key={event.id}>
						<TableCell>{event.eventType}</TableCell>
						<TableCell>{event.actorUserId || "System"}</TableCell>
						<TableCell>{event.targetUserId || "None"}</TableCell>
						<TableCell>{event.createdAt}</TableCell>
					</TableRow>
				))}
			</TableBody>
		</Table>
	);
}
