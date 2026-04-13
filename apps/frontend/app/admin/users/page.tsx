import {
	Table,
	TableBody,
	TableCell,
	TableHead,
	TableHeader,
	TableRow,
} from "@/components/ui/table";
import { getAdminUsers } from "@/lib/rustlens-api";
import { requireInternalAdminSession } from "@/lib/session";

export default async function AdminUsersPage() {
	await requireInternalAdminSession();
	const users = await getAdminUsers();

	return (
		<Table>
			<TableHeader>
				<TableRow>
					<TableHead>User</TableHead>
					<TableHead>Email</TableHead>
					<TableHead>Discord</TableHead>
					<TableHead>Guilds</TableHead>
					<TableHead>Steam links</TableHead>
				</TableRow>
			</TableHeader>
			<TableBody>
				{users.map((user) => (
					<TableRow key={user.user.id}>
						<TableCell>{user.user.displayName}</TableCell>
						<TableCell>{user.primaryEmail || "No email"}</TableCell>
						<TableCell>{user.discordUsername || user.discordId || "None"}</TableCell>
						<TableCell>{user.guildCount}</TableCell>
						<TableCell>{user.steamConnectionCount}</TableCell>
					</TableRow>
				))}
			</TableBody>
		</Table>
	);
}
