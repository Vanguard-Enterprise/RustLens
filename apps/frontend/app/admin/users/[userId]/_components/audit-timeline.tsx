"use client";

import { useState } from "react";
import { Button } from "@/components/ui/button";
import type { AdminUserAuditItem } from "@/lib/rustlens-api";

export function AuditTimeline({
	userId,
	initialItems,
}: {
	userId: string;
	initialItems: AdminUserAuditItem[];
}) {
	const [items, setItems] = useState<AdminUserAuditItem[]>(initialItems);
	const [busy, setBusy] = useState(false);
	const [done, setDone] = useState(initialItems.length < 50);
	const [error, setError] = useState<string | null>(null);

	const loadMore = async () => {
		setBusy(true);
		setError(null);
		try {
			const last = items[items.length - 1];
			const sp = new URLSearchParams({ limit: "50" });
			if (last) sp.set("before", last.createdAt);
			const resp = await fetch(
				`/api/app/admin/users/${encodeURIComponent(userId)}/audit?${sp.toString()}`,
				{ credentials: "include" },
			);
			if (!resp.ok) {
				throw new Error(`request failed: ${resp.status}`);
			}
			const data = (await resp.json()) as { items: AdminUserAuditItem[] };
			setItems((prev) => [...prev, ...data.items]);
			if (data.items.length < 50) setDone(true);
		} catch (err) {
			setError(err instanceof Error ? err.message : "Failed to load more");
		} finally {
			setBusy(false);
		}
	};

	return (
		<div>
			{items.length === 0 ? (
				<p className="text-sm text-muted-foreground">No audit events for this user.</p>
			) : (
				<ul className="space-y-2 text-sm">
					{items.map((ev) => (
						<li key={ev.id} className="rounded border p-2">
							<div className="flex items-baseline gap-3">
								<span className="font-medium">{ev.eventType}</span>
								<span className="text-muted-foreground">
									{ev.createdAt?.slice(0, 19).replace("T", " ")}
								</span>
							</div>
							<div className="text-muted-foreground">actor: {ev.actorUserId || "—"}</div>
							{Object.keys(ev.payload ?? {}).length > 0 ? (
								<pre className="mt-1 overflow-x-auto text-xs">
									{JSON.stringify(ev.payload, null, 2)}
								</pre>
							) : null}
						</li>
					))}
				</ul>
			)}
			{error ? <p className="mt-2 text-sm text-destructive">{error}</p> : null}
			{!done ? (
				<div className="mt-3">
					<Button variant="ghost" onClick={loadMore} disabled={busy}>
						{busy ? "Loading..." : "Load more"}
					</Button>
				</div>
			) : null}
		</div>
	);
}
