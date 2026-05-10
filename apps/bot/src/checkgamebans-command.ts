import type { ChatInputCommandInteraction, Client } from "discord.js";
import { ApplicationIntegrationType, InteractionContextType, MessageFlags, PermissionFlagsBits, SlashCommandBuilder } from "discord.js";
import { BackendClient, type GuildRecentGameBan } from "./backend-client.js";
import { loadConfig } from "./config.js";
import { hasGuildAdminAccess } from "./guild-settings.js";
import { createEmbed } from "./shared.js";

export const CHECK_GAME_BANS_COMMAND_NAME = "checkgamebans";

const checkGameBansCommand = new SlashCommandBuilder()
  .setName(CHECK_GAME_BANS_COMMAND_NAME)
  .setDescription("Check linked guild Steam accounts for recent game bans")
  .setDefaultMemberPermissions(PermissionFlagsBits.Administrator)
  .setDMPermission(false)
  .setContexts(InteractionContextType.Guild)
  .setIntegrationTypes(ApplicationIntegrationType.GuildInstall);

let _backend: BackendClient | undefined;
let _workerID: string | undefined;

function getBackend() {
  if (!_backend) {
    const config = loadConfig();
    _backend = new BackendClient(config.backendUrl, config.jwtSecret, config.jwtIssuer, config.jwtAudience);
    _workerID = `${config.workerId}-checkgamebans`;
  }
  return _backend;
}

function getWorkerID() {
  getBackend();
  return _workerID!;
}

function buildGameBanLine(item: GuildRecentGameBan) {
  return `> * <@${item.discordId}> - **[${item.steamId}](https://steamcommunity.com/profiles/${item.steamId})** - <t:${item.gameBannedUnix}:R>`;
}

function buildGameBansEmbed(items: GuildRecentGameBan[]) {
  return createEmbed()
    .setTitle("Gamebans < 90 days")
    .setDescription(
      items.length > 0
        ? items.map(buildGameBanLine).join("\n")
        : "No linked users have a game ban in the last 90 days.",
    );
}

export async function ensureCheckGameBansCommand(client: Client, shardID: number) {
  if (shardID !== 0 || !client.application) {
    return;
  }

  const data = checkGameBansCommand.toJSON();
  const commands = await client.application.commands.fetch();
  const existing = commands.find((command) => command.name === data.name);

  if (existing) {
    await client.application.commands.edit(existing.id, data);
  } else {
    await client.application.commands.create(data);
  }

  console.log("[bot] ensured /checkgamebans command (global)");
}

export async function handleCheckGameBansChatInput(interaction: ChatInputCommandInteraction) {
  if (interaction.commandName !== CHECK_GAME_BANS_COMMAND_NAME) {
    return false;
  }

  if (!interaction.inGuild() || !interaction.guildId) {
    await interaction.reply({
      content: "This command can only be used in a server.",
      flags: MessageFlags.Ephemeral,
    });
    return true;
  }

  if (!(await hasGuildAdminAccess(interaction))) {
    await interaction.reply({
      content: "Only the configured admin role or administrators can use /checkgamebans.",
      flags: MessageFlags.Ephemeral,
    });
    return true;
  }

  await interaction.deferReply({ flags: MessageFlags.Ephemeral });

  try {
    const response = await getBackend().listGuildRecentGameBans(getWorkerID(), interaction.guildId);
    await interaction.editReply({ embeds: [buildGameBansEmbed(response.items)] });
  } catch (error) {
    console.error("[bot] /checkgamebans backend call failed", error);
    await interaction.editReply({ content: "Failed to check game bans." });
  }

  return true;
}

export const __testables = {
  buildGameBanLine,
  buildGameBansEmbed,
};
