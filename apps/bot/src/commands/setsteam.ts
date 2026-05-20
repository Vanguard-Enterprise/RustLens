import type { ActionRowBuilder, ButtonBuilder, ChatInputCommandInteraction, Client, ModalSubmitInteraction } from "discord.js";
import { ApplicationIntegrationType, EmbedBuilder, InteractionContextType, MessageFlags, SlashCommandBuilder } from "discord.js";
import { BackendClient, type SetSteamCandidate } from "../backend-client.js";
import { ModalV2Builder, StringSelectBuilder, TextInputBuilder, TextInputStyle } from "../builders/index.js";
import { loadConfig } from "../config.js";
import { fetchGuildSettings, resolveConfiguredGuildChannel } from "../guild-settings.js";
import { createEmbed, buildFooter, getFieldValues, logDiscordCatch, RUSTLENS_EMBED_COLOR } from "../shared.js";
import { resolveSteamInputErrorMessage } from "../steam-input.js";
import { buildProfileButtons, buildUserEmbed } from "./view.js";

export const SETSTEAM_COMMAND_NAME = "setsteam";
export const SETSTEAM_MODAL_CUSTOM_ID = "setsteam:modal";
const SETSTEAM_SELECT_CUSTOM_ID = "setsteam:select";
const SETSTEAM_INPUT_CUSTOM_ID = "setsteam:input";

const setSteamCommand = new SlashCommandBuilder()
  .setName(SETSTEAM_COMMAND_NAME)
  .setDescription("Link your Steam account in this server")
  .setDMPermission(false)
  .setContexts(InteractionContextType.Guild)
  .setIntegrationTypes(ApplicationIntegrationType.GuildInstall);

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

function buildErrorEmbed(title: string, description: string) {
  return createEmbed()
    .setTitle(title)
    .setDescription(description);
}

async function replyEphemeralEmbed(
  interaction: ChatInputCommandInteraction | ModalSubmitInteraction,
  embed: EmbedBuilder,
) {
  if (interaction.deferred) {
    await interaction.deleteReply().catch(logDiscordCatch("delete deferred /setsteam reply", undefined, {
      guildId: interaction.guildId,
      discordId: interaction.user.id,
    }));
    await interaction.followUp({ embeds: [embed], flags: MessageFlags.Ephemeral });
    return;
  }

  if (interaction.replied) {
    await interaction.followUp({ embeds: [embed], flags: MessageFlags.Ephemeral });
    return;
  }

  await interaction.reply({
    embeds: [embed],
    flags: MessageFlags.Ephemeral,
  });
}

async function replyPublicEmbed(
  interaction: ModalSubmitInteraction,
  embed: EmbedBuilder,
  components?: ActionRowBuilder<ButtonBuilder>[],
) {
  if (interaction.deferred || interaction.replied) {
    await interaction.editReply({ embeds: [embed], components });
    return;
  }

  await interaction.reply({ embeds: [embed], components });
}

function buildLinkingLogEmbed(
  interaction: ModalSubmitInteraction,
  steamId: string,
  bmId: string | null,
) {
  const embed = createEmbed()
    .setTitle("Steam account linked")
    .setColor(RUSTLENS_EMBED_COLOR)
    .setTimestamp()
    .setAuthor({
      name: interaction.user.tag,
      iconURL: interaction.user.displayAvatarURL({ size: 128 }),
    })
    .setFooter(buildFooter())
    .addFields(
      { name: "Steam ID", value: `\`${steamId}\``, inline: true },
      { name: "Discord", value: `<@${interaction.user.id}>`, inline: true },
    );

  if (bmId) {
    embed.addFields({ name: "BM ID", value: `\`${bmId}\``, inline: true });
  }

  return embed;
}

function truncate(value: string, max: number) {
  if (value.length <= max) {
    return value;
  }
  return value.slice(0, Math.max(0, max - 1)) + "…";
}

function buildSetSteamModal(candidates: SetSteamCandidate[]) {
  const modal = new ModalV2Builder()
    .setTitle("Link a Steam account")
    .setCustomId(SETSTEAM_MODAL_CUSTOM_ID);

  if (candidates.length > 0) {
    const select = new StringSelectBuilder()
      .setCustomId(SETSTEAM_SELECT_CUSTOM_ID)
      .setPlaceholder("Select a linked Steam account")
      .setRequired(false)
      .setMinValues(0)
      .setMaxValues(1);

    for (const candidate of candidates) {
      select.addOption({
        label: truncate(candidate.displayName, 100),
        value: candidate.steamId,
        description: truncate(`Steam ID: ${candidate.steamId}`, 100),
      });
    }

    modal.addComponent("Linked Steam accounts", select);
  }

  modal.addComponent(
    "Or enter a new Steam identifier",
    new TextInputBuilder()
      .setCustomId(SETSTEAM_INPUT_CUSTOM_ID)
      .setStyle(TextInputStyle.Short)
      .setPlaceholder("17-digit Steam ID or steamcommunity.com profile URL")
      .setRequired(false)
      .setMaxLength(200),
  );

  return modal;
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

  const [existingLinks, candidatesResult] = await Promise.all([
    backend.listGuildLinksByDiscord(workerId, interaction.guildId, interaction.user.id),
    backend.listSetSteamCandidates(workerId, interaction.user.id).catch((error) => {
      logDiscordCatch("list /setsteam candidates", undefined, {
        guildId: interaction.guildId!,
        discordId: interaction.user.id,
      })(error);
      return { items: [] as SetSteamCandidate[] };
    }),
  ]);

  if (existingLinks.items.length > 0) {
    const existingSteamId = existingLinks.items[0].steamId;
    await replyEphemeralEmbed(
      interaction,
      buildErrorEmbed(
        "Error",
        `You have already linked your Discord to \`${existingSteamId}\`. Please contact an administrator to change it for you.`,
      ),
    );
    return true;
  }

  await interaction.showModal(buildSetSteamModal(candidatesResult.items).toJSON() as any);
  return true;
}

export async function handleSetSteamModalSubmit(interaction: ModalSubmitInteraction) {
  if (interaction.customId !== SETSTEAM_MODAL_CUSTOM_ID) {
    return false;
  }

  if (!interaction.inGuild() || !interaction.guildId) {
    await interaction.reply({
      content: "This command can only be used in a server.",
      flags: MessageFlags.Ephemeral,
    });
    return true;
  }

  await interaction.deferReply();

  const backend = getBackend();
  const workerId = getWorkerID();

  const existingLinks = await backend.listGuildLinksByDiscord(workerId, interaction.guildId, interaction.user.id);
  if (existingLinks.items.length > 0) {
    const existingSteamId = existingLinks.items[0].steamId;
    await replyEphemeralEmbed(
      interaction,
      buildErrorEmbed(
        "Error",
        `You have already linked your Discord to \`${existingSteamId}\`. Please contact an administrator to change it for you.`,
      ),
    );
    return true;
  }

  const selectedSteamId = getFieldValues(interaction, SETSTEAM_SELECT_CUSTOM_ID)[0]?.trim() ?? "";
  const rawInput = getFieldValues(interaction, SETSTEAM_INPUT_CUSTOM_ID)[0]?.trim() ?? "";

  let steamId: string;
  if (selectedSteamId.length > 0) {
    steamId = selectedSteamId;
  } else if (rawInput.length > 0) {
    try {
      const resolved = await backend.resolveSteamID(workerId, rawInput);
      steamId = resolved.steamId;
    } catch (error) {
      await replyEphemeralEmbed(
        interaction,
        buildErrorEmbed("Error", resolveSteamInputErrorMessage(error)),
      );
      return true;
    }
  } else {
    await replyEphemeralEmbed(
      interaction,
      buildErrorEmbed("Error", "Pick a linked account or enter a Steam identifier."),
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
        buildErrorEmbed("Error", "RustLens could not save your Steam ID."),
      );
      return true;
    }
  }

  try {
    await backend.createGuildLink(workerId, interaction.guildId, interaction.user.id, steamId);
  } catch {
    await replyEphemeralEmbed(
      interaction,
      buildErrorEmbed("Error", "RustLens could not link your Steam ID in this guild."),
    );
    return true;
  }

  const settingsPromise = fetchGuildSettings(interaction.guildId);
  const steamAccount = await backend.getSteamAccount(workerId, steamId).catch(() => null);
  const [settings, profile] = await Promise.all([
    settingsPromise,
    backend.getSteamProfile(workerId, steamId).catch(() => null),
  ]);

  if (settings.linkingLogChannelId && interaction.guild) {
    const channel = await resolveConfiguredGuildChannel(interaction.guild, settings.linkingLogChannelId);
    if (channel?.isTextBased() && "send" in channel) {
      await channel.send({
        embeds: [buildLinkingLogEmbed(interaction, steamId, steamAccount?.bmId ?? null)],
      }).catch(logDiscordCatch("send /setsteam linking log", undefined, {
        guildId: interaction.guildId,
        channelId: channel.id,
        discordId: interaction.user.id,
      }));
    }
  }

  const profileName = profile?.bmName || steamAccount?.bmName || profile?.personaName || steamId;
  const embed = buildUserEmbed(steamId, profile, interaction.user.id, true, steamAccount?.bmId ?? null)
    .setTitle(`${profileName} - successfully linked`);
  await replyPublicEmbed(
    interaction,
    embed,
    buildProfileButtons(steamId, profile?.bmId ?? steamAccount?.bmId),
  );
  return true;
}
