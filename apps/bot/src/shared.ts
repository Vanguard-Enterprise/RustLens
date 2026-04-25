import type { ModalSubmitInteraction, ChatInputCommandInteraction, ButtonInteraction } from "discord.js";
import { ActionRowBuilder, ButtonBuilder, EmbedBuilder, MessageFlags } from "discord.js";

export const RUSTLENS_EMBED_COLOR = 14582045;
export const RUSTLENS_EMBED_ICON_URL =
  "https://i.ibb.co/n8MWVLDN/1ee153f3c14d7decaeb6cb62c28fb5b4.webp";

export function buildWebsiteAdminOnlyEmbed() {
  return new EmbedBuilder()
    .setTitle("Error")
    .setDescription("This is locked to website admins only.")
    .setColor(RUSTLENS_EMBED_COLOR)
    .setTimestamp()
    .setFooter({ text: "RustLens.com", iconURL: RUSTLENS_EMBED_ICON_URL });
}

export type ModalComponentNode = {
  customId?: string;
  value?: unknown;
  values?: unknown;
  component?: ModalComponentNode;
  components?: ModalComponentNode[];
};

export function normalizeModalFieldValue(value: unknown): string[] {
  if (Array.isArray(value)) {
    return value.filter((entry): entry is string => typeof entry === "string");
  }

  if (typeof value === "string") {
    if (value.length === 0) {
      return [];
    }

    return value
      .split(",")
      .map((entry) => entry.trim())
      .filter((entry) => entry.length > 0);
  }

  return [];
}

export function extractFieldValues(
  field: { value?: unknown; values?: unknown } | null | undefined,
) {
  if (!field) {
    return [];
  }

  if ("values" in field) {
    return normalizeModalFieldValue(field.values);
  }

  if ("value" in field) {
    return normalizeModalFieldValue(field.value);
  }

  return [];
}

export function extractComponentValues(
  components: readonly ModalComponentNode[] | undefined,
  customId: string,
): string[] {
  if (!components) {
    return [];
  }

  for (const component of components) {
    if (component.customId === customId) {
      const values = extractFieldValues(component);
      if (values.length > 0) {
        return values;
      }
    }

    const nestedComponentValues = extractComponentValues(
      component.component ? [component.component] : undefined,
      customId,
    );
    if (nestedComponentValues.length > 0) {
      return nestedComponentValues;
    }

    const nestedValues = extractComponentValues(component.components, customId);
    if (nestedValues.length > 0) {
      return nestedValues;
    }
  }

  return [];
}

export function getFieldValues(interaction: ModalSubmitInteraction, customId: string) {
  try {
    const accessorField = interaction.fields.getField(customId) as
      | { value?: unknown; values?: unknown }
      | null
      | undefined;
    const accessorValues = extractFieldValues(accessorField);
    if (accessorValues.length > 0) {
      return accessorValues;
    }
  } catch {
    // Fall through to the raw field map and component tree.
  }

  const mapField = interaction.fields.fields.get(customId) as
    | { value?: unknown; values?: unknown }
    | null
    | undefined;
  const mapValues = extractFieldValues(mapField);
  if (mapValues.length > 0) {
    return mapValues;
  }

  return extractComponentValues(
    interaction.components as unknown as readonly ModalComponentNode[],
    customId,
  );
}

export async function replyEphemeral(
  interaction: ChatInputCommandInteraction | ModalSubmitInteraction | ButtonInteraction,
  content: string,
  components?: ActionRowBuilder<ButtonBuilder>[],
) {
  if (interaction.deferred || interaction.replied) {
    await interaction.editReply({ content, components: components ?? [] });
    return;
  }

  await interaction.reply({
    content,
    components,
    flags: MessageFlags.Ephemeral,
  });
}
