import type {
  AutocompleteInteraction,
  ChatInputCommandInteraction,
  Client,
} from "discord.js";
import {
  ApplicationIntegrationType,
  EmbedBuilder,
  InteractionContextType,
  MessageFlags,
  SlashCommandBuilder,
} from "discord.js";
import {
  BackendClient,
  type SteamProfile,
  type TrackedServer,
} from "./backend-client.js";
import { loadConfig } from "./config.js";
import {
  RUSTLENS_EMBED_COLOR,
  RUSTLENS_EMBED_ICON_URL,
  buildWebsiteAdminOnlyEmbed,
} from "./shared.js";
import {
  buildProfileButtons,
  buildUserEmbed,
  fetchSteamProfileForView,
} from "./view-command.js";
import { mapProxiedRequests } from "./proxy-aware-parallelism.js";
import { isWebsiteAdmin } from "./website-admin.js";

export const SEARCH_COMMAND_NAME = "search";

const SEARCH_STRING_OPTION_NAME = "string";
const SEARCH_SERVER_OPTION_NAME = "server";
const SEARCH_INCLUDE_OFFLINE_OPTION_NAME = "include_offline";
const SEARCH_RESULT_LIMIT = 20;
const TRACKED_SERVER_AUTOCOMPLETE_CACHE_TTL_MS = 30_000;

const shardID = process.env.SHARD_ID ?? "0";

const searchCommand = new SlashCommandBuilder()
  .setName(SEARCH_COMMAND_NAME)
  .setDescription("Search players by name on a tracked server")
  .setDMPermission(false)
  .setContexts(InteractionContextType.Guild)
  .setIntegrationTypes(ApplicationIntegrationType.GuildInstall)
  .addStringOption((option) =>
    option
      .setName(SEARCH_STRING_OPTION_NAME)
      .setDescription("String to match against the player's name")
      .setRequired(true),
  )
  .addStringOption((option) =>
    option
      .setName(SEARCH_SERVER_OPTION_NAME)
      .setDescription("Tracked server to search on")
      .setRequired(true)
      .setAutocomplete(true),
  )
  .addBooleanOption((option) =>
    option
      .setName(SEARCH_INCLUDE_OFFLINE_OPTION_NAME)
      .setDescription("Include players offline for less than 24 hours")
      .setRequired(false),
  );

let _backend: BackendClient | undefined;
let _workerID: string | undefined;
let trackedServerAutocompleteCache: {
  items: TrackedServer[];
  expiresAt: number;
  refresh: Promise<void> | null;
} = {
  items: [],
  expiresAt: 0,
  refresh: null,
};

function getBackend() {
  if (!_backend) {
    const config = loadConfig();
    _backend = new BackendClient(
      config.backendUrl,
      config.jwtSecret,
      config.jwtIssuer,
      config.jwtAudience,
    );
    _workerID = `${config.workerId}-search-${shardID}`;
  }
  return _backend;
}

function getWorkerID() {
  getBackend();
  return _workerID!;
}

function buildSearchEmbed(title: string, description: string) {
  return new EmbedBuilder()
    .setTitle(title)
    .setDescription(description)
    .setColor(RUSTLENS_EMBED_COLOR)
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
      console.error("[bot] failed to refresh search autocomplete servers", error);
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

function formatSearchEta(seconds: number) {
  const normalized = Math.max(0, Math.ceil(seconds));
  return `${normalized} second${normalized === 1 ? "" : "s"}`;
}

function buildSearchProgressEmbed(
  description: string,
  successfulCount: number,
  totalCount: number,
  remainingCount: number,
) {
  return buildSearchEmbed("Search", description).addFields({
    name: "Status",
    value: `ETA: ${formatSearchEta(remainingCount * 2)}\nSuccessful: ${successfulCount}/${totalCount}`,
  });
}

async function fetchPlayerProfile(steamId: string): Promise<SteamProfile | null> {
  try {
    return await getBackend().getSteamProfile(getWorkerID(), steamId);
  } catch {
    return fetchSteamProfileForView(steamId);
  }
}

export async function ensureSearchCommand(client: Client, shardID: number) {
  if (shardID !== 0 || !client.application) {
    return;
  }

  const data = searchCommand.toJSON();
  const commands = await client.application.commands.fetch();
  const existing = commands.find((command) => command.name === data.name);

  if (existing) {
    await client.application.commands.edit(existing.id, data);
  } else {
    await client.application.commands.create(data);
  }

  console.log("[bot] ensured /search command (global)");
}

export async function handleSearchAutocomplete(interaction: AutocompleteInteraction) {
  if (interaction.commandName !== SEARCH_COMMAND_NAME) {
    return false;
  }

  const focused = interaction.options.getFocused(true);
  if (focused.name !== SEARCH_SERVER_OPTION_NAME) {
    await interaction.respond([]);
    return true;
  }

  const trackedServers = getTrackedServerAutocompleteItems();

  const needle = String(focused.value ?? "").trim().toLocaleLowerCase();
  const choices = trackedServers
    .filter((server) => {
      if (needle.length === 0) {
        return true;
      }
      return (
        server.name.toLocaleLowerCase().includes(needle) ||
        server.bmId.toLocaleLowerCase().includes(needle)
      );
    })
    .slice(0, 25)
    .map((server) => ({ name: server.name, value: server.bmId }));

  await interaction.respond(choices);
  return true;
}

export async function handleSearchChatInput(interaction: ChatInputCommandInteraction) {
  if (interaction.commandName !== SEARCH_COMMAND_NAME) {
    return false;
  }

  if (!interaction.inGuild()) {
    await interaction.reply({
      embeds: [buildSearchEmbed("Error", "This command can only be used inside a server.")],
      flags: MessageFlags.Ephemeral,
    });
    return true;
  }

  if (!(await isWebsiteAdmin(interaction.user.id))) {
    await interaction.reply({
      embeds: [buildWebsiteAdminOnlyEmbed()],
      flags: MessageFlags.Ephemeral,
    });
    return true;
  }

  const query = interaction.options.getString(SEARCH_STRING_OPTION_NAME, true).trim();
  const serverId = interaction.options.getString(SEARCH_SERVER_OPTION_NAME, true).trim();
  const includeOffline =
    interaction.options.getBoolean(SEARCH_INCLUDE_OFFLINE_OPTION_NAME) ?? false;

  if (query.length === 0) {
    await interaction.reply({
      embeds: [buildSearchEmbed("Error", "Search string cannot be empty.")],
      flags: MessageFlags.Ephemeral,
    });
    return true;
  }

  await interaction.deferReply();

  let results;
  try {
    const response = await getBackend().searchPlayerStatesByName(
      getWorkerID(),
      serverId,
      query,
      SEARCH_RESULT_LIMIT + 1,
      includeOffline,
    );
    results = response.items;
  } catch (error) {
    console.error("[bot] search backend call failed", error);
    await interaction.editReply({
      embeds: [buildSearchEmbed("Error", "Failed to search players. Please try again.")],
    });
    return true;
  }

  if (results.length === 0) {
    await interaction.editReply({
      embeds: [
        buildSearchEmbed(
          "No matches",
          includeOffline
            ? `No online or recently offline players found with **${query}** in their name on this server.`
            : `No online players found with **${query}** in their name on this server.`,
        ),
      ],
    });
    return true;
  }

  if (results.length > SEARCH_RESULT_LIMIT) {
    await interaction.editReply({
      embeds: [
        buildSearchEmbed(
          "Scope too wide",
          `More than ${SEARCH_RESULT_LIMIT} players matched **${query}**. Try narrowing your search.`,
        ),
      ],
    });
    return true;
  }

  const serverName = results[0]?.serverName ?? "this server";
  const searchDescription = includeOffline
    ? `Found **${results.length}** player${results.length === 1 ? "" : "s"} matching **${query}** on **${serverName}** including players offline for less than 24 hours.`
    : `Found **${results.length}** player${results.length === 1 ? "" : "s"} matching **${query}** on **${serverName}**.`;

  let completedCount = 0;
  let successfulCount = 0;
  let progressUpdate = Promise.resolve();

  await interaction.editReply({
    embeds: [buildSearchProgressEmbed(searchDescription, successfulCount, results.length, results.length)],
  });

  const profiles = await mapProxiedRequests(results, async (result) => {
    const profile = await fetchPlayerProfile(result.steamId);
    completedCount += 1;
    if (profile) {
      successfulCount += 1;
    }

    progressUpdate = progressUpdate.then(async () => {
      await interaction.editReply({
        embeds: [
          buildSearchProgressEmbed(
            searchDescription,
            successfulCount,
            results.length,
            results.length - completedCount,
          ),
        ],
      });
    });

    return profile;
  });

  await progressUpdate;

  for (const [index, result] of results.entries()) {
    const profile = profiles[index];
    try {
      await interaction.followUp({
        embeds: [buildUserEmbed(result.steamId, profile)],
        components: buildProfileButtons(result.steamId),
      });
    } catch (error) {
      console.error(`[bot] failed to post search result for steam ${result.steamId}`, error);
    }
  }

  return true;
}
