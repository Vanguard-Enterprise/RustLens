import type { ChatInputCommandInteraction, Client } from "discord.js";
import { EmbedBuilder, MessageFlags, SlashCommandBuilder } from "discord.js";
import { BackendClient } from "./backend-client.js";
import { loadConfig } from "./config.js";
import { RUSTLENS_EMBED_COLOR, RUSTLENS_EMBED_ICON_URL } from "./shared.js";

export const SETSTEAM_COMMAND_NAME = "setsteam";

const SETSTEAM_STEAM_ID_OPTION_NAME = "steam_id";

const setSteamCommand = new SlashCommandBuilder()
  .setName(SETSTEAM_COMMAND_NAME)
  .setDescription("Set your Steam ID for this guild")
  .setDMPermission(false)
  .addStringOption((option) =>
    option
      .setName(SETSTEAM_STEAM_ID_OPTION_NAME)
      .setDescription("Your 17-digit Steam ID or steamcommunity.com profile URL")
      .setRequired(true),
  );

let _backend: BackendClient | undefined;
let _workerID: string | undefined;

function getBackend() {
  if (!_backend) {
    const config = loadConfig();
    _backend = new BackendClient(config.backendUrl, config.jwtSecret, config.jwtIssuer, config.jwtAudience);
    _workerID = `${config.workerId}-setsteam`;
  }
  return _backend;
}

function getWorkerID() {
  getBackend();
  return _workerID!;
}

function buildEmbed(title: string, description: string) {
  return new EmbedBuilder()
    .setTitle(title)
    .setDescription(description)
    .setColor(RUSTLENS_EMBED_COLOR)
    .setTimestamp()
    .setFooter({ text: "RustLens.com", iconURL: RUSTLENS_EMBED_ICON_URL });
}

async function replyEphemeralEmbed(interaction: ChatInputCommandInteraction, embed: EmbedBuilder) {
  if (interaction.deferred || interaction.replied) {
    await interaction.editReply({ embeds: [embed] });
    return;
  }

  await interaction.reply({
    embeds: [embed],
    flags: MessageFlags.Ephemeral,
  });
}

export async function ensureSetSteamCommand(client: Client, shardID: number) {
  if (shardID !== 0 || !client.application) {
    return;
  }

  const data = setSteamCommand.toJSON();
  const commands = await client.application.commands.fetch();
  const existing = commands.find((command) => command.name === data.name);

  if (existing) {
    await client.application.commands.edit(existing.id, data);
  } else {
    await client.application.commands.create(data);
  }

  console.log("[bot] ensured /setsteam command (global)");
}

export async function handleSetSteamChatInput(interaction: ChatInputCommandInteraction) {
  if (interaction.commandName !== SETSTEAM_COMMAND_NAME) {
    return false;
  }

  if (!interaction.inGuild() || !interaction.guildId) {
    await interaction.reply({
      content: "This command can only be used in a server.",
      flags: MessageFlags.Ephemeral,
    });
    return true;
  }

  const backend = getBackend();
  const workerId = getWorkerID();
  const existingLinks = await backend.listGuildLinksByDiscord(workerId, interaction.guildId, interaction.user.id);
  if (existingLinks.items.length > 0) {
    await replyEphemeralEmbed(
      interaction,
      buildEmbed(
        "Error",
        "You can not change your steam ID once set, please contact an administrator to change it for you.",
      ),
    );
    return true;
  }

  await interaction.deferReply({ flags: MessageFlags.Ephemeral });

  const rawInput = interaction.options.getString(SETSTEAM_STEAM_ID_OPTION_NAME, true).trim();
  let steamId: string;
  try {
    const resolved = await backend.resolveSteamID(workerId, rawInput);
    steamId = resolved.steamId;
  } catch {
    await replyEphemeralEmbed(
      interaction,
      buildEmbed("Error", "Enter a 17-digit Steam ID or a supported steamcommunity.com profile URL."),
    );
    return true;
  }

  try {
    await backend.createSoftLink(workerId, steamId, interaction.user.id, "bot_command");
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    if (!message.includes("409")) {
      await replyEphemeralEmbed(
        interaction,
        buildEmbed("Error", "RustLens could not save your Steam ID."),
      );
      return true;
    }
  }

  try {
    await backend.createGuildLink(workerId, interaction.guildId, interaction.user.id, steamId);
  } catch {
    await replyEphemeralEmbed(
      interaction,
      buildEmbed("Error", "RustLens could not link your Steam ID in this guild."),
    );
    return true;
  }

  await replyEphemeralEmbed(
    interaction,
    buildEmbed("Steam ID linked", `Your Steam ID \`${steamId}\` has been linked for this guild.`),
  );
  return true;
}
