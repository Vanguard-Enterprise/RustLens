import type { ChatInputCommandInteraction, Client } from "discord.js";
import { EmbedBuilder, MessageFlags, SlashCommandBuilder } from "discord.js";
import { BackendClient } from "./backend-client.js";
import { loadConfig } from "./config.js";
import { RUSTLENS_EMBED_COLOR, RUSTLENS_EMBED_ICON_URL } from "./shared.js";

export const SERVER_COMMAND_NAME = "server";

const SERVER_ADD_SUBCOMMAND_NAME = "add";
const SERVER_REMOVE_SUBCOMMAND_NAME = "remove";
const SERVER_LIST_SUBCOMMAND_NAME = "list";
const SERVER_BM_ID_OPTION_NAME = "bm_id";
const SERVER_NAME_OPTION_NAME = "name";

const serverCommand = new SlashCommandBuilder()
  .setName(SERVER_COMMAND_NAME)
  .setDescription("Manage tracked BattleMetrics servers")
  .setDMPermission(false)
  .addSubcommand((subcommand) =>
    subcommand
      .setName(SERVER_ADD_SUBCOMMAND_NAME)
      .setDescription("Add a tracked BattleMetrics server")
      .addStringOption((option) =>
        option
          .setName(SERVER_BM_ID_OPTION_NAME)
          .setDescription("BattleMetrics server ID")
          .setRequired(true),
      )
      .addStringOption((option) =>
        option
          .setName(SERVER_NAME_OPTION_NAME)
          .setDescription("Display name for this tracked server")
          .setRequired(true),
      ),
  )
  .addSubcommand((subcommand) =>
    subcommand
      .setName(SERVER_REMOVE_SUBCOMMAND_NAME)
      .setDescription("Remove a tracked BattleMetrics server")
      .addStringOption((option) =>
        option
          .setName(SERVER_BM_ID_OPTION_NAME)
          .setDescription("BattleMetrics server ID")
          .setRequired(true),
      ),
  )
  .addSubcommand((subcommand) =>
    subcommand
      .setName(SERVER_LIST_SUBCOMMAND_NAME)
      .setDescription("List tracked BattleMetrics servers"),
  );

let _backend: BackendClient | undefined;
let _workerID: string | undefined;
let _ownerGuildId: string | undefined;
let _ownerDiscordIds: string[] | undefined;

function getBackend() {
  if (!_backend) {
    const config = loadConfig();
    _backend = new BackendClient(
      config.backendUrl,
      config.jwtSecret,
      config.jwtIssuer,
      config.jwtAudience,
    );
    _workerID = `${config.workerId}-server`;
    _ownerGuildId = config.ownerGuildId;
    _ownerDiscordIds = config.ownerDiscordIds;
  }
  return _backend;
}

function getWorkerID() {
  getBackend();
  return _workerID!;
}

function getOwnerGuildID() {
  getBackend();
  return _ownerGuildId ?? "";
}

function getOwnerDiscordIDs() {
  getBackend();
  return _ownerDiscordIds ?? [];
}

function normalizeName(name: string) {
  return name
    .trim()
    .split(/\s+/)
    .filter((part) => part.length > 0)
    .join(" ");
}

function buildServerEmbed(title: string, description: string) {
  return new EmbedBuilder()
    .setTitle(title)
    .setDescription(description)
    .setColor(RUSTLENS_EMBED_COLOR)
    .setTimestamp()
    .setFooter({ text: "RustLens.com", iconURL: RUSTLENS_EMBED_ICON_URL });
}

async function replyPublicEmbed(
  interaction: ChatInputCommandInteraction,
  embed: EmbedBuilder,
) {
  if (interaction.deferred || interaction.replied) {
    await interaction.editReply({ embeds: [embed] });
    return;
  }

  await interaction.reply({ embeds: [embed] });
}

async function replyOwnerOnly(interaction: ChatInputCommandInteraction, content: string) {
  if (interaction.deferred || interaction.replied) {
    await interaction.editReply({ content, embeds: [] });
    return;
  }

  await interaction.reply({
    content,
    flags: MessageFlags.Ephemeral,
  });
}

async function deferPublic(interaction: ChatInputCommandInteraction) {
  if (interaction.deferred || interaction.replied) {
    return;
  }

  await interaction.deferReply();
}

function isOwnerCommandConfigured() {
  return getOwnerGuildID().length > 0 && getOwnerDiscordIDs().length > 0;
}

function isAuthorizedOwner(interaction: ChatInputCommandInteraction) {
  return (
    interaction.guildId === getOwnerGuildID() &&
    getOwnerDiscordIDs().includes(interaction.user.id)
  );
}

export async function ensureServerCommand(client: Client, shardID: number) {
  if (shardID !== 0 || !client.application) {
    return;
  }

  if (!isOwnerCommandConfigured()) {
    console.warn("[bot] skipping /server command registration because BOT_OWNER_GUILD_ID or BOT_OWNER_DISCORD_IDS is missing");
    return;
  }

  const guildId = getOwnerGuildID();
  const data = serverCommand.toJSON();
  const commands = await client.application.commands.fetch({ guildId });
  const existing = commands.find((command) => command.name === data.name);

  if (existing) {
    await client.application.commands.edit(existing.id, data, guildId);
  } else {
    await client.application.commands.create(data, guildId);
  }

  console.log(`[bot] ensured /server command in guild ${guildId}`);
}

export async function handleServerChatInput(interaction: ChatInputCommandInteraction) {
  if (interaction.commandName !== SERVER_COMMAND_NAME) {
    return false;
  }

  if (!interaction.inGuild()) {
    await replyOwnerOnly(interaction, "This command can only be used in a server.");
    return true;
  }

  if (!isOwnerCommandConfigured()) {
    await replyOwnerOnly(
      interaction,
      "BOT_OWNER_GUILD_ID and BOT_OWNER_DISCORD_IDS must be configured before /server can be used.",
    );
    return true;
  }

  if (!isAuthorizedOwner(interaction)) {
    await replyOwnerOnly(
      interaction,
      "Only the configured bot owner can use /server from the owner guild.",
    );
    return true;
  }

  const subcommand = interaction.options.getSubcommand();
  await deferPublic(interaction);

  if (subcommand === SERVER_ADD_SUBCOMMAND_NAME) {
    const bmId = interaction.options.getString(SERVER_BM_ID_OPTION_NAME, true).trim();
    const name = normalizeName(interaction.options.getString(SERVER_NAME_OPTION_NAME, true));

    if (!bmId) {
      await replyPublicEmbed(
        interaction,
        buildServerEmbed("Tracked Server Add Failed", "BattleMetrics server ID is required."),
      );
      return true;
    }

    if (!name) {
      await replyPublicEmbed(
        interaction,
        buildServerEmbed("Tracked Server Add Failed", "Tracked server name is required."),
      );
      return true;
    }

    try {
      const created = await getBackend().createTrackedServer(getWorkerID(), bmId, name);
      await replyPublicEmbed(
        interaction,
        buildServerEmbed(
          "Tracked Server Added",
          `**${created.name}**\nBattleMetrics ID: \`${created.bmId}\``,
        ),
      );
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      const description = message.includes("409")
        ? `BattleMetrics ID \`${bmId}\` is already being tracked.`
        : "RustLens could not add that tracked server.";
      await replyPublicEmbed(
        interaction,
        buildServerEmbed("Tracked Server Add Failed", description),
      );
    }

    return true;
  }

  if (subcommand === SERVER_REMOVE_SUBCOMMAND_NAME) {
    const bmId = interaction.options.getString(SERVER_BM_ID_OPTION_NAME, true).trim();

    if (!bmId) {
      await replyPublicEmbed(
        interaction,
        buildServerEmbed("Tracked Server Remove Failed", "BattleMetrics server ID is required."),
      );
      return true;
    }

    try {
      await getBackend().deleteTrackedServer(getWorkerID(), bmId);
      await replyPublicEmbed(
        interaction,
        buildServerEmbed("Tracked Server Removed", `Removed BattleMetrics ID \`${bmId}\` from tracking.`),
      );
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      const description = message.includes("404")
        ? `BattleMetrics ID \`${bmId}\` is not currently tracked.`
        : "RustLens could not remove that tracked server.";
      await replyPublicEmbed(
        interaction,
        buildServerEmbed("Tracked Server Remove Failed", description),
      );
    }

    return true;
  }

  try {
    const { items } = await getBackend().listTrackedServers(getWorkerID());
    const description = items.length > 0
      ? items.map((item, index) => `${index + 1}. **${item.name}**\n\`${item.bmId}\``).join("\n\n")
      : "No tracked BattleMetrics servers configured.";

    await replyPublicEmbed(
      interaction,
      buildServerEmbed("Tracked Servers", description),
    );
  } catch {
    await replyPublicEmbed(
      interaction,
      buildServerEmbed("Tracked Server List Failed", "RustLens could not load the tracked server list."),
    );
  }

  return true;
}
