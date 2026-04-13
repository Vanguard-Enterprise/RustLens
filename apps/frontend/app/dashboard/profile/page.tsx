import {
	Card,
	CardContent,
	CardDescription,
	CardHeader,
	CardTitle,
} from "@/components/ui/card";
import { getCurrentUser } from "@/lib/rustlens-api";
import { requireSession } from "@/lib/session";

export default async function DashboardProfilePage() {
	await requireSession();
	const currentUser = await getCurrentUser();

	return (
		<Card>
			<CardHeader>
				<CardTitle>Profile</CardTitle>
				<CardDescription>
					Core RustLens account metadata resolved from your current session.
				</CardDescription>
			</CardHeader>
			<CardContent className="flex flex-col gap-3">
				<div>
					<p className="text-muted-foreground text-sm">Display name</p>
					<p>{currentUser.user.displayName}</p>
				</div>
				<div>
					<p className="text-muted-foreground text-sm">Primary email</p>
					<p>{currentUser.primaryEmail?.email ?? "No email linked"}</p>
				</div>
				<div>
					<p className="text-muted-foreground text-sm">Discord ID</p>
					<p>{currentUser.discordAccount?.providerAccountId ?? "Unavailable"}</p>
				</div>
			</CardContent>
		</Card>
	);
}
