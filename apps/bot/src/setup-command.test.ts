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
        max_values: 9,
        options: [
          { label: "All", value: "all_roles" },
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
        max_values: 7,
        options: [
          { label: "All", value: "all_channels" },
          { label: "Voice Calls", value: "voice_calls" },
          { label: "Announcements", value: "announcements" },
          { label: "Wipe react", value: "wipe_react" },
          { label: "Invite", value: "invite" },
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

  it("extracts values from nested modal components", () => {
    expect(
      __testables.extractComponentValues(
        [
          {
            component: {
              customId: "setup_roles",
              values: ["leader", "member"],
            },
          },
          {
            component: {
              customId: "setup_channels",
              values: ["voice_calls", "general"],
            },
          },
        ],
        "setup_channels",
      ),
    ).toEqual(["voice_calls", "general"]);
  });

  it("orders selected role keys by configured hierarchy", () => {
    expect(
      __testables.getSelectedRoleKeys([
        "member",
        "keyrole",
        "pvp",
        "leader",
      ]),
    ).toEqual(["keyrole", "leader", "bot", "member", "pvp"]);
    expect(__testables.getSelectedRoleKeys(["all_roles"])).toEqual([
      "keyrole",
      "leader",
      "co_leader",
      "bot",
      "member",
      "builder",
      "electrician",
      "monument",
      "pvp",
    ]);
  });

  it("expands the all channels option", () => {
    expect(__testables.getSelectedChannelKeys(["all_channels"])).toEqual([
      "voice_calls",
      "announcements",
      "wipe_react",
      "invite",
      "general",
      "pics",
    ]);
  });

  it("omits builder-only channels when builder is not selected", () => {
    expect(
      __testables.getTargetChannelNames(
        ["general", "voice_calls"],
        ["leader", "co_leader", "bot", "member"],
      ),
    ).not.toContain("build");
    expect(
      __testables.getTargetChannelNames(
        ["general", "voice_calls"],
        ["leader", "co_leader", "bot", "member"],
      ),
    ).not.toContain("builder call");
  });

  it("includes invite, wipe log, and afk channels in the target channel names", () => {
    expect(
      __testables.getTargetChannelNames(
        ["announcements", "invite", "voice_calls"],
        ["leader", "co_leader", "bot", "member"],
      ),
    ).toEqual(
      expect.arrayContaining([
        "Important",
        "announcements",
        "press for invite",
        "wipe log",
        "Voice Channels",
        "afk",
      ]),
    );
  });

  it("does not include wipe log when only invite is selected", () => {
    expect(
      __testables.getTargetChannelNames(
        ["invite"],
        ["leader", "co_leader", "bot", "member"],
      ),
    ).not.toContain("wipe log");
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
    ).toBeNull();
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
        ["leader", "co_leader", "bot", "member"],
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
        ["leader", "co_leader", "bot", "member"],
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
        ["leader", "co_leader", "bot", "member"],
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
