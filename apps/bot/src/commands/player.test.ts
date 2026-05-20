import { describe, expect, it } from "vitest";
import { __testables } from "./player.js";

describe("player", () => {
  it("builds the BM private profile error embed", () => {
    const embed = __testables.buildPrivateProfileEmbed("76561198000000000", "918543318").toJSON();

    expect(embed.title).toBe("Error");
    expect(embed.description).toBe("BM profile private");
    expect(embed.fields?.[0]?.name).toBe("Information");
    expect(embed.fields?.[0]?.value).toContain("76561198000000000");
    expect(embed.fields?.[0]?.value).toContain("918543318");
  });

  it("recognizes the backend BM private profile error", () => {
    const error = new Error(
      'backend call failed (404): {"error":"battlemetrics profile private"}',
    ) as Error & { status: number };
    error.status = 404;

    expect(__testables.isBmPrivateProfileError(error)).toBe(true);
    expect(__testables.isBmPrivateProfileError(new Error("backend call failed (500): boom"))).toBe(false);
  });

  it("hides soft links from non-admin PRO users", () => {
    const links = [
      {
        steamId: "76561198000000000",
        discordId: "100000000000000001",
        type: "hard",
        source: "battlemetrics",
        createdAt: "2026-01-01T00:00:00Z",
      },
      {
        steamId: "76561198000000001",
        discordId: "100000000000000001",
        type: "soft",
        source: "bot_command",
        createdAt: "2026-01-02T00:00:00Z",
      },
    ] as const;

    expect(__testables.filterVisibleLinks(links, false).map((link) => link.type)).toEqual(["hard"]);
    expect(__testables.filterVisibleLinks(links, true).map((link) => link.type)).toEqual(["hard", "soft"]);
  });

  it("only includes visible Discord links in the linked Discord dropdown", () => {
    const links = [
      {
        steamId: "76561198000000000",
        discordId: "100000000000000001",
        type: "hard",
        source: "battlemetrics",
        createdAt: "2026-01-01T00:00:00Z",
      },
      {
        steamId: "76561198000000000",
        discordId: "100000000000000002",
        type: "soft",
        source: "bot_command",
        createdAt: "2026-01-02T00:00:00Z",
      },
    ] as const;

    const proRows = __testables.buildLinkedDiscordSelect(links, false);
    const adminRows = __testables.buildLinkedDiscordSelect(links, true);
    const proOptions = proRows[0].components[0].toJSON().options;
    const adminOptions = adminRows[0].components[0].toJSON().options;

    expect(proOptions.map((option) => option.value)).toEqual(["100000000000000001"]);
    expect(proOptions[0].label).toBe("100000000000000001");
    expect(proOptions[0].description).toBe("Strong link");
    expect(adminOptions.map((option) => option.value)).toEqual([
      "100000000000000002",
      "100000000000000001",
    ]);
    expect(adminOptions[0].description).toBe("Soft link");
  });

});
