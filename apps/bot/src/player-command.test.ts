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
});
