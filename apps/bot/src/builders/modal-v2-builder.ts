import type { APIComponent, APIModalV2, ToJSON } from "./types.js";
import { ComponentType } from "./types.js";

type ComponentBuilder = ToJSON<APIComponent>;

type LabeledComponent = {
  label: string;
  builder: ComponentBuilder;
};

export class ModalV2Builder {
  private title: string | undefined;
  private customId: string | undefined;
  private readonly components: LabeledComponent[] = [];

  setTitle(title: string): this {
    this.title = title;
    return this;
  }

  setCustomId(id: string): this {
    this.customId = id;
    return this;
  }

  addComponent(label: string, builder: ComponentBuilder): this {
    this.components.push({ label, builder });
    return this;
  }

  toJSON(): APIModalV2 {
    if (!this.title) {
      throw new Error("ModalV2Builder: title is required");
    }
    if (!this.customId) {
      throw new Error("ModalV2Builder: custom_id is required");
    }
    if (this.components.length === 0) {
      throw new Error(
        "ModalV2Builder: at least one component is required",
      );
    }

    return {
      title: this.title,
      custom_id: this.customId,
      components: this.components.map(({ label, builder }) => ({
        type: ComponentType.Label as typeof ComponentType.Label,
        label,
        component: builder.toJSON(),
      })),
    };
  }
}
