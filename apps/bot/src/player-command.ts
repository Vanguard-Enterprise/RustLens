import type { ButtonInteraction, ChatInputCommandInteraction, Client } from "discord.js";
import { ApplicationIntegrationType, EmbedBuilder, InteractionContextType, MessageFlags, SlashCommandBuilder } from "discord.js";
import { BackendClient, type SteamAccount, type SteamAnalytics } from "./backend-client.js";
import { loadConfig } from "./config.js";
import { RUSTLENS_EMBED_COLOR, RUSTLENS_EMBED_ICON_URL } from "./shared.js";
import {
  resolveSteamInputErrorMessage,
  resolveSteamProfileInput,
} from "./steam-input.js";
import { buildProfileButtons, buildUserEmbed, fetchSteamProfileForView } from "./view-command.js";

export const PLAYER_COMMAND_NAME = "player";

const PLAYER_EMOJIS = {
  steam: "<:steam:1444016467648581642>",
  battlemetrics: "<:battlemetrics:1444011821529956472>",
} as const;

const steamIDPattern = /^\d{17}$/;
const bmIDPattern = /^\d{1,16}$/;

const playerCommand = new SlashCommandBuilder()
  .setName(PLAYER_COMMAND_NAME)
  .setDescription("Look up any player by Steam ID or BattleMetrics ID")
  .setContexts(InteractionContextType.Guild, InteractionContextType.BotDM, InteractionContextType.PrivateChannel)
  .setIntegrationTypes(ApplicationIntegrationType.GuildInstall, ApplicationIntegrationType.UserInstall)
  .addStringOption((opt) =>
    opt
      .setName("identifier")
      .setDescription("Steam ID, BattleMetrics ID, or steamcommunity.com profile URL")
      .setRequired(true),
  );

let _backend: BackendClient | undefined;
let _workerID: string | undefined;

function getBackend() {
  if (!_backend) {
    const config = loadConfig();
    _backend = new BackendClient(config.backendUrl, config.jwtSecret, config.jwtIssuer, config.jwtAudience);
    _workerID = `${config.workerId}-player`;
  }
  return { backend: _backend, workerId: _workerID! };
}

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

  const identifier = interaction.options.getString("identifier", true).trim();
  await interaction.deferReply();

  const lookupIdentifier = await resolveLookupIdentifier(identifier);
  if (!lookupIdentifier.ok) {
    await interaction.editReply({ content: lookupIdentifier.error });
    return true;
  }

  const account = await lookupAccountByIdentifier(lookupIdentifier.identifier);
  if (!account) {
    await interaction.editReply({
      content: "No record found for that Steam ID, BattleMetrics ID, or Steam profile URL.",
    });
    return true;
  }

  const profileResult = await fetchPlayerProfile(account.steamId);
  if (profileResult.privateProfile) {
    await interaction.editReply({ embeds: [buildPrivateProfileEmbed(account.steamId, account.bmId)] });
    return true;
  }

  await interaction.editReply({
    embeds: [buildUserEmbed(account.steamId, profileResult.profile)],
    components: buildProfileButtons(account.steamId),
  });
  return true;
}

async function fetchPlayerProfile(steamId: string) {
  const { backend, workerId } = getBackend();

  try {
    return { privateProfile: false as const, profile: await backend.getSteamProfile(workerId, steamId) };
  } catch (error) {
    if (isBmPrivateProfileError(error)) {
      return { privateProfile: true as const, profile: null };
    }

    return { privateProfile: false as const, profile: await fetchSteamProfileForView(steamId) };
  }
}

function isBmPrivateProfileError(error: unknown) {
  if (!error || typeof error !== "object") {
    return false;
  }

  const status = "status" in error ? error.status : undefined;
  const message = "message" in error && typeof error.message === "string" ? error.message : "";
  return status === 404 && message.includes("battlemetrics profile private");
}

function buildPrivateProfileEmbed(steamId: string, bmId: string | null) {
  const embed = new EmbedBuilder()
    .setTitle("Error")
    .setDescription("BM profile private")
    .setColor(RUSTLENS_EMBED_COLOR)
    .setTimestamp()
    .setFooter({ text: "RustLens.com", iconURL: RUSTLENS_EMBED_ICON_URL });

  const info: string[] = [
    `${PLAYER_EMOJIS.steam} [${steamId}](https://steamcommunity.com/profiles/${steamId})`,
  ];
  if (bmId) {
    info.push(`${PLAYER_EMOJIS.battlemetrics} [${bmId}](https://www.battlemetrics.com/players/${bmId})`);
  }

  embed.addFields({ name: "Information", value: info.join("\n") });
  return embed;
}

async function resolveLookupIdentifier(identifier: string) {
  const { backend, workerId } = getBackend();

  try {
    const steamId = await resolveSteamProfileInput(
      identifier,
      (input) => backend.resolveSteamID(workerId, input),
    );
    return { ok: true as const, identifier: steamId ?? identifier.trim() };
  } catch (error) {
    return { ok: false as const, error: resolveSteamInputErrorMessage(error) };
  }
}

async function lookupAccountByIdentifier(identifier: string): Promise<SteamAccount | null> {
  const { backend, workerId } = getBackend();

  if (steamIDPattern.test(identifier)) {
    try {
      return await backend.getSteamAccount(workerId, identifier);
    } catch {
      return null;
    }
  }

  if (bmIDPattern.test(identifier)) {
    try {
      return await backend.getSteamAccountByBmID(workerId, identifier);
    } catch {
      return null;
    }
  }

  return null;
}

export async function handlePlayerButtonInteraction(interaction: ButtonInteraction) {
  if (!interaction.customId.startsWith("player:analytics:")) {
    return false;
  }

  const steamId = interaction.customId.slice("player:analytics:".length);
  if (!/^\d{17}$/.test(steamId)) {
    await interaction.reply({
      content: "Invalid analytics button.",
      flags: MessageFlags.Ephemeral,
    });
    return true;
  }

  await interaction.deferReply({ flags: MessageFlags.Ephemeral });

  try {
    const { backend, workerId } = getBackend();
    const analytics = await backend.getSteamAnalytics(workerId, steamId);
    await interaction.editReply({ embeds: [buildAnalyticsEmbed(analytics)] });
  } catch (error) {
    console.error("[bot] failed to load player analytics", error);
    await interaction.editReply({ content: "Failed to load analytics for this player." });
  }

  return true;
}

function formatGapDurationDays(fromUnix: number, toUnix: number) {
  const durationSeconds = Math.max(0, toUnix - fromUnix);
  const durationDays = Math.max(1, Math.round(durationSeconds / 86400));
  return `${durationDays} day${durationDays === 1 ? "" : "s"}`;
}

export function buildAnalyticsEmbed(analytics: SteamAnalytics) {
  const title = analytics.bmName ? `Analytics - ${analytics.bmName}` : "Analytics";
  const embed = new EmbedBuilder()
    .setTitle(title)
    .setColor(RUSTLENS_EMBED_COLOR)
    .setTimestamp()
    .setFooter({ text: "RustLens.com", iconURL: RUSTLENS_EMBED_ICON_URL });

  if (analytics.avatarImage) {
    embed.setThumbnail(analytics.avatarImage);
  }

  const info: string[] = [];
  info.push(`${PLAYER_EMOJIS.steam} [${analytics.steamId}](https://steamcommunity.com/profiles/${analytics.steamId})`);
  if (analytics.bmId) {
    info.push(`${PLAYER_EMOJIS.battlemetrics} [${analytics.bmId}](https://www.battlemetrics.com/players/${analytics.bmId})`);
  }
  embed.addFields({ name: "Information", value: info.join("\n") });

  if (analytics.gaps.length > 0) {
    const sortedGaps = [...analytics.gaps]
      .sort((a, b) => b.toUnix - a.toUnix)
      .slice(0, 10);
    const gapLines = sortedGaps.map((gap) =>
      `<t:${gap.fromUnix}:R> - <t:${gap.toUnix}:R> (${formatGapDurationDays(gap.fromUnix, gap.toUnix)})`
    );
    embed.addFields({ name: "**Did not play:**", value: gapLines.join("\n") });
  }

  if (analytics.totalGames !== null && analytics.topGames.length > 0) {
    const hasPlaytime = analytics.topGames.some((game) => game.hours > 0);
    const value = hasPlaytime
      ? analytics.topGames.map((game) => `> * ${game.name} **${game.hours}** hours`).join("\n")
      : "*Playtime is private*";
    embed.addFields({
      name: `Games owned: ${analytics.totalGames}`,
      value,
    });
  }

  if (analytics.topServers.length > 0) {
    const serverLines = analytics.topServers.map((server) => `> * ${server.name} **${server.hours}** hours`);
    embed.addFields({
      name: `Servers played: ${analytics.totalServers}`,
      value: serverLines.join("\n"),
    });
  }

  return embed;
}

export const __testables = {
  buildPrivateProfileEmbed,
  buildAnalyticsEmbed,
  isBmPrivateProfileError,
};
