import type {
  AutocompleteInteraction,
  ChatInputCommandInteraction,
  Client,
} from "discord.js";
import {
  EmbedBuilder,
  MessageFlags,
  SlashCommandBuilder,
} from "discord.js";
import {
  BackendClient,
  type SteamProfile,
  type TrackedServer,
} from "./backend-client.js";
import { loadConfig } from "./config.js";
import { RUSTLENS_EMBED_COLOR, RUSTLENS_EMBED_ICON_URL } from "./shared.js";
import {
  buildProfileButtons,
  buildUserEmbed,
  fetchSteamProfileForView,
} from "./view-command.js";

export const SEARCH_COMMAND_NAME = "search";

const SEARCH_STRING_OPTION_NAME = "string";
const SEARCH_SERVER_OPTION_NAME = "server";
const SEARCH_RESULT_LIMIT = 20;

const shardID = process.env.SHARD_ID ?? "0";

const searchCommand = new SlashCommandBuilder()
  .setName(SEARCH_COMMAND_NAME)
  .setDescription("Search players by name on a tracked server")
  .setDMPermission(false)
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
  );

let _backend: BackendClient | undefined;
let _workerID: string | undefined;

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

  let trackedServers: TrackedServer[] = [];
  try {
    const response = await getBackend().listTrackedServers(getWorkerID());
    trackedServers = response.items;
  } catch {
    await interaction.respond([]);
    return true;
  }

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

  const query = interaction.options.getString(SEARCH_STRING_OPTION_NAME, true).trim();
  const serverId = interaction.options.getString(SEARCH_SERVER_OPTION_NAME, true).trim();

  if (query.length === 0) {
    await interaction.reply({
      embeds: [buildSearchEmbed("Error", "Search string cannot be empty.")],
      flags: MessageFlags.Ephemeral,
    });
    return true;
  }

  await interaction.deferReply({ flags: MessageFlags.Ephemeral });

  let results;
  try {
    const response = await getBackend().searchPlayerStatesByName(
      getWorkerID(),
      serverId,
      query,
      SEARCH_RESULT_LIMIT + 1,
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
          `No online players found with **${query}** in their name on this server.`,
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
  await interaction.editReply({
    embeds: [
      buildSearchEmbed(
        "Search",
        `Found **${results.length}** player${results.length === 1 ? "" : "s"} matching **${query}** on **${serverName}**.`,
      ),
    ],
  });

  for (const result of results) {
    const profile = await fetchPlayerProfile(result.steamId);
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
