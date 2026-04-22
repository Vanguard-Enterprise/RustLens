import { Badge } from "@/components/ui/badge";
import {
	Table,
	TableBody,
	TableCell,
	TableHead,
	TableHeader,
	TableRow,
} from "@/components/ui/table";
import { getCsrfToken } from "@/lib/csrf";
import { getAdminUser } from "@/lib/rustlens-api";
import { requireAdminSession, requirePermission } from "@/lib/session";
import {
	AddEmailForm,
	RemoveEmailButton,
	ResendVerificationButton,
	SetPrimaryButton,
} from "../_components/email-actions";

export default async function UserEmailsPage({
	params,
}: {
	params: Promise<{ userId: string }>;
}) {
	await requirePermission("users.read");
	const { userId } = await params;
	const [user, session, csrfToken] = await Promise.all([
		getAdminUser(userId),
		requireAdminSession(),
		getCsrfToken(),
	]);
	const canManage = (session.permissions ?? []).includes("users.email.manage");

	return (
		<div className="grid gap-6">
			<Table>
				<TableHeader>
					<TableRow>
						<TableHead>Email</TableHead>
						<TableHead>Primary</TableHead>
						<TableHead>Verified</TableHead>
						{canManage ? <TableHead>Actions</TableHead> : null}
					</TableRow>
				</TableHeader>
				<TableBody>
					{user.emails.map((e) => (
						<TableRow key={e.email}>
							<TableCell>{e.email}</TableCell>
							<TableCell>
								{e.isPrimary ? (
									<Badge>primary</Badge>
								) : (
									<span className="text-muted-foreground">—</span>
								)}
							</TableCell>
							<TableCell>
								{e.isVerified ? (
									<Badge variant="secondary">verified</Badge>
								) : (
									<Badge variant="destructive">unverified</Badge>
								)}
							</TableCell>
							{canManage ? (
								<TableCell className="flex flex-wrap items-start gap-2">
									{!e.isPrimary && e.isVerified ? (
										<SetPrimaryButton
											userId={user.user.id}
											email={e.email}
											csrfToken={csrfToken}
										/>
									) : null}
									{!e.isVerified ? (
										<ResendVerificationButton
											userId={user.user.id}
											email={e.email}
											csrfToken={csrfToken}
										/>
									) : null}
									{!e.isPrimary ? (
										<RemoveEmailButton
											userId={user.user.id}
											email={e.email}
											csrfToken={csrfToken}
										/>
									) : null}
								</TableCell>
							) : null}
						</TableRow>
					))}
					{user.emails.length === 0 ? (
						<TableRow>
							<TableCell
								colSpan={canManage ? 4 : 3}
								className="py-6 text-center text-muted-foreground"
							>
								No emails recorded.
							</TableCell>
						</TableRow>
					) : null}
				</TableBody>
			</Table>

			{canManage ? (
				<section>
					<h2 className="mb-2 text-sm font-medium">Add email</h2>
					<AddEmailForm userId={user.user.id} csrfToken={csrfToken} />
				</section>
			) : null}
		</div>
	);
}
