import {
	Table,
	TableBody,
	TableCell,
	TableHead,
	TableHeader,
	TableRow,
} from "@/components/ui/table";
import { getAdminGuilds } from "@/lib/rustlens-api";
import { requireInternalAdminSession } from "@/lib/session";

export default async function AdminGuildsPage() {
	await requireInternalAdminSession();
	const guilds = await getAdminGuilds();

	return (
		<Table>
			<TableHeader>
				<TableRow>
					<TableHead>Guild</TableHead>
					<TableHead>Active members</TableHead>
					<TableHead>Linked members</TableHead>
					<TableHead>Tracked servers</TableHead>
					<TableHead>Admin role</TableHead>
				</TableRow>
			</TableHeader>
			<TableBody>
				{guilds.map((guild) => (
					<TableRow key={guild.guildId}>
						<TableCell>{guild.guildId}</TableCell>
						<TableCell>{guild.activeMembers}</TableCell>
						<TableCell>{guild.linkedMembers}</TableCell>
						<TableCell>{guild.trackedServers}</TableCell>
						<TableCell>{guild.adminRoleId || "Unset"}</TableCell>
					</TableRow>
				))}
			</TableBody>
		</Table>
	);
}
