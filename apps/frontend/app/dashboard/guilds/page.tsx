import {
	Card,
	CardContent,
	CardDescription,
	CardHeader,
	CardTitle,
} from "@/components/ui/card";
import { getCurrentUser } from "@/lib/rustlens-api";
import { requireSession } from "@/lib/session";

export default async function DashboardGuildsPage() {
	await requireSession();
	const currentUser = await getCurrentUser();

	return (
		<Card>
			<CardHeader>
				<CardTitle>Guild memberships</CardTitle>
				<CardDescription>
					Memberships synced from the bot-side guild tracking table.
				</CardDescription>
			</CardHeader>
			<CardContent className="flex flex-col gap-2">
				{currentUser.guilds.length ? (
					currentUser.guilds.map((guild) => (
						<div
							className="rounded-md border px-3 py-2"
							key={`${guild.guildId}-${guild.lastSeenAt}`}
						>
							<p>{guild.guildId}</p>
							<p className="text-muted-foreground text-sm">
								{guild.isActive ? "Active member" : "Inactive member"}
							</p>
						</div>
					))
				) : (
					<p className="text-muted-foreground text-sm">
						No guild memberships have been recorded for this account yet.
					</p>
				)}
			</CardContent>
		</Card>
	);
}
