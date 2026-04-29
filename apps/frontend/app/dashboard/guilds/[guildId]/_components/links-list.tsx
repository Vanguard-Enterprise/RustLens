import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { getMyGuildLinks, type GuildLink } from "@/lib/rustlens-api";

function formatLinkedAt(iso: string): string {
	const date = new Date(iso);
	if (Number.isNaN(date.getTime())) {
		return iso;
	}
	return new Intl.DateTimeFormat(undefined, {
		year: "numeric",
		month: "short",
		day: "numeric",
	}).format(date);
}

function EmptyState() {
	return (
		<div className="flex flex-col items-center justify-center gap-2 rounded-xl border border-dashed py-12 text-center">
			<div className="font-heading text-base">
				No Steam accounts linked in this guild yet
			</div>
			<p className="max-w-md text-sm text-muted-foreground">
				When you link a Steam account through the bot in this server, it
				will show up here.
			</p>
		</div>
	);
}

function LinksTable({ links }: { links: GuildLink[] }) {
	return (
		<Card>
			<CardHeader>
				<CardTitle>Your linked Steam accounts</CardTitle>
			</CardHeader>
			<CardContent className="p-0">
				<table className="w-full text-sm">
					<thead className="border-b text-left text-xs uppercase tracking-wide text-muted-foreground">
						<tr>
							<th className="px-6 py-3 font-medium">Steam ID</th>
							<th className="px-6 py-3 font-medium">Linked</th>
						</tr>
					</thead>
					<tbody>
						{links.map((link) => (
							<tr key={link.steamId} className="border-b last:border-0">
								<td className="px-6 py-3 font-mono">{link.steamId}</td>
								<td className="px-6 py-3 text-muted-foreground">
									{formatLinkedAt(link.linkedAt)}
								</td>
							</tr>
						))}
					</tbody>
				</table>
			</CardContent>
		</Card>
	);
}

export async function LinksList({ guildId }: { guildId: string }) {
	const links = await getMyGuildLinks(guildId);
	if (links.length === 0) {
		return <EmptyState />;
	}
	return <LinksTable links={links} />;
}
