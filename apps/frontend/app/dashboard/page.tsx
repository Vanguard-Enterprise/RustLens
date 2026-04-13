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

export default async function DashboardPage() {
	await requireSession();
	const currentUser = await getCurrentUser();

	return (
		<div className="flex flex-col gap-4">
			<Card>
				<CardHeader>
					<CardTitle>Dashboard overview</CardTitle>
					<CardDescription>
						Your RustLens identity, linked services, and guild footprint.
					</CardDescription>
				</CardHeader>
				<CardContent className="grid gap-3 md:grid-cols-3">
					<Card>
						<CardHeader>
							<CardTitle className="text-base">Guild memberships</CardTitle>
						</CardHeader>
						<CardContent>
							<p className="font-heading text-3xl">{currentUser.guilds.length}</p>
						</CardContent>
					</Card>
					<Card>
						<CardHeader>
							<CardTitle className="text-base">Steam links</CardTitle>
						</CardHeader>
						<CardContent>
							<p className="font-heading text-3xl">
								{currentUser.steamAccounts.length}
							</p>
						</CardContent>
					</Card>
					<Card>
						<CardHeader>
							<CardTitle className="text-base">Roles</CardTitle>
						</CardHeader>
						<CardContent className="flex flex-wrap gap-2">
							{currentUser.user.roles.map((role) => (
								<Badge key={role} variant="secondary">
									{role}
								</Badge>
							))}
						</CardContent>
					</Card>
				</CardContent>
			</Card>
		</div>
	);
}
