import { describe, expect, it } from "vitest";
import type { TrackGroupAnalyticsResponse, TrackGroupOnlineRow } from "./backend-client.js";
import { __testables } from "./track-command.js";

const { buildAnalyticsPages, buildTrackEmbeds, buildWeekdayFieldValue } = __testables;

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

function makeOnlineRow(steamId: string, bmName: string, isOnline: boolean): TrackGroupOnlineRow {
  return {
    steamId,
    bmId: steamId.slice(-10),
    bmName,
    discordId: "",
    avatarImage: "",
    isOnline,
    onlineUnix: 1767571200,
  };
}

describe("buildTrackEmbeds", () => {
  it("renders online and offline lists in descriptions instead of fields", () => {
    const embeds = buildTrackEmbeds(
      { name: "kap", serverName: "Vitality EU 10x" },
      [
        makeOnlineRow("76561198127315384", "Jigsaw", true),
        makeOnlineRow("76561198151846617", "lack", false),
      ],
    );
    const data = embeds[0].toJSON();

    expect(data.title).toBe("kap - Vitality EU 10x");
    expect(data.fields).toBeUndefined();
    expect(data.description).toContain("**Online 1**");
    expect(data.description).toContain("Jigsaw");
    expect(data.description).toContain("**Offline 1**");
    expect(data.description).toContain("lack");
    expect(data.description).not.toContain("battlemetrics.com");
  });

  it("splits long track lists across multiple embeds", () => {
    const rows = Array.from({ length: 250 }, (_value, index) =>
      makeOnlineRow(`7656119${String(index).padStart(10, "0")}`, `player-${index}`, false),
    );

    const embeds = buildTrackEmbeds({ name: "kap", serverName: "Vitality EU 10x" }, rows);

    expect(embeds.length).toBeGreaterThan(1);
    expect(embeds[1].toJSON().title).toBe("kap - Vitality EU 10x cont.");
    for (const embed of embeds) {
      expect(embed.toJSON().description?.length ?? 0).toBeLessThanOrEqual(3900);
      expect(embed.toJSON().fields).toBeUndefined();
    }
    const totalLength = embeds.reduce((sum, embed) => sum + (embed.toJSON().description?.length ?? 0), 0);
    expect(totalLength).toBeLessThanOrEqual(5800);
  });
});

describe("buildWeekdayFieldValue", () => {
  it("includes the best-time line and one average for that timeframe", () => {
    const day = makeDay("Monday", 0, [0, 0, 1.5, 0.2, 1, 3, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0]);
    const value = buildWeekdayFieldValue(day);
    expect(value).toContain("Best time to go for an offline:");
    expect(value).toContain(`<t:${day.bestStart}:t> - <t:${day.bestEnd}:t>`);
    expect(value).toContain("Average people online:");
    expect(value).toContain("**2**");
    expect(value).not.toContain(`<t:${day.hours[2].hourUnix}:t> **1.5**`);
  });

  it("renders zero when every hour in the timeframe is zero", () => {
    const day = makeDay("Tuesday", 1, Array(24).fill(0));
    const value = buildWeekdayFieldValue(day);
    expect(value).toContain("**0**");
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
    expect(data.fields?.[0].inline).toBe(true);
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
