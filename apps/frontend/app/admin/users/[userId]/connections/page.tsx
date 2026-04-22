import { Badge } from "@/components/ui/badge";
import { getCsrfToken } from "@/lib/csrf";
import { getAdminUser } from "@/lib/rustlens-api";
import { requireAdminSession, requirePermission } from "@/lib/session";
import { UnlinkConnectionButton } from "../_components/unlink-connection-button";

export default async function UserConnectionsPage({
	params,
}: {
	params: Promise<{ userId: string }>;
}) {
	await requirePermission("users.read");
	const { userId } = await params;
	const [user, session, csrfToken] = await Promise.all([
		getAdminUser(userId),
		requireAdminSession(),
		getCsrfToken(),
	]);
	const canManage = (session.permissions ?? []).includes(
		"users.connections.manage",
	);

	const discord = user.connections.filter((c) => c.provider === "discord");
	const steam = user.connections.filter((c) => c.provider === "steam");

	return (
		<div className="grid gap-6">
			<section>
				<h2 className="mb-2 text-lg font-semibold">Discord</h2>
				{discord.length === 0 ? (
					<p className="text-sm text-muted-foreground">No Discord connection.</p>
				) : (
					<ul className="grid gap-2">
						{discord.map((c) => (
							<li
								key={`${c.provider}:${c.providerAccountId}`}
								className="flex items-start justify-between rounded border p-3 text-sm"
							>
								<div>
									<div className="font-medium">
										{c.username || c.providerAccountId}
									</div>
									<div className="text-muted-foreground">
										ID: {c.providerAccountId}
									</div>
									<div className="text-muted-foreground">
										Connected {c.connectedAt?.slice(0, 10)}
									</div>
								</div>
								{canManage ? (
									<UnlinkConnectionButton
										userId={user.user.id}
										provider="discord"
										providerAccountId={c.providerAccountId}
										csrfToken={csrfToken}
									/>
								) : null}
							</li>
						))}
					</ul>
				)}
			</section>

			<section>
				<h2 className="mb-2 text-lg font-semibold">Steam</h2>
				{steam.length === 0 ? (
					<p className="text-sm text-muted-foreground">No Steam connections.</p>
				) : (
					<ul className="grid gap-2">
						{steam.map((c) => (
							<li
								key={`${c.provider}:${c.providerAccountId}`}
								className="flex items-start justify-between rounded border p-3 text-sm"
							>
								<div>
									<div className="font-medium">
										{c.username || c.providerAccountId}
									</div>
									<div className="text-muted-foreground">
										Steam ID: {c.providerAccountId}
									</div>
									<div className="text-muted-foreground">
										Connected {c.connectedAt?.slice(0, 10)}
									</div>
								</div>
								{canManage ? (
									<UnlinkConnectionButton
										userId={user.user.id}
										provider="steam"
										providerAccountId={c.providerAccountId}
										csrfToken={csrfToken}
									/>
								) : null}
							</li>
						))}
					</ul>
				)}
			</section>

			<section>
				<h2 className="mb-2 text-lg font-semibold">Guilds</h2>
				{user.guilds.length === 0 ? (
					<p className="text-sm text-muted-foreground">
						Not in any tracked guild.
					</p>
				) : (
					<ul className="flex flex-wrap gap-2">
						{user.guilds.map((g) => (
							<li key={g.guildId}>
								<Badge variant="secondary">
									{g.guildId}
									{g.nick ? ` · ${g.nick}` : ""}
								</Badge>
							</li>
						))}
					</ul>
				)}
			</section>
		</div>
	);
}
