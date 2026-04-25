import type { ChatInputCommandInteraction, Client } from "discord.js";
import { ApplicationIntegrationType, EmbedBuilder, InteractionContextType, MessageFlags, SlashCommandBuilder } from "discord.js";
import { BackendClient } from "./backend-client.js";
import { loadConfig } from "./config.js";
import { fetchGuildSettings, resolveConfiguredGuildChannel } from "./guild-settings.js";
import { RUSTLENS_EMBED_COLOR, RUSTLENS_EMBED_ICON_URL } from "./shared.js";
import { resolveSteamInputErrorMessage } from "./steam-input.js";

export const SETSTEAM_COMMAND_NAME = "setsteam";

const SETSTEAM_STEAM_ID_OPTION_NAME = "steam_id";

const setSteamCommand = new SlashCommandBuilder()
  .setName(SETSTEAM_COMMAND_NAME)
  .setDescription("Set your Steam ID for this guild")
  .setDMPermission(false)
  .setContexts(InteractionContextType.Guild)
  .setIntegrationTypes(ApplicationIntegrationType.GuildInstall)
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

function buildLinkingLogEmbed(
  interaction: ChatInputCommandInteraction,
  steamId: string,
  bmId: string | null,
) {
  const embed = new EmbedBuilder()
    .setTitle("Steam account linked")
    .setColor(RUSTLENS_EMBED_COLOR)
    .setTimestamp()
    .setAuthor({
      name: interaction.user.tag,
      iconURL: interaction.user.displayAvatarURL({ size: 128 }),
    })
    .setFooter({ text: "RustLens.com", iconURL: RUSTLENS_EMBED_ICON_URL })
    .addFields(
      { name: "Steam ID", value: `\`${steamId}\``, inline: true },
      { name: "Discord", value: `<@${interaction.user.id}>`, inline: true },
    );

  if (bmId) {
    embed.addFields({ name: "BM ID", value: `\`${bmId}\``, inline: true });
  }

  return embed;
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

  await interaction.deferReply({ flags: MessageFlags.Ephemeral });

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

  const rawInput = interaction.options.getString(SETSTEAM_STEAM_ID_OPTION_NAME, true).trim();
  let steamId: string;
  try {
    const resolved = await backend.resolveSteamID(workerId, rawInput);
    steamId = resolved.steamId;
  } catch (error) {
    await replyEphemeralEmbed(
      interaction,
      buildEmbed("Error", resolveSteamInputErrorMessage(error)),
    );
    return true;
  }

  try {
    await backend.createSoftLink(workerId, steamId, interaction.user.id, "bot_command");
  } catch (error) {
    const status = error instanceof Error ? (error as any).status : undefined;
    if (status !== 409) {
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

  const [settings, steamAccount] = await Promise.all([
    fetchGuildSettings(interaction.guildId),
    backend.getSteamAccount(workerId, steamId).catch(() => null),
  ]);

  if (settings.linkingLogChannelId && interaction.guild) {
    const channel = await resolveConfiguredGuildChannel(interaction.guild, settings.linkingLogChannelId);
    if (channel?.isTextBased() && "send" in channel) {
      await channel.send({
        embeds: [buildLinkingLogEmbed(interaction, steamId, steamAccount?.bmId ?? null)],
      }).catch(() => undefined);
    }
  }

  await replyEphemeralEmbed(
    interaction,
    buildEmbed("Steam ID linked", `Your Steam ID \`${steamId}\` has been linked for this guild.`),
  );
  return true;
}
