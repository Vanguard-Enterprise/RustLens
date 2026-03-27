import type { APITextInput, TextInputStyleValue, ToJSON } from "./types.js";
import { ComponentType, TextInputStyle } from "./types.js";

export class TextInputBuilder implements ToJSON<APITextInput> {
  private customId: string | undefined;
  private style: TextInputStyleValue = TextInputStyle.Short;
  private placeholder: string | undefined;
  private maxLength: number | undefined;
  private isRequired: boolean | undefined;
  private inputValue: string | undefined;
  private label: string | undefined;

  setCustomId(id: string): this {
    this.customId = id;
    return this;
  }

  setStyle(style: TextInputStyleValue): this {
    this.style = style;
    return this;
  }

  setPlaceholder(placeholder: string): this {
    this.placeholder = placeholder;
    return this;
  }

  setMaxLength(maxLength: number): this {
    this.maxLength = maxLength;
    return this;
  }

  setRequired(required: boolean): this {
    this.isRequired = required;
    return this;
  }

  setValue(value: string): this {
    this.inputValue = value;
    return this;
  }

  setLabel(label: string): this {
    this.label = label;
    return this;
  }

  toJSON(): APITextInput {
    if (!this.customId) {
      throw new Error("TextInputBuilder: custom_id is required");
    }

    return {
      type: ComponentType.TextInput,
      custom_id: this.customId,
      style: this.style,
      ...(this.placeholder !== undefined && { placeholder: this.placeholder }),
      ...(this.maxLength !== undefined && { max_length: this.maxLength }),
      ...(this.isRequired !== undefined && { required: this.isRequired }),
      ...(this.inputValue !== undefined && { value: this.inputValue }),
      ...(this.label !== undefined && { label: this.label }),
    };
  }
}
