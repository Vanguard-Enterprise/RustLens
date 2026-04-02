import type { ChatInputCommandInteraction, Client, GuildMember } from "discord.js";
import { EmbedBuilder, MessageFlags, SlashCommandBuilder } from "discord.js";
import { BackendClient } from "./backend-client.js";
import { loadConfig } from "./config.js";
import { getGuildMembers } from "./guild-member-cache.js";
import { hasGuildAdminAccess } from "./guild-settings.js";
import { RUSTLENS_EMBED_COLOR, RUSTLENS_EMBED_ICON_URL } from "./shared.js";

export const NICKNAME_COMMAND_NAME = "nickname";

const FOOTER_TEXT = "RustLens.com";
const DISCORD_NICKNAME_MAX_LENGTH = 32;
const NICKNAME_SEPARATOR = " - ";
const shardID = process.env.SHARD_ID ?? "0";

const nicknameCommand = new SlashCommandBuilder()
  .setName(NICKNAME_COMMAND_NAME)
  .setDescription("Set guild nicknames to Discord nickname - BattleMetrics name")
  .setDMPermission(false);

let _backend: BackendClient | undefined;
let _workerID: string | undefined;

function getBackend() {
  if (!_backend) {
    const config = loadConfig();
    _backend = new BackendClient(
      config.backendUrl,
      config.jwtSecret,
      config.jwtIssuer,
      config.jwtAudience,
    );
    _workerID = `${config.workerId}-nickname-${shardID}`;
  }

  return _backend;
}

function getWorkerID() {
  getBackend();
  return _workerID!;
}

function truncateWithEllipsis(value: string, maxLength: number) {
  if (value.length <= maxLength) {
    return value;
  }

  if (maxLength <= 3) {
    return value.slice(0, maxLength);
  }

  return `${value.slice(0, maxLength - 3)}...`;
}

function resolveBaseNickname(member: GuildMember, bmName: string) {
  const fallback = member.user.globalName?.trim() || member.user.username.trim() || member.user.username;
  const source = member.nickname?.trim() || fallback;
  const suffix = `${NICKNAME_SEPARATOR}${bmName}`;

  if (source.endsWith(suffix)) {
    const stripped = source.slice(0, source.length - suffix.length).trimEnd();
    return stripped || fallback;
  }

  return source;
}

function buildTargetNickname(member: GuildMember, bmName: string) {
  const normalizedBmName = bmName.trim();
  const baseNickname = resolveBaseNickname(member, normalizedBmName);
  const fullNickname = `${baseNickname}${NICKNAME_SEPARATOR}${normalizedBmName}`;
  if (fullNickname.length <= DISCORD_NICKNAME_MAX_LENGTH) {
    return fullNickname;
  }

  const available = DISCORD_NICKNAME_MAX_LENGTH - NICKNAME_SEPARATOR.length;
  const bmBudget = Math.max(1, Math.min(normalizedBmName.length, Math.floor(available * 0.5)));
  const baseBudget = Math.max(1, available - bmBudget);

  return `${truncateWithEllipsis(baseNickname, baseBudget)}${NICKNAME_SEPARATOR}${truncateWithEllipsis(normalizedBmName, available - baseBudget)}`;
}

function buildCompletionEmbed(successCount: number, failedCount: number, totalCount: number) {
  return new EmbedBuilder()
    .setTitle("Nickname Change Complete")
    .setTimestamp()
    .setColor(RUSTLENS_EMBED_COLOR)
    .setFooter({ text: FOOTER_TEXT, iconURL: RUSTLENS_EMBED_ICON_URL })
    .addFields({
      name: "Results",
      value: `> ✅ Changed: ${successCount}\n> ❌ Failed: ${failedCount}\n> 👥 Processed: ${totalCount}`,
      inline: false,
    });
}

async function replyEphemeralEmbed(
  interaction: ChatInputCommandInteraction,
  embed: EmbedBuilder,
) {
  if (interaction.deferred || interaction.replied) {
    await interaction.editReply({ embeds: [embed] });
    return;
  }

  await interaction.reply({
    embeds: [embed],
    flags: MessageFlags.Ephemeral,
  });
}

export async function ensureNicknameCommand(client: Client, shardID: number) {
  if (shardID !== 0 || !client.application) {
    return;
  }

  const data = nicknameCommand.toJSON();
  const commands = await client.application.commands.fetch();
  const existing = commands.find((command) => command.name === data.name);

  if (existing) {
    await client.application.commands.edit(existing.id, data);
  } else {
    await client.application.commands.create(data);
  }

  console.log("[bot] ensured /nickname command (global)");
}

export async function handleNicknameChatInput(interaction: ChatInputCommandInteraction) {
  if (interaction.commandName !== NICKNAME_COMMAND_NAME) {
    return false;
  }

  if (!interaction.inGuild() || !interaction.guildId || !interaction.guild) {
    await interaction.reply({
      content: "This command can only be used in a server.",
      flags: MessageFlags.Ephemeral,
    });
    return true;
  }

  if (!(await hasGuildAdminAccess(interaction))) {
    await interaction.reply({
      content: "Only the configured admin role or administrators can use /nickname.",
      flags: MessageFlags.Ephemeral,
    });
    return true;
  }

  await interaction.deferReply({ flags: MessageFlags.Ephemeral });

  const { items } = await getBackend().listGuildNicknameTargets(getWorkerID(), interaction.guildId);
  const members = await getGuildMembers(interaction.guild);
  let successCount = 0;
  let failedCount = 0;

  for (const item of items) {
    const bmId = item.bmId?.trim() ?? "";
    const bmName = item.bmName?.trim() ?? "";
    if (!bmId || !bmName) {
      failedCount += 1;
      continue;
    }

    const member = members.get(item.discordId);
    if (!member) {
      failedCount += 1;
      continue;
    }

    const targetNickname = buildTargetNickname(member, bmName);
    if (!member.manageable && member.nickname !== targetNickname) {
      failedCount += 1;
      continue;
    }

    try {
      if (member.manageable && member.nickname !== targetNickname) {
        await member.setNickname(targetNickname, "RustLens /nickname sync");
      }
      successCount += 1;
    } catch {
      failedCount += 1;
    }
  }

  await replyEphemeralEmbed(
    interaction,
    buildCompletionEmbed(successCount, failedCount, items.length),
  );
  return true;
}

export const __testables = {
  buildCompletionEmbed,
  buildTargetNickname,
  resolveBaseNickname,
  truncateWithEllipsis,
};
