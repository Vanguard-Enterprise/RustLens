import type { ChatInputCommandInteraction, Client, GuildMember } from "discord.js";
import { EmbedBuilder, MessageFlags, PermissionFlagsBits, SlashCommandBuilder } from "discord.js";
import { BackendClient } from "./backend-client.js";
import { loadConfig } from "./config.js";
import { getGuildMembers } from "./guild-member-cache.js";
import { hasGuildAdminAccess } from "./guild-settings.js";
import { RUSTLENS_EMBED_COLOR, RUSTLENS_EMBED_ICON_URL } from "./shared.js";

export const NICKNAME_COMMAND_NAME = "nickname";

const FOOTER_TEXT = "RustLens.com";
const DISCORD_NICKNAME_MAX_LENGTH = 32;
const NICKNAME_SEPARATOR = " - ";
const EMBED_FIELD_VALUE_FALLBACK = "\u200b";
const MAX_FIELD_VALUE_LENGTH = 1024;
const shardID = process.env.SHARD_ID ?? "0";

const NICKNAME_CHANGE_SUCCESS_REASON = "Changed.";
const NICKNAME_FAILURE_REASON_MISSING_PERMISSIONS = "Missing required permissions to change nickname.";
const NICKNAME_FAILURE_REASON_ROLE_BELOW = "Bot's role is below the user's role.";
const NICKNAME_FAILURE_REASON_MISSING_BM_ID = "Missing BM ID.";
const NICKNAME_FAILURE_REASON_MISSING_BM_NAME = "Missing BM name.";
const NICKNAME_FAILURE_REASON_INTERNAL = "Internal error, this issue has been reported to the developers.";

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

type NicknameOutcome = {
  discordId: string;
  reason: string;
};

function truncateFieldValue(value: string) {
  if (value.length <= MAX_FIELD_VALUE_LENGTH) {
    return value;
  }

  return `${value.slice(0, MAX_FIELD_VALUE_LENGTH - 3)}...`;
}

function formatOutcomeLines(items: readonly NicknameOutcome[]) {
  if (items.length === 0) {
    return EMBED_FIELD_VALUE_FALLBACK;
  }

  return truncateFieldValue(items.map((item) => `> * <@${item.discordId}> - ${item.reason}`).join("\n"));
}

function buildCompletionEmbed(
  successCount: number,
  failedCount: number,
  totalCount: number,
  failedItems: readonly NicknameOutcome[],
) {
  return new EmbedBuilder()
    .setTitle("Nickname Change Complete")
    .setTimestamp()
    .setColor(RUSTLENS_EMBED_COLOR)
    .setFooter({ text: FOOTER_TEXT, iconURL: RUSTLENS_EMBED_ICON_URL })
    .addFields(
      {
        name: `> ✅ Changed: ${successCount}`,
        value: EMBED_FIELD_VALUE_FALLBACK,
        inline: false,
      },
      {
        name: `> ❌ Failed: ${failedCount}`,
        value: formatOutcomeLines(failedItems),
        inline: false,
      },
      {
        name: `👥 Processed: ${totalCount}`,
        value: EMBED_FIELD_VALUE_FALLBACK,
        inline: false,
      },
    );
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

async function resolveBotMember(interaction: ChatInputCommandInteraction) {
  if (!interaction.guild) {
    return null;
  }

  return interaction.guild.members.me ?? interaction.guild.members.fetchMe().catch(() => null);
}

function resolveNicknameFailureReason(
  member: GuildMember,
  botMember: GuildMember | null,
  guildOwnerId: string,
) {
  if (!botMember?.permissions.has(PermissionFlagsBits.ManageNicknames)) {
    return NICKNAME_FAILURE_REASON_MISSING_PERMISSIONS;
  }

  if (member.id === guildOwnerId) {
    return NICKNAME_FAILURE_REASON_ROLE_BELOW;
  }

  if (member.roles.highest.position >= botMember.roles.highest.position) {
    return NICKNAME_FAILURE_REASON_ROLE_BELOW;
  }

  return NICKNAME_FAILURE_REASON_INTERNAL;
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
  const botMember = await resolveBotMember(interaction);
  let successCount = 0;
  let failedCount = 0;
  const failedItems: NicknameOutcome[] = [];

  for (const item of items) {
    const bmId = item.bmId?.trim() ?? "";
    const bmName = item.bmName?.trim() ?? "";
    if (!bmId) {
      failedCount += 1;
      const outcome = { discordId: item.discordId, reason: NICKNAME_FAILURE_REASON_MISSING_BM_ID };
      failedItems.push(outcome);
      continue;
    }

    if (!bmName) {
      failedCount += 1;
      const outcome = { discordId: item.discordId, reason: NICKNAME_FAILURE_REASON_MISSING_BM_NAME };
      failedItems.push(outcome);
      continue;
    }

    const member = members.get(item.discordId);
    if (!member) {
      failedCount += 1;
      const outcome = { discordId: item.discordId, reason: NICKNAME_FAILURE_REASON_INTERNAL };
      failedItems.push(outcome);
      continue;
    }

    const targetNickname = buildTargetNickname(member, bmName);
    if (!member.manageable && member.nickname !== targetNickname) {
      failedCount += 1;
      const outcome = {
        discordId: item.discordId,
        reason: resolveNicknameFailureReason(member, botMember, interaction.guild.ownerId),
      };
      failedItems.push(outcome);
      continue;
    }

    try {
      if (member.manageable && member.nickname !== targetNickname) {
        await member.setNickname(targetNickname, "RustLens /nickname sync");
      }
      successCount += 1;
    } catch {
      failedCount += 1;
      const outcome = { discordId: item.discordId, reason: NICKNAME_FAILURE_REASON_INTERNAL };
      failedItems.push(outcome);
    }
  }

  await replyEphemeralEmbed(
    interaction,
    buildCompletionEmbed(successCount, failedCount, items.length, failedItems),
  );
  return true;
}

export const __testables = {
  buildCompletionEmbed,
  buildTargetNickname,
  resolveBaseNickname,
  truncateWithEllipsis,
};
