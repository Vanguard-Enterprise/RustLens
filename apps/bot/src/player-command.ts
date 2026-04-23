import type { ChatInputCommandInteraction, Client } from "discord.js";
import { MessageFlags, SlashCommandBuilder } from "discord.js";
import { buildUserEmbed, resolvePlayerProfileForGuild } from "./view-command.js";

export const PLAYER_COMMAND_NAME = "player";

const playerCommand = new SlashCommandBuilder()
  .setName(PLAYER_COMMAND_NAME)
  .setDescription("Show a linked player's profile in this guild")
  .setDMPermission(false)
  .addStringOption((opt) =>
    opt
      .setName("identifier")
      .setDescription("Steam ID or BattleMetrics ID")
      .setRequired(true),
  );

export async function ensurePlayerCommand(client: Client, shardID: number) {
  if (shardID !== 0 || !client.application) {
    return;
  }

  const data = playerCommand.toJSON();
  const commands = await client.application.commands.fetch();
  const existing = commands.find((command) => command.name === data.name);

  if (existing) {
    await client.application.commands.edit(existing.id, data);
  } else {
    await client.application.commands.create(data);
  }

  console.log("[bot] ensured /player command (global)");
}

export async function handlePlayerChatInput(interaction: ChatInputCommandInteraction) {
  if (interaction.commandName !== PLAYER_COMMAND_NAME) {
    return false;
  }

  if (!interaction.inGuild() || !interaction.guild) {
    await interaction.reply({
      content: "This command can only be used in a server.",
      flags: MessageFlags.Ephemeral,
    });
    return true;
  }

  const identifier = interaction.options.getString("identifier", true);
  await interaction.deferReply();

  const result = await resolvePlayerProfileForGuild(interaction.guildId!, identifier, { allowDiscordLookup: false });
  if (!result.ok) {
    await interaction.editReply({ content: result.error });
    return true;
  }

  await interaction.editReply({ embeds: [buildUserEmbed(result.steamId, result.account)] });
  return true;
}
