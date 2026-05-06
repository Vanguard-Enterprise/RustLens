// Tiny zero-dependency relative-time formatting helpers used by the admin
// audit log and any other place that shows "5m ago" style timestamps.

const SECOND = 1000;
const MINUTE = 60 * SECOND;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;

/**
 * Return a short human-readable relative time string for the given ISO
 * timestamp. Examples: "just now", "42s ago", "3m ago", "5h ago", "2d ago".
 *
 * For timestamps older than 30 days, returns a locale-formatted short date
 * like "Apr 3" (same year) or "Apr 3, 2025" (different year) so the column
 * stays scannable instead of showing stale day counts.
 */
export function formatRelative(iso: string, now: number = Date.now()): string {
	const t = Date.parse(iso);
	if (Number.isNaN(t)) {
		return iso;
	}
	const diff = now - t;

	// Future timestamps: fall through to absolute for now.
	if (diff < 0) {
		return formatAbsolute(iso);
	}

	if (diff < 10 * SECOND) {
		return "just now";
	}
	if (diff < MINUTE) {
		return `${Math.floor(diff / SECOND)}s ago`;
	}
	if (diff < HOUR) {
		return `${Math.floor(diff / MINUTE)}m ago`;
	}
	if (diff < DAY) {
		return `${Math.floor(diff / HOUR)}h ago`;
	}
	if (diff < 30 * DAY) {
		return `${Math.floor(diff / DAY)}d ago`;
	}

	const d = new Date(t);
	const nowYear = new Date(now).getFullYear();
	if (d.getFullYear() === nowYear) {
		return d.toLocaleDateString(undefined, { month: "short", day: "numeric" });
	}
	return d.toLocaleDateString(undefined, {
		month: "short",
		day: "numeric",
		year: "numeric",
	});
}

/**
 * Full locale-formatted timestamp, suitable for a `title` tooltip.
 */
export function formatAbsolute(iso: string): string {
	const t = Date.parse(iso);
	if (Number.isNaN(t)) {
		return iso;
	}
	return new Date(t).toLocaleString();
}

/**
 * Formats an ISO timestamp with a friendly day label and a precise time
 * of day. Examples: "Today at 4:00:39 PM", "Yesterday at 11:42:00 AM",
 * "Mon 3 at 9:15:02 AM", "Apr 3, 2025 at 9:15:02 AM".
 */
export function formatDayTime(iso: string, now: number = Date.now()): string {
	const t = Date.parse(iso);
	if (Number.isNaN(t)) {
		return iso;
	}
	const d = new Date(t);
	const nowDate = new Date(now);
	const time = d.toLocaleTimeString(undefined, {
		hour: "numeric",
		minute: "2-digit",
		second: "2-digit",
	});

	const startOfDay = (x: Date) =>
		new Date(x.getFullYear(), x.getMonth(), x.getDate()).getTime();
	const dayDiff = Math.round((startOfDay(nowDate) - startOfDay(d)) / DAY);

	if (dayDiff === 0) return `Today at ${time}`;
	if (dayDiff === 1) return `Yesterday at ${time}`;
	if (dayDiff > 1 && dayDiff < 7) {
		const weekday = d.toLocaleDateString(undefined, { weekday: "short" });
		return `${weekday} at ${time}`;
	}
	if (d.getFullYear() === nowDate.getFullYear()) {
		const md = d.toLocaleDateString(undefined, { month: "short", day: "numeric" });
		return `${md} at ${time}`;
	}
	const full = d.toLocaleDateString(undefined, {
		month: "short",
		day: "numeric",
		year: "numeric",
	});
	return `${full} at ${time}`;
}
