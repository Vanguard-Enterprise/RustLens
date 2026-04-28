import type { TrackGroupAnalyticsResponse } from "./backend-client.js";

const WEEKDAY_INDEX: Record<string, number> = {
  Monday: 0,
  Tuesday: 1,
  Wednesday: 2,
  Thursday: 3,
  Friday: 4,
  Saturday: 5,
  Sunday: 6,
};

export type NextBestOfflineWindow = {
  startUnix: number;
  endUnix: number;
};

export function computeNextBestOfflineWindow(
  analytics: TrackGroupAnalyticsResponse,
  now: Date = new Date(),
): NextBestOfflineWindow | null {
  const tz = analytics.timeZone || "Europe/London";
  const formatter = new Intl.DateTimeFormat("en-US", {
    timeZone: tz,
    weekday: "long",
    hour: "2-digit",
    hour12: false,
  });

  const hourInTz = (date: Date): number | null => {
    const parts = formatter.formatToParts(date);
    const hourStr = parts.find((part) => part.type === "hour")?.value ?? "";
    const parsed = Number(hourStr);
    if (!Number.isFinite(parsed)) return null;
    return parsed % 24;
  };

  const lookup = Array.from({ length: 7 }, () => new Array<number>(24).fill(Number.POSITIVE_INFINITY));
  let populated = 0;
  for (const day of analytics.days) {
    const wd = WEEKDAY_INDEX[day.weekday];
    if (wd === undefined) continue;
    for (const hour of day.hours) {
      const h = hour.hour ?? hourInTz(new Date(hour.hourUnix * 1000));
      if (h === null || h < 0 || h > 23) continue;
      lookup[wd][h] = hour.avg;
      populated += 1;
    }
  }
  if (populated === 0) return null;

  const localizedAvg = (date: Date): number => {
    const parts = formatter.formatToParts(date);
    const weekday = parts.find((part) => part.type === "weekday")?.value ?? "";
    const hourStr = parts.find((part) => part.type === "hour")?.value ?? "0";
    const wd = WEEKDAY_INDEX[weekday];
    const hour = Number(hourStr) % 24;
    if (wd === undefined || !Number.isFinite(hour)) return Number.POSITIVE_INFINITY;
    return lookup[wd][hour];
  };

  const start = new Date(now);
  start.setUTCMinutes(0, 0, 0);
  start.setUTCHours(start.getUTCHours() + 1);

  let bestStart = -1;
  let bestSum = Number.POSITIVE_INFINITY;

  for (let i = 0; i < 168; i += 1) {
    const first = new Date(start.getTime() + i * 3600_000);
    const second = new Date(start.getTime() + (i + 1) * 3600_000);
    const sum = localizedAvg(first) + localizedAvg(second);
    if (sum < bestSum) {
      bestSum = sum;
      bestStart = Math.floor(first.getTime() / 1000);
    }
  }

  if (bestStart < 0) return null;
  return { startUnix: bestStart, endUnix: bestStart + 7200 };
}

export function buildNextBestOfflineFieldValue(window: NextBestOfflineWindow): string {
  return `> * <t:${window.startUnix}:t> - <t:${window.endUnix}:t>\n> * <t:${window.startUnix}:R>`;
}
