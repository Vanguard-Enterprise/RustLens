import type { ChatInputCommandInteraction, Client } from "discord.js";
import {
  ApplicationIntegrationType,
  AttachmentBuilder,
  EmbedBuilder,
  InteractionContextType,
  SlashCommandBuilder,
} from "discord.js";
import { BackendClient, type TrackGroupAnalyticsResponse } from "./backend-client.js";
import { loadConfig } from "./config.js";
import { RUSTLENS_EMBED_COLOR, RUSTLENS_EMBED_ICON_URL } from "./shared.js";
import { resolveSteamInputErrorMessage, resolveSteamProfileInput } from "./steam-input.js";
import { renderTrackAnalyticsChart } from "./track-analytics-chart.js";

export const ANALYZE_COMMAND_NAME = "analyze";

const ANALYZE_OFFLINE_SUBCOMMAND = "offline";
const ANALYZE_IDENTIFIER_OPTION = "identifier";
const steamIDPattern = /^\d{17}$/;
const bmIDPattern = /^\d{1,16}$/;
const shardID = process.env.SHARD_ID ?? "0";

const analyzeCommand = new SlashCommandBuilder()
  .setName(ANALYZE_COMMAND_NAME)
  .setDescription("Analyze player activity")
  .setContexts(InteractionContextType.Guild, InteractionContextType.BotDM, InteractionContextType.PrivateChannel)
  .setIntegrationTypes(ApplicationIntegrationType.GuildInstall, ApplicationIntegrationType.UserInstall)
  .addSubcommand((sub) =>
    sub
      .setName(ANALYZE_OFFLINE_SUBCOMMAND)
      .setDescription("Render offline timing analytics for a player")
      .addStringOption((option) =>
        option
          .setName(ANALYZE_IDENTIFIER_OPTION)
          .setDescription("Steam ID, BattleMetrics ID, or steamcommunity.com profile URL")
          .setRequired(true),
      ),
  );

let _backend: BackendClient | undefined;
let _workerID: string | undefined;

function getBackend() {
  if (!_backend) {
    const config = loadConfig();
    _backend = new BackendClient(config.backendUrl, config.jwtSecret, config.jwtIssuer, config.jwtAudience);
    _workerID = `${config.workerId}-analyze-${shardID}`;
  }
  return _backend;
}

function getWorkerID() {
  getBackend();
  return _workerID!;
}

function buildErrorEmbed(description: string) {
  return new EmbedBuilder()
    .setTitle("Error")
    .setDescription(description)
    .setColor(RUSTLENS_EMBED_COLOR)
    .setTimestamp()
    .setFooter({ text: "RustLens.com", iconURL: RUSTLENS_EMBED_ICON_URL });
}

function buildOfflineAnalyticsEmbed(analytics: TrackGroupAnalyticsResponse) {
  return new EmbedBuilder()
    .setTitle(`Offline Analytics - ${analytics.groupName}`)
    .setDescription(`**${analytics.serverName}**\nBest offline timing based on the last 200 BattleMetrics sessions in ${analytics.timeZone || "Europe/London"}.`)
    .setColor(RUSTLENS_EMBED_COLOR)
    .setImage("attachment://offline-analytics.png")
    .setTimestamp()
    .setFooter({ text: "RustLens.com", iconURL: RUSTLENS_EMBED_ICON_URL });
}

async function resolveIdentifierToBmID(identifier: string) {
  const normalized = identifier.trim();
  const backend = getBackend();
  const workerID = getWorkerID();

  if (steamIDPattern.test(normalized)) {
    const account = await backend.getSteamAccount(workerID, normalized);
    return account.bmId;
  }

  const steamID = await resolveSteamProfileInput(normalized, (input) => backend.resolveSteamID(workerID, input));
  if (steamID) {
    const account = await backend.getSteamAccount(workerID, steamID);
    return account.bmId;
  }

  if (bmIDPattern.test(normalized)) {
    return normalized;
  }

  return null;
}

export async function ensureAnalyzeCommand(client: Client, shardID: number) {
  if (shardID !== 0 || !client.application) {
    return;
  }

  const data = analyzeCommand.toJSON();
  const commands = await client.application.commands.fetch();
  const existing = commands.find((command) => command.name === data.name);

  if (existing) {
    await client.application.commands.edit(existing.id, data);
  } else {
    await client.application.commands.create(data);
  }

  console.log("[bot] ensured /analyze command (global)");
}

export async function handleAnalyzeChatInput(interaction: ChatInputCommandInteraction) {
  if (interaction.commandName !== ANALYZE_COMMAND_NAME) {
    return false;
  }

  if (interaction.options.getSubcommand() !== ANALYZE_OFFLINE_SUBCOMMAND) {
    return false;
  }

  const identifier = interaction.options.getString(ANALYZE_IDENTIFIER_OPTION, true).trim();
  await interaction.deferReply();

  let bmID: string | null;
  try {
    bmID = await resolveIdentifierToBmID(identifier);
  } catch (error) {
    await interaction.editReply({ embeds: [buildErrorEmbed(resolveSteamInputErrorMessage(error))] });
    return true;
  }

  if (!bmID) {
    await interaction.editReply({ embeds: [buildErrorEmbed("Enter a Steam ID, BattleMetrics ID, or supported steamcommunity.com profile URL.")] });
    return true;
  }

  let analytics: TrackGroupAnalyticsResponse;
  try {
    analytics = await getBackend().getPlayerOfflineAnalytics(getWorkerID(), bmID);
  } catch (error) {
    console.error("[bot] failed to load offline analytics", error);
    await interaction.editReply({ embeds: [buildErrorEmbed("RustLens could not load offline analytics for this player.")] });
    return true;
  }

  const hasData = analytics.days.some((day) => day.hours.some((hour) => (hour.minutes ?? hour.avg) > 0));
  if (analytics.analyzed === 0 || !hasData) {
    await interaction.editReply({ embeds: [buildErrorEmbed("Not enough activity in the last 200 sessions to compute offline analytics.")] });
    return true;
  }

  try {
    const image = await renderTrackAnalyticsChart(analytics);
    const file = new AttachmentBuilder(image, { name: "offline-analytics.png" });
    await interaction.editReply({ embeds: [buildOfflineAnalyticsEmbed(analytics)], files: [file] });
  } catch (error) {
    console.error("[bot] failed to render offline analytics chart", error);
    await interaction.editReply({ embeds: [buildErrorEmbed("RustLens could not render the offline analytics chart.")] });
  }

  return true;
}
