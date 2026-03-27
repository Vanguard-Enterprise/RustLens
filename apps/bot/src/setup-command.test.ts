import { describe, expect, it } from "vitest";
import { __testables } from "./setup-command.js";
import { ComponentType } from "./builders/index.js";

describe("setup command", () => {
  it("builds setup modal with ordered role options", () => {
    const modal = __testables.buildSetupStartModal().toJSON();

    expect(modal.title).toBe("Setup Roles");
    expect(modal.custom_id).toBe("setup_start_modal");
    expect(modal.components).toHaveLength(1);

    expect(modal.components[0]).toEqual({
      type: ComponentType.Label,
      label: "Roles",
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
    expect(__testables.validateSelectedRoleKeys(["builder"])).toBe(
      "Builder, Electrician, Monument, and PvP require Member to be selected too.",
    );
    expect(__testables.validateSelectedRoleKeys(["member", "builder"])).toBeNull();
  });
});
