import type { ChatInputCommandInteraction, Client } from "discord.js";
import { MessageFlags, SlashCommandBuilder } from "discord.js";
import { getGuildMembers } from "./guild-member-cache.js";
import { hasGuildAdminAccess } from "./guild-settings.js";

export const SUMMON_COMMAND_NAME = "summon";

const summonCommand = new SlashCommandBuilder()
  .setName(SUMMON_COMMAND_NAME)
  .setDescription("Move all online members of a role into your voice channel")
  .setDMPermission(false)
  .addRoleOption((option) =>
    option
      .setName("scope")
      .setDescription("The role whose members to summon")
      .setRequired(true),
  );

export async function ensureSummonCommand(client: Client, shardID: number) {
  if (shardID !== 0 || !client.application) {
    return;
  }

  const data = summonCommand.toJSON();
  const commands = await client.application.commands.fetch();
  const existing = commands.find((command) => command.name === data.name);

  if (existing) {
    await client.application.commands.edit(existing.id, data);
  } else {
    await client.application.commands.create(data);
  }

  console.log("[bot] ensured /summon command (global)");
}

export async function handleSummonChatInput(interaction: ChatInputCommandInteraction) {
  if (interaction.commandName !== SUMMON_COMMAND_NAME) {
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
      content: "Only the configured admin role or administrators can use /summon.",
    });
    return true;
  }

  const member = await guild.members.fetch(interaction.user.id).catch(() => null);
  const voiceChannel = member?.voice.channel;

  if (!voiceChannel) {
    await interaction.editReply({
      content: "You must be in a voice channel to use this command.",
    });
    return true;
  }

  const role = interaction.options.getRole("scope", true);

  const members = await getGuildMembers(guild);
  const targets = members.filter(
    (m) => m.roles.cache.has(role.id) && m.voice.channel && m.voice.channelId !== voiceChannel.id,
  );

  if (targets.size === 0) {
    await interaction.editReply({
      content: `No online members with **${role.name}** in voice to move.`,
    });
    return true;
  }

  let moved = 0;
  let failed = 0;

  await Promise.all(
    targets.map(async (target) => {
      try {
        await target.voice.setChannel(voiceChannel);
        moved++;
      } catch {
        failed++;
      }
    }),
  );

  const parts = [`Summoned **${moved}** member${moved !== 1 ? "s" : ""}  to **${voiceChannel.name}**.`];
  if (failed > 0) {
    parts.push(`Failed to move **${failed}** member${failed !== 1 ? "s" : ""}.`);
  }

  await interaction.editReply({ content: parts.join(" ") });
  return true;
}
