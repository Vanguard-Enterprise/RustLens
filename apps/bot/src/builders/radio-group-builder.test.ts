import { describe, expect, it } from "vitest";
import { RadioGroupBuilder } from "./radio-group-builder.js";
import { ComponentType } from "./types.js";

describe("RadioGroupBuilder", () => {
  it("produces correct JSON with options", () => {
    const json = new RadioGroupBuilder()
      .setCustomId("pick_one")
      .addOptions(
        { label: "A", value: "a" },
        { label: "B", value: "b" },
      )
      .toJSON();

    expect(json).toEqual({
      type: ComponentType.RadioGroup,
      custom_id: "pick_one",
      options: [
        { label: "A", value: "a" },
        { label: "B", value: "b" },
      ],
    });
  });

  it("supports addOption for single options", () => {
    const json = new RadioGroupBuilder()
      .setCustomId("x")
      .addOption({ label: "One", value: "1" })
      .addOption({ label: "Two", value: "2" })
      .toJSON();

    expect(json.options).toHaveLength(2);
    expect(json.options[0]).toEqual({ label: "One", value: "1" });
  });

  it("throws when custom_id is missing", () => {
    const builder = new RadioGroupBuilder().addOption({
      label: "A",
      value: "a",
    });
    expect(() => builder.toJSON()).toThrow("custom_id is required");
  });

  it("throws when no options are provided", () => {
    const builder = new RadioGroupBuilder().setCustomId("x");
    expect(() => builder.toJSON()).toThrow("at least one option");
  });

  it("supports fluent chaining", () => {
    const builder = new RadioGroupBuilder();
    expect(builder.setCustomId("x")).toBe(builder);
    expect(builder.addOption({ label: "A", value: "a" })).toBe(builder);
    expect(builder.addOptions({ label: "B", value: "b" })).toBe(builder);
  });

  it("copies options to prevent external mutation", () => {
    const options = [{ label: "A", value: "a" }];
    const builder = new RadioGroupBuilder()
      .setCustomId("x")
      .addOptions(...options);
    const json = builder.toJSON();

    options.push({ label: "B", value: "b" });
    expect(json.options).toHaveLength(1);
  });
});
