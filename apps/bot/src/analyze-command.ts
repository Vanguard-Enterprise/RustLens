import type { AutocompleteInteraction, ChatInputCommandInteraction, Client } from "discord.js";
import {
  ApplicationIntegrationType,
  AttachmentBuilder,
  EmbedBuilder,
  InteractionContextType,
  SlashCommandBuilder,
} from "discord.js";
import {
  BackendClient,
  type ServerAnalyticsResponse,
  type ServerAnalyticsTimeframe,
  type TrackGroupAnalyticsResponse,
  type TrackedServer,
} from "./backend-client.js";
import { extractBmID } from "./bm-input.js";
import { loadConfig } from "./config.js";
import { buildNextBestOfflineFieldValue, computeNextBestOfflineWindow } from "./next-best-offline.js";
import { RUSTLENS_EMBED_COLOR, RUSTLENS_EMBED_ICON_URL } from "./shared.js";
import { resolveSteamInputErrorMessage, resolveSteamProfileInput } from "./steam-input.js";
import { renderServerAnalyticsChart } from "./server-analytics-chart.js";
import { renderTrackAnalyticsChart } from "./track-analytics-chart.js";

export const ANALYZE_COMMAND_NAME = "analyze";

const ANALYZE_OFFLINE_SUBCOMMAND = "offline";
const ANALYZE_SERVER_SUBCOMMAND = "server";
const ANALYZE_IDENTIFIER_OPTION = "identifier";
const ANALYZE_SERVER_OPTION = "server";
const ANALYZE_TIMEFRAME_OPTION = "timeframe";
const TRACKED_SERVER_AUTOCOMPLETE_CACHE_TTL_MS = 60_000;
const SERVER_ANALYTICS_TIMEFRAMES: ServerAnalyticsTimeframe[] = ["7d", "14d", "30d", "60d", "90d"];
const steamIDPattern = /^\d{17}$/;
const shardID = process.env.SHARD_ID ?? "0";

const trackedServerAutocompleteCache: {
  items: TrackedServer[];
  expiresAt: number;
  refresh: Promise<void> | null;
} = {
  items: [],
  expiresAt: 0,
  refresh: null,
};

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
          .setDescription("Steam ID, BattleMetrics ID/URL, or steamcommunity.com profile URL")
          .setRequired(true),
      ),
  )
  .addSubcommand((sub) =>
    sub
      .setName(ANALYZE_SERVER_SUBCOMMAND)
      .setDescription("Render player-count analytics for a tracked server")
      .addStringOption((option) =>
        option
          .setName(ANALYZE_SERVER_OPTION)
          .setDescription("Tracked BattleMetrics server")
          .setRequired(true)
          .setAutocomplete(true),
      )
      .addStringOption((option) => {
        option
          .setName(ANALYZE_TIMEFRAME_OPTION)
          .setDescription("Timeframe for the top trend chart")
          .setRequired(true);
        for (const timeframe of SERVER_ANALYTICS_TIMEFRAMES) {
          option.addChoices({ name: timeframe, value: timeframe });
        }
        return option;
      }),
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
  const embed = new EmbedBuilder()
    .setTitle(`Offline Analytics - ${analytics.groupName}`)
    .setDescription(`**${analytics.serverName}**`)
    .setColor(RUSTLENS_EMBED_COLOR)
    .setImage("attachment://offline-analytics.png")
    .setTimestamp()
    .setFooter({ text: "RustLens.com", iconURL: RUSTLENS_EMBED_ICON_URL });

  const window = computeNextBestOfflineWindow(analytics);
  if (window) {
    embed.addFields({
      name: "Next best offline time",
      value: buildNextBestOfflineFieldValue(window),
    });
  }
  return embed;
}

function buildServerAnalyticsEmbed(analytics: ServerAnalyticsResponse) {
  return new EmbedBuilder()
    .setTitle(`Server Analytics - ${analytics.serverName}`)
    .setDescription(`Trend: **${analytics.timeframe}** | Weekday distribution: **${analytics.cacheWindow} average**`)
    .setColor(RUSTLENS_EMBED_COLOR)
    .setImage("attachment://server-analytics.png")
    .setTimestamp()
    .setFooter({ text: "RustLens.com", iconURL: RUSTLENS_EMBED_ICON_URL });
}

function refreshTrackedServerAutocompleteCache() {
  const now = Date.now();
  if (trackedServerAutocompleteCache.refresh) {
    return trackedServerAutocompleteCache.refresh;
  }

  trackedServerAutocompleteCache.refresh = getBackend()
    .listTrackedServers(getWorkerID())
    .then((response) => {
      trackedServerAutocompleteCache.items = response.items;
      trackedServerAutocompleteCache.expiresAt = Date.now() + TRACKED_SERVER_AUTOCOMPLETE_CACHE_TTL_MS;
    })
    .catch((error) => {
      console.error("[bot] failed to refresh analyze server autocomplete", error);
      if (trackedServerAutocompleteCache.items.length === 0) {
        trackedServerAutocompleteCache.expiresAt = now + 5_000;
      }
    })
    .finally(() => {
      trackedServerAutocompleteCache.refresh = null;
    });

  return trackedServerAutocompleteCache.refresh;
}

function getTrackedServerAutocompleteItems() {
  if (trackedServerAutocompleteCache.expiresAt <= Date.now()) {
    void refreshTrackedServerAutocompleteCache();
  }
  return trackedServerAutocompleteCache.items;
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

  const bmID = extractBmID(normalized);
  if (bmID) {
    return bmID;
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

export async function handleAnalyzeAutocomplete(interaction: AutocompleteInteraction) {
  if (interaction.commandName !== ANALYZE_COMMAND_NAME) {
    return false;
  }

  const focused = interaction.options.getFocused(true);
  if (interaction.options.getSubcommand(false) !== ANALYZE_SERVER_SUBCOMMAND || focused.name !== ANALYZE_SERVER_OPTION) {
    await interaction.respond([]);
    return true;
  }

  const needle = String(focused.value ?? "").trim().toLocaleLowerCase();
  const choices = getTrackedServerAutocompleteItems()
    .filter((server) => {
      if (needle.length === 0) {
        return true;
      }
      return server.name.toLocaleLowerCase().includes(needle) || server.bmId.includes(needle);
    })
    .slice(0, 25)
    .map((server) => ({ name: server.name, value: server.bmId }));

  await interaction.respond(choices);
  return true;
}

export async function handleAnalyzeChatInput(interaction: ChatInputCommandInteraction) {
  if (interaction.commandName !== ANALYZE_COMMAND_NAME) {
    return false;
  }

  const subcommand = interaction.options.getSubcommand();

  if (subcommand === ANALYZE_SERVER_SUBCOMMAND) {
    const serverBmID = interaction.options.getString(ANALYZE_SERVER_OPTION, true).trim();
    const timeframe = interaction.options.getString(ANALYZE_TIMEFRAME_OPTION, true) as ServerAnalyticsTimeframe;
    await interaction.deferReply();

    if (!SERVER_ANALYTICS_TIMEFRAMES.includes(timeframe)) {
      await interaction.editReply({ embeds: [buildErrorEmbed("Timeframe must be one of 7d, 14d, 30d, 60d, or 90d.")] });
      return true;
    }

    let analytics: ServerAnalyticsResponse;
    try {
      analytics = await getBackend().getServerAnalytics(getWorkerID(), serverBmID, timeframe);
    } catch (error) {
      console.error("[bot] failed to load server analytics", error);
      await interaction.editReply({ embeds: [buildErrorEmbed("RustLens could not load analytics for this server.")] });
      return true;
    }

    if (analytics.points.length === 0) {
      await interaction.editReply({ embeds: [buildErrorEmbed("BattleMetrics did not return player-count history for this server and timeframe.")] });
      return true;
    }

    try {
      const image = await renderServerAnalyticsChart(analytics);
      const file = new AttachmentBuilder(image, { name: "server-analytics.png" });
      await interaction.editReply({ embeds: [buildServerAnalyticsEmbed(analytics)], files: [file] });
    } catch (error) {
      console.error("[bot] failed to render server analytics chart", error);
      await interaction.editReply({ embeds: [buildErrorEmbed("RustLens could not render the server analytics chart.")] });
    }

    return true;
  }

  if (subcommand !== ANALYZE_OFFLINE_SUBCOMMAND) {
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
    await interaction.editReply({ embeds: [buildErrorEmbed("Enter a Steam ID, BattleMetrics ID/URL, or supported steamcommunity.com profile URL.")] });
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
