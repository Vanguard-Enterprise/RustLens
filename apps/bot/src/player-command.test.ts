import { describe, expect, it } from "vitest";
import { __testables } from "./player-command.js";

describe("player-command", () => {
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

  it("builds the BM-only player embed without Steam fields or buttons", () => {
    const embed = __testables.buildBmOnlyPlayerEmbed({
      bmId: "918543318",
      bmName: "TestPlayer",
      createdAt: "2024-01-15T10:00:00Z",
      servers: [
        { serverId: "1001", serverName: "Vitality 10x", timePlayedSec: 7200 },
        { serverId: "1002", serverName: "Atlas Solo", timePlayedSec: 3600 },
      ],
    }).toJSON();

    expect(embed.title).toBe("Player - TestPlayer");
    const info = embed.fields?.[0];
    expect(info?.name).toBe("Information");
    expect(info?.value).toContain("918543318");
    expect(info?.value).not.toContain("steamcommunity.com");
    expect(info?.value).toContain("First seen");

    const servers = embed.fields?.[1];
    expect(servers?.name).toBe("Servers played: 2");
    expect(servers?.value).toContain("Vitality 10x");
    expect(servers?.value).toContain("**2** hours");
  });

  it("falls back to BM ID title when bmName is empty", () => {
    const embed = __testables.buildBmOnlyPlayerEmbed({
      bmId: "918543318",
      bmName: "",
      servers: [],
    }).toJSON();
    expect(embed.title).toBe("Player - 918543318");
  });
});
