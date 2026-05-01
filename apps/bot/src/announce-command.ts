import type { ChatInputCommandInteraction, Client } from "discord.js";
import { ApplicationIntegrationType, EmbedBuilder, InteractionContextType, MessageFlags, SlashCommandBuilder } from "discord.js";
import { isGuildTooLargeForDms, replyDmLimitError } from "./dm-limit.js";
import { getGuildMembers } from "./guild-member-cache.js";
import { hasGuildAdminAccess } from "./guild-settings.js";
import { RUSTLENS_EMBED_COLOR, RUSTLENS_EMBED_ICON_URL } from "./shared.js";

export const ANNOUNCE_COMMAND_NAME = "announce";

const announceCommand = new SlashCommandBuilder()
  .setName(ANNOUNCE_COMMAND_NAME)
  .setDescription("DM an announcement to all members of a role")
  .setDMPermission(false)
  .setContexts(InteractionContextType.Guild)
  .setIntegrationTypes(ApplicationIntegrationType.GuildInstall)
  .addRoleOption((option) =>
    option
      .setName("scope")
      .setDescription("The role whose members to DM")
      .setRequired(true),
  )
  .addStringOption((option) =>
    option
      .setName("message")
      .setDescription("The announcement message")
      .setRequired(true),
  );

function buildOutboundDmEmbed(title: string, description: string) {
  return new EmbedBuilder()
    .setTitle(title)
    .setDescription(description)
    .setColor(RUSTLENS_EMBED_COLOR)
    .setTimestamp()
    .setFooter({ text: "RustLens.com", iconURL: RUSTLENS_EMBED_ICON_URL });
}

function buildAnnouncementStatusEmbed(sent: number, failed: number) {
  return new EmbedBuilder()
    .setTitle("Announcement status")
    .setColor(RUSTLENS_EMBED_COLOR)
    .setTimestamp()
    .setFooter({ text: "RustLens.com", iconURL: RUSTLENS_EMBED_ICON_URL })
    .addFields({
      name: "Message status",
      value:
        `> * Successfully sent to **${sent}** member${sent === 1 ? "" : "s"}\n` +
        `> * Failed to send to **${failed}** member${failed === 1 ? "" : "s"}`,
    });
}

export async function ensureAnnounceCommand(client: Client, shardID: number) {
  if (shardID !== 0 || !client.application) {
    return;
  }

  const data = announceCommand.toJSON();
  const commands = await client.application.commands.fetch();
  const existing = commands.find((command) => command.name === data.name);

  if (existing) {
    await client.application.commands.edit(existing.id, data);
  } else {
    await client.application.commands.create(data);
  }

  console.log("[bot] ensured /announce command (global)");
}

export async function handleAnnounceChatInput(interaction: ChatInputCommandInteraction) {
  if (interaction.commandName !== ANNOUNCE_COMMAND_NAME) {
    return false;
  }

  if (!interaction.inGuild()) {
    await interaction.reply({
      content: "This command can only be used in a server.",
      flags: MessageFlags.Ephemeral,
    });
    return true;
  }

  const guild = interaction.guild;
  if (!guild) {
    await interaction.reply({
      content: "Could not resolve guild.",
      flags: MessageFlags.Ephemeral,
    });
    return true;
  }

  await interaction.deferReply({ flags: MessageFlags.Ephemeral });

  if (!(await hasGuildAdminAccess(interaction))) {
    await interaction.editReply({
      content: "Only the configured admin role or administrators can use /announce.",
    });
    return true;
  }

  const role = interaction.options.getRole("scope", true);
  const message = interaction.options.getString("message", true);

  if (isGuildTooLargeForDms(guild)) {
    await replyDmLimitError(interaction, guild);
    return true;
  }

  if (message.length > 4096) {
    await interaction.editReply({
      content: "The announcement message must be 4096 characters or fewer.",
    });
    return true;
  }

  const members = await getGuildMembers(guild);
  const targets = members.filter((m) =>
    !m.user.bot &&
    m.id !== interaction.user.id &&
    m.roles.cache.has(role.id)
  );

  if (targets.size === 0) {
    await interaction.editReply({
      content: `No members with **${role.name}** to send to.`,
    });
    return true;
  }

  const embed = buildOutboundDmEmbed("Announcement", message);

  let sent = 0;
  let failed = 0;

  await Promise.all(
    targets.map(async (target) => {
      try {
        await target.send({ embeds: [embed] });
        sent++;
      } catch {
        failed++;
      }
    }),
  );

  await interaction.editReply({ embeds: [buildAnnouncementStatusEmbed(sent, failed)] });
  return true;
}
