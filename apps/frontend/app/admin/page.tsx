import {
	Card,
	CardContent,
	CardDescription,
	CardHeader,
	CardTitle,
} from "@/components/ui/card";
import {
	getAdminGuilds,
	getAdminServers,
	getAdminUsers,
} from "@/lib/rustlens-api";

// Auth is enforced by `app/admin/layout.tsx` via `requireAdminSession`; no
// duplicate check here.
export default async function AdminPage() {
	const [users, guilds, servers] = await Promise.all([
		getAdminUsers(),
		getAdminGuilds(),
		getAdminServers(),
	]);

	return (
		<div className="grid gap-4 md:grid-cols-3">
			{[
				{ label: "Users", value: users.total },
				{ label: "Guilds", value: guilds.length },
				{ label: "Tracked servers", value: servers.length },
			].map((item) => (
				<Card key={item.label}>
					<CardHeader>
						<CardTitle className="text-base">{item.label}</CardTitle>
						<CardDescription>Current internal inventory</CardDescription>
					</CardHeader>
					<CardContent>
						<p className="font-heading text-3xl">{item.value}</p>
					</CardContent>
				</Card>
			))}
		</div>
	);
}
