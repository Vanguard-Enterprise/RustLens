import type { APIEmbedField, ChatInputCommandInteraction, Client } from "discord.js";
import { ApplicationIntegrationType, EmbedBuilder, InteractionContextType, MessageFlags, SlashCommandBuilder } from "discord.js";
import { BackendClient, type GuildOnlineSummaryItem } from "./backend-client.js";
import { loadConfig } from "./config.js";
import { replyPaginatedEmbeds } from "./embed-paginator.js";
import { RUSTLENS_EMBED_COLOR, RUSTLENS_EMBED_ICON_URL } from "./shared.js";

export const ONLINE_COMMAND_NAME = "online";

const ONLINE_SUMMARY_TITLE = "Online summary";
const ONLINE_SUMMARY_CONT_TITLE = "Online summary cont.";
const MAX_EMBED_FIELDS = 25;
const MAX_FIELD_VALUE_LENGTH = 1024;
const MAX_FIELD_NAME_LENGTH = 256;
const MAX_EMBED_BODY_LENGTH = 5800;
const FOOTER_TEXT = "RustLens.com";

const onlineCommand = new SlashCommandBuilder()
  .setName(ONLINE_COMMAND_NAME)
  .setDescription("List guild-linked users currently online on tracked servers")
  .setDMPermission(false)
  .setContexts(InteractionContextType.Guild)
  .setIntegrationTypes(ApplicationIntegrationType.GuildInstall);

let _backend: BackendClient | undefined;
let _workerID: string | undefined;

function getBackend() {
  if (!_backend) {
    const config = loadConfig();
    _backend = new BackendClient(config.backendUrl, config.jwtSecret, config.jwtIssuer, config.jwtAudience);
    _workerID = `${config.workerId}-online`;
  }
  return _backend;
}

function getWorkerID() {
  getBackend();
  return _workerID!;
}

function sanitizeBmName(name: string, fallback: string) {
  const trimmed = name.trim();
  if (trimmed.length === 0) {
    return fallback;
  }

  return trimmed
    .replace(/\\/g, "\\\\")
    .replace(/\[/g, "\\[")
    .replace(/\]/g, "\\]")
    .replace(/\*/g, "\\*")
    .replace(/_/g, "\\_")
    .replace(/`/g, "\\`");
}

function truncateText(value: string, maxLength: number) {
  if (value.length <= maxLength) {
    return value;
  }

  return `${value.slice(0, Math.max(0, maxLength - 3))}...`;
}

function buildSummaryLine(item: GuildOnlineSummaryItem) {
  const bmName = sanitizeBmName(item.bmName, item.playerBmId);
  return `> * <@${item.discordId}> as **[${bmName}](https://www.battlemetrics.com/rcon/players/${item.playerBmId})** since <t:${item.onlineUnix}:R>`;
}

function groupSummaryItems(items: GuildOnlineSummaryItem[]) {
  const groups = new Map<string, { serverName: string; lines: string[] }>();

  for (const item of items) {
    const current = groups.get(item.serverBmId) ?? { serverName: item.serverName, lines: [] };
    current.lines.push(buildSummaryLine(item));
    groups.set(item.serverBmId, current);
  }

  return [...groups.values()];
}

function splitFieldValues(serverName: string, lines: string[]): APIEmbedField[] {
  const fields: APIEmbedField[] = [];
  let currentLines: string[] = [];
  let currentLength = 0;
  let chunkIndex = 0;

  const flush = () => {
    if (currentLines.length === 0) {
      return;
    }

    const suffix = chunkIndex === 0 ? "" : " cont.";
    fields.push({
      name: truncateText(`${serverName}${suffix}`, MAX_FIELD_NAME_LENGTH),
      value: currentLines.join("\n"),
      inline: false,
    });
    currentLines = [];
    currentLength = 0;
    chunkIndex += 1;
  };

  for (const line of lines) {
    if (line.length > MAX_FIELD_VALUE_LENGTH) {
      const truncatedLine = truncateText(line, MAX_FIELD_VALUE_LENGTH);
      if (currentLines.length > 0) {
        flush();
      }
      currentLines.push(truncatedLine);
      currentLength = truncatedLine.length;
      flush();
      continue;
    }

    const separatorLength = currentLines.length > 0 ? 1 : 0;
    if (currentLength + separatorLength + line.length > MAX_FIELD_VALUE_LENGTH) {
      flush();
    }

    currentLines.push(line);
    currentLength += (currentLines.length > 1 ? 1 : 0) + line.length;
  }

  flush();
  return fields;
}

function buildPagedEmbeds(items: GuildOnlineSummaryItem[]) {
  if (items.length === 0) {
    return [
      new EmbedBuilder()
        .setTitle(ONLINE_SUMMARY_TITLE)
        .setDescription("No linked users are currently online on tracked servers.")
        .setColor(RUSTLENS_EMBED_COLOR)
        .setTimestamp()
        .setFooter({ text: FOOTER_TEXT, iconURL: RUSTLENS_EMBED_ICON_URL }),
    ];
  }

  const fields = groupSummaryItems(items).flatMap((group) => splitFieldValues(group.serverName, group.lines));
  const embeds: EmbedBuilder[] = [];
  let currentEmbed = new EmbedBuilder()
    .setTitle(ONLINE_SUMMARY_TITLE)
    .setColor(RUSTLENS_EMBED_COLOR)
    .setTimestamp()
    .setFooter({ text: FOOTER_TEXT, iconURL: RUSTLENS_EMBED_ICON_URL });
  let currentCharCount = ONLINE_SUMMARY_TITLE.length + FOOTER_TEXT.length;
  let currentFieldCount = 0;

  const pushCurrent = () => {
    embeds.push(currentEmbed);
    currentEmbed = new EmbedBuilder()
      .setTitle(ONLINE_SUMMARY_CONT_TITLE)
      .setColor(RUSTLENS_EMBED_COLOR)
      .setTimestamp()
      .setFooter({ text: FOOTER_TEXT, iconURL: RUSTLENS_EMBED_ICON_URL });
    currentCharCount = ONLINE_SUMMARY_CONT_TITLE.length + FOOTER_TEXT.length;
    currentFieldCount = 0;
  };

  for (const field of fields) {
    const fieldLength = field.name.length + field.value.length;
    if (
      currentFieldCount >= MAX_EMBED_FIELDS ||
      currentCharCount + fieldLength > MAX_EMBED_BODY_LENGTH
    ) {
      pushCurrent();
    }

    currentEmbed.addFields(field);
    currentCharCount += fieldLength;
    currentFieldCount += 1;
  }

  embeds.push(currentEmbed);
  return embeds;
}

export async function ensureOnlineCommand(client: Client, shardID: number) {
  if (shardID !== 0 || !client.application) {
    return;
  }

  const data = onlineCommand.toJSON();
  const commands = await client.application.commands.fetch();
  const existing = commands.find((command) => command.name === data.name);

  if (existing) {
    await client.application.commands.edit(existing.id, data);
  } else {
    await client.application.commands.create(data);
  }

  console.log("[bot] ensured /online command (global)");
}

export async function handleOnlineChatInput(interaction: ChatInputCommandInteraction) {
  if (interaction.commandName !== ONLINE_COMMAND_NAME) {
    return false;
  }

  if (!interaction.inGuild() || !interaction.guildId) {
    await interaction.reply({
      content: "This command can only be used in a server.",
      flags: MessageFlags.Ephemeral,
    });
    return true;
  }

  await interaction.deferReply();

  let items: GuildOnlineSummaryItem[];
  try {
    const response = await getBackend().listGuildOnlineSummary(getWorkerID(), interaction.guildId);
    items = response.items;
  } catch (error) {
    console.error("[bot] /online backend call failed", error);
    await interaction.editReply({ content: "Failed to fetch online summary." });
    return true;
  }
  const embeds = buildPagedEmbeds(items);

  await replyPaginatedEmbeds(interaction, embeds);

  return true;
}

export const __testables = {
  buildPagedEmbeds,
  buildSummaryLine,
  splitFieldValues,
};
