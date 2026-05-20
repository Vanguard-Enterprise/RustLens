"use client";

import * as React from "react";

import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import {
	Table,
	TableBody,
	TableCell,
	TableHead,
	TableHeader,
	TableRow,
} from "@/components/ui/table";
import { SortHeader, type SortDir } from "@/components/admin/sort-header";
import { guildIconUrl, userAvatarUrl } from "@/lib/discord-cdn";
import type { AdminUserGuild } from "@/lib/rustlens-api";

type Props = {
	guilds: AdminUserGuild[];
};

type SortKey = "name" | "guildId" | "owner" | "members" | "bot";

function formatNumber(n: number | null): string {
	if (n === null) return "—";
	return n.toLocaleString();
}

function formatRelativeLeftAt(iso: string | null): string {
	if (!iso) return "";
	const ts = new Date(iso).getTime();
	if (!Number.isFinite(ts)) return "";
	const diff = Date.now() - ts;
	const days = Math.floor(diff / (1000 * 60 * 60 * 24));
	if (days < 1) return "today";
	if (days === 1) return "1 day ago";
	if (days < 30) return `${days} days ago`;
	const months = Math.floor(days / 30);
	if (months === 1) return "1 month ago";
	if (months < 12) return `${months} months ago`;
	const years = Math.floor(days / 365);
	return years === 1 ? "1 year ago" : `${years} years ago`;
}

function ownerLabel(g: AdminUserGuild): string {
	return g.ownerGlobalName || g.ownerUsername || "—";
}

function ownerInitial(g: AdminUserGuild): string {
	const label = ownerLabel(g);
	return label === "—" ? "?" : label.charAt(0).toUpperCase();
}

function botRank(g: AdminUserGuild): number {
	if (g.isInstalled) return 0;
	if (g.leftAt) return 1;
	return 2;
}

function compareGuilds(a: AdminUserGuild, b: AdminUserGuild, key: SortKey): number {
	switch (key) {
		case "name": {
			const an = a.name || "";
			const bn = b.name || "";
			if (an && !bn) return -1;
			if (!an && bn) return 1;
			return an.localeCompare(bn, undefined, { sensitivity: "base" });
		}
		case "guildId":
			return a.guildId.localeCompare(b.guildId);
		case "owner":
			return ownerLabel(a).localeCompare(ownerLabel(b), undefined, {
				sensitivity: "base",
			});
		case "members": {
			const av = a.approximateMemberCount;
			const bv = b.approximateMemberCount;
			if (av === null && bv === null) return 0;
			if (av === null) return 1;
			if (bv === null) return -1;
			return av - bv;
		}
		case "bot":
			return botRank(a) - botRank(b);
	}
}


export function ServersSection({ guilds }: Props) {
	const [search, setSearch] = React.useState("");
	const [sortKey, setSortKey] = React.useState<SortKey>("name");
	const [sortDir, setSortDir] = React.useState<SortDir>("asc");

	const toggleSort = (key: SortKey) => {
		if (key === sortKey) {
			setSortDir((d) => (d === "asc" ? "desc" : "asc"));
		} else {
			setSortKey(key);
			setSortDir(key === "members" ? "desc" : "asc");
		}
	};

	const filtered = React.useMemo(() => {
		const q = search.trim().toLowerCase();
		if (!q) return guilds;
		return guilds.filter((g) => {
			if (g.guildId.toLowerCase().includes(q)) return true;
			if (g.name.toLowerCase().includes(q)) return true;
			if (g.ownerUsername && g.ownerUsername.toLowerCase().includes(q))
				return true;
			if (g.ownerGlobalName && g.ownerGlobalName.toLowerCase().includes(q))
				return true;
			return false;
		});
	}, [guilds, search]);

	const sorted = React.useMemo(() => {
		const copy = [...filtered];
		copy.sort((a, b) => {
			const cmp = compareGuilds(a, b, sortKey);
			return sortDir === "asc" ? cmp : -cmp;
		});
		return copy;
	}, [filtered, sortKey, sortDir]);

	return (
		<section className="flex flex-col gap-3 rounded-lg border bg-card p-4">
			<div>
				<h2 className="text-lg font-semibold">Servers</h2>
				<p className="text-sm text-muted-foreground">
					Discord servers this user is a member of.
				</p>
			</div>

			{guilds.length === 0 ? (
				<p className="text-sm text-muted-foreground">No server memberships.</p>
			) : (
				<>
					<div className="flex items-center gap-2">
						<Input
							placeholder="Search by name, guild ID, or owner…"
							value={search}
							onChange={(e) => setSearch(e.target.value)}
							className="max-w-md"
							aria-label="Search servers"
						/>
						<span className="text-sm text-muted-foreground">
							{sorted.length === guilds.length
								? `${guilds.length} ${guilds.length === 1 ? "server" : "servers"}`
								: `${sorted.length} of ${guilds.length}`}
						</span>
					</div>

					<div className="overflow-hidden rounded-lg border">
						<Table>
							<TableHeader>
								<TableRow>
									<TableHead className="w-12"></TableHead>
									<SortHeader
										label="Name"
										sortKey="name"
										currentKey={sortKey}
										currentDir={sortDir}
										onSort={toggleSort}
									/>
									<SortHeader
										label="Guild ID"
										sortKey="guildId"
										currentKey={sortKey}
										currentDir={sortDir}
										onSort={toggleSort}
									/>
									<SortHeader
										label="Owner"
										sortKey="owner"
										currentKey={sortKey}
										currentDir={sortDir}
										onSort={toggleSort}
									/>
									<SortHeader
										label="Members"
										sortKey="members"
										currentKey={sortKey}
										currentDir={sortDir}
										onSort={toggleSort}
										className="text-right"
										align="right"
									/>
									<SortHeader
										label="Bot"
										sortKey="bot"
										currentKey={sortKey}
										currentDir={sortDir}
										onSort={toggleSort}
									/>
								</TableRow>
							</TableHeader>
							<TableBody>
								{sorted.length === 0 ? (
									<TableRow>
										<TableCell
											colSpan={6}
											className="h-24 text-center text-muted-foreground"
										>
											No servers match your search.
										</TableCell>
									</TableRow>
								) : (
									sorted.map((g) => {
										const iconUrl = guildIconUrl(g.guildId, g.iconHash, 64);
										const ownerAvatar = g.ownerDiscordId
											? userAvatarUrl(g.ownerDiscordId, g.ownerAvatarHash, 64)
											: null;
										return (
											<TableRow key={g.guildId}>
												<TableCell>
													<Avatar className="size-8">
														{iconUrl && (
															<AvatarImage src={iconUrl} alt={g.name} />
														)}
														<AvatarFallback>
															{g.name ? g.name.charAt(0).toUpperCase() : "?"}
														</AvatarFallback>
													</Avatar>
												</TableCell>
												<TableCell className="font-medium">
													{g.name || "—"}
												</TableCell>
												<TableCell>
													<button
														type="button"
														onClick={() =>
															navigator.clipboard?.writeText(g.guildId)
														}
														className="font-mono text-xs text-muted-foreground hover:text-foreground"
														title="Click to copy"
													>
														{g.guildId}
													</button>
												</TableCell>
												<TableCell>
													<div className="flex items-center gap-2">
														<Avatar className="size-6">
															{ownerAvatar && (
																<AvatarImage
																	src={ownerAvatar}
																	alt={ownerLabel(g)}
																/>
															)}
															<AvatarFallback className="text-[10px]">
																{ownerInitial(g)}
															</AvatarFallback>
														</Avatar>
														<span className="text-sm">{ownerLabel(g)}</span>
													</div>
												</TableCell>
												<TableCell className="text-right tabular-nums">
													{formatNumber(g.approximateMemberCount)}
												</TableCell>
												<TableCell>
													{g.isInstalled ? (
														<Badge variant="default">Installed</Badge>
													) : g.leftAt ? (
														<Badge variant="secondary" title={g.leftAt}>
															Left {formatRelativeLeftAt(g.leftAt)}
														</Badge>
													) : (
														<Badge variant="outline">Not installed</Badge>
													)}
												</TableCell>
											</TableRow>
										);
									})
								)}
							</TableBody>
						</Table>
					</div>
				</>
			)}
		</section>
	);
}
