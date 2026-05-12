import type { ButtonInteraction, ChatInputCommandInteraction, Guild } from "discord.js";
import { MessageFlags } from "discord.js";
import { createEmbed } from "./shared.js";

export const DM_GUILD_MEMBER_LIMIT = 100;

export function isGuildTooLargeForDms(guild: Guild) {
  return guild.memberCount > DM_GUILD_MEMBER_LIMIT;
}

export function buildDmLimitEmbed(guild: Guild) {
  return createEmbed()
    .setTitle("Error")
    .setDescription(
      `This server has ${guild.memberCount} members. RustLens will not send DMs from commands in servers with more than ${DM_GUILD_MEMBER_LIMIT} members.`,
    );
}

export async function replyDmLimitError(
  interaction: ChatInputCommandInteraction | ButtonInteraction,
  guild: Guild,
) {
  const payload = { embeds: [buildDmLimitEmbed(guild)], components: [] };

  if (interaction.deferred || interaction.replied) {
    await interaction.editReply(payload);
    return;
  }

  await interaction.reply({
    ...payload,
    flags: MessageFlags.Ephemeral,
  });
}
