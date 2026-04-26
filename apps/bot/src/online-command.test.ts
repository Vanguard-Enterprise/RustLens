import { describe, expect, it } from "vitest";
import { __testables } from "./online-command.js";

describe("online-command", () => {
  it("splits long server summaries across embed pages", () => {
    const items = Array.from({ length: 80 }, (_, index) => ({
      guildId: "123",
      discordId: `${1000 + index}`,
      steamId: `7656119800000${index}`,
      playerBmId: `${900000000 + index}`,
      bmName: `Player ${index}`,
      serverBmId: "16741517",
      serverName: "Atlas - EU 2X Monthly | Vanilla+ | No BP Wipes",
      onlineUnix: 1773019298 + index,
    }));

    const embeds = __testables.buildPagedEmbeds(items);

    expect(embeds.length).toBeGreaterThan(1);
    expect(embeds[0].data.title).toBe("Online summary");
    expect(embeds[1].data.title).toBe("Online summary cont.");
  });

  it("formats linked bm player lines", () => {
    const line = __testables.buildSummaryLine({
      guildId: "123",
      discordId: "456",
      steamId: "76561198000000000",
      playerBmId: "918543318",
      bmName: "turn it up",
      serverBmId: "16741517",
      serverName: "Atlas",
      onlineUnix: 1773019298,
    });

    expect(line).toContain("<@456>");
    expect(line).toContain("https://www.battlemetrics.com/rcon/players/918543318");
    expect(line).toContain("<t:1773019298:R>");
  });
});
