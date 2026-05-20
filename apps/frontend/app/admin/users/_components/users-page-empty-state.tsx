import Link from "next/link";

import { Button } from "@/components/ui/button";

export function UsersPageEmptyState() {
	return (
		<div className="flex flex-col items-center gap-3 rounded-md border border-dashed p-10 text-center">
			<h2 className="text-lg font-semibold">No admin roles defined yet</h2>
			<p className="max-w-md text-sm text-muted-foreground">
				Create a role with permissions in Roles to start adding users to the
				admin team.
			</p>
			<Button asChild variant="default" size="sm">
				<Link href="/admin/roles">Go to Roles</Link>
			</Button>
		</div>
	);
}
