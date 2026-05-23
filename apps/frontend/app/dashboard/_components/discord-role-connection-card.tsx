import { Button } from "@/components/ui/button";
import {
	Card,
	CardContent,
	CardDescription,
	CardHeader,
	CardTitle,
} from "@/components/ui/card";
import type { DiscordRoleConnectionSummary } from "@/lib/rustlens-api";

import {
	connectDiscordRoleAction,
	disconnectDiscordRoleAction,
} from "./discord-role-connection-actions";

interface Props {
	summary: DiscordRoleConnectionSummary | null | undefined;
	hasPro: boolean;
}

export function DiscordRoleConnectionCard({ summary, hasPro }: Props) {
	const linked = Boolean(summary?.linked);
	const revoked = Boolean(summary?.revoked);

	return (
		<Card>
			<CardHeader>
				<CardTitle>Discord community role</CardTitle>
				<CardDescription>
					Link your Discord account to automatically receive the PRO role in our
					community server when you have PRO active on RustLens.
				</CardDescription>
			</CardHeader>
			<CardContent className="flex flex-col gap-3">
				{!linked && !revoked && (
					<form action={connectDiscordRoleAction}>
						<Button type="submit">Connect Discord</Button>
					</form>
				)}
				{linked && (
					<>
						<p className="text-sm text-muted-foreground">
							{hasPro
								? `Connected. Your community role is in sync${
										summary?.lastPushedAt
											? ` (last verified ${new Date(summary.lastPushedAt).toLocaleString()})`
											: ""
									}.`
								: "Connected. Your community role will appear automatically when you have PRO."}
						</p>
						{summary?.lastPushError && (
							<p className="text-sm text-amber-600">
								Last sync failed. The system will retry automatically.
							</p>
						)}
						<form action={disconnectDiscordRoleAction}>
							<Button type="submit" variant="outline">
								Disconnect
							</Button>
						</form>
					</>
				)}
				{revoked && (
					<>
						<p className="text-sm text-amber-600">
							Discord disconnected the app. Please reconnect to keep the
							community role in sync.
						</p>
						<form action={connectDiscordRoleAction}>
							<Button type="submit">Reconnect</Button>
						</form>
					</>
				)}
			</CardContent>
		</Card>
	);
}
