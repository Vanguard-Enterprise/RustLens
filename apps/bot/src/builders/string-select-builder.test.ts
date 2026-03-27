import { describe, expect, it } from "vitest";
import { StringSelectBuilder } from "./string-select-builder.js";
import { ComponentType } from "./types.js";

describe("StringSelectBuilder", () => {
  it("produces correct JSON with all fields", () => {
    const json = new StringSelectBuilder()
      .setCustomId("priority")
      .setPlaceholder("Select priority...")
      .setMinValues(1)
      .setMaxValues(1)
      .addOptions(
        { label: "Low", value: "low" },
        { label: "Medium", value: "medium" },
        { label: "High", value: "high" },
      )
      .toJSON();

    expect(json).toEqual({
      type: ComponentType.StringSelect,
      custom_id: "priority",
      placeholder: "Select priority...",
      min_values: 1,
      max_values: 1,
      options: [
        { label: "Low", value: "low" },
        { label: "Medium", value: "medium" },
        { label: "High", value: "high" },
      ],
    });
  });

  it("produces minimal JSON without optional fields", () => {
    const json = new StringSelectBuilder()
      .setCustomId("pick")
      .addOption({ label: "Only", value: "only" })
      .toJSON();

    expect(json).toEqual({
      type: ComponentType.StringSelect,
      custom_id: "pick",
      options: [{ label: "Only", value: "only" }],
    });
    expect(json).not.toHaveProperty("placeholder");
    expect(json).not.toHaveProperty("min_values");
    expect(json).not.toHaveProperty("max_values");
  });

  it("throws when custom_id is missing", () => {
    const builder = new StringSelectBuilder().addOption({
      label: "A",
      value: "a",
    });
    expect(() => builder.toJSON()).toThrow("custom_id is required");
  });

  it("throws when no options are provided", () => {
    const builder = new StringSelectBuilder().setCustomId("x");
    expect(() => builder.toJSON()).toThrow("at least one option");
  });

  it("supports fluent chaining", () => {
    const builder = new StringSelectBuilder();
    expect(builder.setCustomId("x")).toBe(builder);
    expect(builder.setPlaceholder("p")).toBe(builder);
    expect(builder.setMinValues(1)).toBe(builder);
    expect(builder.setMaxValues(3)).toBe(builder);
    expect(builder.addOption({ label: "A", value: "a" })).toBe(builder);
    expect(builder.addOptions({ label: "B", value: "b" })).toBe(builder);
  });
});
