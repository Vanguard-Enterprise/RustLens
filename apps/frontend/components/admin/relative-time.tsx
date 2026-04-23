"use client";

import { useEffect, useState } from "react";

import { formatAbsolute, formatRelative } from "@/lib/relative-time";

/**
 * Client island that renders a live-ticking relative timestamp. Re-renders
 * every 60s so "5m ago" advances to "6m ago" without a page reload. The
 * absolute locale timestamp is exposed as a `title` tooltip.
 */
export function RelativeTime({ iso }: { iso: string }) {
	const [label, setLabel] = useState(() => formatRelative(iso));

	useEffect(() => {
		const id = setInterval(() => {
			setLabel(formatRelative(iso));
		}, 60_000);
		return () => clearInterval(id);
	}, [iso]);

	return (
		<span
			title={formatAbsolute(iso)}
			className="tabular-nums"
			// SSR renders with server clock; hydration re-computes with client clock.
			// Sub-second differences can straddle the formatter's second/minute
			// boundaries, so we accept the mismatch rather than flashing raw ISO.
			suppressHydrationWarning
		>
			{label}
		</span>
	);
}
