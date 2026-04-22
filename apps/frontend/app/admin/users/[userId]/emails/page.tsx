import { Badge } from "@/components/ui/badge";
import {
	Table,
	TableBody,
	TableCell,
	TableHead,
	TableHeader,
	TableRow,
} from "@/components/ui/table";
import { getAdminUser } from "@/lib/rustlens-api";
import { requirePermission } from "@/lib/session";

export default async function UserEmailsPage({
	params,
}: {
	params: Promise<{ userId: string }>;
}) {
	await requirePermission("users.read");
	const { userId } = await params;
	const user = await getAdminUser(userId);

	return (
		<Table>
			<TableHeader>
				<TableRow>
					<TableHead>Email</TableHead>
					<TableHead>Primary</TableHead>
					<TableHead>Verified</TableHead>
				</TableRow>
			</TableHeader>
			<TableBody>
				{user.emails.map((e) => (
					<TableRow key={e.email}>
						<TableCell>{e.email}</TableCell>
						<TableCell>
							{e.isPrimary ? <Badge>primary</Badge> : <span className="text-muted-foreground">—</span>}
						</TableCell>
						<TableCell>
							{e.isVerified ? (
								<Badge variant="secondary">verified</Badge>
							) : (
								<Badge variant="destructive">unverified</Badge>
							)}
						</TableCell>
					</TableRow>
				))}
				{user.emails.length === 0 ? (
					<TableRow>
						<TableCell colSpan={3} className="py-6 text-center text-muted-foreground">
							No emails recorded.
						</TableCell>
					</TableRow>
				) : null}
			</TableBody>
		</Table>
	);
}
