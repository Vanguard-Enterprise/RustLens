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
					<p className="text-muted-foreground text-sm">Discord username</p>
					<p>@{currentUser.user.discordUsername}</p>
				</div>
				<div>
					<p className="text-muted-foreground text-sm">Discord ID</p>
					<p>{currentUser.user.discordId || "Unavailable"}</p>
				</div>
			</CardContent>
		</Card>
	);
}
