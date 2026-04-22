"use client";

import { Badge } from "@/components/ui/badge";
import { AdminActionButton } from "@/components/admin/admin-action-button";
import type { AdminRoleItem } from "@/lib/rustlens-api";
import { useRouter } from "next/navigation";

export interface RoleGrantControlsProps {
	userId: string;
	currentRoles: string[];
	allRoles: AdminRoleItem[];
	canAssign: boolean;
	canGrantSuperadmin: boolean;
	csrfToken: string;
}

export function RoleGrantControls({
	userId,
	currentRoles,
	allRoles,
	canAssign,
	canGrantSuperadmin,
	csrfToken,
}: RoleGrantControlsProps) {
	const router = useRouter();
	const refresh = () => router.refresh();
	const current = new Set(currentRoles);
	const currentItems = allRoles.filter((r) => current.has(r.key));
	const available = allRoles.filter((r) => !current.has(r.key));

	const isSA = (key: string) => key === "superadmin";
	const canActOn = (key: string) =>
		canAssign && (!isSA(key) || canGrantSuperadmin);

	return (
		<div className="grid gap-6 md:grid-cols-2">
			<section>
				<h3 className="mb-2 font-medium">Current roles</h3>
				{currentItems.length === 0 ? (
					<p className="text-sm text-muted-foreground">No admin roles assigned.</p>
				) : (
					<ul className="space-y-2">
						{currentItems.map((r) => (
							<li
								key={r.key}
								className="flex items-center justify-between rounded border p-2"
							>
								<div className="flex items-center gap-2">
									<Badge variant={isSA(r.key) ? "destructive" : "secondary"}>
										{r.key}
									</Badge>
									<span className="text-sm text-muted-foreground">{r.name}</span>
								</div>
								{canActOn(r.key) ? (
									<AdminActionButton
										action="users.roles.revoke"
										params={{ userId, roleKey: r.key }}
										csrfToken={csrfToken}
										label="Revoke"
										variant="destructive"
										onSuccess={refresh}
									/>
								) : null}
							</li>
						))}
					</ul>
				)}
			</section>
			<section>
				<h3 className="mb-2 font-medium">Available roles</h3>
				{available.length === 0 ? (
					<p className="text-sm text-muted-foreground">All roles are assigned.</p>
				) : (
					<ul className="space-y-2">
						{available.map((r) => (
							<li
								key={r.key}
								className="flex items-center justify-between rounded border p-2"
							>
								<div className="flex items-center gap-2">
									<Badge variant={isSA(r.key) ? "destructive" : "secondary"}>
										{r.key}
									</Badge>
									<span className="text-sm text-muted-foreground">{r.name}</span>
								</div>
								{canActOn(r.key) ? (
									<AdminActionButton
										action="users.roles.grant"
										params={{ userId, roleKey: r.key }}
										csrfToken={csrfToken}
										label="Grant"
										onSuccess={refresh}
									/>
								) : null}
							</li>
						))}
					</ul>
				)}
			</section>
		</div>
	);
}
