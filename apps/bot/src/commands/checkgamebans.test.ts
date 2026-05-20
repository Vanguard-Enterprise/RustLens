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
      source: "direct_guild_link",
    });

    expect(line).toContain("<@456>");
    expect(line).toContain("https://steamcommunity.com/profiles/76561198000000000");
    expect(line).toContain("<t:1773019298:R>");
  });

  it("renders an empty-state description when there are no recent game bans", () => {
    const embed = __testables.buildGameBansEmbed([]);

    expect(embed.data.title).toBe("Gamebans - 90 days");
    expect(embed.data.description).toBe("No linked users have a game ban in the last 90 days.");
  });

  it("renders the selected timeframe in the empty state", () => {
    const embed = __testables.buildGameBansEmbed([], "180d");

    expect(embed.data.title).toBe("Gamebans - 180 days");
    expect(embed.data.description).toBe("No linked users have a game ban in the last 180 days.");
  });

  it("groups direct guild links and other linked accounts", () => {
    const embed = __testables.buildGameBansEmbed([
      {
        guildId: "123",
        discordId: "456",
        steamId: "76561198000000000",
        gameBannedAt: "2026-03-08T22:41:38.864Z",
        gameBannedUnix: 1773019298,
        source: "direct_guild_link",
      },
      {
        guildId: "123",
        discordId: "789",
        steamId: "76561198111111111",
        gameBannedAt: "2026-04-08T22:41:38.864Z",
        gameBannedUnix: 1775697698,
        source: "linked_account",
      },
    ], "90d", true);

    expect(embed.data.description).toContain("**Direct guild links**");
    expect(embed.data.description).toContain("<@456>");
    expect(embed.data.description).toContain("**Other linked accounts**");
    expect(embed.data.description).toContain("<@789>");
  });

  it("shows an empty other-linked section in deep mode", () => {
    const embed = __testables.buildGameBansEmbed([
      {
        guildId: "123",
        discordId: "456",
        steamId: "76561198000000000",
        gameBannedAt: "2026-03-08T22:41:38.864Z",
        gameBannedUnix: 1773019298,
        source: "direct_guild_link",
      },
    ], "90d", true);

    expect(embed.data.description).toContain("**Direct guild links**");
    expect(embed.data.description).toContain("**Other linked accounts**\n> None");
  });
});
