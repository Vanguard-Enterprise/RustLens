import type { ChatInputCommandInteraction, Client } from "discord.js";
import { EmbedBuilder, MessageFlags, PermissionFlagsBits, SlashCommandBuilder } from "discord.js";
import { RUSTLENS_EMBED_COLOR, RUSTLENS_EMBED_ICON_URL } from "./shared.js";

export const ANNOUNCE_COMMAND_NAME = "announce";

const announceCommand = new SlashCommandBuilder()
  .setName(ANNOUNCE_COMMAND_NAME)
  .setDescription("DM an announcement to all members of a role")
  .setDefaultMemberPermissions(PermissionFlagsBits.Administrator)
  .setDMPermission(false)
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

  const role = interaction.options.getRole("scope", true);
  const message = interaction.options.getString("message", true);

  await interaction.deferReply({ flags: MessageFlags.Ephemeral });

  const members = await guild.members.fetch();
  const targets = members.filter((m) => !m.user.bot && m.roles.cache.has(role.id));

  if (targets.size === 0) {
    await interaction.editReply({
      content: `No members with **${role.name}** to send to.`,
    });
    return true;
  }

  const embed = new EmbedBuilder()
    .setTitle("Announcement")
    .setDescription(message)
    .setColor(RUSTLENS_EMBED_COLOR)
    .setTimestamp()
    .setFooter({ text: "RustLens.com", iconURL: RUSTLENS_EMBED_ICON_URL });

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

  const parts = [`Sent announcement to **${sent}** member${sent !== 1 ? "s" : ""}.`];
  if (failed > 0) {
    parts.push(`Failed to DM **${failed}** member${failed !== 1 ? "s" : ""} (DMs likely disabled).`);
  }

  await interaction.editReply({ content: parts.join(" ") });
  return true;
}
