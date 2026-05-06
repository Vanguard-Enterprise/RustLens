"use client";

import { useEffect, useState } from "react";

import { formatAbsolute, formatDayTime } from "@/lib/relative-time";

/**
 * Friendly day/time label like "Today at 4:00:39 PM". Re-renders every
 * minute so "Today" rolls over to "Yesterday" without a reload.
 */
export function DayTime({ iso }: { iso: string }) {
	const [label, setLabel] = useState(() => formatDayTime(iso));

	useEffect(() => {
		const id = setInterval(() => {
			setLabel(formatDayTime(iso));
		}, 60_000);
		return () => clearInterval(id);
	}, [iso]);

	return (
		<span title={formatAbsolute(iso)} className="tabular-nums" suppressHydrationWarning>
			{label}
		</span>
	);
}
