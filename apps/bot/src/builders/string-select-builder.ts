import type { APIStringSelect, SelectOption, ToJSON } from "./types.js";
import { ComponentType } from "./types.js";

export class StringSelectBuilder implements ToJSON<APIStringSelect> {
  private customId: string | undefined;
  private placeholder: string | undefined;
  private minValues: number | undefined;
  private maxValues: number | undefined;
  private readonly options: SelectOption[] = [];

  setCustomId(id: string): this {
    this.customId = id;
    return this;
  }

  setPlaceholder(placeholder: string): this {
    this.placeholder = placeholder;
    return this;
  }

  setMinValues(min: number): this {
    this.minValues = min;
    return this;
  }

  setMaxValues(max: number): this {
    this.maxValues = max;
    return this;
  }

  addOption(option: SelectOption): this {
    this.options.push(option);
    return this;
  }

  addOptions(...options: SelectOption[]): this {
    this.options.push(...options);
    return this;
  }

  toJSON(): APIStringSelect {
    if (!this.customId) {
      throw new Error("StringSelectBuilder: custom_id is required");
    }
    if (this.options.length === 0) {
      throw new Error(
        "StringSelectBuilder: at least one option is required",
      );
    }

    return {
      type: ComponentType.StringSelect,
      custom_id: this.customId,
      options: [...this.options],
      ...(this.placeholder !== undefined && { placeholder: this.placeholder }),
      ...(this.minValues !== undefined && { min_values: this.minValues }),
      ...(this.maxValues !== undefined && { max_values: this.maxValues }),
    };
  }
}
