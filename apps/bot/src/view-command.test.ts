import { describe, expect, it } from "vitest";
import type { SteamAccount } from "./backend-client.js";
import { __testables, buildUserEmbed } from "./view-command.js";

const OLD_FIRST_SEEN_UNIX = 1577836800;

describe("view-command", () => {
  it("normalizes raw discord ids", () => {
    expect(__testables.normalizeDiscordIdentifier("123456789012345678")).toBeNull();
  });

  it("normalizes discord mentions", () => {
    expect(__testables.normalizeDiscordIdentifier("<@123456789012345678>")).toBe("123456789012345678");
    expect(__testables.normalizeDiscordIdentifier("<@!123456789012345678>")).toBe("123456789012345678");
  });

  it("ignores non-discord identifiers", () => {
    expect(__testables.normalizeDiscordIdentifier("bm-player-123")).toBeNull();
  });

  it("builds the player profile embed with linked ids", () => {
    const embed = buildUserEmbed("76561198000000000", {
      steamId: "76561198000000000",
      bmId: "918543318",
      bmName: "turn it up",
      personaName: "",
      avatarImage: "https://cdn.example/avatar.png",
      firstSeenUnix: OLD_FIRST_SEEN_UNIX,
      serversPlayed: 12,
      nameGaps: [],
      hours: {
        vitality: 10,
        atlas: 20,
        vital: 30,
        modded: 40,
        official: 50,
        build: 60,
        bm: 210,
        rust: 7347,
      },
    }).toJSON();

    expect(embed.title).toBe("turn it up");
    expect(embed.thumbnail?.url).toBe("https://cdn.example/avatar.png");
    expect(embed.fields?.[0]?.value).toContain("https://steamcommunity.com/profiles/76561198000000000");
    expect(embed.fields?.[0]?.value).toContain("https://www.battlemetrics.com/rcon/players/918543318");
    expect(embed.fields?.find((field) => field.name.includes("Rust"))?.value).toBe("**7347** hours");
    expect(embed.fields?.find((field) => field.name.startsWith("Risk Factor"))?.name).toBe("Risk Factor <:risk_5:1487595720956448949>");
    expect(embed.fields).toHaveLength(11);
  });

  it("can retitle the player profile embed for setsteam success", () => {
    const embed = buildUserEmbed("76561198000000000", {
      steamId: "76561198000000000",
      bmId: "918543318",
      bmName: "turn it up",
      personaName: "",
      avatarImage: "https://cdn.example/avatar.png",
      firstSeenUnix: OLD_FIRST_SEEN_UNIX,
      serversPlayed: 12,
      nameGaps: [],
      hours: {
        vitality: 10,
        atlas: 20,
        vital: 30,
        modded: 40,
        official: 50,
        build: 60,
        bm: 210,
        rust: 7347,
      },
    }, "123456789012345678").setTitle("turn it up - successfully linked").toJSON();

    expect(embed.title).toBe("turn it up - successfully linked");
    expect(embed.fields?.find((field) => field.name.includes("Rust"))?.value).toBe("**7347** hours");
    expect(embed.fields?.find((field) => field.name.startsWith("Risk Factor"))?.name).toBe("Risk Factor <:risk_5:1487595720956448949>");
    expect(embed.fields).toHaveLength(11);
    expect(embed.fields?.[0]?.value).toContain("<@123456789012345678>");
  });

  it("adds online status below information when profile has current player state", () => {
    const embed = buildUserEmbed("76561198000000000", {
      steamId: "76561198000000000",
      bmId: "918543318",
      bmName: "turn it up",
      personaName: "",
      avatarImage: "",
      firstSeenUnix: OLD_FIRST_SEEN_UNIX,
      serversPlayed: 12,
      nameGaps: [],
      status: {
        isOnline: true,
        serverBmId: "16741517",
        serverName: "Vital 10x",
        onlineUnix: 1767225600,
        offlineUnix: null,
      },
      hours: {
        vitality: 10,
        atlas: 20,
        vital: 30,
        modded: 40,
        official: 50,
        build: 60,
        bm: 210,
        rust: 7347,
      },
    }).toJSON();

    expect(embed.fields?.[1]?.name).toBe("Status");
    expect(embed.fields?.[1]?.value).toBe("<:online:1497993468981805076> **Online** on [Vital 10x](https://www.battlemetrics.com/servers/rust/16741517) for <t:1767225600:R>");
  });

  it("adds offline status with the last tracked server", () => {
    const field = __testables.buildStatusField({
      status: {
        isOnline: false,
        serverBmId: "16741517",
        serverName: "Vital 10x",
        onlineUnix: null,
        offlineUnix: 1767225600,
      },
    } as never);

    expect(field).toEqual({
      name: "Status",
      value: "<:offline:1497993467660599476> **Offline** for <t:1767225600:R>\nLast online on: [Vital 10x](https://www.battlemetrics.com/servers/rust/16741517)",
    });
  });

  it("calculates steam to battlemetrics hour risk", () => {
    const field = __testables.buildRiskFactorField({
      steamId: "76561198000000000",
      bmId: "918543318",
      bmName: "turn it up",
      personaName: "",
      avatarImage: "https://cdn.example/avatar.png",
      firstSeenUnix: OLD_FIRST_SEEN_UNIX,
      serversPlayed: 40,
      nameGaps: [],
      hours: {
        vitality: 0,
        atlas: 0,
        vital: 0,
        modded: 0,
        official: 0,
        build: 0,
        bm: 2200,
        rust: 3300,
      },
    });

    expect(field.name).toBe("Risk Factor <:risk_2:1487595710030417970>");
    expect(field.value).toContain("<:risk_4:1487595713553502359> Steam → BM hour difference: **1100** hours");
    expect(field.value).toContain("<:risk_0:1487595959809609758> Servers played: **40**");
    expect(field.value).toContain("<:risk_0:1487595959809609758> BM hours: **2200**");
  });

  it("uses zero risk when steam hours are close to battlemetrics hours", () => {
    expect(__testables.calculateSteamBmHourRisk({
      steamId: "76561198000000000",
      bmId: "918543318",
      bmName: "turn it up",
      personaName: "",
      avatarImage: "https://cdn.example/avatar.png",
      firstSeenUnix: OLD_FIRST_SEEN_UNIX,
      serversPlayed: 20,
      nameGaps: [],
      hours: {
        vitality: 0,
        atlas: 0,
        vital: 0,
        modded: 0,
        official: 0,
        build: 0,
        bm: 1000,
        rust: 1040,
      },
    })).toEqual({ level: 0, difference: 40 });
  });

  it("rounds to the nearest risk level", () => {
    expect(__testables.calculateSteamBmHourRisk({
      steamId: "76561198000000000",
      bmId: "918543318",
      bmName: "turn it up",
      personaName: "",
      avatarImage: "https://cdn.example/avatar.png",
      firstSeenUnix: OLD_FIRST_SEEN_UNIX,
      serversPlayed: 20,
      nameGaps: [],
      hours: {
        vitality: 0,
        atlas: 0,
        vital: 0,
        modded: 0,
        official: 0,
        build: 0,
        bm: 1933,
        rust: 2200,
      },
    })).toEqual({ level: 1, difference: 267 });
  });

  it("scores recent breaks risk by summing months per break", () => {
    const now = new Date("2026-04-01T00:00:00Z");
    const nowUnix = Math.floor(now.getTime() / 1000);
    const profile = {
      nameGaps: [
        { fromUnix: nowUnix - 335 * 86400, toUnix: nowUnix - 215 * 86400 },
        { fromUnix: nowUnix - 150 * 86400, toUnix: nowUnix - 60 * 86400 },
      ],
    } as never;

    expect(__testables.calculateRecentBreaksRisk(profile, now)).toEqual({ level: 10, breaks: 2, longestBreakDays: 120 });
  });

  it("counts breaks longer than one month", () => {
    const now = new Date("2026-04-01T00:00:00Z");
    const nowUnix = Math.floor(now.getTime() / 1000);
    const profile = {
      nameGaps: [
        { fromUnix: nowUnix - 80 * 86400, toUnix: nowUnix - 45 * 86400 },
      ],
    } as never;

    expect(__testables.calculateRecentBreaksRisk(profile, now)).toEqual({ level: 2, breaks: 1, longestBreakDays: 35 });
  });

  it("scores breaks that ended within four months with the recent multiplier", () => {
    const now = new Date("2026-04-01T00:00:00Z");
    const nowUnix = Math.floor(now.getTime() / 1000);
    const profile = {
      nameGaps: [
        { fromUnix: nowUnix - 95 * 86400, toUnix: nowUnix - 5 * 86400 },
      ],
    } as never;

    expect(__testables.calculateRecentBreaksRisk(profile, now)).toEqual({ level: 6, breaks: 1, longestBreakDays: 90 });
  });

  it("scores breaks that ended four to eight months ago with the middle multiplier", () => {
    const now = new Date("2026-04-01T00:00:00Z");
    const nowUnix = Math.floor(now.getTime() / 1000);
    const profile = {
      nameGaps: [
        { fromUnix: nowUnix - 300 * 86400, toUnix: nowUnix - 180 * 86400 },
      ],
    } as never;

    expect(__testables.calculateRecentBreaksRisk(profile, now)).toEqual({ level: 6, breaks: 1, longestBreakDays: 120 });
  });

  it("scores breaks that ended eight to twelve months ago with the older multiplier", () => {
    const now = new Date("2026-04-01T00:00:00Z");
    const nowUnix = Math.floor(now.getTime() / 1000);
    const profile = {
      nameGaps: [
        { fromUnix: nowUnix - 390 * 86400, toUnix: nowUnix - 270 * 86400 },
      ],
    } as never;

    expect(__testables.calculateRecentBreaksRisk(profile, now)).toEqual({ level: 5, breaks: 1, longestBreakDays: 120 });
  });

  it("ignores breaks that ended more than a year ago", () => {
    const now = new Date("2026-04-01T00:00:00Z");
    const nowUnix = Math.floor(now.getTime() / 1000);
    const profile = {
      nameGaps: [
        { fromUnix: nowUnix - 500 * 86400, toUnix: nowUnix - 400 * 86400 },
      ],
    } as never;

    expect(__testables.calculateRecentBreaksRisk(profile, now)).toEqual({ level: 0, breaks: 0, longestBreakDays: 0 });
  });

  it("scores servers-played risk by 3-server steps under 30", () => {
    expect(__testables.calculateServersPlayedRisk({ serversPlayed: 31 } as never)).toEqual({ level: 0, servers: 31 });
    expect(__testables.calculateServersPlayedRisk({ serversPlayed: 30 } as never)).toEqual({ level: 0, servers: 30 });
    expect(__testables.calculateServersPlayedRisk({ serversPlayed: 27 } as never)).toEqual({ level: 1, servers: 27 });
    expect(__testables.calculateServersPlayedRisk({ serversPlayed: 12 } as never)).toEqual({ level: 6, servers: 12 });
    expect(__testables.calculateServersPlayedRisk({ serversPlayed: 0 } as never)).toEqual({ level: 10, servers: 0 });
  });

  it("scores BM hours risk with 25-hour buckets", () => {
    expect(__testables.calculateBmHoursRisk({ hours: { bm: 0 } } as never)).toEqual({ level: 10, hours: 0 });
    expect(__testables.calculateBmHoursRisk({ hours: { bm: 24 } } as never)).toEqual({ level: 10, hours: 24 });
    expect(__testables.calculateBmHoursRisk({ hours: { bm: 25 } } as never)).toEqual({ level: 10, hours: 25 });
    expect(__testables.calculateBmHoursRisk({ hours: { bm: 50 } } as never)).toEqual({ level: 9, hours: 50 });
    expect(__testables.calculateBmHoursRisk({ hours: { bm: 75 } } as never)).toEqual({ level: 8, hours: 75 });
    expect(__testables.calculateBmHoursRisk({ hours: { bm: 275 } } as never)).toEqual({ level: 0, hours: 275 });
  });

  it("scores account-created risk in 10-day buckets", () => {
    const now = new Date("2026-04-01T00:00:00Z");

    expect(__testables.calculateAccountCreatedRisk({ firstSeenUnix: Math.floor(new Date("2026-03-27T00:00:00Z").getTime() / 1000) } as never, now))
      .toEqual({ level: 10, firstSeenUnix: Math.floor(new Date("2026-03-27T00:00:00Z").getTime() / 1000) });
    expect(__testables.calculateAccountCreatedRisk({ firstSeenUnix: Math.floor(new Date("2026-03-12T00:00:00Z").getTime() / 1000) } as never, now))
      .toEqual({ level: 9, firstSeenUnix: Math.floor(new Date("2026-03-12T00:00:00Z").getTime() / 1000) });
    expect(__testables.calculateAccountCreatedRisk({ firstSeenUnix: Math.floor(new Date("2026-03-02T00:00:00Z").getTime() / 1000) } as never, now))
      .toEqual({ level: 8, firstSeenUnix: Math.floor(new Date("2026-03-02T00:00:00Z").getTime() / 1000) });
  });

  it("includes the account-created line in the risk factor field", () => {
    const firstSeenUnix = Math.floor(new Date("2026-03-27T00:00:00Z").getTime() / 1000);
    const field = __testables.buildRiskFactorField({
      firstSeenUnix,
      hours: {
        bm: null,
        rust: null,
      },
    } as never, new Date("2026-04-01T00:00:00Z"));

    expect(field.value).toContain(`<:risk_10:1487595719790432447> Account created: <t:${firstSeenUnix}:R>`);
  });

  it("includes the linked discord user when provided", () => {
    const embed = buildUserEmbed("76561198000000000", {
      steamId: "76561198000000000",
      bmId: "918543318",
      bmName: "turn it up",
      personaName: "",
      avatarImage: "https://cdn.example/avatar.png",
      firstSeenUnix: OLD_FIRST_SEEN_UNIX,
      serversPlayed: 5,
      nameGaps: [],
      hours: {
        vitality: 10,
        atlas: 20,
        vital: 30,
        modded: 40,
        official: 50,
        build: 60,
        bm: 210,
        rust: null,
      },
    }, "123456789012345678").toJSON();

    expect(embed.fields?.[0]?.value).toContain("<:discord:1496989877831729284> <@123456789012345678>");
  });

  it("marks Rust playtime as private when unavailable", () => {
    const embed = buildUserEmbed("76561198000000000", null).toJSON();

    expect(embed.fields?.find((field) => field.name.includes("Rust"))?.value).toBe("**private**");
    expect(embed.fields?.find((field) => field.name.startsWith("Risk Factor"))?.value).toContain("<:risk_0:1487595959809609758> Steam → BM hour difference: **private**");
  });

  it("can omit the risk factor field", () => {
    const embed = buildUserEmbed("76561198000000000", null, "123456789012345678", false).toJSON();

    expect(embed.fields?.some((field) => field.name.startsWith("Risk Factor"))).toBe(false);
  });

  it("builds paginated /view all embeds with 20 users per page", () => {
    const items = Array.from({ length: 25 }, (_, index) => ({
      discordId: `${index + 1}`,
      steamId: `76561198000000${String(index).padStart(3, "0")}`,
    }));
    const accountMap = new Map<string, SteamAccount>(items.map((item, index) => [item.steamId, {
      steamId: item.steamId,
      bmId: `${9000 + index}`,
      bmName: "",
      avatarImage: "",
      firstSeenAt: null,
      createdAt: "2026-01-01T00:00:00Z",
      updatedAt: "2026-01-01T00:00:00Z",
    }]));

    const firstPage = __testables.buildViewAllPage(items, accountMap, 0).embed.toJSON();
    const secondPage = __testables.buildViewAllPage(items, accountMap, 1).embed.toJSON();

    expect(firstPage.description).toContain("<@1>");
    expect(firstPage.description).toContain("<@20>");
    expect(firstPage.description).not.toContain("<@21>");
    expect(firstPage.footer?.text).toBe("RustLens.com • Page 1/2");

    expect(secondPage.description).toContain("<@21>");
    expect(secondPage.description).toContain("<@25>");
    expect(secondPage.description).not.toContain("<@20>");
    expect(secondPage.footer?.text).toBe("RustLens.com • Page 2/2");
  });

  it("builds paginator buttons with correct disabled states", () => {
    const firstPageRows = __testables.buildViewAllPageButtons("123", 0, 3);
    const lastPageRows = __testables.buildViewAllPageButtons("123", 2, 3);

    const firstPageButtons = firstPageRows[0].toJSON().components;
    const lastPageButtons = lastPageRows[0].toJSON().components;

    expect(firstPageButtons[0]?.disabled).toBe(true);
    expect(firstPageButtons[1]?.disabled).toBe(false);
    expect(lastPageButtons[0]?.disabled).toBe(false);
    expect(lastPageButtons[1]?.disabled).toBe(true);
  });

  it("parses /view all paginator custom ids", () => {
    expect(__testables.parseViewAllPageCustomId("view:all:page:2:123456789012345678")).toEqual({
      page: 2,
      ownerId: "123456789012345678",
    });
    expect(__testables.parseViewAllPageCustomId("view:all:page:x:123456789012345678")).toBeNull();
  });
});
