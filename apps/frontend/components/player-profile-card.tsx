import Link from "next/link";

import type { PlayerProfile } from "@/lib/rustlens-api";

type Props = {
	profile: PlayerProfile;
	compact?: boolean;
};

type CategoryKey = "vitality" | "atlas" | "vital" | "modded" | "official" | "build";

const CATEGORY_LABELS: Array<[CategoryKey, string]> = [
	["vitality", "Vitality"],
	["atlas", "Atlas"],
	["vital", "Vital"],
	["modded", "Modded"],
	["official", "Official"],
	["build", "Build"],
];

function fmtUnix(unix: number | null | undefined): string | null {
	if (unix == null) return null;
	const ms = unix * 1000;
	const date = new Date(ms);
	if (Number.isNaN(date.getTime())) return null;
	return date.toISOString().slice(0, 10);
}

export function PlayerProfileCard({ profile, compact = false }: Props) {
	const displayName = profile.bmName || profile.personaName || profile.steamId;
	const firstSeen = fmtUnix(profile.firstSeenUnix);
	const categoryRows = CATEGORY_LABELS.filter(([key]) => profile.hours[key] > 0);
	const status = profile.status ?? null;

	return (
		<div className={compact ? "p-3" : "p-6"}>
			<div className="flex items-center gap-4">
				{profile.avatarImage ? (
					// eslint-disable-next-line @next/next/no-img-element
					<img
						alt=""
						src={profile.avatarImage}
						className={compact ? "h-12 w-12 rounded" : "h-16 w-16 rounded"}
					/>
				) : (
					<div
						className={
							compact ? "h-12 w-12 rounded bg-muted" : "h-16 w-16 rounded bg-muted"
						}
					/>
				)}
				<div className="flex flex-col min-w-0">
					<h2 className="truncate text-lg font-semibold">{displayName}</h2>
					{profile.personaName && profile.personaName !== displayName ? (
						<span className="truncate text-xs text-muted-foreground">
							Steam: {profile.personaName}
						</span>
					) : null}
					<div className="flex flex-wrap gap-2 text-xs text-muted-foreground">
						{profile.bmId ? (
							<a
								href={`https://www.battlemetrics.com/players/${profile.bmId}`}
								target="_blank"
								rel="noreferrer"
								className="hover:underline"
							>
								BM #{profile.bmId}
							</a>
						) : null}
						<Link
							href={`https://steamcommunity.com/profiles/${profile.steamId}`}
							target="_blank"
							rel="noreferrer"
							className="hover:underline"
						>
							Steam {profile.steamId}
						</Link>
					</div>
				</div>
			</div>

			{status ? (
				<div
					className={
						"mt-3 inline-flex items-center gap-2 rounded px-2 py-1 text-xs " +
						(status.isOnline
							? "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400"
							: "bg-muted text-muted-foreground")
					}
				>
					<span
						aria-hidden
						className={
							"inline-block h-2 w-2 rounded-full " +
							(status.isOnline ? "bg-emerald-500" : "bg-muted-foreground/40")
						}
					/>
					{status.isOnline ? "Online" : "Offline"}
					{status.serverName ? <span>· {status.serverName}</span> : null}
				</div>
			) : null}

			<dl className="mt-4 grid grid-cols-2 gap-2 text-sm">
				<dt className="text-muted-foreground">Total BM hours</dt>
				<dd className="tabular-nums">{profile.hours.bm.toLocaleString()}</dd>

				{profile.hours.rust != null ? (
					<>
						<dt className="text-muted-foreground">Steam Rust hours</dt>
						<dd className="tabular-nums">{profile.hours.rust.toLocaleString()}</dd>
					</>
				) : null}

				<dt className="text-muted-foreground">Servers played</dt>
				<dd className="tabular-nums">{profile.serversPlayed}</dd>

				{firstSeen ? (
					<>
						<dt className="text-muted-foreground">First seen</dt>
						<dd>{firstSeen}</dd>
					</>
				) : null}
			</dl>

			{categoryRows.length > 0 ? (
				<div className="mt-4">
					<h3 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
						Category hours
					</h3>
					<ul className="mt-1 grid grid-cols-2 gap-1 text-sm">
						{categoryRows.map(([key, label]) => (
							<li key={key} className="flex justify-between">
								<span>{label}</span>
								<span className="tabular-nums">{profile.hours[key].toLocaleString()}</span>
							</li>
						))}
					</ul>
				</div>
			) : null}
		</div>
	);
}
