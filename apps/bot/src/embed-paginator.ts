import {
  ActionRowBuilder,
  ButtonBuilder,
  ButtonStyle,
  ComponentType,
  EmbedBuilder,
  type ButtonInteraction,
  type ChatInputCommandInteraction,
  type Message,
  type RepliableInteraction,
} from "discord.js";
import { logDiscordCatch } from "./shared.js";

export const PAGINATOR_PREV_ID = "embed-paginator:prev";
export const PAGINATOR_NEXT_ID = "embed-paginator:next";
const PAGINATOR_TIMEOUT_MS = 5 * 60 * 1000;
export const PAGE_PREVIOUS_EMOJI = "<:arrow_left:1499045624207773736>";
export const PAGE_NEXT_EMOJI = "<:arrow_right:1499045625571053710>";

export type PaginatedSection = {
  name: string;
  lines: readonly string[];
};

export function buildPaginatorButtons(page: number, totalPages: number) {
  return new ActionRowBuilder<ButtonBuilder>().addComponents(
    new ButtonBuilder()
      .setCustomId(PAGINATOR_PREV_ID)
      .setEmoji(PAGE_PREVIOUS_EMOJI)
      .setStyle(ButtonStyle.Secondary)
      .setDisabled(page === 0),
    new ButtonBuilder()
      .setCustomId(PAGINATOR_NEXT_ID)
      .setEmoji(PAGE_NEXT_EMOJI)
      .setStyle(ButtonStyle.Secondary)
      .setDisabled(page >= totalPages - 1),
  );
}

export type PaginateOptions = {
  maxFieldValueLength?: number;
  maxFieldsPerEmbed?: number;
  maxEmbedBodyLength?: number;
};

// Splits one logical section's lines into multiple field-value chunks, each
// bounded by maxFieldValueLength.
function chunkSectionLines(
  lines: readonly string[],
  maxFieldValueLength: number,
): string[] {
  const chunks: string[] = [];
  let buffer: string[] = [];
  let bufferLength = 0;

  for (const rawLine of lines) {
    const line = rawLine.length > maxFieldValueLength
      ? `${rawLine.slice(0, maxFieldValueLength - 1)}…`
      : rawLine;

    const separator = buffer.length > 0 ? 1 : 0;
    if (bufferLength + separator + line.length > maxFieldValueLength) {
      if (buffer.length > 0) {
        chunks.push(buffer.join("\n"));
      }
      buffer = [line];
      bufferLength = line.length;
      continue;
    }

    buffer.push(line);
    bufferLength += separator + line.length;
  }

  if (buffer.length > 0) {
    chunks.push(buffer.join("\n"));
  }

  return chunks;
}

// Builds one or more embeds from a list of sections, paginating to respect
// Discord embed limits. Each call to buildBaseEmbed must return a new instance
// (no shared mutable state across pages).
export function paginateSections(
  sections: readonly PaginatedSection[],
  buildBaseEmbed: (pageIndex: number) => EmbedBuilder,
  opts: PaginateOptions = {},
): EmbedBuilder[] {
  const maxFieldValueLength = opts.maxFieldValueLength ?? 1024;
  const maxFieldsPerEmbed = opts.maxFieldsPerEmbed ?? 25;
  const maxEmbedBodyLength = opts.maxEmbedBodyLength ?? 5500;

  type ChunkedField = { name: string; value: string };
  const chunkedFields: ChunkedField[] = [];

  for (const section of sections) {
    if (section.lines.length === 0) {
      continue;
    }
    const sectionChunks = chunkSectionLines(section.lines, maxFieldValueLength);
    if (sectionChunks.length === 1) {
      chunkedFields.push({ name: section.name, value: sectionChunks[0] });
    } else {
      sectionChunks.forEach((value, index) => {
        chunkedFields.push({
          name: `${section.name} (${index + 1}/${sectionChunks.length})`,
          value,
        });
      });
    }
  }

  if (chunkedFields.length === 0) {
    return [buildBaseEmbed(0)];
  }

  const embeds: EmbedBuilder[] = [];
  let pageIndex = 0;
  let currentEmbed = buildBaseEmbed(pageIndex);
  let currentFieldCount = 0;
  let currentBodyLength = 0;

  for (const field of chunkedFields) {
    const fieldLen = field.name.length + field.value.length;
    if (
      currentFieldCount >= maxFieldsPerEmbed ||
      (currentFieldCount > 0 && currentBodyLength + fieldLen > maxEmbedBodyLength)
    ) {
      embeds.push(currentEmbed);
      pageIndex += 1;
      currentEmbed = buildBaseEmbed(pageIndex);
      currentFieldCount = 0;
      currentBodyLength = 0;
    }
    currentEmbed.addFields(field);
    currentFieldCount += 1;
    currentBodyLength += fieldLen;
  }

  embeds.push(currentEmbed);
  return embeds;
}

// Replies to a deferred interaction with a paginator. If only one page exists,
// renders without buttons.
export async function replyPaginatedEmbeds(
  interaction: ChatInputCommandInteraction,
  embeds: readonly EmbedBuilder[],
): Promise<void> {
  if (embeds.length === 0) {
    return;
  }

  if (embeds.length === 1) {
    await interaction.editReply({ embeds: [embeds[0]], components: [] });
    return;
  }

  let page = 0;
  const message = (await interaction.editReply({
    embeds: [embeds[page]],
    components: [buildPaginatorButtons(page, embeds.length)],
  })) as Message;

  attachPaginatorCollector(message, interaction, embeds, () => page, (next) => {
    page = next;
  });
}

function attachPaginatorCollector(
  message: Message,
  interaction: RepliableInteraction,
  embeds: readonly EmbedBuilder[],
  getPage: () => number,
  setPage: (next: number) => void,
) {
  const collector = message.createMessageComponentCollector({
    componentType: ComponentType.Button,
    filter: (button: ButtonInteraction) =>
      button.user.id === interaction.user.id &&
      (button.customId === PAGINATOR_PREV_ID || button.customId === PAGINATOR_NEXT_ID),
    time: PAGINATOR_TIMEOUT_MS,
  });

  collector.on("collect", async (button: ButtonInteraction) => {
    const current = getPage();
    const next = button.customId === PAGINATOR_NEXT_ID
      ? Math.min(current + 1, embeds.length - 1)
      : Math.max(current - 1, 0);
    setPage(next);
    try {
      await button.update({
        embeds: [embeds[next]],
        components: [buildPaginatorButtons(next, embeds.length)],
      });
    } catch (error) {
      console.error("[bot] paginator button update failed", error);
    }
  });

  collector.on("end", () => {
    interaction.editReply({ components: [] }).catch(logDiscordCatch("clear paginator components", undefined, {
      guildId: interaction.guildId,
      discordId: interaction.user.id,
    }));
  });
}

export const __testables = {
  chunkSectionLines,
};
