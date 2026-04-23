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
    expect(embed.fields).toHaveLength(11);
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
  });

  it("can omit the risk factor field", () => {
    const embed = buildUserEmbed("76561198000000000", null, "123456789012345678", false).toJSON();

    expect(embed.fields?.some((field) => field.name.startsWith("Risk Factor"))).toBe(false);
  });
});
