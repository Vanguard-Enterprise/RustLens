import { getAdminUserAudit } from "@/lib/rustlens-api";
import { requirePermission } from "@/lib/session";
import { AuditTimeline } from "../_components/audit-timeline";

export default async function UserAuditPage({
	params,
}: {
	params: Promise<{ userId: string }>;
}) {
	await requirePermission("users.audit.read");
	const { userId } = await params;
	const { items } = await getAdminUserAudit(userId, { limit: 50 });
	return <AuditTimeline userId={userId} initialItems={items} />;
}
