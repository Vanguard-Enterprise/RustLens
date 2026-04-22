import { getCsrfToken } from "@/lib/csrf";
import { getAdminUser } from "@/lib/rustlens-api";
import { requirePermission } from "@/lib/session";
import { EditIdentityForm } from "./_components/edit-identity-form";

export default async function UserOverviewPage({
	params,
}: {
	params: Promise<{ userId: string }>;
}) {
	const session = await requirePermission("users.read");
	const { userId } = await params;
	const [user, csrfToken] = await Promise.all([getAdminUser(userId), getCsrfToken()]);
	const canEdit = (session.permissions ?? []).includes("users.edit");

	const guildCount = user.guilds.length;
	const steamCount = user.connections.filter((c) => c.provider === "steam").length;

	return (
		<div className="grid gap-6">
			<section>
				<h2 className="mb-2 text-lg font-semibold">Identity</h2>
				<dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-sm">
					<dt className="text-muted-foreground">User ID</dt>
					<dd className="font-mono">{user.user.id}</dd>
					<dt className="text-muted-foreground">Created</dt>
					<dd>{user.user.createdAt?.slice(0, 19).replace("T", " ") || "—"}</dd>
					<dt className="text-muted-foreground">Updated</dt>
					<dd>{user.user.updatedAt?.slice(0, 19).replace("T", " ") || "—"}</dd>
					<dt className="text-muted-foreground">Last login</dt>
					<dd>{user.user.lastLoginAt?.slice(0, 19).replace("T", " ") || "—"}</dd>
					<dt className="text-muted-foreground">Primary email</dt>
					<dd>{user.primaryEmail || "—"}</dd>
					<dt className="text-muted-foreground">Guilds</dt>
					<dd>{guildCount}</dd>
					<dt className="text-muted-foreground">Steam accounts</dt>
					<dd>{steamCount}</dd>
				</dl>
			</section>

			{canEdit ? (
				<section>
					<h2 className="mb-2 text-lg font-semibold">Edit identity</h2>
					<EditIdentityForm
						userId={user.user.id}
						initialDisplayName={user.user.displayName ?? ""}
						initialAvatarUrl={user.user.avatarUrl ?? ""}
						csrfToken={csrfToken}
					/>
				</section>
			) : null}

			<section>
				<h2 className="mb-2 text-lg font-semibold">Recent activity</h2>
				{user.recentAudit.length === 0 ? (
					<p className="text-sm text-muted-foreground">No recent audit events.</p>
				) : (
					<ul className="space-y-1 text-sm">
						{user.recentAudit.map((ev) => (
							<li key={ev.id} className="flex gap-4">
								<span className="text-muted-foreground">
									{ev.createdAt?.slice(0, 19).replace("T", " ")}
								</span>
								<span className="font-medium">{ev.eventType}</span>
								<span className="text-muted-foreground">by {ev.actorUserId || "—"}</span>
							</li>
						))}
					</ul>
				)}
			</section>
		</div>
	);
}
