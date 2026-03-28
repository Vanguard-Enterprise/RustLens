import { describe, expect, it } from "vitest";
import { ComponentType } from "./builders/index.js";
import { __testables } from "./wipereact-command.js";

describe("wipereact command", () => {
  it("builds the create modal with timezone input and checkbox options", () => {
    const modal = __testables.buildWipeReactModal("wipereact_create_modal").toJSON();

    expect(modal.title).toBe("Wipe React");
    expect(modal.custom_id).toBe("wipereact_create_modal");
    expect(modal.components).toHaveLength(5);
    expect(modal.components[0].component).toEqual({
      type: ComponentType.TextInput,
      custom_id: "wipereact_name",
      style: 1,
      max_length: 100,
      required: true,
      value: "",
    });
    expect(modal.components[3].component).toEqual({
      type: ComponentType.TextInput,
      custom_id: "wipereact_timezone",
      style: 1,
      placeholder: "Defaults to Europe/London",
      max_length: 100,
      required: false,
      value: "",
    });
    expect(modal.components[4].component).toEqual({
      type: ComponentType.CheckboxGroup,
      custom_id: "wipereact_options",
      required: false,
      min_values: 0,
      max_values: 2,
      options: [
        { label: "Ask for late reason?", value: "ask_late_reason" },
        { label: "Ask for declined reason?", value: "ask_declined_reason" },
      ],
    });
  });

  it("registers delete, missing, and react management subcommands", () => {
    const command = __testables.wipeReactCommand.toJSON();
    const options = command.options as any[];

    expect(options.map((option) => option.name)).toEqual([
      "create",
      "delete",
      "missing",
      "react",
    ]);

    const reactGroup = options.find((option) => option.name === "react");
    expect((reactGroup?.options ?? []).map((option: any) => option.name)).toEqual(["add", "remove"]);
  });

  it("parses flexible date and time formats", () => {
    const now = new Date(Date.UTC(2026, 2, 28, 12, 0, 0, 0));

    expect(__testables.parseFlexibleDateTime("01.02.26 18:30", "Europe/London", now)).toEqual({
      date: new Date(Date.UTC(2026, 1, 1, 18, 30, 0, 0)),
      error: null,
    });
    expect(__testables.parseFlexibleDateTime("1-2-2027 18.45", "Europe/London", now)).toEqual({
      date: new Date(Date.UTC(2027, 1, 1, 18, 45, 0, 0)),
      error: null,
    });
    expect(__testables.parseFlexibleDateTime("1.2", "Europe/London", now)).toEqual({
      date: new Date(Date.UTC(2026, 1, 1, 0, 0, 0, 0)),
      error: null,
    });
    expect(__testables.parseFlexibleDateTime("18-45", "Europe/London", now)).toEqual({
      date: new Date(Date.UTC(2026, 2, 28, 18, 45, 0, 0)),
      error: null,
    });
  });

  it("parses button actions and reason toggles", () => {
    expect(__testables.parseActionButton("wipereact_accept")).toEqual({
      type: "accepted",
      requiresReason: false,
    });
    expect(__testables.parseActionButton("wipereact_decline:1")).toEqual({
      type: "declined",
      requiresReason: true,
    });
    expect(__testables.parseActionButton("wipereact_late:0")).toEqual({
      type: "late",
      requiresReason: false,
    });
  });

  it("moves a user into the selected reaction bucket", () => {
    const result = __testables.applyUserReaction(
      {
        name: "EU Monday",
        description: "Main roam",
        timeZone: "Europe/London",
        scheduledUnix: 1774557951,
        creatorId: "999",
        askLateReason: true,
        askDeclinedReason: true,
        acceptedUserIds: ["1"],
        declinedUserIds: ["2"],
        lateUserIds: ["3"],
        embedTimestamp: "2026-03-02T20:44:18.917Z",
      },
      "2",
      "accepted",
    );

    expect(result.changed).toBe(true);
    expect(result.state.acceptedUserIds).toEqual(["1", "2"]);
    expect(result.state.declinedUserIds).toEqual([]);
    expect(result.state.lateUserIds).toEqual(["3"]);
  });

  it("parses the wipe react message state from embeds and buttons", () => {
    const state = __testables.parseWipeReactMessage({
      embeds: [
        {
          title: "Friday Wipe",
          description: "Be online 15 minutes early.",
          timestamp: "2026-03-02T20:44:18.917Z",
          fields: [
            { name: "Timezone", value: "Europe/London" },
            {
              name: "Date & Time",
              value: "<t:1774557951:F>\n<t:1774557951:R>",
            },
            {
              name: "✅ Accepted (2)",
              value: "<@500975995751366657>\n<@332835624900100096>",
            },
            {
              name: "❌ Declined (1)",
              value: "<@500975995751366657>",
            },
            {
              name: "🕑 Late (0)",
              value: "-",
            },
          ],
        },
      ],
      components: [
        {
          components: [
            { customId: "wipereact_accept" },
            { customId: "wipereact_decline:1" },
            { customId: "wipereact_late:0" },
            { customId: "wipereact_edit:1234567890" },
          ],
        },
      ],
    } as any);

    expect(state).toEqual({
      name: "Friday Wipe",
      description: "Be online 15 minutes early.",
      timeZone: "Europe/London",
      scheduledUnix: 1774557951,
      creatorId: "1234567890",
      askLateReason: false,
      askDeclinedReason: true,
      acceptedUserIds: ["500975995751366657", "332835624900100096"],
      declinedUserIds: ["500975995751366657"],
      lateUserIds: [],
      embedTimestamp: "2026-03-02T20:44:18.917Z",
    });
  });

  it("builds the missing reactants embed with quoted mentions", () => {
    const embed = __testables.buildMissingReactantsEmbed(
      {
        name: "Friday Wipe",
        description: "Be online 15 minutes early.",
        timeZone: "Europe/London",
        scheduledUnix: 1774557951,
        creatorId: "1234567890",
        askLateReason: false,
        askDeclinedReason: true,
        acceptedUserIds: [],
        declinedUserIds: [],
        lateUserIds: [],
        embedTimestamp: "2026-03-02T20:44:18.917Z",
      },
      { name: "Member" } as any,
      ["111", "222"],
    );

    expect(embed.title).toBe("Missing reactants");
    expect(embed.description).toBe("> * <@111>\n> * <@222>");
  });
});
