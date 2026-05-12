import {  } from "discord.js";
import { BAN_CATEGORIES, labelForCategory, type BanCategory } from "../ban-categories.js";
import { CheckboxGroupBuilder, ModalV2Builder } from "../builders/index.js";
import { createEmbed } from "../shared.js";

export const BAN_MODAL_PREFIX = "ban_modal:";
export const BAN_CATEGORIES_FIELD_CUSTOM_ID = "ban_categories";

export function buildBanEmbed(title: string, description: string) {
  return createEmbed()
    .setTitle(title)
    .setDescription(description);
}

export function buildBanModal(targetDiscordID: string, currentCategories: ReadonlySet<string>) {
  return new ModalV2Builder()
    .setTitle("Restrict User")
    .setCustomId(`${BAN_MODAL_PREFIX}${targetDiscordID}`)
    .addComponent(
      "Feature categories to block",
      new CheckboxGroupBuilder()
        .setCustomId(BAN_CATEGORIES_FIELD_CUSTOM_ID)
        .setRequired(false)
        .setMinValues(0)
        .setMaxValues(BAN_CATEGORIES.length)
        .addOptions(
          ...BAN_CATEGORIES.map((category) => ({
            label: category.label,
            value: category.value,
            default: currentCategories.has(category.value),
          })),
        ),
    );
}

export function parseTargetFromModalCustomId(customId: string) {
  return customId.startsWith(BAN_MODAL_PREFIX) ? customId.slice(BAN_MODAL_PREFIX.length) : null;
}

export function formatCategoriesDescription(categories: BanCategory[]) {
  if (categories.length === 0) {
    return "No restrictions — user has full access.";
  }
  return categories.map((value) => `> * ${labelForCategory(value)}`).join("\n");
}
