import { describe, expect, it } from "vitest";
import { __testables } from "./setup-command.js";
import { ComponentType } from "./builders/index.js";
import { ChannelType } from "discord.js";

describe("setup command", () => {
  it("builds setup modal with ordered role options", () => {
    const modal = __testables.buildSetupStartModal().toJSON();

    expect(modal.title).toBe("Setup Roles");
    expect(modal.custom_id).toBe("setup_start_modal");
    expect(modal.components).toHaveLength(2);

    expect(modal.components[0]).toEqual({
      type: ComponentType.Label,
      label: "Roles to create",
      component: {
        type: ComponentType.CheckboxGroup,
        custom_id: "setup_roles",
        required: true,
        min_values: 1,
        max_values: 8,
        options: [
          { label: "Keyrole", value: "keyrole" },
          { label: "Leader", value: "leader" },
          { label: "Co-Leader", value: "co_leader" },
          { label: "Member", value: "member" },
          { label: "Builder", value: "builder" },
          { label: "Electrician", value: "electrician" },
          { label: "Monument", value: "monument" },
          { label: "PvP", value: "pvp" },
        ],
      },
    });

    expect(modal.components[1]).toEqual({
      type: ComponentType.Label,
      label: "Channels to create",
      component: {
        type: ComponentType.CheckboxGroup,
        custom_id: "setup_channels",
        required: false,
        min_values: 0,
        max_values: 5,
        options: [
          { label: "Voice Calls", value: "voice_calls" },
          { label: "Announcements", value: "announcements" },
          { label: "Wipe react", value: "wipe_react" },
          { label: "General", value: "general" },
          { label: "Pics", value: "pics" },
        ],
      },
    });
  });

  it("normalizes modal field values", () => {
    expect(__testables.normalizeModalFieldValue(undefined)).toEqual([]);
    expect(__testables.normalizeModalFieldValue(["one", "two"])).toEqual([
      "one",
      "two",
    ]);
    expect(__testables.normalizeModalFieldValue("one,two")).toEqual([
      "one",
      "two",
    ]);
  });

  it("extracts values from modal fields", () => {
    expect(
      __testables.extractFieldValues({
        values: ["keyrole", "leader"],
      }),
    ).toEqual(["keyrole", "leader"]);
    expect(
      __testables.extractFieldValues({
        values: ["keyrole", "leader"],
      }),
    ).toEqual(["keyrole", "leader"]);
    expect(
      __testables.extractFieldValues({
        value: "keyrole,leader",
      }),
    ).toEqual(["keyrole", "leader"]);
  });

  it("orders selected role keys by configured hierarchy", () => {
    expect(
      __testables.getSelectedRoleKeys([
        "member",
        "keyrole",
        "pvp",
        "leader",
      ]),
    ).toEqual(["keyrole", "leader", "member", "pvp"]);
  });

  it("requires member when member-scoped roles are selected", () => {
    expect(__testables.validateSetupSelections(["builder"], [])).toBe(
      "Builder, Electrician, Monument, and PvP require Member to be selected too.",
    );
    expect(__testables.validateSetupSelections(["member", "builder"], [])).toBeNull();
  });

  it("requires dependent roles for channel setup", () => {
    expect(
      __testables.validateSetupSelections(["member"], ["announcements"]),
    ).toBe("Channel creation requires these roles too: Leader, Co-Leader.");
    expect(
      __testables.validateSetupSelections(
        ["leader", "co_leader", "member"],
        ["general"],
      ),
    ).toBe("General and Voice Calls channel setup require the Builder role to be selected too.");
  });

  it("plans bootstrap for a single existing setup channel", () => {
    expect(
      __testables.planSetupBootstrap(
        [
          {
            id: "general-id",
            name: "general",
            type: ChannelType.GuildText,
          },
        ],
        "general-id",
        ["general"],
      ),
    ).toEqual({
      bootstrapChannelId: "general-id",
      blockingConflicts: [],
      error: null,
    });
  });

  it("rejects channel setup when the server has more than one channel", () => {
    expect(
      __testables.planSetupBootstrap(
        [
          {
            id: "general-id",
            name: "general",
            type: ChannelType.GuildText,
          },
          {
            id: "other-id",
            name: "other",
            type: ChannelType.GuildText,
          },
        ],
        "general-id",
        ["general"],
      ),
    ).toEqual({
      bootstrapChannelId: null,
      blockingConflicts: [],
      error:
        "Channel setup can only run when the server has exactly one existing channel. Delete the extras first.",
    });
  });

  it("rejects channel setup when not launched from the only server channel", () => {
    expect(
      __testables.planSetupBootstrap(
        [
          {
            id: "general-id",
            name: "general",
            type: ChannelType.GuildText,
          },
        ],
        "different-id",
        ["general"],
      ),
    ).toEqual({
      bootstrapChannelId: null,
      blockingConflicts: [],
      error:
        "Run /setup start from the server's only channel so RustLens can replace it.",
    });
  });
});
