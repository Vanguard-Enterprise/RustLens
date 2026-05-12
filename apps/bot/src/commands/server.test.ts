import { describe, expect, it } from "vitest";
import { __testables } from "./server.js";

describe("server", () => {
  it("formats an empty guild tracked server list", () => {
    expect(__testables.buildGuildTrackedServerListDescription([])).toBe("> * None");
  });

  it("formats a guild tracked server list as bulleted names", () => {
    const description = __testables.buildGuildTrackedServerListDescription([
      { guildId: "1", bmId: "100", name: "Vital EU Main" },
      { guildId: "1", bmId: "200", name: "Atlas EU 2x" },
    ]);
    expect(description).toBe("> * Vital EU Main\n> * Atlas EU 2x");
  });

  it("builds an autocomplete choice combining name and bm id", () => {
    expect(__testables.buildTrackedServerChoiceName({ bmId: "16741517", name: "Vital 10x" })).toBe("Vital 10x (16741517)");
  });

  it("trims long autocomplete choice labels to 100 chars", () => {
    const longName = "a".repeat(120);
    const result = __testables.buildTrackedServerChoiceName({ bmId: "1", name: longName });
    expect(result.length).toBe(100);
    expect(result.endsWith("...")).toBe(true);
  });
});
