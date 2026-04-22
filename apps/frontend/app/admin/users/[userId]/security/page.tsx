import { getCsrfToken } from "@/lib/csrf";
import { getAdminUser } from "@/lib/rustlens-api";
import { requireAdminSession } from "@/lib/session";
import { SuspendPanel } from "../_components/suspend-panel";
import { DeleteRestorePanel } from "../_components/delete-restore-panel";
import { PurgePanel } from "../_components/purge-panel";
import { ForceResetButton } from "../_components/force-reset-button";

export default async function UserSecurityPage({
	params,
}: {
	params: Promise<{ userId: string }>;
}) {
	const session = await requireAdminSession();
	const { userId } = await params;
	const [user, csrfToken] = await Promise.all([
		getAdminUser(userId),
		getCsrfToken(),
	]);

	const perms = new Set(session.permissions ?? []);
	const roles = new Set(session.roles ?? []);
	const canSuspend = perms.has("users.suspend");
	const canDelete = perms.has("users.delete");
	const canReset = perms.has("users.security.reset");
	const isSuperadmin = roles.has("superadmin");
	const selfTarget = session.userId === user.user.id;

	const suspended = Boolean(user.suspendedAt);
	const deleted = Boolean(user.deletedAt);

	return (
		<div className="grid gap-6">
			{canSuspend ? (
				<section>
					<h2 className="mb-2 text-lg font-semibold">Suspension</h2>
					<SuspendPanel
						userId={user.user.id}
						csrfToken={csrfToken}
						suspended={suspended}
						suspendReason={user.suspendReason}
						selfTarget={selfTarget}
					/>
				</section>
			) : null}

			{canReset ? (
				<section>
					<h2 className="mb-2 text-lg font-semibold">Force password reset</h2>
					<p className="mb-2 text-sm text-muted-foreground">
						Generates a one-time link you can share securely with the user.
						Existing outstanding reset tokens are invalidated.
					</p>
					<ForceResetButton
						userId={user.user.id}
						csrfToken={csrfToken}
						selfTarget={selfTarget}
					/>
				</section>
			) : null}

			{canDelete ? (
				<section>
					<h2 className="mb-2 text-lg font-semibold">Delete / Restore</h2>
					<DeleteRestorePanel
						userId={user.user.id}
						csrfToken={csrfToken}
						deleted={deleted}
						selfTarget={selfTarget}
					/>
				</section>
			) : null}

			{isSuperadmin ? (
				<section>
					<h2 className="mb-2 text-lg font-semibold text-destructive">
						Danger zone
					</h2>
					<PurgePanel
						userId={user.user.id}
						csrfToken={csrfToken}
						selfTarget={selfTarget}
					/>
				</section>
			) : null}

			{!canSuspend && !canDelete && !canReset && !isSuperadmin ? (
				<p className="text-sm text-muted-foreground">
					You do not have permission to perform security actions on users.
				</p>
			) : null}
		</div>
	);
}
