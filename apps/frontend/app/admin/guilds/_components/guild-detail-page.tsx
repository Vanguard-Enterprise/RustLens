import Link from "next/link";
import type { ReactNode } from "react";

import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import {
	Card,
	CardContent,
	CardDescription,
	CardHeader,
	CardTitle,
} from "@/components/ui/card";
import {
	Table,
	TableBody,
	TableCell,
	TableHead,
	TableHeader,
	TableRow,
} from "@/components/ui/table";
import { guildIconUrl, userAvatarUrl } from "@/lib/discord-cdn";
import type {
	AdminGuildDetail,
	AdminGuildMember,
	AdminGuildUsageItem,
	AdminGuildWipeReact,
} from "@/lib/rustlens-api";

type Props = {
	detail: AdminGuildDetail;
};

function formatNumber(value: number | null | undefined): string {
	if (value === null || value === undefined) return "-";
	return value.toLocaleString();
}

function formatDate(value: string | null | undefined): string {
	if (!value) return "-";
	const date = new Date(value);
	if (!Number.isFinite(date.getTime())) return "-";
	return date.toLocaleString(undefined, {
		year: "numeric",
		month: "short",
		day: "numeric",
		hour: "2-digit",
		minute: "2-digit",
	});
}

function formatUnix(value: number): string {
	if (!value) return "-";
	return formatDate(new Date(value * 1000).toISOString());
}

function userLabel(user: {
	displayName?: string | null;
	globalName?: string | null;
	discordUsername?: string | null;
	discordId: string;
}) {
	return user.displayName || user.globalName || user.discordUsername || user.discordId;
}

function shortCommand(row: AdminGuildUsageItem): string {
	return row.subcommand ? `/${row.command} ${row.subcommand}` : `/${row.command}`;
}

function Stat({ label, value }: { label: string; value: string | number }) {
	return (
		<Card size="sm">
			<CardHeader>
				<CardDescription>{label}</CardDescription>
				<CardTitle className="text-xl tabular-nums">{value}</CardTitle>
			</CardHeader>
		</Card>
	);
}

function Section({
	title,
	description,
	children,
}: {
	title: string;
	description?: string;
	children: ReactNode;
}) {
	return (
		<Card>
			<CardHeader>
				<CardTitle>{title}</CardTitle>
				{description ? <CardDescription>{description}</CardDescription> : null}
			</CardHeader>
			<CardContent>{children}</CardContent>
		</Card>
	);
}

function EmptyRow({ colSpan, label }: { colSpan: number; label: string }) {
	return (
		<TableRow>
			<TableCell colSpan={colSpan} className="h-20 text-center text-muted-foreground">
				{label}
			</TableCell>
		</TableRow>
	);
}

function WipeReactTable({ items }: { items: AdminGuildWipeReact[] }) {
	return (
		<div className="overflow-hidden rounded-lg border">
			<Table>
				<TableHeader>
					<TableRow>
						<TableHead>Name</TableHead>
						<TableHead>Scheduled</TableHead>
						<TableHead>Channel</TableHead>
						<TableHead className="text-right">Accepted</TableHead>
						<TableHead className="text-right">Declined</TableHead>
						<TableHead className="text-right">Late</TableHead>
					</TableRow>
				</TableHeader>
				<TableBody>
					{items.length === 0 ? (
						<EmptyRow colSpan={6} label="No wipe reacts stored for this view." />
					) : (
						items.map((item) => (
							<TableRow key={item.messageId}>
								<TableCell className="font-medium">{item.name || item.messageId}</TableCell>
								<TableCell>{formatUnix(item.scheduledUnix)}</TableCell>
								<TableCell className="font-mono text-xs text-muted-foreground">
									{item.channelId || "-"}
								</TableCell>
								<TableCell className="text-right tabular-nums">
									{item.acceptedUserIds.length}
								</TableCell>
								<TableCell className="text-right tabular-nums">
									{item.declinedUserIds.length}
								</TableCell>
								<TableCell className="text-right tabular-nums">
									{item.lateUserIds.length}
								</TableCell>
							</TableRow>
						))
					)}
				</TableBody>
			</Table>
		</div>
	);
}

function MembersTable({ members }: { members: AdminGuildMember[] }) {
	return (
		<div className="overflow-hidden rounded-lg border">
			<Table>
				<TableHeader>
					<TableRow>
						<TableHead>Member</TableHead>
						<TableHead>Discord ID</TableHead>
						<TableHead>Status</TableHead>
						<TableHead className="text-right">Steam Links</TableHead>
						<TableHead>Last Seen</TableHead>
					</TableRow>
				</TableHeader>
				<TableBody>
					{members.length === 0 ? (
						<EmptyRow colSpan={5} label="No member snapshots stored." />
					) : (
						members.map((member) => {
							const avatar = userAvatarUrl(member.discordId, member.avatarHash, 64);
							return (
								<TableRow key={member.discordId}>
									<TableCell>
										<div className="flex items-center gap-2">
											<Avatar className="size-7">
												{avatar ? <AvatarImage src={avatar} alt={userLabel(member)} /> : null}
												<AvatarFallback className="text-[10px]">
													{userLabel(member).charAt(0).toUpperCase()}
												</AvatarFallback>
											</Avatar>
											<div className="min-w-0">
												<div className="truncate font-medium">{userLabel(member)}</div>
												{member.appUserId ? (
													<Link
														href={`/admin/users/${encodeURIComponent(member.appUserId)}`}
														className="text-xs text-muted-foreground hover:text-foreground"
													>
														App user
													</Link>
												) : null}
											</div>
										</div>
									</TableCell>
									<TableCell className="font-mono text-xs text-muted-foreground">
										{member.discordId}
									</TableCell>
									<TableCell>
										{member.leftAt || !member.isActive ? (
											<Badge variant="secondary">Left</Badge>
										) : member.isOwner ? (
											<Badge variant="outline">Owner</Badge>
										) : (
											<Badge variant="default">Active</Badge>
										)}
									</TableCell>
									<TableCell className="text-right tabular-nums">
										{member.steamIds.length}
									</TableCell>
									<TableCell>{formatDate(member.lastSeenAt)}</TableCell>
								</TableRow>
							);
						})
					)}
				</TableBody>
			</Table>
		</div>
	);
}

export function GuildDetailPage({ detail }: Props) {
	const guild = detail.guild;
	const iconUrl = guildIconUrl(guild.guildId, guild.iconHash, 96);
	const ownerAvatar = guild.ownerDiscordId
		? userAvatarUrl(guild.ownerDiscordId, guild.ownerAvatarHash, 64)
		: null;
	const ownerName = guild.ownerGlobalName || guild.ownerUsername || "Unknown owner";
	const errorRate =
		detail.usageSummary.totalCount > 0
			? `${Math.round(detail.usageSummary.errorRate * 1000) / 10}%`
			: "0%";

	return (
		<div className="flex flex-col gap-6">
			<div className="flex flex-col gap-3">
				<Link href="/admin/guilds" className="text-sm text-muted-foreground hover:text-foreground">
					Back to guilds
				</Link>
				<div className="flex flex-col gap-4 md:flex-row md:items-center md:justify-between">
					<div className="flex items-center gap-3">
						<Avatar className="size-12">
							{iconUrl ? <AvatarImage src={iconUrl} alt={guild.name} /> : null}
							<AvatarFallback>{guild.name ? guild.name.charAt(0).toUpperCase() : "?"}</AvatarFallback>
						</Avatar>
						<div>
							<div className="flex flex-wrap items-center gap-2">
								<h1 className="text-2xl font-semibold">{guild.name || guild.guildId}</h1>
								{guild.isInstalled ? (
									<Badge variant="default">Installed</Badge>
								) : (
									<Badge variant="secondary">Left</Badge>
								)}
								{detail.settings.limited ? <Badge variant="outline">Limited</Badge> : null}
							</div>
							<p className="font-mono text-xs text-muted-foreground">{guild.guildId}</p>
						</div>
					</div>
					<div className="flex items-center gap-2 text-sm">
						<Avatar className="size-8">
							{ownerAvatar ? <AvatarImage src={ownerAvatar} alt={ownerName} /> : null}
							<AvatarFallback className="text-[10px]">
								{ownerName.charAt(0).toUpperCase()}
							</AvatarFallback>
						</Avatar>
						<div>
							<div className="font-medium">{ownerName}</div>
							<div className="text-xs text-muted-foreground">Owner</div>
						</div>
					</div>
				</div>
			</div>

			<div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
				<Stat label="Members" value={formatNumber(detail.stats.activeMemberCount)} />
				<Stat label="Linked Members" value={formatNumber(detail.stats.linkedMemberCount)} />
				<Stat label="Wipe Reacts" value={formatNumber(detail.stats.activeWipeReactCount)} />
				<Stat label="30d Usage" value={formatNumber(detail.stats.usage30dCount)} />
			</div>

			<div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_minmax(320px,0.6fr)]">
				<Section title="Stored Settings">
					<div className="grid gap-3 sm:grid-cols-2">
						{[
							["Admin Role", detail.settings.adminRoleId],
							["Alert Role", detail.settings.alertRoleId],
							["Wipe React Log", detail.settings.wipeReactLogChannelId],
							["Linking Log", detail.settings.linkingLogChannelId],
							["Tracked Server Log", detail.settings.trackedServerLogChannelId],
							["Tracked Users Category", detail.settings.trackedUsersCategoryId],
						].map(([label, value]) => (
							<div key={label} className="rounded-md border p-3">
								<div className="text-xs text-muted-foreground">{label}</div>
								<div className="mt-1 font-mono text-xs">{value || "-"}</div>
							</div>
						))}
					</div>
				</Section>

				<Section title="Usage Summary" description={`30 day error rate: ${errorRate}`}>
					<div className="flex flex-col gap-3">
						{detail.usageSummary.topCommands.length === 0 ? (
							<div className="text-sm text-muted-foreground">No command usage in the last 30 days.</div>
						) : (
							detail.usageSummary.topCommands.map((command) => (
								<div key={command.command} className="flex items-center justify-between gap-3">
									<span className="font-mono text-sm">/{command.command}</span>
									<span className="text-sm tabular-nums text-muted-foreground">{command.count}</span>
								</div>
							))
						)}
					</div>
				</Section>
			</div>

			<Section
				title="Current Wipe Reacts"
				description={`${detail.stats.activeWipeReactCount} active, ${detail.stats.wipeReactCount} total stored`}
			>
				<WipeReactTable items={detail.activeWipeReacts} />
			</Section>

			<div className="grid gap-4 xl:grid-cols-2">
				<Section title="Tracked Servers">
					<div className="overflow-hidden rounded-lg border">
						<Table>
							<TableHeader>
								<TableRow>
									<TableHead>Name</TableHead>
									<TableHead>BM ID</TableHead>
								</TableRow>
							</TableHeader>
							<TableBody>
								{detail.trackedServers.length === 0 ? (
									<EmptyRow colSpan={2} label="No tracked servers selected." />
								) : (
									detail.trackedServers.map((server) => (
										<TableRow key={server.bmId}>
											<TableCell className="font-medium">{server.name}</TableCell>
											<TableCell className="font-mono text-xs text-muted-foreground">
												{server.bmId}
											</TableCell>
										</TableRow>
									))
								)}
							</TableBody>
						</Table>
					</div>
				</Section>

				<Section title="Track Groups">
					<div className="overflow-hidden rounded-lg border">
						<Table>
							<TableHeader>
								<TableRow>
									<TableHead>Name</TableHead>
									<TableHead>Server</TableHead>
									<TableHead className="text-right">Members</TableHead>
									<TableHead className="text-right">Online</TableHead>
								</TableRow>
							</TableHeader>
							<TableBody>
								{detail.trackGroups.length === 0 ? (
									<EmptyRow colSpan={4} label="No track groups stored." />
								) : (
									detail.trackGroups.map((group) => (
										<TableRow key={group.id}>
											<TableCell className="font-medium">{group.name}</TableCell>
											<TableCell>{group.serverName || group.serverBmId}</TableCell>
											<TableCell className="text-right tabular-nums">{group.memberCount}</TableCell>
											<TableCell className="text-right tabular-nums">{group.onlineCount}</TableCell>
										</TableRow>
									))
								)}
							</TableBody>
						</Table>
					</div>
				</Section>
			</div>

			<Section
				title="Member Snapshot"
				description={`Showing up to ${detail.limits.members} stored members`}
			>
				<MembersTable members={detail.members} />
			</Section>

			<div className="grid gap-4 xl:grid-cols-2">
				<Section title="Steam Links" description={`Showing up to ${detail.limits.links} guild links`}>
					<div className="overflow-hidden rounded-lg border">
						<Table>
							<TableHeader>
								<TableRow>
									<TableHead>Discord</TableHead>
									<TableHead>Steam ID</TableHead>
									<TableHead>Created</TableHead>
								</TableRow>
							</TableHeader>
							<TableBody>
								{detail.links.length === 0 ? (
									<EmptyRow colSpan={3} label="No guild Steam links stored." />
								) : (
									detail.links.map((link) => (
										<TableRow key={`${link.discordId}:${link.steamId}`}>
											<TableCell>{link.displayName || link.discordUsername || link.discordId}</TableCell>
											<TableCell className="font-mono text-xs text-muted-foreground">
												{link.steamId}
											</TableCell>
											<TableCell>{formatDate(link.createdAt)}</TableCell>
										</TableRow>
									))
								)}
							</TableBody>
						</Table>
					</div>
				</Section>

				<Section title="Wipe React Schedules">
					<div className="overflow-hidden rounded-lg border">
						<Table>
							<TableHeader>
								<TableRow>
									<TableHead>Name</TableHead>
									<TableHead>Time</TableHead>
									<TableHead>Channel</TableHead>
								</TableRow>
							</TableHeader>
							<TableBody>
								{detail.schedules.length === 0 ? (
									<EmptyRow colSpan={3} label="No wipe react schedules stored." />
								) : (
									detail.schedules.map((schedule) => (
										<TableRow key={schedule.id}>
											<TableCell className="font-medium">{schedule.name || `Schedule ${schedule.id}`}</TableCell>
											<TableCell>{schedule.time} {schedule.timeZone}</TableCell>
											<TableCell className="font-mono text-xs text-muted-foreground">
												{schedule.channelId || "-"}
											</TableCell>
										</TableRow>
									))
								)}
							</TableBody>
						</Table>
					</div>
				</Section>
			</div>

			<Section title="Recent Usage" description={`Showing up to ${detail.limits.recentUsage} interactions`}>
				<div className="overflow-hidden rounded-lg border">
					<Table>
						<TableHeader>
							<TableRow>
								<TableHead>Command</TableHead>
								<TableHead>User</TableHead>
								<TableHead>Outcome</TableHead>
								<TableHead className="text-right">Latency</TableHead>
								<TableHead>Created</TableHead>
							</TableRow>
						</TableHeader>
						<TableBody>
							{detail.recentUsage.length === 0 ? (
								<EmptyRow colSpan={5} label="No command usage stored for this guild." />
							) : (
								detail.recentUsage.map((row) => (
									<TableRow key={row.id}>
										<TableCell className="font-mono text-xs">{shortCommand(row)}</TableCell>
										<TableCell>{row.displayName || row.discordUsername || row.discordId}</TableCell>
										<TableCell>
											<Badge variant={row.outcome === "success" ? "outline" : "secondary"}>
												{row.outcome}
											</Badge>
										</TableCell>
										<TableCell className="text-right tabular-nums">{row.latencyMs} ms</TableCell>
										<TableCell>{formatDate(row.createdAt)}</TableCell>
									</TableRow>
								))
							)}
						</TableBody>
					</Table>
				</div>
			</Section>

			<Section title="Recent Wipe React History">
				<WipeReactTable items={detail.recentWipeReacts} />
			</Section>
		</div>
	);
}
