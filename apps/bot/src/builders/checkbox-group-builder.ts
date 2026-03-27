import type { APICheckboxGroup, SelectOption, ToJSON } from "./types.js";
import { ComponentType } from "./types.js";

export class CheckboxGroupBuilder implements ToJSON<APICheckboxGroup> {
  private customId: string | undefined;
  private isRequired: boolean | undefined;
  private minValues: number | undefined;
  private maxValues: number | undefined;
  private readonly options: SelectOption[] = [];

  setCustomId(id: string): this {
    this.customId = id;
    return this;
  }

  setRequired(required: boolean): this {
    this.isRequired = required;
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

  toJSON(): APICheckboxGroup {
    if (!this.customId) {
      throw new Error("CheckboxGroupBuilder: custom_id is required");
    }
    if (this.options.length === 0) {
      throw new Error(
        "CheckboxGroupBuilder: at least one option is required",
      );
    }

    return {
      type: ComponentType.CheckboxGroup,
      custom_id: this.customId,
      ...(this.isRequired !== undefined && { required: this.isRequired }),
      options: [...this.options],
      ...(this.minValues !== undefined && { min_values: this.minValues }),
      ...(this.maxValues !== undefined && { max_values: this.maxValues }),
    };
  }
}
