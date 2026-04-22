import {
	Table,
	TableBody,
	TableCell,
	TableHead,
	TableHeader,
	TableRow,
} from "@/components/ui/table";
import { getAdminServers } from "@/lib/rustlens-api";
import { requireAdminSession } from "@/lib/session";

export default async function AdminServersPage() {
	await requireAdminSession();
	const servers = await getAdminServers();

	return (
		<Table>
			<TableHeader>
				<TableRow>
					<TableHead>Server</TableHead>
					<TableHead>BattleMetrics ID</TableHead>
					<TableHead>Guild count</TableHead>
					<TableHead>Last poll</TableHead>
					<TableHead>Poll error</TableHead>
				</TableRow>
			</TableHeader>
			<TableBody>
				{servers.map((server) => (
					<TableRow key={server.serverBmId}>
						<TableCell>{server.name}</TableCell>
						<TableCell>{server.serverBmId}</TableCell>
						<TableCell>{server.guildCount}</TableCell>
						<TableCell>{server.lastPolledAt ?? "Never"}</TableCell>
						<TableCell>{server.lastPollError || "None"}</TableCell>
					</TableRow>
				))}
			</TableBody>
		</Table>
	);
}
