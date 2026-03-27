import { describe, expect, it } from "vitest";
import { ModalV2Builder } from "./modal-v2-builder.js";
import { RadioGroupBuilder } from "./radio-group-builder.js";
import { CheckboxGroupBuilder } from "./checkbox-group-builder.js";
import { StringSelectBuilder } from "./string-select-builder.js";
import { TextInputBuilder } from "./text-input-builder.js";
import { ComponentType, TextInputStyle } from "./types.js";

describe("ModalV2Builder", () => {
  it("produces a full modal with label-wrapped components", () => {
    const json = new ModalV2Builder()
      .setTitle("Feedback Form")
      .setCustomId("feedback_modal")
      .addComponent(
        "Category",
        new RadioGroupBuilder()
          .setCustomId("category")
          .addOptions(
            { label: "Bug Report", value: "bug" },
            { label: "Feature Request", value: "feature" },
          ),
      )
      .addComponent(
        "Priority",
        new StringSelectBuilder()
          .setCustomId("priority")
          .setPlaceholder("Select...")
          .addOptions(
            { label: "Low", value: "low" },
            { label: "High", value: "high" },
          ),
      )
      .addComponent(
        "Tags",
        new CheckboxGroupBuilder()
          .setCustomId("tags")
          .setMinValues(1)
          .setMaxValues(3)
          .addOptions(
            { label: "UI", value: "ui" },
            { label: "API", value: "api" },
            { label: "Auth", value: "auth" },
          ),
      )
      .addComponent(
        "Description",
        new TextInputBuilder()
          .setCustomId("description")
          .setStyle(TextInputStyle.Paragraph)
          .setPlaceholder("Details...")
          .setMaxLength(2000),
      )
      .toJSON();

    expect(json).toEqual({
      title: "Feedback Form",
      custom_id: "feedback_modal",
      components: [
        {
          type: ComponentType.Label,
          label: "Category",
          component: {
            type: ComponentType.RadioGroup,
            custom_id: "category",
            options: [
              { label: "Bug Report", value: "bug" },
              { label: "Feature Request", value: "feature" },
            ],
          },
        },
        {
          type: ComponentType.Label,
          label: "Priority",
          component: {
            type: ComponentType.StringSelect,
            custom_id: "priority",
            placeholder: "Select...",
            options: [
              { label: "Low", value: "low" },
              { label: "High", value: "high" },
            ],
          },
        },
        {
          type: ComponentType.Label,
          label: "Tags",
          component: {
            type: ComponentType.CheckboxGroup,
            custom_id: "tags",
            min_values: 1,
            max_values: 3,
            options: [
              { label: "UI", value: "ui" },
              { label: "API", value: "api" },
              { label: "Auth", value: "auth" },
            ],
          },
        },
        {
          type: ComponentType.Label,
          label: "Description",
          component: {
            type: ComponentType.TextInput,
            custom_id: "description",
            style: TextInputStyle.Paragraph,
            placeholder: "Details...",
            max_length: 2000,
          },
        },
      ],
    });
  });

  it("preserves component ordering", () => {
    const json = new ModalV2Builder()
      .setTitle("Test")
      .setCustomId("test")
      .addComponent(
        "First",
        new TextInputBuilder().setCustomId("first"),
      )
      .addComponent(
        "Second",
        new TextInputBuilder().setCustomId("second"),
      )
      .addComponent(
        "Third",
        new TextInputBuilder().setCustomId("third"),
      )
      .toJSON();

    expect(json.components.map((c) => c.label)).toEqual([
      "First",
      "Second",
      "Third",
    ]);
  });

  it("wraps each component in a Label with type 18", () => {
    const json = new ModalV2Builder()
      .setTitle("Test")
      .setCustomId("test")
      .addComponent(
        "My Label",
        new RadioGroupBuilder()
          .setCustomId("r")
          .addOption({ label: "A", value: "a" }),
      )
      .toJSON();

    expect(json.components[0].type).toBe(ComponentType.Label);
    expect(json.components[0].label).toBe("My Label");
    expect(json.components[0].component.type).toBe(ComponentType.RadioGroup);
  });

  it("throws when title is missing", () => {
    const builder = new ModalV2Builder()
      .setCustomId("x")
      .addComponent(
        "L",
        new TextInputBuilder().setCustomId("t"),
      );
    expect(() => builder.toJSON()).toThrow("title is required");
  });

  it("throws when custom_id is missing", () => {
    const builder = new ModalV2Builder()
      .setTitle("T")
      .addComponent(
        "L",
        new TextInputBuilder().setCustomId("t"),
      );
    expect(() => builder.toJSON()).toThrow("custom_id is required");
  });

  it("throws when no components are added", () => {
    const builder = new ModalV2Builder().setTitle("T").setCustomId("x");
    expect(() => builder.toJSON()).toThrow("at least one component");
  });

  it("supports fluent chaining", () => {
    const builder = new ModalV2Builder();
    expect(builder.setTitle("T")).toBe(builder);
    expect(builder.setCustomId("x")).toBe(builder);
    expect(
      builder.addComponent(
        "L",
        new TextInputBuilder().setCustomId("t"),
      ),
    ).toBe(builder);
  });
});
