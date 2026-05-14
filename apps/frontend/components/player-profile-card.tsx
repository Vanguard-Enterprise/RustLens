import Link from "next/link";

import type { PlayerProfile } from "@/lib/rustlens-api";

type Props = {
	profile: PlayerProfile;
	compact?: boolean; // for hover-card; trims down spacing
};

export function PlayerProfileCard({ profile, compact = false }: Props) {
	return (
		<div className={compact ? "p-3" : "p-6"}>
			<div className="flex items-center gap-4">
				{profile.avatarImage ? (
					<img alt="" src={profile.avatarImage} className="h-16 w-16 rounded" />
				) : (
					<div className="h-16 w-16 rounded bg-muted" />
				)}
				<div className="flex flex-col">
					<h2 className="text-lg font-semibold">{profile.bmName ?? profile.steamId}</h2>
					{profile.bmId ? (
						<a
							href={`https://www.battlemetrics.com/players/${profile.bmId}`}
							target="_blank"
							rel="noreferrer"
							className="text-xs text-muted-foreground hover:underline"
						>
							BM #{profile.bmId}
						</a>
					) : null}
					<Link
						href={`https://steamcommunity.com/profiles/${profile.steamId}`}
						target="_blank"
						rel="noreferrer"
						className="text-xs text-muted-foreground hover:underline"
					>
						Steam {profile.steamId}
					</Link>
				</div>
			</div>

			<dl className="mt-4 grid grid-cols-2 gap-2 text-sm">
				{profile.totalBmHours !== null ? (
					<>
						<dt className="text-muted-foreground">Total BM hours</dt>
						<dd>{profile.totalBmHours.toFixed(1)}</dd>
					</>
				) : null}
				{profile.steamRustHours !== null ? (
					<>
						<dt className="text-muted-foreground">Steam Rust hours</dt>
						<dd>{profile.steamRustHours.toFixed(1)}</dd>
					</>
				) : null}
				{profile.currentServer ? (
					<>
						<dt className="text-muted-foreground">Currently on</dt>
						<dd>{profile.currentServer.name}</dd>
					</>
				) : profile.lastServer ? (
					<>
						<dt className="text-muted-foreground">Last seen on</dt>
						<dd>{profile.lastServer.name}</dd>
					</>
				) : null}
			</dl>

			{Object.keys(profile.categoryHours).length > 0 ? (
				<div className="mt-4">
					<h3 className="text-xs font-semibold uppercase text-muted-foreground">Category hours</h3>
					<ul className="mt-1 grid grid-cols-2 gap-1 text-sm">
						{Object.entries(profile.categoryHours).map(([cat, hours]) => (
							<li key={cat} className="flex justify-between">
								<span>{cat}</span>
								<span className="tabular-nums">{hours.toFixed(1)}</span>
							</li>
						))}
					</ul>
				</div>
			) : null}
		</div>
	);
}
