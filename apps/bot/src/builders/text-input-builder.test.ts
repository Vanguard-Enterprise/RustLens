import { describe, expect, it } from "vitest";
import { TextInputBuilder } from "./text-input-builder.js";
import { ComponentType, TextInputStyle } from "./types.js";

describe("TextInputBuilder", () => {
  it("produces correct JSON with all fields", () => {
    const json = new TextInputBuilder()
      .setCustomId("subject")
      .setStyle(TextInputStyle.Paragraph)
      .setPlaceholder("Enter text...")
      .setMaxLength(500)
      .setRequired(false)
      .setValue("prefilled")
      .setLabel("Subject")
      .toJSON();

    expect(json).toEqual({
      type: ComponentType.TextInput,
      custom_id: "subject",
      style: TextInputStyle.Paragraph,
      placeholder: "Enter text...",
      max_length: 500,
      required: false,
      value: "prefilled",
      label: "Subject",
    });
  });

  it("produces minimal JSON with only required fields", () => {
    const json = new TextInputBuilder().setCustomId("name").toJSON();

    expect(json).toEqual({
      type: ComponentType.TextInput,
      custom_id: "name",
      style: TextInputStyle.Short,
    });
    expect(json).not.toHaveProperty("placeholder");
    expect(json).not.toHaveProperty("max_length");
    expect(json).not.toHaveProperty("required");
    expect(json).not.toHaveProperty("value");
    expect(json).not.toHaveProperty("label");
  });

  it("throws when custom_id is missing", () => {
    const builder = new TextInputBuilder();
    expect(() => builder.toJSON()).toThrow("custom_id is required");
  });

  it("supports fluent chaining", () => {
    const builder = new TextInputBuilder();
    expect(builder.setCustomId("x")).toBe(builder);
    expect(builder.setStyle(TextInputStyle.Short)).toBe(builder);
    expect(builder.setPlaceholder("p")).toBe(builder);
    expect(builder.setMaxLength(100)).toBe(builder);
    expect(builder.setRequired(true)).toBe(builder);
    expect(builder.setValue("v")).toBe(builder);
    expect(builder.setLabel("l")).toBe(builder);
  });

  it("defaults style to Short", () => {
    const json = new TextInputBuilder().setCustomId("x").toJSON();
    expect(json.style).toBe(TextInputStyle.Short);
  });
});
