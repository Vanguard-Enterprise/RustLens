import { describe, expect, it } from "vitest";
import { __testables } from "./checkgamebans.js";

describe("checkgamebans", () => {
  it("formats a recent game ban line", () => {
    const line = __testables.buildGameBanLine({
      guildId: "123",
      discordId: "456",
      steamId: "76561198000000000",
      gameBannedAt: "2026-03-08T22:41:38.864Z",
      gameBannedUnix: 1773019298,
    });

    expect(line).toContain("<@456>");
    expect(line).toContain("https://steamcommunity.com/profiles/76561198000000000");
    expect(line).toContain("<t:1773019298:R>");
  });

  it("renders an empty-state description when there are no recent game bans", () => {
    const embed = __testables.buildGameBansEmbed([]);

    expect(embed.data.title).toBe("Gamebans < 90 days");
    expect(embed.data.description).toBe("No linked users have a game ban in the last 90 days.");
  });
});
