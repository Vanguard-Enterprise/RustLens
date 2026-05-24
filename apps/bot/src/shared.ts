import type { ModalSubmitInteraction, ChatInputCommandInteraction, ButtonInteraction } from "discord.js";
import { ActionRowBuilder, ButtonBuilder, EmbedBuilder, MessageFlags } from "discord.js";
import { logger } from "./logger.js";

export const RUSTLENS_EMBED_COLOR = 14582045;
export const RUSTLENS_EMBED_ICON_URL =
  "https://i.ibb.co/n8MWVLDN/1ee153f3c14d7decaeb6cb62c28fb5b4.webp";
export const FOOTER_TEXT = "RustLens.com";

export function buildFooter(text: string = FOOTER_TEXT) {
  return { text, iconURL: RUSTLENS_EMBED_ICON_URL };
}

export function formatDiscordUserName(user: {
  globalName?: string | null;
  username?: string | null;
  tag?: string | null;
  id?: string | null;
}) {
  return user.globalName?.trim() || user.username?.trim() || user.tag?.trim() || user.id?.trim() || "Unknown user";
}

export function createEmbed() {
  return new EmbedBuilder()
    .setColor(RUSTLENS_EMBED_COLOR)
    .setTimestamp()
    .setFooter(buildFooter());
}

export function logDiscordError(
  context: string,
  error: unknown,
  details?: Record<string, unknown>,
) {
  logger.error({ ...details, error }, `Discord error: ${context}`);
}

export function logDiscordCatch<T>(
  context: string,
  fallback: T,
  details?: Record<string, unknown>,
) {
  return (error: unknown) => {
    logDiscordError(context, error, details);
    return fallback;
  };
}

export const DISCORD_MESSAGE_MAX_LENGTH = 2000;

const CODE_BLOCK_WRAPPER_LENGTH = "```\n\n```".length;

export function chunkRawMentionsCodeBlocks(
  userIds: readonly string[],
  maxLength = DISCORD_MESSAGE_MAX_LENGTH,
) {
  const innerLimit = maxLength - CODE_BLOCK_WRAPPER_LENGTH;
  const chunks: string[] = [];
  let current = "";
  for (const id of userIds) {
    const mention = `<@${id}>`;
    const candidate = current.length === 0 ? mention : `${current}\n${mention}`;
    if (candidate.length > innerLimit) {
      if (current.length === 0) {
        chunks.push(`\`\`\`\n${mention.slice(0, innerLimit)}\n\`\`\``);
        current = "";
      } else {
        chunks.push(`\`\`\`\n${current}\n\`\`\``);
        current = mention;
      }
    } else {
      current = candidate;
    }
  }
  if (current.length > 0) {
    chunks.push(`\`\`\`\n${current}\n\`\`\``);
  }
  return chunks;
}

export function buildProOnlyFeatureEmbed() {
  return createEmbed()
    .setTitle("Pro required to use this feature")
    .setDescription("You can purchase RustLens PRO at https://rustlens.com");
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
