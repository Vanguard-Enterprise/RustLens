import { describe, expect, it } from "vitest";
import { CheckboxGroupBuilder } from "./checkbox-group-builder.js";
import { ComponentType } from "./types.js";

describe("CheckboxGroupBuilder", () => {
  it("produces correct JSON with all fields", () => {
    const json = new CheckboxGroupBuilder()
      .setCustomId("areas")
      .setMinValues(1)
      .setMaxValues(3)
      .addOptions(
        { label: "UI", value: "ui" },
        { label: "API", value: "api" },
        { label: "DB", value: "db" },
      )
      .toJSON();

    expect(json).toEqual({
      type: ComponentType.CheckboxGroup,
      custom_id: "areas",
      min_values: 1,
      max_values: 3,
      options: [
        { label: "UI", value: "ui" },
        { label: "API", value: "api" },
        { label: "DB", value: "db" },
      ],
    });
  });

  it("produces minimal JSON without min/max values", () => {
    const json = new CheckboxGroupBuilder()
      .setCustomId("tags")
      .addOption({ label: "One", value: "1" })
      .toJSON();

    expect(json).toEqual({
      type: ComponentType.CheckboxGroup,
      custom_id: "tags",
      options: [{ label: "One", value: "1" }],
    });
    expect(json).not.toHaveProperty("min_values");
    expect(json).not.toHaveProperty("max_values");
  });

  it("throws when custom_id is missing", () => {
    const builder = new CheckboxGroupBuilder().addOption({
      label: "A",
      value: "a",
    });
    expect(() => builder.toJSON()).toThrow("custom_id is required");
  });

  it("throws when no options are provided", () => {
    const builder = new CheckboxGroupBuilder().setCustomId("x");
    expect(() => builder.toJSON()).toThrow("at least one option");
  });

  it("supports fluent chaining", () => {
    const builder = new CheckboxGroupBuilder();
    expect(builder.setCustomId("x")).toBe(builder);
    expect(builder.setMinValues(1)).toBe(builder);
    expect(builder.setMaxValues(5)).toBe(builder);
    expect(builder.addOption({ label: "A", value: "a" })).toBe(builder);
    expect(builder.addOptions({ label: "B", value: "b" })).toBe(builder);
  });
});
