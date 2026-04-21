import {
	Table,
	TableBody,
	TableCell,
	TableHead,
	TableHeader,
	TableRow,
} from "@/components/ui/table";
import { getAdminRoles } from "@/lib/rustlens-api";
import { requirePermission } from "@/lib/session";

export default async function AdminRolesPage() {
	await requirePermission("admins.grant");
	const roles = await getAdminRoles();

	return (
		<Table>
			<TableHeader>
				<TableRow>
					<TableHead>Role</TableHead>
					<TableHead>Key</TableHead>
					<TableHead>System</TableHead>
					<TableHead>Permissions</TableHead>
				</TableRow>
			</TableHeader>
			<TableBody>
				{roles.map((role) => (
					<TableRow key={role.id}>
						<TableCell>{role.name}</TableCell>
						<TableCell>{role.key}</TableCell>
						<TableCell>{role.isSystem ? "Yes" : "No"}</TableCell>
						<TableCell>
							{role.key === "superadmin"
								? "(implicit all)"
								: role.permissions.length === 0
									? "(none)"
									: role.permissions.join(", ")}
						</TableCell>
					</TableRow>
				))}
			</TableBody>
		</Table>
	);
}
