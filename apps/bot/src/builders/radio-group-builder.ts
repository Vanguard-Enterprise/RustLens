import type { APIRadioGroup, SelectOption, ToJSON } from "./types.js";
import { ComponentType } from "./types.js";

export class RadioGroupBuilder implements ToJSON<APIRadioGroup> {
  private customId: string | undefined;
  private readonly options: SelectOption[] = [];

  setCustomId(id: string): this {
    this.customId = id;
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

  toJSON(): APIRadioGroup {
    if (!this.customId) {
      throw new Error("RadioGroupBuilder: custom_id is required");
    }
    if (this.options.length === 0) {
      throw new Error("RadioGroupBuilder: at least one option is required");
    }

    return {
      type: ComponentType.RadioGroup,
      custom_id: this.customId,
      options: [...this.options],
    };
  }
}
