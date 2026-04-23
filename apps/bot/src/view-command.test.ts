import { describe, expect, it } from "vitest";
import { __testables, buildUserEmbed } from "./view-command.js";

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
      avatarImage: "https://cdn.example/avatar.png",
      firstSeenUnix: 1766616480,
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
    expect(embed.fields?.[0]?.value).toContain("https://www.battlemetrics.com/players/918543318");
    expect(embed.fields?.find((field) => field.name.includes("Rust"))?.value).toBe("**7347** hours");
    expect(embed.fields?.find((field) => field.name.startsWith("Risk Factor"))?.name).toBe("Risk Factor <:risk_10:1487595719790432447>");
    expect(embed.fields).toHaveLength(11);
  });

  it("calculates steam to battlemetrics hour risk", () => {
    const field = __testables.buildRiskFactorField({
      steamId: "76561198000000000",
      bmId: "918543318",
      bmName: "turn it up",
      avatarImage: "https://cdn.example/avatar.png",
      firstSeenUnix: 1766616480,
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

    expect(field.name).toBe("Risk Factor <:risk_4:1487595713553502359>");
    expect(field.value).toBe("<:risk_4:1487595713553502359> Steam → BM hour difference: **1100** hours.");
  });

  it("uses zero risk when steam hours are close to battlemetrics hours", () => {
    expect(__testables.calculateSteamBmHourRisk({
      steamId: "76561198000000000",
      bmId: "918543318",
      bmName: "turn it up",
      avatarImage: "https://cdn.example/avatar.png",
      firstSeenUnix: 1766616480,
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
      avatarImage: "https://cdn.example/avatar.png",
      firstSeenUnix: 1766616480,
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

  it("includes the linked discord user when provided", () => {
    const embed = buildUserEmbed("76561198000000000", {
      steamId: "76561198000000000",
      bmId: "918543318",
      bmName: "turn it up",
      avatarImage: "https://cdn.example/avatar.png",
      firstSeenUnix: 1766616480,
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
    expect(embed.fields?.find((field) => field.name.startsWith("Risk Factor"))?.value).toBe("<:risk_0:1487595959809609758> Steam → BM hour difference: **private**.");
  });

  it("can omit the risk factor field", () => {
    const embed = buildUserEmbed("76561198000000000", null, "123456789012345678", false).toJSON();

    expect(embed.fields?.some((field) => field.name.startsWith("Risk Factor"))).toBe(false);
  });
});
