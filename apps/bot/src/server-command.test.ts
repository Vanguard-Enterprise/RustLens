import { describe, expect, it } from "vitest";
import { __testables } from "./server-command.js";

describe("server-command", () => {
  it("builds autocomplete labels with server name and bm id", () => {
    expect(__testables.buildTrackedServerChoiceName({
      bmId: "16741517",
      name: "Atlas - EU 2X Monthly | Vanilla+ | No BP Wipes",
    })).toContain("(16741517)");
  });

  it("formats tracked server categories", () => {
    expect(__testables.formatTrackedServerCategories({
      isVitality: true,
      isAtlas: false,
      isVital: true,
      isModded: true,
      isOfficial: false,
      isBuild: false,
    })).toBe("Vitality, Vital, Modded");
  });

  it("builds a tracked server modal with category options", () => {
    const modal = __testables.buildTrackedServerModal({
      bmId: "16741517",
      name: "Atlas Main",
      isVitality: false,
      isAtlas: true,
      isVital: false,
      isModded: true,
      isOfficial: false,
      isBuild: false,
      lastPolledAt: null,
      lastPollError: "",
    }).toJSON();

    expect(modal.title).toBe("Edit Tracked Server");
    expect(modal.components).toHaveLength(3);
  });

  it("builds the tracked server list description", () => {
    const description = __testables.buildTrackedServerListDescription([{
      bmId: "16741517",
      name: "Atlas Main",
      isVitality: false,
      isAtlas: true,
      isVital: false,
      isModded: true,
      isOfficial: false,
      isBuild: false,
      lastPolledAt: "2026-04-23T19:43:35.536Z",
      lastPollError: "",
    }]);

    expect(description).toContain("Atlas Main");
    expect(description).toContain("Categories: Atlas, Modded");
    expect(description).toContain("BM: `16741517`");
  });
});
