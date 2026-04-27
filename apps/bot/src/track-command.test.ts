import { describe, expect, it } from "vitest";
import type { TrackGroupAnalyticsResponse } from "./backend-client.js";
import { __testables } from "./track-command.js";

const { buildAnalyticsPages, buildWeekdayFieldValue } = __testables;

const MONDAY_BASE = 1767571200; // 2026-01-05 00:00 UTC

function makeDay(weekday: string, weekdayIndex: number, hours: number[]): TrackGroupAnalyticsResponse["days"][number] {
  const dayBase = MONDAY_BASE + weekdayIndex * 86400;
  return {
    weekday,
    bestStart: dayBase + 4 * 3600,
    bestEnd: dayBase + 6 * 3600,
    hours: hours.map((avg, h) => ({ hourUnix: dayBase + h * 3600, avg })),
  };
}

function makeAnalytics(
  groupName: string,
  serverName: string,
  hoursPerDay: number[],
): TrackGroupAnalyticsResponse {
  const weekdays = ["Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday"];
  return {
    groupName,
    serverName,
    members: 5,
    analyzed: 5,
    days: weekdays.map((name, idx) => makeDay(name, idx, hoursPerDay)),
  };
}

describe("buildWeekdayFieldValue", () => {
  it("includes the best-time line and the top hours sorted by avg", () => {
    const day = makeDay("Monday", 0, [0, 0, 1.5, 0.2, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0]);
    const value = buildWeekdayFieldValue(day);
    expect(value).toContain("Best time to go for an offline:");
    expect(value).toContain(`<t:${day.bestStart}:t> - <t:${day.bestEnd}:t>`);
    expect(value).toContain("**1.5**");
    expect(value).toContain("**0.2**");
    // Hours with avg 0 must be excluded.
    expect(value).not.toContain("**0**\n");
  });

  it("renders a no-activity placeholder when every hour is zero", () => {
    const day = makeDay("Tuesday", 1, Array(24).fill(0));
    const value = buildWeekdayFieldValue(day);
    expect(value).toContain("No measurable activity");
  });
});

describe("buildAnalyticsPages", () => {
  it("returns a single page when all 7 weekdays fit", () => {
    const analytics = makeAnalytics("Team", "Server", [0, 0, 1, 2, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0]);
    const pages = buildAnalyticsPages(analytics);
    expect(pages).toHaveLength(1);
    const data = pages[0].toJSON();
    expect(data.title).toBe("Analytics - Team");
    expect(data.fields).toHaveLength(7);
    expect(data.fields?.[0].name).toBe("Monday");
    expect(data.fields?.[6].name).toBe("Sunday");
  });

  it("splits into multiple pages when fields exceed the embed body limit", () => {
    // Force pagination by lowering the body limit; each field is roughly
    // 200 chars, so a 600-char budget should fit ~3 fields per page.
    const heavy = Array.from({ length: 12 }, (_v, h) => 9.99 - h * 0.01);
    const analytics = makeAnalytics("Team", "Server", heavy);
    const pages = buildAnalyticsPages(analytics, 600);
    expect(pages.length).toBeGreaterThan(1);
    const totalFields = pages.reduce((sum, embed) => sum + (embed.toJSON().fields?.length ?? 0), 0);
    expect(totalFields).toBe(7);
  });
});
