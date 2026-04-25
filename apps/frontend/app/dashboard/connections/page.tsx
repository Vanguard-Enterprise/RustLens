import { Badge } from "@/components/ui/badge";
import {
	Card,
	CardContent,
	CardDescription,
	CardHeader,
	CardTitle,
} from "@/components/ui/card";
import { getCurrentUser } from "@/lib/rustlens-api";
import { requireSession } from "@/lib/session";

export default async function DashboardConnectionsPage() {
	await requireSession();
	const currentUser = await getCurrentUser();

	return (
		<div className="grid gap-4 lg:grid-cols-2">
			<Card>
				<CardHeader>
					<CardTitle>Discord</CardTitle>
					<CardDescription>Linked Discord identity used to sign in</CardDescription>
				</CardHeader>
				<CardContent className="flex flex-col gap-2">
					<p>{currentUser.user.discordGlobalName || `@${currentUser.user.discordUsername}`}</p>
					<p className="text-muted-foreground text-sm">
						Discord ID: {currentUser.user.discordId}
					</p>
				</CardContent>
			</Card>
			<Card className="lg:col-span-2">
				<CardHeader>
					<CardTitle>Steam & BattleMetrics</CardTitle>
					<CardDescription>
						Linked game identities sourced from the existing Discord linkage model.
					</CardDescription>
				</CardHeader>
				<CardContent className="flex flex-col gap-2">
					{currentUser.steamAccounts.length ? (
						currentUser.steamAccounts.map((account) => (
							<div
								className="flex items-center justify-between gap-2 rounded-md border px-3 py-2"
								key={`${account.steamId}-${account.type}`}
							>
								<div>
									<p>{account.bmName || account.steamId}</p>
									<p className="text-muted-foreground text-sm">
										Steam {account.steamId}
									</p>
								</div>
								<div className="flex gap-2">
									<Badge variant="secondary">{account.type}</Badge>
									{account.bmId ? <Badge>BM {account.bmId}</Badge> : null}
								</div>
							</div>
						))
					) : (
						<p className="text-muted-foreground text-sm">
							No Steam/BattleMetrics links found for this Discord account.
						</p>
					)}
				</CardContent>
			</Card>
		</div>
	);
}
