import { describe, expect, it } from "vitest";
import { __testables } from "./compare-command.js";

describe("compare-command", () => {
  it("formats wipe react autocomplete labels with name and date", () => {
    expect(__testables.formatWipeReactChoiceName({
      guildId: "1",
      channelId: "2",
      messageId: "3",
      creatorId: "4",
      name: "Thursday Wipe",
      description: "",
      timeZone: "Europe/London",
      scheduledUnix: 1774557951,
      askLateReason: false,
      askDeclinedReason: false,
      acceptedUserIds: [],
      declinedUserIds: [],
      lateUserIds: [],
      embedTimestamp: "2026-03-02T20:44:18.917Z",
    })).toContain("Thursday Wipe | ");
  });

  it("builds wipe react comparison embeds with out-of-sync fields", () => {
    const embed = __testables.buildWipeReactComparisonEmbed(
      {
        guildId: "1",
        channelId: "2",
        messageId: "3",
        creatorId: "4",
        name: "Thursday Wipe",
        description: "",
        timeZone: "Europe/London",
        scheduledUnix: 1774557951,
        askLateReason: false,
        askDeclinedReason: false,
        acceptedUserIds: [],
        declinedUserIds: [],
        lateUserIds: [],
        embedTimestamp: "2026-03-02T20:44:18.917Z",
      },
      ["11"],
      ["12"],
      ["13"],
    ).toJSON();

    expect(embed.title).toBe("Wipe React Comparison");
    expect(embed.description).toContain("3 users found out of sync");
    expect(embed.fields).toHaveLength(3);
  });

  it("builds server comparison embeds with both mismatch buckets", () => {
    const embed = __testables.buildServerComparisonEmbed(
      "Vitality 10x EU",
      ["11"],
      ["12", "13"],
    ).toJSON();

    expect(embed.title).toBe("Server Comparison ");
    expect(embed.description).toContain("3 users out of sync");
    expect(embed.fields).toHaveLength(2);
  });

  it("builds wipe react vs server embeds with server-specific mismatch buckets", () => {
    const embed = __testables.buildWipeReactServerComparisonEmbed(
      {
        guildId: "1",
        channelId: "2",
        messageId: "3",
        creatorId: "4",
        name: "Thursday Wipe",
        description: "",
        timeZone: "Europe/London",
        scheduledUnix: 1774557951,
        askLateReason: false,
        askDeclinedReason: false,
        acceptedUserIds: [],
        declinedUserIds: [],
        lateUserIds: [],
        embedTimestamp: "2026-03-02T20:44:18.917Z",
      },
      "Vitality 10x EU",
      ["11"],
      ["12"],
      ["13"],
    ).toJSON();

    expect(embed.title).toBe("Wipe React vs Server Comparison");
    expect(embed.description).toContain("Vitality 10x EU");
    expect(embed.description).toContain("3 users found out of sync");
    expect(embed.fields).toHaveLength(3);
  });
});
