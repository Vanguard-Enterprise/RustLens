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
      createdAt: "2026-03-02T20:44:18.917Z",
    }).toJSON();

    expect(embed.title).toBe("Player Profile");
    expect(embed.thumbnail?.url).toBe("https://cdn.example/avatar.png");
    expect(embed.fields?.[0]?.value).toContain("https://steamcommunity.com/profiles/76561198000000000");
    expect(embed.fields?.[0]?.value).toContain("https://www.battlemetrics.com/players/918543318");
    expect(embed.fields).toHaveLength(10);
  });
});
