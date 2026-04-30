import { describe, expect, it } from "vitest";
import type { TrackGroupAnalyticsResponse, TrackGroupOnlineRow } from "./backend-client.js";
import { __testables } from "./track-command.js";

const { buildAddRemoveModal, buildAnalyticsPages, buildPlayerSelectComponents, buildTrackButtons, buildTrackEmbeds, buildWeekdayFieldValue, dedupeTrackRows } = __testables;

type TestModalComponent = {
  custom_id?: string;
  required?: boolean;
};

type TestButtonComponent = {
  custom_id?: string;
  emoji?: { id?: string; name?: string };
};

type TestSelectComponent = {
  custom_id?: string;
  options?: { label: string; value: string }[];
};

function modalComponentAt(modal: ReturnType<ReturnType<typeof buildAddRemoveModal>["toJSON"]>, index: number) {
  return modal.components[index].component as TestModalComponent;
}

function actionComponentAt(row: ReturnType<ReturnType<typeof buildTrackButtons>[number]["toJSON"]>, index: number) {
  return row.components[index] as TestButtonComponent;
}

const MONDAY_BASE = 1767571200; // 2026-01-05 00:00 UTC

function makeDay(weekday: string, weekdayIndex: number, hours: number[]): TrackGroupAnalyticsResponse["days"][number] {
  const dayBase = MONDAY_BASE + weekdayIndex * 86400;
  return {
    weekday,
    bestStart: dayBase + 4 * 3600,
    bestEnd: dayBase + 6 * 3600,
    hours: hours.map((avg, h) => ({ hourUnix: dayBase + h * 3600, hour: h, avg, minutes: avg })),
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
    timeZone: "Europe/London",
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
  it("renders only the selected online list with BattleMetrics links", () => {
    const embeds = buildTrackEmbeds(
      { name: "kap", serverName: "Vitality EU 10x" },
      [
        makeOnlineRow("76561198127315384", "Jigsaw", true),
        makeOnlineRow("76561198151846617", "lack", false),
      ],
    );
    const data = embeds[0].toJSON();

    expect(data.title).toBe("<:Online:1497993468981805076> 1 - kap - Vitality EU 10x");
    expect(data.fields).toBeUndefined();
    expect(data.description).toContain("> * [Jigsaw]");
    expect(data.description).toContain("battlemetrics.com");
    expect(data.description).not.toContain("Online");
    expect(data.description).not.toContain("Offline");
    expect(data.description).not.toContain("lack");
  });

  it("renders the offline list when requested", () => {
    const embeds = buildTrackEmbeds(
      { name: "kap", serverName: "Vitality EU 10x" },
      [
        makeOnlineRow("76561198127315384", "Jigsaw", true),
        makeOnlineRow("76561198151846617", "lack", false),
      ],
      "offline",
    );
    const data = embeds[0].toJSON();

    expect(data.title).toBe("<:Offline:1497993467660599476> 1 - kap - Vitality EU 10x");
    expect(data.description).toContain("> * [lack]");
    expect(data.description).not.toContain("Offline");
    expect(data.description).not.toContain("Jigsaw");
  });

  it("splits long track lists across multiple embeds and summarizes hidden rows", () => {
    const rows = Array.from({ length: 250 }, (_value, index) =>
      makeOnlineRow(`7656119${String(index).padStart(10, "0")}`, `player-${index}`, false),
    );

    const embeds = buildTrackEmbeds({ name: "kap", serverName: "Vitality EU 10x" }, rows, "offline");

    expect(embeds.length).toBeGreaterThan(1);
    expect(embeds[1].toJSON().title).toBe("<:Offline:1497993467660599476> 250 - kap - Vitality EU 10x cont.");
    for (const embed of embeds) {
      expect(embed.toJSON().description?.length ?? 0).toBeLessThanOrEqual(3900);
      expect(embed.toJSON().fields).toBeUndefined();
    }
    const totalLength = embeds.reduce((sum, embed) => sum + (embed.toJSON().description?.length ?? 0), 0);
    expect(totalLength).toBeLessThanOrEqual(5800);
    expect(embeds.at(-1)?.toJSON().description).toMatch(/\+\d+ more/);
  });
});

describe("dedupeTrackRows", () => {
  it("keeps one row per Steam ID and prefers rows with Discord data", () => {
    const first = makeOnlineRow("76561198127315384", "Jigsaw", false);
    const second = { ...makeOnlineRow("76561198127315384", "Jigsaw", false), discordId: "123" };
    const rows = dedupeTrackRows([first, second, makeOnlineRow("76561198151846617", "lack", true)]);

    expect(rows).toHaveLength(2);
    expect(rows[0].steamId).toBe("76561198127315384");
    expect(rows[0].discordId).toBe("123");
  });
});

describe("buildAddRemoveModal", () => {
  it("renders add with optional identifiers and player name fields", () => {
    const modal = buildAddRemoveModal(true, 123).toJSON();

    expect(modal.custom_id).toBe("track_modal:add:123");
    expect(modal.components).toHaveLength(2);
    expect(modalComponentAt(modal, 0).custom_id).toBe("track_ids");
    expect(modalComponentAt(modal, 0).required).toBe(false);
    expect(modalComponentAt(modal, 1).custom_id).toBe("track_player_name");
    expect(modalComponentAt(modal, 1).required).toBe(false);
  });

  it("renders remove with optional identifiers and player name fields", () => {
    const modal = buildAddRemoveModal(false, 123).toJSON();

    expect(modal.custom_id).toBe("track_modal:remove:123");
    expect(modal.components).toHaveLength(2);
    expect(modalComponentAt(modal, 0).custom_id).toBe("track_ids");
    expect(modalComponentAt(modal, 0).required).toBe(false);
    expect(modalComponentAt(modal, 1).custom_id).toBe("track_player_name");
    expect(modalComponentAt(modal, 1).required).toBe(false);
  });
});

describe("buildTrackButtons", () => {
  it("uses the expected add and remove icons", () => {
    const row = buildTrackButtons(123)[0].toJSON();

    expect(actionComponentAt(row, 0).custom_id).toBe("track:add:123");
    expect(actionComponentAt(row, 0).emoji?.name).toBe("user_add");
    expect(actionComponentAt(row, 1).custom_id).toBe("track:remove:123");
    expect(actionComponentAt(row, 1).emoji?.name).toBe("user_remove");
  });
});

describe("buildPlayerSelectComponents", () => {
  it("renders ambiguous player choices with BM IDs", () => {
    const row = buildPlayerSelectComponents("add", 123, [
      { steamId: "76561198000000001", bmId: "111", bmName: "kap" },
      { steamId: "76561198000000002", bmId: "222", bmName: "kap2" },
    ])[0].toJSON();
    const select = row.components[0] as TestSelectComponent;

    expect(select.custom_id).toBe("track_select:player:add:123");
    expect(select.options?.[0].label).toBe("kap (111)");
    expect(select.options?.[0].value).toBe("76561198000000001:111");
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
