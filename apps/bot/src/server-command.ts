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
  PermissionFlagsBits,
  SlashCommandBuilder,
} from "discord.js";
import { BackendClient, type GuildTrackedServer, type TrackedServer } from "./backend-client.js";
import { loadConfig } from "./config.js";
import { createEmbed, logDiscordError } from "./shared.js";

export const SERVER_COMMAND_NAME = "server";

const SERVER_ADD_SUBCOMMAND_NAME = "add";
const SERVER_REMOVE_SUBCOMMAND_NAME = "remove";
const SERVER_LIST_SUBCOMMAND_NAME = "list";
const SERVER_TARGET_OPTION_NAME = "server";

const serverCommand = new SlashCommandBuilder()
  .setName(SERVER_COMMAND_NAME)
  .setDescription("Configure tracked BattleMetrics servers for this guild")
  .setDefaultMemberPermissions(PermissionFlagsBits.Administrator)
  .setDMPermission(false)
  .setContexts(InteractionContextType.Guild)
  .setIntegrationTypes(ApplicationIntegrationType.GuildInstall)
  .addSubcommand((subcommand) =>
    subcommand
      .setName(SERVER_ADD_SUBCOMMAND_NAME)
      .setDescription("Enable a tracked server for this guild")
      .addStringOption((option) =>
        option
          .setName(SERVER_TARGET_OPTION_NAME)
          .setDescription("Tracked server to enable")
          .setRequired(true)
          .setAutocomplete(true),
      ),
  )
  .addSubcommand((subcommand) =>
    subcommand
      .setName(SERVER_REMOVE_SUBCOMMAND_NAME)
      .setDescription("Disable a tracked server for this guild")
      .addStringOption((option) =>
        option
          .setName(SERVER_TARGET_OPTION_NAME)
          .setDescription("Tracked server to disable")
          .setRequired(true)
          .setAutocomplete(true),
      ),
  )
  .addSubcommand((subcommand) =>
    subcommand
      .setName(SERVER_LIST_SUBCOMMAND_NAME)
      .setDescription("List tracked servers enabled for this guild"),
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
    _workerID = `${config.workerId}-server`;
  }
  return _backend;
}

function getWorkerID() {
  getBackend();
  return _workerID!;
}

function buildServerEmbed(title: string, description: string) {
  return createEmbed()
    .setTitle(title)
    .setDescription(description);
}

function trimChoiceLabel(value: string, maxLength = 100) {
  return value.length <= maxLength ? value : `${value.slice(0, maxLength - 3)}...`;
}

function buildTrackedServerChoiceName(server: { bmId: string; name: string }) {
  return trimChoiceLabel(`${server.name} (${server.bmId})`);
}

function buildGuildTrackedServerListDescription(items: GuildTrackedServer[]) {
  if (items.length === 0) {
    return "> * None";
  }
  return items.map((item) => `> * ${item.name}`).join("\n");
}

async function replyEphemeralEmbed(interaction: ChatInputCommandInteraction, embed: EmbedBuilder) {
  if (interaction.deferred || interaction.replied) {
    await interaction.editReply({ embeds: [embed] });
    return;
  }
  await interaction.reply({ embeds: [embed], flags: MessageFlags.Ephemeral });
}

async function replyError(interaction: ChatInputCommandInteraction, content: string) {
  if (interaction.deferred || interaction.replied) {
    await interaction.editReply({ content });
    return;
  }
  await interaction.reply({ content, flags: MessageFlags.Ephemeral });
}

function isUnknownApplicationCommandError(error: unknown) {
  if (!error || typeof error !== "object") {
    return false;
  }

  const code = "code" in error ? error.code : undefined;
  return code === 10063;
}

export async function ensureServerCommand(client: Client, shardID: number) {
  if (shardID !== 0 || !client.application) {
    return;
  }

  const data = serverCommand.toJSON();
  const commands = await client.application.commands.fetch();
  const existing = commands.find((command) => command.name === data.name);

  if (existing) {
    try {
      await client.application.commands.edit(existing.id, data);
    } catch (error) {
      if (!isUnknownApplicationCommandError(error)) {
        throw error;
      }
      await client.application.commands.create(data);
    }
  } else {
    await client.application.commands.create(data);
  }

  console.log("[bot] ensured /server command (global)");
}

export async function handleServerAutocomplete(interaction: AutocompleteInteraction) {
  if (interaction.commandName !== SERVER_COMMAND_NAME) {
    return false;
  }

  if (!interaction.inGuild() || !interaction.guildId) {
    await interaction.respond([]);
    return true;
  }

  const subcommand = interaction.options.getSubcommand();
  const focused = interaction.options.getFocused(true);
  if (focused.name !== SERVER_TARGET_OPTION_NAME) {
    await interaction.respond([]);
    return true;
  }

  try {
    const query = focused.value.trim().toLocaleLowerCase();
    let candidates: { bmId: string; name: string }[] = [];

    if (subcommand === SERVER_ADD_SUBCOMMAND_NAME) {
      const [allTracked, guildEnabled] = await Promise.all([
        getBackend().listTrackedServers(getWorkerID()),
        getBackend().listGuildTrackedServers(getWorkerID(), interaction.guildId),
      ]);
      const enabledIds = new Set(guildEnabled.items.map((server) => server.bmId));
      candidates = (allTracked.items as TrackedServer[]).filter((server) => !enabledIds.has(server.bmId));
    } else if (subcommand === SERVER_REMOVE_SUBCOMMAND_NAME) {
      const guildEnabled = await getBackend().listGuildTrackedServers(getWorkerID(), interaction.guildId);
      candidates = guildEnabled.items;
    } else {
      await interaction.respond([]);
      return true;
    }

    const filtered = candidates
      .filter(
        (server) =>
          query.length === 0 ||
          server.name.toLocaleLowerCase().includes(query) ||
          server.bmId.includes(query),
      )
      .slice(0, 25)
      .map((server) => ({
        name: buildTrackedServerChoiceName(server),
        value: server.bmId,
      }));
    await interaction.respond(filtered);
  } catch (error) {
    logDiscordError("respond to /manage server autocomplete", error, {
      guildId: interaction.guildId,
      discordId: interaction.user.id,
    });
    await interaction.respond([]);
  }

  return true;
}

export async function handleServerChatInput(interaction: ChatInputCommandInteraction) {
  if (interaction.commandName !== SERVER_COMMAND_NAME) {
    return false;
  }

  if (!interaction.inGuild() || !interaction.guildId) {
    await replyError(interaction, "This command can only be used in a server.");
    return true;
  }

  if (!interaction.memberPermissions?.has(PermissionFlagsBits.Administrator)) {
    await replyError(interaction, "Only administrators can use /server.");
    return true;
  }

  await interaction.deferReply({ flags: MessageFlags.Ephemeral });

  const subcommand = interaction.options.getSubcommand();
  const guildId = interaction.guildId;

  if (subcommand === SERVER_LIST_SUBCOMMAND_NAME) {
    try {
      const { items } = await getBackend().listGuildTrackedServers(getWorkerID(), guildId);
      await replyEphemeralEmbed(
        interaction,
        buildServerEmbed("Tracked Servers", buildGuildTrackedServerListDescription(items)),
      );
    } catch (error) {
      console.error("[bot] /server list failed", error);
      await replyError(interaction, "RustLens could not load the tracked server list.");
    }
    return true;
  }

  if (subcommand === SERVER_ADD_SUBCOMMAND_NAME) {
    const bmId = interaction.options.getString(SERVER_TARGET_OPTION_NAME, true).trim();
    try {
      await getBackend().addGuildTrackedServer(getWorkerID(), guildId, bmId);
      const tracked = (await getBackend().listTrackedServers(getWorkerID())).items.find(
        (server) => server.bmId === bmId,
      );
      const label = tracked ? `**${tracked.name}** (\`${bmId}\`)` : `\`${bmId}\``;
      await replyEphemeralEmbed(
        interaction,
        buildServerEmbed("Tracked Server Enabled", `Enabled ${label} for this guild.`),
      );
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      const description = message.includes("404")
        ? `BattleMetrics ID \`${bmId}\` is not a configured tracked server.`
        : message.includes("409")
          ? `BattleMetrics ID \`${bmId}\` is already enabled for this guild.`
          : "RustLens could not enable that tracked server for this guild.";
      await replyEphemeralEmbed(
        interaction,
        buildServerEmbed("Tracked Server Enable Failed", description),
      );
    }
    return true;
  }

  if (subcommand === SERVER_REMOVE_SUBCOMMAND_NAME) {
    const bmId = interaction.options.getString(SERVER_TARGET_OPTION_NAME, true).trim();
    try {
      await getBackend().removeGuildTrackedServer(getWorkerID(), guildId, bmId);
      await replyEphemeralEmbed(
        interaction,
        buildServerEmbed("Tracked Server Disabled", `Disabled \`${bmId}\` for this guild.`),
      );
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      const description = message.includes("404")
        ? `BattleMetrics ID \`${bmId}\` is not currently enabled for this guild.`
        : "RustLens could not disable that tracked server for this guild.";
      await replyEphemeralEmbed(
        interaction,
        buildServerEmbed("Tracked Server Disable Failed", description),
      );
    }
    return true;
  }

  await replyError(interaction, "Unknown subcommand.");
  return true;
}

export const __testables = {
  buildGuildTrackedServerListDescription,
  buildTrackedServerChoiceName,
};
